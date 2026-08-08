package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Scenario 1 — init on a seeded canonical.
func TestInitFreshOnSeededCanonical(t *testing.T) {
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})

	out := w.initWorkspace()
	requireContains(t, "init output", out.stdout, "workspace initialized")

	// Canonical content became this workspace's docs.
	if got := readFile(t, w.appPath("docs", "api.md")); got != "canonical api\n" {
		t.Fatalf("docs/api.md = %q, want canonical content", got)
	}

	// v2 config and base file.
	config := readFile(t, w.appPath(".sanho.json"))
	requireContains(t, "config", config, `"schema_version": 2`)
	requireContains(t, "config", config, `"docs_repo_url"`)
	requireNotContains(t, "config", config, "socket_path")

	base := readFile(t, w.appPath(".sanho_base.json"))
	requireContains(t, "base file", base, `"version": 2`)
	requireContains(t, "base file", base, w.canonicalHead())

	// The six hooks, one line each; post-commit is gone in v0.2.
	wantHooks := map[string]string{
		"pre-commit":    "hook pre-commit",
		"commit-msg":    `hook commit-msg "$1"`,
		"pre-push":      `hook pre-push "$@"`,
		"post-checkout": `hook post-checkout "$@"`,
		"post-merge":    "hook post-merge",
		"post-rewrite":  `hook post-rewrite "$@"`,
	}
	for name, line := range wantHooks {
		content := readFile(t, w.hookPath(name))
		requireContains(t, "hook "+name, content, line)
		requireNotContains(t, "hook "+name, content, "sanho "+line)
	}
	if fileExists(t, w.hookPath("post-commit")) {
		t.Error("a post-commit hook was installed; v0.2 has none")
	}

	// .gitignore carries the v2 and legacy names.
	gitignore := readFile(t, w.appPath(".gitignore"))
	for _, entry := range []string{".sanho.json", ".sanho.json.bak", ".sanho_base.json", ".sanho_docs_hash"} {
		requireContains(t, ".gitignore", gitignore, entry)
	}

	// The private clone lives under the git common dir (§5.2).
	if !fileExists(t, w.appPath(".git", "sanho", "canonical")) {
		t.Fatal("the private canonical clone was not created")
	}

	// The registry knows the project and this workspace.
	state := w.sanho(w.app, "state", "--json").stdout
	requireContains(t, "state", state, `"project": "product"`)
	requireContains(t, "state", state, w.origin)
}

func TestInitRefusesCustomHooksPathBeforeMutation(t *testing.T) {
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})
	custom := w.appPath(".githooks")
	mkdirAll(t, custom)
	foreign := "#!/bin/sh\necho foreign\n"
	writeFile(t, filepath.Join(custom, "pre-commit"), foreign)
	w.git(w.app, "config", "core.hooksPath", custom)

	refused := w.run(w.app, "init",
		"--project", "product",
		"--docs-repo-url", w.origin,
		"--actor-email", "author@example.test")
	if refused.exitCode != 1 {
		t.Fatalf("init exited %d, want 1\n%s", refused.exitCode, refused.combined())
	}
	requireContains(t, "init refusal", refused.stderr, "custom core.hooksPath")
	requireContains(t, "init refusal", refused.stderr, custom)
	for _, path := range []string{".sanho.json", ".sanho_base.json", ".gitignore", filepath.Join(".git", "sanho", "canonical")} {
		if fileExists(t, w.appPath(path)) {
			t.Errorf("refused init created %s", path)
		}
	}
	if got := readFile(t, filepath.Join(custom, "pre-commit")); got != foreign {
		t.Fatalf("custom hook changed from %q to %q", foreign, got)
	}
	if fileExists(t, filepath.Join(w.home, "state.json")) {
		t.Fatal("refused init wrote the registry")
	}
}

func TestDoctorDoesNotRepairCustomHooksPath(t *testing.T) {
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})
	w.initAndAdoptDocs()
	custom := w.appPath(".githooks")
	mkdirAll(t, custom)
	foreign := "#!/bin/sh\necho foreign\n"
	writeFile(t, filepath.Join(custom, "pre-commit"), foreign)
	w.git(w.app, "config", "core.hooksPath", custom)
	defaultBefore := readFile(t, w.appPath(".git", "hooks", "pre-commit"))

	for _, args := range [][]string{{"doctor"}, {"doctor", "--fix"}} {
		out := w.sanho(w.app, args...)
		requireContains(t, strings.Join(args, " "), out.stdout, "custom core.hooksPath")
		if got := readFile(t, filepath.Join(custom, "pre-commit")); got != foreign {
			t.Fatalf("%s changed custom hook from %q to %q", strings.Join(args, " "), foreign, got)
		}
		if got := readFile(t, w.appPath(".git", "hooks", "pre-commit")); got != defaultBefore {
			t.Fatalf("%s changed the default hook", strings.Join(args, " "))
		}
	}
}

func TestStatusAndDoctorExposePublicationMissedDuringHookOutage(t *testing.T) {
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})
	w.initAndAdoptDocs()
	canonicalBefore := w.canonicalHead()
	for _, name := range []string{"pre-commit", "commit-msg", "pre-push", "post-checkout", "post-merge", "post-rewrite"} {
		if err := os.Chmod(w.hookPath(name), 0644); err != nil {
			t.Fatalf("disable hook %s: %v", name, err)
		}
	}

	w.commitDocs("docs: committed during hook outage", map[string]string{"api.md": "outage edit\n"})
	if pushed := w.push(); pushed.exitCode != 0 {
		t.Fatalf("app push during hook outage failed\n%s", pushed.combined())
	}
	if got := w.canonicalHead(); got != canonicalBefore {
		t.Fatalf("canonical moved during hook outage from %s to %s", canonicalBefore, got)
	}
	requireContains(t, "status", w.sanho(w.app, "status").stdout, "publish   : committed docs changes are pending publication")

	var status struct {
		Publication struct {
			Known   bool `json:"known"`
			Pending bool `json:"pending"`
		} `json:"publication"`
	}
	statusJSON := w.sanho(w.app, "status", "--json").stdout
	if err := json.Unmarshal([]byte(statusJSON), &status); err != nil {
		t.Fatalf("parse status JSON: %v\n%s", err, statusJSON)
	}
	if !status.Publication.Known || !status.Publication.Pending {
		t.Fatalf("publication = %+v, want known and pending", status.Publication)
	}

	fixed := w.sanho(w.app, "doctor", "--fix")
	requireContains(t, "doctor --fix", fixed.stdout, "make another docs-changing commit, then run 'git push'")
	if noUpdate := w.push(); noUpdate.exitCode != 0 {
		t.Fatalf("no-op push after repair failed\n%s", noUpdate.combined())
	}
	if got := w.canonicalHead(); got != canonicalBefore {
		t.Fatalf("a no-op app push unexpectedly republished docs: %s", got)
	}

	w.commitDocs("docs: republish after hook repair", map[string]string{"api.md": "republished edit\n"})
	if pushed := w.push(); pushed.exitCode != 0 {
		t.Fatalf("push after the advised commit failed\n%s", pushed.combined())
	}
	if got := w.canonicalFile(w.canonicalHead(), "api.md"); got != "republished edit\n" {
		t.Fatalf("canonical api.md = %q, want republished content", got)
	}
	statusJSON = w.sanho(w.app, "status", "--json").stdout
	if err := json.Unmarshal([]byte(statusJSON), &status); err != nil {
		t.Fatalf("parse converged status JSON: %v\n%s", err, statusJSON)
	}
	if !status.Publication.Known || status.Publication.Pending {
		t.Fatalf("converged publication = %+v, want known and not pending", status.Publication)
	}
}

// Scenario 2 — init on an empty canonical, then the first push
// bootstraps it (§5.3 bootstrap).
func TestInitOnEmptyCanonicalThenFirstPushPublishes(t *testing.T) {
	w := newWorld(t, nil)

	out := w.initWorkspace()
	requireContains(t, "init output", out.combined(),
		"canonical repository is empty; your first push will publish docs")
	if fileExists(t, w.appPath(".sanho_base.json")) {
		t.Fatal("a base file was written against an empty canonical")
	}

	// Sync has nothing to do and says so, rather than failing.
	sync := w.sanho(w.app, "sync")
	requireContains(t, "sync output", sync.stdout, "no commits yet")

	// The first push creates canonical's root commit.
	w.commitDocs("docs: first draft", map[string]string{"api.md": "first draft\n"})
	push := w.push()
	if push.exitCode != 0 {
		t.Fatalf("the bootstrap push failed\n%s", push.combined())
	}
	requireContains(t, "push output", push.combined(), "published docs")

	head := w.canonicalHead()
	if got := w.canonicalFile(head, "api.md"); got != "first draft\n" {
		t.Fatalf("canonical api.md = %q, want the published draft", got)
	}
	// A root commit: nothing before it.
	if parents := strings.TrimSpace(w.git(w.origin, "rev-list", "--count", head).stdout); parents != "1" {
		t.Fatalf("canonical history has %s commits, want 1 root commit", parents)
	}

	// The base advanced to what was published (§5.3 step 6).
	requireContains(t, "base file", readFile(t, w.appPath(".sanho_base.json")), head)
}

// Scenario 3 — the P2 principle at CLI level: a code-only commit
// succeeds while canonical is unreachable. The audit's Critical C1 was
// exactly this failing.
func TestCommitSucceedsWhileCanonicalIsUnreachable(t *testing.T) {
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})
	w.initAndAdoptDocs()

	// Point the private clone at a path that does not exist. This is the
	// offline state as the clone experiences it, and it is permanent, so
	// a hook that reaches for the network cannot accidentally pass.
	clone := w.appPath(".git", "sanho", "canonical")
	w.git(clone, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "vanished.git"))

	// A sync-state read failure that is not corruption is equally unable
	// to veto a commit. A directory at the note path makes os.ReadFile
	// fail before JSON parsing, driving the distinct fail-open branch.
	mkdirAll(t, w.appPath(".git", "sanho", "sync.json"))

	writeFile(t, w.appPath("src", "main.go"), "package main\n")
	w.git(w.app, "add", "-A")
	commit := w.gitExit(w.app, "commit", "-m", "feat: add main")
	if commit.exitCode != 0 {
		t.Fatalf("a code-only commit failed with canonical unreachable (exit %d)\n%s",
			commit.exitCode, commit.combined())
	}
	requireContains(t, "commit diagnostic", commit.combined(), "skipped the sync-state check")

	// And a docs commit too: the commit path never opens a connection.
	docs := w.commitDocs("docs: local edit", map[string]string{"api.md": "local edit\n"})
	if docs.exitCode != 0 {
		t.Fatalf("a docs commit failed with canonical unreachable\n%s", docs.combined())
	}
}

// Scenario 4 — commit-msg stamps the trailer pair (§5.1).
func TestDocsCommitIsStampedWithProvenanceTrailers(t *testing.T) {
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})
	w.initAndAdoptDocs()
	head := w.canonicalHead()

	w.commitDocs("docs: local edit", map[string]string{"api.md": "local edit\n"})

	message := w.headMessage()
	requireContains(t, "commit message", message, "docs-base: "+head)
	requireContains(t, "commit message", message, "docs-base-tree: ")

	// Both keys appear exactly once.
	if got := strings.Count(message, "docs-base:"); got != 1 {
		t.Fatalf("docs-base appears %d times, want 1:\n%s", got, message)
	}
}

// Scenario 5 — an out-of-band canonical advance shows up as a pre-commit
// warning and in status (§5.6).
func TestOutOfBandAdvanceWarnsOnCommitAndShowsInStatus(t *testing.T) {
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})
	w.initAndAdoptDocs()

	w.advanceCanonical(map[string]string{
		"api.md":   "canonical api\n",
		"guide.md": "new upstream guide\n",
	}, "canonical: add guide")

	// Nothing is known until a fetch: the commit path never fetches.
	refreshed := w.sanho(w.app, "status", "--refresh")
	requireContains(t, "status", refreshed.stdout, "behind 1")
	requireContains(t, "status", refreshed.stdout, "will merge cleanly")

	// The next commit carries the one-line warning and still succeeds.
	writeFile(t, w.appPath("src", "main.go"), "package main\n")
	w.git(w.app, "add", "-A")
	commit := w.gitExit(w.app, "commit", "-m", "feat: unrelated code")
	if commit.exitCode != 0 {
		t.Fatalf("commit failed on a stale base (exit %d)\n%s", commit.exitCode, commit.combined())
	}
	requireContains(t, "pre-commit warning", commit.combined(),
		"sanho: docs base is 1 commits behind")
	requireContains(t, "pre-commit warning", commit.combined(), "'sanho sync' will merge cleanly")
}

// Scenario 6 — a clean sync creates the user-authored sync commit.
func TestCleanSyncCreatesTheSyncCommit(t *testing.T) {
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})
	w.initAndAdoptDocs()

	target := w.advanceCanonical(map[string]string{
		"api.md":   "canonical api\n",
		"guide.md": "new upstream guide\n",
	}, "canonical: add guide")

	out := w.sanho(w.app, "sync")
	requireContains(t, "sync output", out.stdout, "synced docs to "+target[:12])

	if got := readFile(t, w.appPath("docs", "guide.md")); got != "new upstream guide\n" {
		t.Fatalf("docs/guide.md = %q, want the upstream content", got)
	}
	subject := strings.TrimSpace(w.git(w.app, "log", "-1", "--format=%s").stdout)
	if subject != "docs: sync to "+target[:12] {
		t.Fatalf("sync commit subject = %q, want 'docs: sync to %s'", subject, target[:12])
	}
	// Sync creates an ordinary commit authored by the user — no [SANHO].
	requireNotContains(t, "sync commit subject", subject, "[SANHO]")
}

// Scenario 7 — push publishes, with the §5.3 canonical subject format.
func TestPushPublishesWithTheCanonicalSubjectFormat(t *testing.T) {
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})
	w.initAndAdoptDocs()

	w.commitDocs("docs: local edit", map[string]string{"api.md": "local edit\n"})
	push := w.push()
	if push.exitCode != 0 {
		t.Fatalf("push failed\n%s", push.combined())
	}
	requireContains(t, "push output", push.combined(), "published docs")

	head := w.canonicalHead()
	if got := w.canonicalFile(head, "api.md"); got != "local edit\n" {
		t.Fatalf("canonical api.md = %q, want the pushed content", got)
	}

	// docs: <repo>/<branch> (<N> app commits)
	subject := w.canonicalSubject(head)
	if !strings.HasPrefix(subject, "docs: code/main (") || !strings.HasSuffix(subject, " app commits)") {
		t.Fatalf("canonical subject = %q, want 'docs: code/main (<N> app commits)'", subject)
	}
	body := w.git(w.origin, "log", "-1", "--format=%b", head).stdout
	requireContains(t, "canonical body", body, "source: ")
	requireContains(t, "canonical body", body, "commits:")
	requireContains(t, "canonical body", body, "docs: local edit")
}

// Scenario 8 — the full conflict route: push rejected (template 3) →
// sync (template 2) → resolve → add → commit → push succeeds. This is
// the guidance-closure path in miniature: every command the tool names
// actually works in the state it was named.
func TestConflictRouteFromPushRejectionToSuccessfulPush(t *testing.T) {
	w := newWorld(t, map[string]string{"api.md": "line one\nline two\n"})
	w.initAndAdoptDocs()

	w.commitDocs("docs: my edit", map[string]string{"api.md": "line one\nMINE\n"})
	w.advanceCanonical(map[string]string{"api.md": "line one\nTHEIRS\n"}, "canonical: their edit")

	// Push is rejected with §5.9 template 3.
	push := w.push()
	if push.exitCode == 0 {
		t.Fatal("the conflicting push succeeded, want a rejection")
	}
	rejected := push.combined()
	requireContains(t, "push rejection", rejected, "sanho: your docs changes conflict with upstream (base ")
	requireContains(t, "push rejection", rejected, "Run 'sanho sync', resolve, commit, then push again.")
	requireContains(t, "push rejection", rejected, "error: push rejected")

	// Nothing was published.
	if head := w.canonicalHead(); w.canonicalFile(head, "api.md") != "line one\nTHEIRS\n" {
		t.Fatal("the rejected push changed canonical")
	}

	// The advised command runs, and reports §5.9 template 2.
	sync := w.sanho(w.app, "sync")
	conflictOutput := sync.stdout
	requireContains(t, "sync conflict", conflictOutput, "sanho: merged docs with upstream — 1 files have conflicts:")
	requireContains(t, "sync conflict", conflictOutput, "  docs/api.md")
	requireContains(t, "sync conflict", conflictOutput, "Resolve the markers, then:  git add docs/ && git commit")
	requireContains(t, "sync conflict", conflictOutput, "Then complete the sync:     sanho sync --continue")
	requireContains(t, "sync conflict", conflictOutput, "To undo this sync:          sanho sync --abort")

	// The markers carry the §5.4 labels, not temp paths (audit L1).
	conflicted := readFile(t, w.appPath("docs", "api.md"))
	requireContains(t, "conflict markers", conflicted, "<<<<<<< sanho-ours")
	requireContains(t, "conflict markers", conflicted, ">>>>>>> sanho-upstream")

	// Committing while markers remain is blocked. The sync-note gate
	// answers first, on purpose: an unresolved sync leaves markers by
	// construction, so "finish or abort the sync" is the useful reading
	// of the state, not "you have markers" (§5.6).
	w.git(w.app, "add", "docs/api.md")
	blocked := w.gitExit(w.app, "commit", "-m", "docs: resolve")
	if blocked.exitCode == 0 {
		t.Fatal("a commit with staged conflict markers succeeded, want it blocked")
	}
	reminder := blocked.combined()
	requireContains(t, "marker gate", reminder, "sanho: a sync is in progress — 1 files still have conflicts:")
	requireContains(t, "marker gate", reminder, "  docs/api.md")
	requireContains(t, "marker gate", reminder, "Resolve the markers, then:  git add docs/ && git commit")
	requireContains(t, "marker gate", reminder, "Then complete the sync:     sanho sync --continue")
	requireContains(t, "marker gate", reminder, "To undo this sync:          sanho sync --abort")

	// Resolve the standard way, and then say so. The commit is ordinary
	// git work; the sync ends when the user completes it, which is the
	// step template 2 named two lines above.
	writeFile(t, w.appPath("docs", "api.md"), "line one\nRESOLVED\n")
	w.git(w.app, "add", "docs/api.md")
	w.git(w.app, "commit", "-m", "docs: resolve the conflict")

	early := w.push()
	if early.exitCode == 0 {
		t.Fatal("the push succeeded before the sync was completed, want a rejection")
	}
	requireContains(t, "rejection", early.combined(), "sanho sync --continue")

	completed := w.sanho(w.app, "sync", "--continue")
	requireContains(t, "completion", completed.stdout, "sync completed")

	// And the push now succeeds.
	final := w.push()
	if final.exitCode != 0 {
		t.Fatalf("the post-resolution push failed\n%s", final.combined())
	}
	requireContains(t, "push output", final.combined(), "published docs")
	if got := w.canonicalFile(w.canonicalHead(), "api.md"); got != "line one\nRESOLVED\n" {
		t.Fatalf("canonical api.md = %q, want the resolved content", got)
	}
}

// Scenario 9 — `clean --dry-run` is byte-for-byte a no-op (audit M4),
// and the real run removes everything.
func TestCleanDryRunChangesNothingThenCleanRemovesEverything(t *testing.T) {
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})
	w.initAndAdoptDocs()

	beforeApp := snapshotTree(t, w.app)
	beforeHome := snapshotTree(t, w.home)

	dry := w.sanho(w.app, "clean", "--dry-run")
	requireContains(t, "dry run", dry.stdout, "would remove")
	requireContains(t, "dry run", dry.stdout, "nothing was changed")

	requireSameTree(t, "workspace after --dry-run", beforeApp, snapshotTree(t, w.app))
	requireSameTree(t, "sanho home after --dry-run", beforeHome, snapshotTree(t, w.home))

	// The real run refuses without -y.
	refused := w.run(w.app, "clean")
	if refused.exitCode != 1 {
		t.Fatalf("clean without -y exited %d, want 1", refused.exitCode)
	}
	requireContains(t, "refusal", refused.stderr, "rerun with -y to confirm")
	requireSameTree(t, "workspace after a refused clean", beforeApp, snapshotTree(t, w.app))

	w.sanho(w.app, "clean", "-y")

	for _, path := range []string{
		w.appPath(".sanho.json"),
		w.appPath(".sanho_base.json"),
		w.appPath(".git", "sanho", "canonical"),
	} {
		if fileExists(t, path) {
			t.Errorf("%s survived clean", path)
		}
	}
	for _, name := range []string{"pre-commit", "commit-msg", "pre-push", "post-checkout", "post-merge", "post-rewrite"} {
		if fileExists(t, w.hookPath(name)) {
			t.Errorf("hook %s survived clean", name)
		}
	}
	// The docs directory is kept unless --remove-docs was asked for.
	if !fileExists(t, w.appPath("docs", "api.md")) {
		t.Error("clean removed the docs directory without --remove-docs")
	}

	state := w.sanho(w.app, "state", "--all", "--json").stdout
	var document struct {
		Workspaces []struct {
			LocalPath string `json:"local_path"`
		} `json:"workspaces"`
	}
	if err := json.Unmarshal([]byte(state), &document); err != nil {
		t.Fatalf("parse state JSON: %v\n%s", err, state)
	}
	for _, entry := range document.Workspaces {
		if entry.LocalPath == w.app {
			t.Fatal("the registry entry survived clean")
		}
	}
}

// The staged-marker gate on its own (§5.6 step 1), outside a sync: a
// commit that stages conflict markers is blocked whether or not sanho
// put them there.
func TestStagedConflictMarkersBlockACommit(t *testing.T) {
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})
	w.initAndAdoptDocs()

	writeFile(t, w.appPath("docs", "api.md"),
		"<<<<<<< sanho-ours\nmine\n=======\ntheirs\n>>>>>>> sanho-upstream\n")
	w.git(w.app, "add", "docs/api.md")

	blocked := w.gitExit(w.app, "commit", "-m", "docs: oops")
	if blocked.exitCode == 0 {
		t.Fatal("a commit with staged conflict markers succeeded, want it blocked")
	}
	requireContains(t, "marker gate", blocked.combined(), "sanho: staged docs contain conflict markers:")
	requireContains(t, "marker gate", blocked.combined(), "  docs/api.md")

	// Resolving makes the same commit succeed — the advised next step
	// actually works (D3 guidance closure).
	writeFile(t, w.appPath("docs", "api.md"), "resolved\n")
	w.git(w.app, "add", "docs/api.md")
	w.git(w.app, "commit", "-m", "docs: resolved")
}

// `sanho sync --abort` restores the pre-sync state and cannot fail once
// a sync note exists (§5.5 step 7).
func TestSyncAbortRestoresThePreSyncState(t *testing.T) {
	w := newWorld(t, map[string]string{"api.md": "line one\nline two\n"})
	w.initAndAdoptDocs()

	w.commitDocs("docs: my edit", map[string]string{"api.md": "line one\nMINE\n"})
	baseBefore := readFile(t, w.appPath(".sanho_base.json"))
	w.advanceCanonical(map[string]string{"api.md": "line one\nTHEIRS\n"}, "canonical: their edit")

	sync := w.sanho(w.app, "sync")
	requireContains(t, "sync", sync.stdout, "have conflicts")

	abort := w.sanho(w.app, "sync", "--abort")
	requireContains(t, "abort", abort.stdout, "sync aborted")

	if got := readFile(t, w.appPath("docs", "api.md")); got != "line one\nMINE\n" {
		t.Fatalf("docs/api.md = %q, want the pre-sync content restored", got)
	}
	if got := readFile(t, w.appPath(".sanho_base.json")); got != baseBefore {
		t.Fatalf("base file = %q, want it restored to %q", got, baseBefore)
	}
	if status := strings.TrimSpace(w.git(w.app, "status", "--porcelain", "--", "docs").stdout); status != "" {
		t.Fatalf("docs status after abort = %q, want clean", status)
	}
}

// `sanho doctor` reports and `--fix` re-derives a lost base from the
// trailers already in history (§5.10, audit H4).
func TestDoctorFixReDerivesTheBase(t *testing.T) {
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})
	w.initAndAdoptDocs()
	w.commitDocs("docs: local edit", map[string]string{"api.md": "local edit\n"})

	head := w.canonicalHead()
	if err := os.Remove(w.appPath(".sanho_base.json")); err != nil {
		t.Fatalf("remove the base file: %v", err)
	}

	before := w.sanho(w.app, "doctor")
	requireContains(t, "doctor", before.stdout, "no docs base is recorded")

	fixed := w.sanho(w.app, "doctor", "--fix")
	requireContains(t, "doctor --fix", fixed.stdout, "re-derived the base as "+head[:12])
	requireContains(t, "base file", readFile(t, w.appPath(".sanho_base.json")), head)
}

// `sanho pull` fast-forwards docs and refuses when there are local edits
// it would overwrite (§5.5).
func TestPullFastForwardsAndRefusesOnLocalEdits(t *testing.T) {
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})
	w.initAndAdoptDocs()

	w.advanceCanonical(map[string]string{
		"api.md":   "canonical api\n",
		"guide.md": "upstream guide\n",
	}, "canonical: add guide")

	pulled := w.sanho(w.app, "pull")
	requireContains(t, "pull", pulled.stdout, "pulled docs to ")
	if got := readFile(t, w.appPath("docs", "guide.md")); got != "upstream guide\n" {
		t.Fatalf("docs/guide.md = %q, want the pulled content", got)
	}

	// With local docs edits, pull refuses and names sync.
	w.commitDocs("docs: local edit", map[string]string{"api.md": "local edit\n"})
	w.advanceCanonical(map[string]string{
		"api.md":   "canonical api\n",
		"guide.md": "upstream guide v2\n",
	}, "canonical: revise guide")

	refused := w.run(w.app, "pull")
	if refused.exitCode != 1 {
		t.Fatalf("pull with local edits exited %d, want 1", refused.exitCode)
	}
	requireContains(t, "pull refusal", refused.stderr, "sanho sync")
}
