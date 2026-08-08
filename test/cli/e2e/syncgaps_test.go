package e2e

// Coverage the sync window never had, closed alongside the third review
// wave (Z5).
//
// None of these is a defect report. Each is a shape of the conflicted
// window that the suite had no test for at all, and every one of them
// crosses the code the wave changed: the reporting classes, the base
// invariant, the stand-down guards, and the new completion verb.
//
//	resolution by deletion       a conflicted path can be settled by
//	                             removing the file, which no fixture did
//	partial resolution           two conflicted files, one of them left
//	legacy note through hooks    only unit tests had ever driven one
//	pull inside the window       only sync and push were covered
//	linked worktree + conflict   two features that had never met
//	post-merge / post-rewrite    only post-checkout stood down in tests
//	a hook that cannot decide    the silent-failure branch of the hook contract

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- resolution shapes --------------------------------------------------

// TestResolvingByDeletingTheConflictedFileCompletes covers a resolution
// nobody had tested: the answer to "whose version of this document
// wins?" can be "neither, it should not exist".
//
// It is worth its own test because deletion moves the conflicted path in
// a direction the reporting has to read the same way as an edit, and
// because publishing a deletion is the one docs change that can destroy
// content upstream still has.
func TestResolvingByDeletingTheConflictedFileCompletes(t *testing.T) {
	t.Parallel()

	w := newWorld(t, map[string]string{"api.md": "line one\nline two\n", "guide.md": "guide\n"})
	ws := w.setup("resolve-by-deleting")

	ws.commitDocs("docs: my edit", map[string]string{"api.md": "line one\nMINE\n"})
	theirs := w.advanceCanonical(map[string]string{
		"api.md":   "line one\nTHEIRS\n",
		"guide.md": "guide\n",
	}, "canonical: their edit")

	conflicted := ws.sanho("sync")
	requireContains(t, "sync output", conflicted.combined(), "have conflicts")

	// -f because the conflicted merge left the path staged: the user's
	// action is "this document should not exist", and git asks them to
	// say so explicitly.
	ws.git("rm", "--quiet", "-f", "docs/api.md")
	deleted := ws.gitExit("commit", "-m", "docs: retire the api document")
	requireExit(t, "the deletion that resolves the conflict", deleted, 0)

	// A deletion is a resolution like any other: it settles the path, and
	// it still does not complete the sync.
	requireContains(t, "commit output", deleted.combined(), "is not completed")
	if !syncNoteExists(t, ws) {
		t.Fatal("the deletion cleared the sync note; only --continue may")
	}
	requireExit(t, "push before the sync is completed", ws.push(), 1)

	ws.sanho("sync", "--continue")
	requireEqual(t, "base file after --continue", recordedBase(t, ws), theirs)

	push := ws.push()
	requireExit(t, "push after completing the sync", push, 0)
	for _, path := range w.canonicalPaths(w.canonicalHead()) {
		if path == "api.md" {
			t.Error("canonical still carries the document the resolution deleted")
		}
	}
	requireEqual(t, "canonical guide.md",
		w.canonicalFile(w.canonicalHead(), "guide.md"), "guide\n")
}

// TestAPartiallyResolvedSyncCannotBeCompleted is the multi-file half of
// the two-hunk scenario: two documents conflict, one is resolved, and
// the other still carries markers.
//
// Both gates have to hold on the file that was left. The commit is
// blocked by the marker gate, and `--continue` — the new way to end a
// sync — refuses too, naming the file that is not ready. Completing here
// would record a base for a docs tree with conflict markers in it.
func TestAPartiallyResolvedSyncCannotBeCompleted(t *testing.T) {
	t.Parallel()

	w := newWorld(t, map[string]string{"api.md": "api one\n", "guide.md": "guide one\n"})
	ws := w.setup("partial-resolution")

	ws.commitDocs("docs: my edits", map[string]string{
		"api.md":   "api one\nMINE\n",
		"guide.md": "guide one\nMINE\n",
	})
	w.advanceCanonical(map[string]string{
		"api.md":   "api one\nTHEIRS\n",
		"guide.md": "guide one\nTHEIRS\n",
	}, "canonical: their edits")

	conflicted := ws.sanho("sync")
	requireContains(t, "sync output", conflicted.combined(), "2 files have conflicts")

	// Resolve one document and leave the other as it is.
	ws.writeDocs(map[string]string{"api.md": "api one\nRESOLVED\n"})
	ws.git("add", "docs/api.md")

	blocked := ws.gitExit("commit", "-m", "docs: resolve half of it")
	requireExit(t, "a commit with one file still conflicted", blocked, 1)
	requireContains(t, "marker gate", blocked.combined(), "docs/guide.md")

	refused := ws.run("sync", "--continue")
	requireExit(t, "--continue with one file still conflicted", refused, 1)
	requireContains(t, "refusal", refused.combined(), "not ready to be completed")
	requireContains(t, "refusal", refused.combined(), "docs/guide.md")
	requireNotContains(t, "refusal", refused.combined(), "docs/api.md")
	if !syncNoteExists(t, ws) {
		t.Fatal("a refused --continue cleared the sync note")
	}

	// Finish the other half, and the same two commands go through.
	ws.writeDocs(map[string]string{"guide.md": "guide one\nRESOLVED\n"})
	ws.git("add", "docs/guide.md")
	requireExit(t, "the complete resolution", ws.gitExit("commit", "-m", "docs: resolve both"), 0)
	requireExit(t, "--continue once nothing is conflicted", ws.run("sync", "--continue"), 0)

	push := ws.push()
	requireExit(t, "push after completing the sync", push, 0)
	requireEqual(t, "canonical api.md",
		w.canonicalFile(w.canonicalHead(), "api.md"), "api one\nRESOLVED\n")
	requireEqual(t, "canonical guide.md",
		w.canonicalFile(w.canonicalHead(), "guide.md"), "guide one\nRESOLVED\n")
}

// --- the legacy note, through the real hooks ----------------------------

// writeLegacyNote replaces the sync note with one in the shape a build
// before the entry record wrote: valid JSON, a target, and nothing that
// can say whether a commit settled anything.
func writeLegacyNote(t *testing.T, ws *workspace, prev, target string) {
	t.Helper()

	document, err := json.MarshalIndent(map[string]any{
		"prev_base":  map[string]string{"commit": prev, "tree": ""},
		"target":     map[string]string{"commit": target, "tree": ""},
		"started_at": time.Now().UTC().Format(time.RFC3339Nano),
	}, "", "  ")
	if err != nil {
		t.Fatalf("build a legacy sync note: %v", err)
	}
	writeFile(t, ws.path(".git", "sanho", "sync.json"), string(document)+"\n")
}

// TestALegacyNoteIsDrivenByTheRealHooks closes the gap the third review
// named twice: a note written before the entry fields existed had only
// ever been exercised in unit tests, and the message the real hooks
// printed for it stated a reason nothing knew to be true.
//
// Such a note cannot say whether a commit settled the conflict, so the
// tool must not claim that none did — and the workspace must have a way
// out that is not only the abort. Both are asserted here through `git`
// and `sanho`, with no test double anywhere.
func TestALegacyNoteIsDrivenByTheRealHooks(t *testing.T) {
	t.Parallel()

	w := newWorld(t, defaultCanonicalDocs())
	ws := w.setup("legacy-note")
	preMerge := w.canonicalHead()

	ws.commitDocs("docs: my edit", map[string]string{"api.md": "line one\nMINE\n"})
	theirs := w.advanceCanonical(map[string]string{"api.md": "line one\nTHEIRS\n"}, "canonical: their edit")
	ws.sanho("sync")

	// The resolution happens the ordinary way; the note is then replaced
	// with the old-format one, which is exactly the shape an upgrade
	// mid-sync leaves behind.
	ws.writeDocs(map[string]string{"api.md": "line one\nRESOLVED\n"})
	ws.git("add", "docs/api.md")
	ws.git("commit", "-m", "docs: resolve the conflict")
	writeLegacyNote(t, ws, preMerge, theirs)

	// pre-commit: reports, does not block, and does not explain itself
	// with a fact it cannot have.
	writeFile(t, ws.path("src", "main.go"), "package main\n")
	ws.git("add", "src/main.go")
	commit := ws.gitExit("commit", "-m", "feat: unrelated code")
	requireExit(t, "an unrelated commit under a legacy note", commit, 0)
	requireContains(t, "pre-commit notice", commit.combined(), "is not completed")
	requireContains(t, "pre-commit notice", commit.combined(), "sanho sync --continue")
	requireNotContains(t, "pre-commit notice", commit.combined(),
		"no commit has changed the files it conflicted on")

	// pre-push: refuses, on the note's existence.
	push := ws.push()
	requireExit(t, "push under a legacy note", push, 1)
	requireContains(t, "rejection", push.combined(), "is not completed")
	requireContains(t, "rejection", push.combined(), "no remote ref was changed")

	// And the way out is the ordinary one. Before `--continue` existed,
	// a legacy note could only be aborted — which threw away a
	// reconciliation the user had already made.
	completed := ws.sanho("sync", "--continue")
	requireContains(t, "completion", completed.combined(), "sync completed")
	requireEqual(t, "base file after --continue", recordedBase(t, ws), theirs)

	final := ws.push()
	requireExit(t, "push after completing a legacy note", final, 0)
	requireEqual(t, "canonical api.md",
		w.canonicalFile(w.canonicalHead(), "api.md"), "line one\nRESOLVED\n")
}

// --- pull inside the window ---------------------------------------------

// TestPullInsideTheConflictWindowIsRefused covers the third verb. `sync`
// and `git push` were both tested against an unfinished sync; `pull`,
// which replaces the docs worktree AND the index wholesale, was not —
// and it is the one that would silently destroy a resolution in
// progress.
func TestPullInsideTheConflictWindowIsRefused(t *testing.T) {
	t.Parallel()

	w := newWorld(t, defaultCanonicalDocs())
	ws := w.setup("pull-in-window")
	preMerge := w.canonicalHead()

	ws.commitDocs("docs: my edit", map[string]string{"api.md": "line one\nMINE\n"})
	w.advanceCanonical(map[string]string{"api.md": "line one\nTHEIRS\n"}, "canonical: their edit")
	ws.sanho("sync")

	markers := ws.readDocs("api.md")
	pull := ws.run("pull")
	requireExit(t, "pull during a conflicted sync", pull, 1)
	requireContains(t, "refusal", pull.combined(), "a conflicted sync is in progress")
	requireContains(t, "refusal", pull.combined(), "sanho sync --continue")

	// Nothing moved: the markers are where the sync put them, the note
	// stands, and the base is still the pre-sync one.
	requireEqual(t, "docs/api.md after the refused pull", ws.readDocs("api.md"), markers)
	if !syncNoteExists(t, ws) {
		t.Error("a refused pull cleared the sync note")
	}
	requireEqual(t, "base file after the refused pull", recordedBase(t, ws), preMerge)

	// The same refusal, machine-readable, since an agent branches on it.
	document := ws.run("pull", "--json")
	requireExit(t, "pull --json during a conflicted sync", document, 1)
	requireContains(t, "envelope", document.stdout, `"code": "sync_in_progress"`)
}

// --- linked worktrees ---------------------------------------------------

// linkedWorktree adds a `git worktree` of ws and returns it as a
// workspace in its own right. `.sanho.json` is gitignored and therefore
// never travels into a linked checkout (F-H3), so everything below
// exercises the config-root fallback as well as the sync window.
func linkedWorktree(t *testing.T, ws *workspace, name string) *workspace {
	t.Helper()

	dir := filepath.Join(ws.w.root, name)
	ws.git("worktree", "add", "--quiet", "-b", name, dir)
	return &workspace{w: ws.w, name: name, dir: resolvePath(t, dir), codeOrigin: ws.codeOrigin}
}

// TestAConflictedSyncInALinkedWorktreeStaysThere is the combination
// neither feature's tests had: a conflicted sync inside a linked
// worktree.
//
// The two halves of the sync state live in different places by design —
// the base file at the worktree root, the note under that worktree's own
// private git directory — so a window opened in one checkout must be
// invisible in the other. If it were not, one worktree's unfinished
// reconciliation would block or, worse, be completed from the other.
func TestAConflictedSyncInALinkedWorktreeStaysThere(t *testing.T) {
	t.Parallel()

	w := newWorld(t, defaultCanonicalDocs())
	main := w.setup("main-checkout")
	linked := linkedWorktree(t, main, "feature")

	// The linked checkout records its own base first, the way any
	// workspace does on its first sync.
	requireExit(t, "the first sync in the linked worktree", linked.run("sync"), 0)
	if !fileExists(t, linked.basePath()) {
		t.Fatal("a sync in the linked worktree recorded no base of its own")
	}

	linked.commitDocs("docs: my edit", map[string]string{"api.md": "line one\nMINE\n"})
	theirs := w.advanceCanonical(map[string]string{"api.md": "line one\nTHEIRS\n"}, "canonical: their edit")

	conflicted := linked.sanho("sync")
	requireContains(t, "sync output", conflicted.combined(), "have conflicts")

	// The note is the linked worktree's own, and the main checkout knows
	// nothing about it.
	if !fileExists(t, linked.path(".git")) {
		t.Fatal("the linked worktree has no .git entry to resolve")
	}
	if syncNoteExists(t, main) {
		t.Error("a sync in the linked worktree left a note in the main checkout")
	}
	requireContains(t, "linked status", linked.sanho("status").combined(), "sanho sync --continue")
	requireNotContains(t, "main status", main.sanho("status").combined(), "IN PROGRESS")

	// The window's gates hold in the worktree that owns it.
	requireExit(t, "push from the linked worktree during a sync",
		linked.gitExit("push", "--quiet", "origin", "feature"), 1)

	linked.writeDocs(map[string]string{"api.md": "line one\nRESOLVED\n"})
	linked.git("add", "docs/api.md")
	linked.git("commit", "-m", "docs: resolve the conflict")
	requireExit(t, "--continue in the linked worktree", linked.run("sync", "--continue"), 0)
	requireEqual(t, "linked base file", recordedBase(t, linked), theirs)

	push := linked.gitExit("push", "--quiet", "origin", "feature")
	requireExit(t, "push after completing the sync", push, 0)
	requireEqual(t, "canonical api.md",
		w.canonicalFile(w.canonicalHead(), "api.md"), "line one\nRESOLVED\n")
}

// --- the other two HEAD-moved hooks -------------------------------------

// TestPostMergeAndPostRewriteStandDownDuringASync extends the stand-down
// assertion past post-checkout, which was the only one a test had ever
// driven.
//
// All three hooks share one body, but "shares a body today" is not a
// property a regression suite can rely on, and these two are reached by
// completely different git operations. A merge and an amend both move
// HEAD while a sync is unfinished, and neither may write the base the
// note is holding still.
func TestPostMergeAndPostRewriteStandDownDuringASync(t *testing.T) {
	t.Parallel()

	w := newWorld(t, defaultCanonicalDocs())
	ws := w.setup("head-moved-hooks")
	preMerge := w.canonicalHead()

	// A side branch to merge later, made before the window opens.
	ws.git("checkout", "--quiet", "-b", "side")
	writeFile(t, ws.path("src", "side.go"), "package main\n")
	ws.git("add", "src/side.go")
	ws.git("commit", "-m", "feat: side work")
	ws.git("checkout", "--quiet", "main")

	ws.commitDocs("docs: my edit", map[string]string{"api.md": "line one\nMINE\n"})
	theirs := w.advanceCanonical(map[string]string{"api.md": "line one\nTHEIRS\n"}, "canonical: their edit")
	ws.sanho("sync")

	// Put the conflict aside so the docs match HEAD: the hook contract
	// guard must not be what stops the re-derivation.
	ws.git("stash", "push", "--quiet", "--", "docs")

	// A commit carrying the merge target as a trailer, from somewhere
	// that is not sanho — the exact thing re-derivation would adopt.
	ws.git("commit", "--allow-empty", "--no-verify", "-m", "chore: a stray trailer\n\ndocs-base: "+theirs)

	merged := ws.gitExit("merge", "--no-ff", "-m", "chore: merge side", "side")
	requireExit(t, "a merge while a sync is unfinished", merged, 0)
	requireNotContains(t, "post-merge output", merged.combined(), "docs base re-derived")
	requireEqual(t, "base file after the merge", recordedBase(t, ws), preMerge)

	amended := ws.gitExit("commit", "--amend", "--no-edit")
	requireExit(t, "an amend while a sync is unfinished", amended, 0)
	requireNotContains(t, "post-rewrite output", amended.combined(), "docs base re-derived")
	requireEqual(t, "base file after the amend", recordedBase(t, ws), preMerge)

	// The note survived both, and the workspace still has both exits.
	if !syncNoteExists(t, ws) {
		t.Fatal("a merge or an amend cleared the sync note")
	}
	requireExit(t, "the abort after both", ws.run("sync", "--abort"), 0)
	requireEqual(t, "base file after the abort", recordedBase(t, ws), preMerge)
}

// TestARederivationThatCannotRunIsSilent covers the hook contract's failure branch:
// the hook could not decide, so it says nothing and exits 0.
//
// P2 is the whole point. A `post-checkout` that failed because sanho
// could not hash the docs directory would turn an unreadable file into a
// broken `git checkout`, which is Critical C1's failure class in a
// different hook. The diagnostic is available under --verbose, where
// somebody is asking.
func TestARederivationThatCannotRunIsSilent(t *testing.T) {
	t.Parallel()

	w := newWorld(t, defaultCanonicalDocs())
	ws := w.setup("hook-cannot-decide")

	// Re-derivation has something to do — the recorded base disagrees
	// with the trailer history — and no way to do it: the workspace root
	// is read-only, so the atomic base write cannot create its temporary
	// file.
	stale := strings.Repeat("b", 40)
	writeFile(t, ws.basePath(), fmt.Sprintf("{\n  \"version\": 2,\n  \"commit\": %q,\n  \"tree\": \"\"\n}\n", stale))
	if err := os.Chmod(ws.dir, 0o555); err != nil {
		t.Fatalf("make the workspace root read-only: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(ws.dir, 0o755) })

	quiet := ws.run("hook", "post-checkout")
	requireExit(t, "post-checkout that cannot write the base", quiet, 0)
	if got := quiet.combined(); got != "" {
		t.Errorf("post-checkout printed %q, want silence", got)
	}

	loud := ws.run("--verbose", "hook", "post-checkout")
	requireExit(t, "post-checkout under --verbose", loud, 0)
	requireContains(t, "verbose output", loud.combined(), "base re-derivation skipped")

	if err := os.Chmod(ws.dir, 0o755); err != nil {
		t.Fatalf("restore the workspace root: %v", err)
	}
	requireEqual(t, "base file after a hook that could not decide", recordedBase(t, ws), stale)
}

// --- doctor, inside the window ------------------------------------------

// TestDoctorFixDoesNotReDeriveTheBaseDuringASync is the third base
// writer, brought under the same rule as the other two.
//
// `sanho doctor --fix` re-derives the base from the newest stamped
// commit. Inside an unfinished sync that is a third party writing the
// one file the window is defined by — and with a trailer from anywhere
// (an older build, a `--no-verify` commit) it could put the base on the
// merge target with pre-merge documents beneath it.
func TestDoctorFixDoesNotReDeriveTheBaseDuringASync(t *testing.T) {
	t.Parallel()

	w := newWorld(t, defaultCanonicalDocs())
	ws := w.setup("doctor-fix-in-window")

	ws.commitDocs("docs: my edit", map[string]string{"api.md": "line one\nMINE\n"})
	theirs := w.advanceCanonical(map[string]string{"api.md": "line one\nTHEIRS\n"}, "canonical: their edit")
	ws.sanho("sync")

	// The base file is removed, which is the state that makes
	// `doctor --fix` want to write one.
	removeFile(t, ws.basePath())
	ws.git("stash", "push", "--quiet", "--", "docs")
	ws.git("commit", "--allow-empty", "--no-verify", "-m", "chore: a stray trailer\n\ndocs-base: "+theirs)

	fixed := ws.run("doctor", "--fix")
	requireExit(t, "doctor --fix during a sync", fixed, 0)
	requireContains(t, "doctor --fix", fixed.combined(), "a sync is in progress")
	if fileExists(t, ws.basePath()) {
		t.Errorf("doctor --fix wrote a base a sync was holding still: %s", readFile(t, ws.basePath()))
	}

	// Ending the sync is what puts a base back, and it is the target the
	// note recorded rather than whatever a trailer claimed.
	requireExit(t, "the abort doctor named", ws.run("sync", "--abort"), 0)
	repaired := ws.run("doctor", "--fix")
	requireExit(t, "doctor --fix after the sync ended", repaired, 0)
	if !fileExists(t, ws.basePath()) {
		t.Error("doctor --fix wrote no base once the sync was over")
	}
}

// TestInitRefusesWhileASyncIsUnfinished is the fourth writer: `sanho
// init --force` re-establishes a base from canonical head and replaces
// the docs directory, which inside the window would leave the base ahead
// of documents the abort then takes back.
func TestInitRefusesWhileASyncIsUnfinished(t *testing.T) {
	t.Parallel()

	w := newWorld(t, defaultCanonicalDocs())
	ws := w.setup("init-in-window")
	preMerge := w.canonicalHead()

	ws.commitDocs("docs: my edit", map[string]string{"api.md": "line one\nMINE\n"})
	w.advanceCanonical(map[string]string{"api.md": "line one\nTHEIRS\n"}, "canonical: their edit")
	ws.sanho("sync")

	refused := ws.run("init",
		"--project", projectName, "--docs-repo-url", w.origin,
		"--actor-email", actorEmail, "--force", "-y")
	requireExit(t, "init --force during a sync", refused, 1)
	requireContains(t, "refusal", refused.combined(), "a conflicted sync is in progress")
	requireEqual(t, "base file after the refused init", recordedBase(t, ws), preMerge)
	if !syncNoteExists(t, ws) {
		t.Error("a refused init cleared the sync note")
	}
	requireContains(t, "docs/api.md", ws.readDocs("api.md"), "<<<<<<< sanho-ours")

	// And the command it names works, which is what keeps the refusal
	// closed.
	requireExit(t, "the abort init named", ws.run("sync", "--abort"), 0)
	requireExit(t, "init --force once the sync is over", ws.run("init",
		"--project", projectName, "--docs-repo-url", w.origin,
		"--actor-email", actorEmail, "--force", "-y"), 0)
}

// --- the machine surface ------------------------------------------------

// TestContinueJSONReportsTheAdoptedBase pins `--continue`'s half of the
// the JSON contract: an agent reads the outcome from the document.
func TestContinueJSONReportsTheAdoptedBase(t *testing.T) {
	t.Parallel()

	w := newWorld(t, defaultCanonicalDocs())
	ws := w.setup("continue-json")

	ws.commitDocs("docs: my edit", map[string]string{"api.md": "line one\nMINE\n"})
	theirs := w.advanceCanonical(map[string]string{"api.md": "line one\nTHEIRS\n"}, "canonical: their edit")
	ws.sanho("sync")

	// The refusal first: markers remain, and the code has to be one an
	// agent can branch on.
	refused := ws.run("sync", "--continue", "--json")
	requireExit(t, "--continue --json with markers present", refused, 1)
	requireContains(t, "envelope", refused.stdout, `"code": "markers_present"`)

	ws.writeDocs(map[string]string{"api.md": "line one\nRESOLVED\n"})
	ws.git("add", "docs/api.md")
	staged := ws.run("sync", "--continue", "--json")
	requireExit(t, "--continue --json before the commit", staged, 1)
	requireContains(t, "envelope", staged.stdout, `"code": "docs_dirty"`)

	ws.git("commit", "-m", "docs: resolve the conflict")

	var document struct {
		Status string `json:"status"`
		Base   *struct {
			Commit string `json:"commit"`
		} `json:"base"`
		Commit    string   `json:"commit"`
		Conflicts []string `json:"conflicts"`
	}
	done := ws.run("sync", "--continue", "--json")
	requireExit(t, "--continue --json", done, 0)
	if err := json.Unmarshal([]byte(done.stdout), &document); err != nil {
		t.Fatalf("parse the --continue document: %v\n%s", err, done.stdout)
	}
	requireEqual(t, "status", document.Status, "completed")
	if document.Base == nil || document.Base.Commit != theirs {
		t.Fatalf("base = %+v, want the merge target %s", document.Base, theirs)
	}
	// P3: completing a sync creates nothing, so there is no commit to
	// report.
	requireEqual(t, "commit", document.Commit, "")
	if len(document.Conflicts) != 0 {
		t.Errorf("conflicts = %v, want none", document.Conflicts)
	}
}

// TestTheThreeSyncModesAreMutuallyExclusive keeps the flag surface
// honest: three ways to end up in different places, named together, is a
// mistake worth a message rather than a precedence rule.
func TestTheThreeSyncModesAreMutuallyExclusive(t *testing.T) {
	t.Parallel()

	w := newWorld(t, defaultCanonicalDocs())
	ws := w.setup("exclusive-flags")

	for _, combination := range [][]string{
		{"sync", "--abort", "--continue"},
		{"sync", "--abort", "--rebase-onto", "HEAD"},
		{"sync", "--continue", "--rebase-onto", "HEAD"},
	} {
		res := ws.run(combination...)
		requireExit(t, fmt.Sprintf("sanho %v", combination), res, 1)
		requireContains(t, "refusal", res.combined(), "cannot be combined")
	}
}
