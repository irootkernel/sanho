package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The §8 migration path, driven against a hand-built v0.1 workspace.
//
// The fixture is written by hand rather than produced by a v0.1 binary
// on purpose: the v0.1 client is deleted, so the *file formats* are what
// migration actually contracts with, and writing them out here states
// that contract explicitly.

// v0.1 hook lines — seven of them, including the post-commit hook v0.2
// has no replacement for.
var legacyHookLines = map[string]string{
	"pre-commit":    "sanho hook pre-commit",
	"commit-msg":    `sanho hook commit-msg "$1"`,
	"pre-push":      `sanho hook pre-push "$@"`,
	"post-checkout": "sanho hook post-checkout",
	"post-merge":    "sanho hook post-merge",
	"post-rewrite":  `sanho hook post-rewrite "$@"`,
	"post-commit":   "sanho hook post-commit",
}

// seedV1Workspace turns the world's app repository into a v0.1
// workspace: a config with socket_path and no schema_version, the legacy
// hash file naming a canonical commit, the seven v0.1 hook lines, and a
// legacy daemon state.json in SANHO_HOME.
//
// withDaemonState controls whether the daemon's project→URL mapping is
// present, which is the one fact v0.2 cannot reconstruct on its own.
func seedV1Workspace(t *testing.T, w *world, baseCommit string, withDaemonState bool) {
	t.Helper()

	writeFile(t, w.appPath(".sanho.json"), `{
  "socket_path": "/tmp/sanhod.sock",
  "workspace_id": "product:`+w.app+`",
  "project": "product",
  "actor_email": "author@example.test",
  "docs_dir": "docs",
  "docs_hash_file": ".sanho_docs_hash"
}
`)
	writeFile(t, w.appPath(".sanho_docs_hash"), baseCommit+"\n")

	for name, line := range legacyHookLines {
		path := w.hookPath(name)
		writeFile(t, path, "#!/bin/sh\n"+line+"\n")
		if err := os.Chmod(path, 0755); err != nil {
			t.Fatalf("chmod hook %s: %v", name, err)
		}
	}

	if withDaemonState {
		// The v0.1 daemon's state.json schema (retired, sanho-v0.2.md
		// §6): project name → docs-repo id → repository URL.
		writeFile(t, filepath.Join(w.home, "state.json"), `{
  "docs_repos": {
    "origin": {"ID": "origin", "Path": "`+w.origin+`", "RepoURL": "`+w.origin+`"}
  },
  "project_to_docs_repo": {"product": "origin"},
  "workspaces": {}
}
`)
	}
}

// Scenario 10 — migrate a v0.1 workspace, then run it again.
func TestMigrateConvertsAV1WorkspaceAndIsIdempotent(t *testing.T) {
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})

	// A v0.1 workspace already has docs committed, carrying the old
	// identity trailer.
	writeFile(t, w.appPath("docs", "api.md"), "canonical api\n")
	w.git(w.app, "add", "-A")
	baseCommit := w.canonicalHead()
	w.git(w.app, "commit", "-m", "docs: adopt canonical\n\ndocs-version: "+baseCommit)

	seedV1Workspace(t, w, baseCommit, true)

	out := w.sanho(w.app, "migrate")
	requireContains(t, "migrate", out.stdout, "migrated this workspace to the v0.2 layout")

	// The daemon-stop instruction is printed verbatim and nothing is
	// executed on the user's behalf (§8 step 2).
	requireContains(t, "migrate", out.stdout, "launchctl bootout")
	requireContains(t, "migrate", out.stdout, "systemctl --user disable --now sanhod")

	// v2 files.
	config := readFile(t, w.appPath(".sanho.json"))
	requireContains(t, "config", config, `"schema_version": 2`)
	requireContains(t, "config", config, w.origin)
	requireNotContains(t, "config", config, "socket_path")

	base := readFile(t, w.appPath(".sanho_base.json"))
	requireContains(t, "base file", base, baseCommit)
	requireContains(t, "base file", base, `"tree"`)

	// .bak siblings for rollback (§8 step 6), and the legacy hash file
	// left intact as a read-only input.
	requireContains(t, "config backup", readFile(t, w.appPath(".sanho.json.bak")), "socket_path")
	requireContains(t, ".gitignore", readFile(t, w.appPath(".gitignore")), ".sanho.json.bak")
	requireContains(t, ".gitignore", readFile(t, w.appPath(".gitignore")), ".sanho_docs_hash.bak")
	if !fileExists(t, w.appPath(".sanho_docs_hash.bak")) {
		t.Error("the legacy hash file has no .bak sibling")
	}
	if ignored := w.gitExit(w.app, "check-ignore", ".sanho_docs_hash.bak"); ignored.exitCode != 0 {
		t.Fatalf("legacy hash backup is not ignored\n%s", ignored.combined())
	}
	if !fileExists(t, w.appPath(".sanho_docs_hash")) {
		t.Error("migrate consumed the legacy hash file; rollback needs it")
	}

	// Hooks swapped: the six v0.2 lines in, all seven v0.1 lines out.
	for name, line := range map[string]string{
		"pre-commit":    "hook pre-commit",
		"commit-msg":    `hook commit-msg "$1"`,
		"pre-push":      `hook pre-push "$@"`,
		"post-checkout": "hook post-checkout",
		"post-merge":    "hook post-merge",
		"post-rewrite":  `hook post-rewrite "$@"`,
	} {
		content := readFile(t, w.hookPath(name))
		if strings.Count(content, line) != 1 {
			t.Errorf("hook %s = %q, want exactly one %q", name, content, line)
		}
		requireNotContains(t, "hook "+name, content, "sanho "+line)
	}
	if fileExists(t, w.hookPath("post-commit")) {
		t.Error("the v0.1 post-commit hook survived migration")
	}

	// The registry conversion rewrites ~/.sanho/state.json (and its
	// .bak) with the v2 schema in place, so migrate must first preserve
	// the v0.1 daemon state as its own backup — otherwise the conversion
	// destroys the only rollback source for that file.
	requireContains(t, "migrate", out.stdout, "preserved the v0.1 daemon state")
	v1Backup := filepath.Join(w.home, "state.json.v1.bak")
	requireContains(t, "v1 state backup", readFile(t, v1Backup), "project_to_docs_repo")

	// The clone exists and the registry knows the workspace.
	if !fileExists(t, w.appPath(".git", "sanho", "canonical")) {
		t.Fatal("the private canonical clone was not created")
	}
	state := w.sanho(w.app, "state", "--json").stdout
	requireContains(t, "state", state, w.app)

	// And the migrated workspace works: a docs commit is stamped with
	// the new trailer pair, and the push publishes.
	w.commitDocs("docs: after migration", map[string]string{"api.md": "post-migration\n"})
	requireContains(t, "commit message", w.headMessage(), "docs-base: "+baseCommit)

	push := w.push()
	if push.exitCode != 0 {
		t.Fatalf("the post-migration push failed\n%s", push.combined())
	}
	requireContains(t, "push", push.combined(), "published docs")

	// Idempotent: a second run detects the v2 config and exits 0, and
	// the preserved v0.1 state backup is never overwritten (the registry
	// is v2 by now; overwriting would replace legacy bytes with v2).
	gitignore := strings.ReplaceAll(readFile(t, w.appPath(".gitignore")), ".sanho_docs_hash.bak\n", "")
	writeFile(t, w.appPath(".gitignore"), gitignore)
	second := w.sanho(w.app, "migrate")
	if strings.TrimSpace(second.stdout) != "sanho: already migrated" {
		t.Fatalf("second migrate said %q, want 'sanho: already migrated'", second.stdout)
	}
	requireContains(t, "v1 state backup after rerun", readFile(t, v1Backup), "project_to_docs_repo")
	requireContains(t, ".gitignore repaired by rerun", readFile(t, w.appPath(".gitignore")), ".sanho_docs_hash.bak")
}

func TestMigrateRefusesCustomHooksPathBeforeMutation(t *testing.T) {
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})
	seedV1Workspace(t, w, w.canonicalHead(), true)
	legacyPreCommit := readFile(t, w.hookPath("pre-commit"))
	custom := w.appPath(".githooks")
	mkdirAll(t, custom)
	foreign := "#!/bin/sh\necho foreign\n"
	writeFile(t, filepath.Join(custom, "pre-commit"), foreign)
	w.git(w.app, "config", "core.hooksPath", custom)

	refused := w.run(w.app, "migrate")
	if refused.exitCode != 1 {
		t.Fatalf("migrate exited %d, want 1\n%s", refused.exitCode, refused.combined())
	}
	requireContains(t, "migrate refusal", refused.stderr, "custom core.hooksPath")
	requireContains(t, "config", readFile(t, w.appPath(".sanho.json")), "socket_path")
	for _, path := range []string{".sanho.json.bak", ".sanho_base.json", filepath.Join(".git", "sanho", "canonical")} {
		if fileExists(t, w.appPath(path)) {
			t.Errorf("refused migration created %s", path)
		}
	}
	if fileExists(t, filepath.Join(w.home, "state.json.v1.bak")) {
		t.Fatal("refused migration preserved daemon state before refusing")
	}
	if got := readFile(t, w.hookPath("pre-commit")); got != legacyPreCommit {
		t.Fatalf("default legacy hook changed from %q to %q", legacyPreCommit, got)
	}
	if got := readFile(t, filepath.Join(custom, "pre-commit")); got != foreign {
		t.Fatalf("custom hook changed from %q to %q", foreign, got)
	}
}

func TestMigrateManagesHusky9HooksWithExplicitOptIn(t *testing.T) {
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})
	writeFile(t, w.appPath("docs", "api.md"), "canonical api\n")
	w.git(w.app, "add", "-A")
	baseCommit := w.canonicalHead()
	w.git(w.app, "commit", "-m", "docs: adopt canonical\n\ndocs-version: "+baseCommit)
	seedV1Workspace(t, w, baseCommit, true)

	for name := range legacyHookLines {
		if err := os.Remove(w.hookPath(name)); err != nil {
			t.Fatalf("remove default v1 hook %s: %v", name, err)
		}
	}
	husky := w.appPath(".husky")
	shimDir := filepath.Join(husky, "_")
	mkdirAll(t, shimDir)
	h := "#!/usr/bin/env sh\nn=$(basename \"$0\")\ns=$(dirname \"$(dirname \"$0\")\")/$n\nsh -e \"$s\" \"$@\"\n"
	writeFile(t, filepath.Join(shimDir, "h"), h)
	shim := "#!/usr/bin/env sh\n. \"$(dirname \"$0\")/h\"\n"
	for _, name := range []string{"pre-commit", "commit-msg", "pre-push", "post-checkout", "post-merge", "post-rewrite"} {
		writeFile(t, filepath.Join(shimDir, name), shim)
		if err := os.Chmod(filepath.Join(shimDir, name), 0755); err != nil {
			t.Fatal(err)
		}
	}
	for name, line := range legacyHookLines {
		content := "#!/usr/bin/env sh\n" + line + "\n"
		if name == "pre-commit" {
			content = "#!/usr/bin/env sh\nbun run lint-staged\n" + line + "\n"
		}
		writeFile(t, filepath.Join(husky, name), content)
		if err := os.Chmod(filepath.Join(husky, name), 0755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(w.binDir, "bun"), "#!/bin/sh\nexit 0\n")
	if err := os.Chmod(filepath.Join(w.binDir, "bun"), 0755); err != nil {
		t.Fatal(err)
	}
	w.git(w.app, "config", "core.hooksPath", ".husky/_")
	w.git(w.app, "add", ".husky")
	w.git(w.app, "commit", "-m", "chore: seed Husky hooks")

	w.sanho(w.app, "migrate", "--manage-custom-hooks")

	config := readFile(t, w.appPath(".sanho.json"))
	requireContains(t, "config", config, `"hook_mode": "husky"`)
	requireContains(t, "config", config, `"hook_dir": ".husky"`)
	if got := readFile(t, filepath.Join(shimDir, "pre-commit")); got != shim {
		t.Fatalf("Husky shim changed from %q to %q", shim, got)
	}
	preCommit := readFile(t, filepath.Join(husky, "pre-commit"))
	requireContains(t, "Husky pre-commit", preCommit, "bun run lint-staged")
	requireContains(t, "Husky pre-commit", preCommit, "command -v sanho")
	requireNotContains(t, "Husky pre-commit", preCommit, "\nsanho hook pre-commit\n")
	for _, name := range []string{"pre-commit", "commit-msg", "pre-push", "post-checkout", "post-merge", "post-rewrite", "post-commit"} {
		if !fileExists(t, filepath.Join(husky, name+".bak")) {
			t.Errorf("Husky hook %s has no rollback backup", name)
		}
	}
	postCommit := readFile(t, filepath.Join(husky, "post-commit"))
	requireNotContains(t, "Husky post-commit", postCommit, "sanho hook post-commit")
	requireContains(t, "Husky post-commit", postCommit, "#!/usr/bin/env sh")
	doctor := w.sanho(w.app, "doctor", "--json").stdout
	requireContains(t, "doctor", doctor, `"warnings": 0`)
	writeFile(t, w.appPath("docs", "api.md"), "Husky migration works\n")
	w.git(w.app, "add", "docs/api.md")
	w.git(w.app, "commit", "-m", "docs: verify migrated Husky hooks")
	requireContains(t, "commit message", w.headMessage(), "docs-base:")
	push := w.push()
	if push.exitCode != 0 {
		t.Fatalf("push through Husky pre-push failed\n%s", push.combined())
	}
	if got := w.canonicalFile(w.canonicalHead(), "api.md"); got != "Husky migration works\n" {
		t.Fatalf("canonical api.md = %q", got)
	}
}

// migrate needs the docs repository URL, which only the daemon knew.
// Without the legacy state it says so and names the flag that supplies
// it — guidance that succeeds where it is printed (D3).
func TestMigrateRequiresTheDocsRepoURLWhenTheLegacyStateIsAbsent(t *testing.T) {
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})
	seedV1Workspace(t, w, w.canonicalHead(), false)

	refused := w.run(w.app, "migrate")
	if refused.exitCode != 1 {
		t.Fatalf("migrate exited %d, want 1", refused.exitCode)
	}
	requireContains(t, "refusal", refused.stderr, "rerun with --docs-repo-url <url>")

	// The named flag works.
	w.sanho(w.app, "migrate", "--docs-repo-url", w.origin)
	requireContains(t, "config", readFile(t, w.appPath(".sanho.json")), `"schema_version": 2`)
}

// §8 step 1: a live v0.1 transaction is the one thing v0.2 will not
// interpret, so migration refuses and names the v0.1 binary.
func TestMigrateRefusesWhileV1TransactionStateExists(t *testing.T) {
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})
	seedV1Workspace(t, w, w.canonicalHead(), true)
	mkdirAll(t, w.appPath(".git", "sanho", "pull-commit"))

	refused := w.run(w.app, "migrate")
	if refused.exitCode != 1 {
		t.Fatalf("migrate exited %d, want 1", refused.exitCode)
	}
	requireContains(t, "refusal", refused.stderr, "pull-commit transaction or pending-fix state")
	requireContains(t, "refusal", refused.stderr, "v0.1 binary")

	// Nothing was rewritten.
	requireContains(t, "config", readFile(t, w.appPath(".sanho.json")), "socket_path")
	if fileExists(t, w.appPath(".sanho.json.bak")) {
		t.Error("a refused migration wrote a backup")
	}
}

func TestMigrateRefusesWhileAPendingFixFileExists(t *testing.T) {
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})
	seedV1Workspace(t, w, w.canonicalHead(), true)
	writeFile(t, w.appPath(".sanho_pending_fix"), `{"base_hash":"x"}`)

	refused := w.run(w.app, "migrate")
	if refused.exitCode != 1 {
		t.Fatalf("migrate exited %d, want 1", refused.exitCode)
	}
	requireContains(t, "refusal", refused.stderr, "pull-commit transaction or pending-fix state")
}

// Scenario 11 — §8 pre-migration degradation. The v0.2 binary is
// installed but `migrate` has not run: commits keep working, and the
// push boundary is where the migration is demanded.
func TestV1WorkspaceDegradesSafelyBeforeMigration(t *testing.T) {
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})
	seedV1Workspace(t, w, w.canonicalHead(), true)

	outside := t.TempDir()
	outsideHook := w.run(outside, "hook", "post-commit")
	if outsideHook.exitCode != 0 || outsideHook.combined() != "" {
		t.Fatalf("post-commit outside a workspace = (exit %d, %q), want (0, silence)",
			outsideHook.exitCode, outsideHook.combined())
	}

	v1Hook := w.run(w.app, "hook", "post-commit")
	if v1Hook.exitCode != 0 {
		t.Fatalf("post-commit in a v1 workspace exited %d\n%s", v1Hook.exitCode, v1Hook.combined())
	}
	requireContains(t, "post-commit v1 hint", v1Hook.combined(),
		"sanho: this workspace uses the v0.1 layout; run 'sanho migrate'")

	// Install the v0.2 hook lines, which is what upgrading the binary
	// effectively does: the lines already invoke `sanho` by name.
	for name, line := range map[string]string{
		"pre-commit":    "sanho hook pre-commit",
		"commit-msg":    `sanho hook commit-msg "$1"`,
		"pre-push":      `sanho hook pre-push "$@"`,
		"post-checkout": "sanho hook post-checkout",
	} {
		path := w.hookPath(name)
		writeFile(t, path, "#!/bin/sh\n"+line+"\n")
		if err := os.Chmod(path, 0755); err != nil {
			t.Fatalf("chmod hook %s: %v", name, err)
		}
	}

	// A commit succeeds and carries the migrate hint (§8, P2).
	writeFile(t, w.appPath("docs", "api.md"), "edited before migrating\n")
	w.git(w.app, "add", "-A")
	commit := w.gitExit(w.app, "commit", "-m", "docs: edit before migrating")
	if commit.exitCode != 0 {
		t.Fatalf("a commit on a v0.1 workspace failed (exit %d)\n%s", commit.exitCode, commit.combined())
	}
	requireContains(t, "commit hint", commit.combined(),
		"sanho: this workspace uses the v0.1 layout; run 'sanho migrate'")

	// A push fails closed with the same message: the push boundary is
	// the natural migration prompt.
	push := w.push()
	if push.exitCode == 0 {
		t.Fatal("a push from a v0.1 workspace succeeded, want it blocked")
	}
	requireContains(t, "push refusal", push.combined(),
		"sanho: this workspace uses the v0.1 layout; run 'sanho migrate'")
	requireContains(t, "push refusal", push.combined(), "error: push rejected")

	// Read commands refuse with the same hint...
	status := w.run(w.app, "status")
	if status.exitCode != 1 {
		t.Fatalf("status on a v0.1 workspace exited %d, want 1", status.exitCode)
	}
	requireContains(t, "status refusal", status.stderr, "run 'sanho migrate'")

	// Registry inventory is the read-only exception. It shows every legacy
	// registration without scoping to a v1 workspace or rewriting the file.
	state := w.sanho(w.app, "state", "--all", "--json").stdout
	requireContains(t, "legacy state", state, `"name": "product"`)
	requireContains(t, "legacy state", state, w.origin)

	// ...and the advised command is the one that succeeds (D3).
	w.sanho(w.app, "migrate")
	v2Hook := w.run(w.app, "hook", "post-commit")
	if v2Hook.exitCode != 0 || v2Hook.combined() != "" {
		t.Fatalf("post-commit in a v2 workspace = (exit %d, %q), want (0, silence)",
			v2Hook.exitCode, v2Hook.combined())
	}
	w.sanho(w.app, "status")
}

// `sanho version --json` keeps its v0.1 schema, so existing scripts
// carry over unchanged.
func TestVersionJSONSchema(t *testing.T) {
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})

	out := w.sanho(w.app, "version", "--json")
	var document struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal([]byte(out.stdout), &document); err != nil {
		t.Fatalf("parse version JSON: %v\n%s", err, out.stdout)
	}
	if document.Name != "sanho" || document.Version == "" {
		t.Fatalf("version JSON = %+v, want name sanho and a version", document)
	}
}

// A command outside any workspace names `sanho init`, and running it
// there is exactly what makes the state right.
func TestCommandsOutsideAWorkspaceNameInit(t *testing.T) {
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})

	out := w.run(w.app, "status")
	if out.exitCode != 1 {
		t.Fatalf("status outside a workspace exited %d, want 1", out.exitCode)
	}
	requireContains(t, "refusal", out.stderr, "not a sanho workspace")
	requireContains(t, "refusal", out.stderr, "run 'sanho init'")
}

// TestMigratePreservesOtherProjectsAndResumes is R1's lab3 scenario and
// the F-H8 regression.
//
// A v0.1 machine holds ONE state.json describing every project. The old
// migrate lifted this workspace's docs-repo URL out of it and then let
// the first registry write replace the whole file with a v2 state
// containing only this workspace — so project B's mapping was destroyed
// by migrating project A, and migrating B afterwards demanded
// --docs-repo-url for a value the machine had recorded all along.
//
// The same run also proves resumability: a migration interrupted by an
// unreachable docs repository leaves a workspace that is still v0.1, and
// re-running it finishes the job.
func TestMigratePreservesOtherProjectsAndResumes(t *testing.T) {
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})
	baseCommit := w.canonicalHead()

	// A v0.1 daemon state describing TWO projects. Only "product" is
	// this workspace's; "sibling" belongs to a checkout elsewhere.
	seedV1Workspace(t, w, baseCommit, false)
	siblingURL := filepath.Join(filepath.Dir(w.origin), "sibling-docs.git")
	writeFile(t, filepath.Join(w.home, "state.json"), `{
  "docs_repos": {
    "origin":  {"ID": "origin",  "Path": "`+w.origin+`",  "RepoURL": "`+w.origin+`"},
    "sibling": {"ID": "sibling", "Path": "`+siblingURL+`", "RepoURL": "`+siblingURL+`"}
  },
  "project_to_docs_repo": {"product": "origin", "sibling": "sibling"},
  "workspaces": {
    "sibling:/elsewhere/checkout": {
      "project": "sibling",
      "local_path": "/elsewhere/checkout",
      "docs_hash": "`+baseCommit+`",
      "last_actor_email": "other@example.test"
    }
  }
}
`)

	// --- resumability ---------------------------------------------------
	//
	// Point the config at a docs repository that does not exist. Migrate
	// gets as far as the clone and stops.
	interrupted := w.run(w.app, "migrate", "--docs-repo-url", filepath.Join(filepath.Dir(w.origin), "gone.git"))
	if interrupted.exitCode == 0 {
		t.Fatal("migrate with an unreachable docs repository succeeded, want a refusal")
	}
	// The workspace is still v0.1, so v0.1 can still read it and v0.2
	// will still migrate it. Writing the v2 config first — the old order
	// — left a workspace neither version could act on.
	requireContains(t, "config after an interrupted migrate",
		readFile(t, w.appPath(".sanho.json")), `"socket_path"`)

	// Re-running completes it, and the URL comes from the legacy state
	// rather than from the flag.
	out := w.sanho(w.app, "migrate")
	requireContains(t, "migrate", out.stdout, "migrated this workspace")
	requireContains(t, "config after migrate", readFile(t, w.appPath(".sanho.json")), `"schema_version": 2`)

	// --- the other project survives -------------------------------------
	state := w.sanho(w.app, "state", "--all", "--json").stdout
	for _, want := range []string{`"sibling"`, siblingURL, "/elsewhere/checkout"} {
		requireContains(t, "converted registry", state, want)
	}
	requireContains(t, "converted registry", state, "other@example.test")

	// And the v0.1 file itself is preserved for rollback, untouched by
	// the conversion.
	v1Backup := filepath.Join(w.home, "state.json.v1.bak")
	requireContains(t, "v1 state backup", readFile(t, v1Backup), "project_to_docs_repo")
	requireContains(t, "v1 state backup", readFile(t, v1Backup), siblingURL)

	// Hook files are backed up before being rewritten (§8 step 6).
	requireContains(t, "migrate summary", out.stdout, "pre-push.bak")
	if !fileExists(t, w.hookPath("pre-push")+".bak") {
		t.Error("migrate rewrote pre-push without leaving a .bak")
	}
}

// State provides the one read-only view of a v0.1 registry. Registry writers
// still refuse so none can overwrite the daemon's project map before migrate.
func TestStateReadsLegacyRegistryWithoutConvertingIt(t *testing.T) {
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})
	w.initAndAdoptDocs()

	// Put a v0.1 daemon state where the v2 registry lives.
	legacy := `{
  "docs_repos": {"origin": {"ID": "origin", "Path": "` + w.origin + `", "RepoURL": "` + w.origin + `"}},
  "project_to_docs_repo": {"product": "origin"},
  "workspaces": {}
}
`
	statePath := filepath.Join(w.home, "state.json")
	writeFile(t, statePath, legacy)

	state := w.sanho(w.app, "state", "--all", "--json").stdout
	requireContains(t, "state", state, `"name": "product"`)
	requireContains(t, "state", state, w.origin)

	refused := w.run(w.app, "project", "add", "other", "--docs-repo-url", "ssh://example.test/other.git")
	if refused.exitCode == 0 {
		t.Fatal("a registry writer accepted v0.1 state, want a refusal")
	}
	requireContains(t, "refusal", refused.stderr, "run 'sanho migrate'")

	// The file is byte-identical: refusing is the whole point.
	if got := readFile(t, statePath); got != legacy {
		t.Fatalf("the v0.1 registry was rewritten:\n%s", got)
	}
}
