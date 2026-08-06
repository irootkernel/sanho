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
	if !fileExists(t, w.appPath(".sanho_docs_hash.bak")) {
		t.Error("the legacy hash file has no .bak sibling")
	}
	if !fileExists(t, w.appPath(".sanho_docs_hash")) {
		t.Error("migrate consumed the legacy hash file; rollback needs it")
	}

	// Hooks swapped: the six v0.2 lines in, all seven v0.1 lines out.
	for name, line := range map[string]string{
		"pre-commit":    "sanho hook pre-commit",
		"commit-msg":    `sanho hook commit-msg "$1"`,
		"pre-push":      `sanho hook pre-push "$@"`,
		"post-checkout": "sanho hook post-checkout",
		"post-merge":    "sanho hook post-merge",
		"post-rewrite":  `sanho hook post-rewrite "$@"`,
	} {
		content := readFile(t, w.hookPath(name))
		if strings.Count(content, line) != 1 {
			t.Errorf("hook %s = %q, want exactly one %q", name, content, line)
		}
	}
	if fileExists(t, w.hookPath("post-commit")) {
		t.Error("the v0.1 post-commit hook survived migration")
	}

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

	// Idempotent: a second run detects the v2 config and exits 0.
	second := w.sanho(w.app, "migrate")
	if strings.TrimSpace(second.stdout) != "sanho: already migrated" {
		t.Fatalf("second migrate said %q, want 'sanho: already migrated'", second.stdout)
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

	// ...and the advised command is the one that succeeds (D3).
	w.sanho(w.app, "migrate")
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
