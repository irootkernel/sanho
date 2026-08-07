package e2e

// The third review wave: completion becomes an act, not an inference.
//
// Two waves narrowed the predicate "was this merge resolved?" and a
// third review walked through the smaller door that was left. The three
// paths below are that review's reproductions, run end to end against
// the real binary:
//
//	C1  conflict → `git stash push -- docs` → keep editing THE
//	    CONFLICTED FILE → commit. The tree evidence is
//	    indistinguishable from a resolution, so the sync was declared
//	    finished and the next push republished pre-merge content.
//	C2  the same start, aborted. The window commit carried the merge
//	    TARGET in its `docs-base` trailer, and one branch switch after
//	    the abort was enough for re-derivation to adopt it.
//	C3  a crash between the base write and the note clear, plus a note
//	    nothing can parse. The abort skipped the base on the assumption
//	    that a conflicted sync never moves it, and left the base on the
//	    target.
//
// The fix is not a fourth predicate. `sanho sync --continue` is the only
// thing that completes a sync, and every base write obeys one invariant:
// a recorded base may never be ahead of the docs the worktree carries,
// so when in doubt the older value wins.

import (
	"encoding/json"
	"testing"
)

// baseFileExists reports whether the workspace records a base at all.
// C3's fix is a *removal*, so the assertion has to be able to see one.
func baseFileExists(t *testing.T, ws *workspace) bool {
	t.Helper()
	return fileExists(t, ws.basePath())
}

// canonicalHeadTree is the docs tree of canonical head, which C3 needs
// in order to forge the base file the crash would have left behind.
func canonicalHeadTree(w *world) string {
	w.t.Helper()
	return trimmed(w.git(w.origin, "rev-parse", "refs/heads/main^{tree}").stdout)
}

func trimmed(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r' || s[len(s)-1] == ' ') {
		s = s[:len(s)-1]
	}
	return s
}

// forgeBaseFile writes the base file directly, which is how the C3
// fixture reaches a state no ordinary command produces.
func forgeBaseFile(t *testing.T, ws *workspace, commit, tree string) {
	t.Helper()
	document, err := json.MarshalIndent(map[string]any{
		"version": 2, "commit": commit, "tree": tree,
	}, "", "  ")
	if err != nil {
		t.Fatalf("build a base file: %v", err)
	}
	writeFile(t, ws.basePath(), string(document)+"\n")
}

// --- C1 -----------------------------------------------------------------

// TestEditingTheConflictedFileAfterAStashDoesNotResolve is the review's
// first path, and the reason inference was abandoned.
//
// After escaping the markers with a stash, the most natural next action
// is to keep working on the same document — and that commit moves HEAD,
// moves the docs tree, and changes a path the merge conflicted on. Every
// question the completion test could ask answers "resolved", while the
// merge stands exactly where it was: upstream's line was never seen, let
// alone reconciled.
//
// Nothing here may be read as a completion. The note survives every
// hook, the push is refused, and canonical still carries upstream's own
// work.
func TestEditingTheConflictedFileAfterAStashDoesNotResolve(t *testing.T) {
	t.Parallel()

	w := newWorld(t, defaultCanonicalDocs())
	ws := w.setup("edit-after-stash")
	preMerge := w.canonicalHead()

	ws.commitDocs("docs: my edit", map[string]string{"api.md": "line one\nMINE\n"})
	theirs := w.advanceCanonical(map[string]string{"api.md": "line one\nTHEIRS\n"}, "canonical: their edit")

	conflicted := ws.sanho("sync")
	requireContains(t, "sync output", conflicted.combined(), "have conflicts")

	// The escape, and then the natural next action: keep editing the very
	// file the merge could not settle.
	ws.git("stash", "push", "--quiet", "--", "docs")
	ws.writeDocs(map[string]string{"api.md": "line one\nMINE\nMORE OF MINE\n"})
	ws.git("add", "docs/api.md")
	commit := ws.gitExit("commit", "-m", "docs: more of my own work")
	requireExit(t, "a commit on the conflicted file during the window", commit, 0)

	// It is not silent, and it does not congratulate anybody: a sync ends
	// when the user says so.
	requireContains(t, "commit output", commit.combined(), "sanho sync --continue")
	requireNotContains(t, "commit output", commit.combined(), "sync resolved")

	// Its provenance names the base file's value. Stamping the target
	// here is C2's poisoned trailer, laid by C1's own commit.
	requireContains(t, "window commit trailers", ws.headMessage(), "docs-base: "+preMerge)
	requireNotContains(t, "window commit trailers", ws.headMessage(), theirs)

	// A later hook must not settle it either — this is where the note
	// used to be cleared, one commit after the fact.
	later := ws.gitExit("commit", "--allow-empty", "-m", "chore: anything at all")
	requireExit(t, "a later commit during the window", later, 0)
	requireNotContains(t, "later commit output", later.combined(), "sync resolved")
	if !syncNoteExists(t, ws) {
		t.Fatal("a commit on the conflicted file cleared the sync note; the merge is still unresolved")
	}

	push := ws.push()
	requireExit(t, "push after editing the conflicted file", push, 1)
	requireContains(t, "rejection", push.combined(), "no remote ref was changed")
	requireContains(t, "rejection", push.combined(), "sanho sync --continue")

	requireEqual(t, "base file", recordedBase(t, ws), preMerge)
	requireEqual(t, "canonical head", w.canonicalHead(), theirs)
	requireEqual(t, "canonical api.md",
		w.canonicalFile(w.canonicalHead(), "api.md"), "line one\nTHEIRS\n")

	// And the read commands agree with the gate rather than with the
	// tidy-looking worktree.
	status := ws.sanho("status")
	requireContains(t, "status", status.combined(), "sanho sync --continue")
	doctor := ws.sanho("doctor")
	requireContains(t, "doctor", doctor.combined(), "sanho sync --continue")
}

// TestContinueCompletesTheSyncTheUserResolved is C1's other half: the
// explicit act works, and it is the only thing that does.
func TestContinueCompletesTheSyncTheUserResolved(t *testing.T) {
	t.Parallel()

	w := newWorld(t, defaultCanonicalDocs())
	ws := w.setup("continue-completes")
	preMerge := w.canonicalHead()

	ws.commitDocs("docs: my edit", map[string]string{"api.md": "line one\nMINE\n"})
	theirs := w.advanceCanonical(map[string]string{"api.md": "line one\nTHEIRS\n"}, "canonical: their edit")
	ws.sanho("sync")

	ws.writeDocs(map[string]string{"api.md": "line one\nRESOLVED\n"})
	ws.git("add", "docs/api.md")
	resolve := ws.gitExit("commit", "-m", "docs: resolve the conflict")
	requireExit(t, "the resolution commit", resolve, 0)

	// The commit is a commit. It does not finish the sync, and the base
	// has not moved: the resolution commit's own trailer names the base
	// file's value, which is still the pre-merge one.
	requireContains(t, "resolution trailers", ws.headMessage(), "docs-base: "+preMerge)
	requireEqual(t, "base file after the resolution commit", recordedBase(t, ws), preMerge)
	if !syncNoteExists(t, ws) {
		t.Fatal("the resolution commit cleared the sync note; only --continue may")
	}

	// The push is refused until the sync is completed — the contract, not
	// an accident.
	refused := ws.push()
	requireExit(t, "push before --continue", refused, 1)
	requireContains(t, "rejection", refused.combined(), "sanho sync --continue")

	done := ws.sanho("sync", "--continue")
	requireContains(t, "continue output", done.combined(), "sync completed")

	// The base adopts the target exactly here, and nothing else changed:
	// --continue creates no commit (P3).
	requireEqual(t, "base file after --continue", recordedBase(t, ws), theirs)
	if syncNoteExists(t, ws) {
		t.Error("--continue left the sync note behind")
	}
	requireEqual(t, "HEAD subject after --continue", ws.headSubject(), "docs: resolve the conflict")

	push := ws.push()
	requireExit(t, "push after --continue", push, 0)
	requireContains(t, "push output", push.combined(), "published docs")
	requireEqual(t, "canonical api.md",
		w.canonicalFile(w.canonicalHead(), "api.md"), "line one\nRESOLVED\n")
}

// --- C2 -----------------------------------------------------------------

// TestAbortThenBranchSwitchDoesNotAdoptAPoisonedTrailer is the review's
// second path.
//
// The trailer outlives the abort. A commit made inside the window used
// to be stamped with the merge target whenever it touched a conflicted
// path, so `sanho sync --abort` — the command the tool itself advises —
// followed by one branch switch let base re-derivation adopt the target
// with pre-merge documents beneath it. The stand-down guard did not
// help: it only holds while a note exists, and the abort had just
// removed it.
//
// The invariant closes it at the source. Inside the window the stamp is
// always the base file's own value, so there is no poisoned trailer to
// adopt; the target reaches the base file through `--continue` and
// nowhere else.
func TestAbortThenBranchSwitchDoesNotAdoptAPoisonedTrailer(t *testing.T) {
	t.Parallel()

	w := newWorld(t, defaultCanonicalDocs())
	ws := w.setup("poisoned-trailer")
	preMerge := w.canonicalHead()

	ws.commitDocs("docs: my edit", map[string]string{"api.md": "line one\nMINE\n"})
	theirs := w.advanceCanonical(map[string]string{"api.md": "line one\nTHEIRS\n"}, "canonical: their edit")
	ws.sanho("sync")

	ws.git("stash", "push", "--quiet", "--", "docs")
	ws.writeDocs(map[string]string{"api.md": "line one\nMINE\nMORE OF MINE\n"})
	ws.git("add", "docs/api.md")
	ws.git("commit", "-m", "docs: more of my own work")

	// The stamp describes content the worktree actually has.
	requireContains(t, "window commit trailers", ws.headMessage(), "docs-base: "+preMerge)
	requireNotContains(t, "window commit trailers", ws.headMessage(), theirs)

	abort := ws.run("sync", "--abort")
	requireExit(t, "the advised abort", abort, 0)

	// Re-derivation is free to run now — the note is gone — and there is
	// nothing in history that could carry it past the worktree.
	checkout := ws.gitExit("checkout", "-b", "probe")
	requireExit(t, "a branch switch after the abort", checkout, 0)
	requireNotContains(t, "post-checkout output", checkout.combined(), theirs[:12])
	requireEqual(t, "base file after the branch switch", recordedBase(t, ws), preMerge)

	push := ws.gitExit("push", "--quiet", "origin", "probe")
	requireExit(t, "push after the abort and the branch switch", push, 1)
	requireContains(t, "rejection", push.combined(), "no remote ref was changed")
	requireEqual(t, "canonical head", w.canonicalHead(), theirs)
	requireEqual(t, "canonical api.md",
		w.canonicalFile(w.canonicalHead(), "api.md"), "line one\nTHEIRS\n")
}

// TestNoCommitInTheWindowEverStampsTheMergeTarget is C2's invariant on
// its own, stated over the two commits that used to be stamped
// differently: one that touches a conflicted path and one that does not.
func TestNoCommitInTheWindowEverStampsTheMergeTarget(t *testing.T) {
	t.Parallel()

	w := newWorld(t, defaultCanonicalDocs())
	ws := w.setup("window-stamping")
	preMerge := w.canonicalHead()

	ws.commitDocs("docs: my edit", map[string]string{"api.md": "line one\nMINE\n"})
	theirs := w.advanceCanonical(map[string]string{"api.md": "line one\nTHEIRS\n"}, "canonical: their edit")
	ws.sanho("sync")

	// 1. The resolution itself.
	ws.writeDocs(map[string]string{"api.md": "line one\nRESOLVED\n"})
	ws.git("add", "docs/api.md")
	ws.git("commit", "-m", "docs: resolve the conflict")
	requireContains(t, "resolution trailers", ws.headMessage(), "docs-base: "+preMerge)
	requireNotContains(t, "resolution trailers", ws.headMessage(), theirs)

	// 2. An unrelated document, committed in the same window.
	writeFile(t, ws.docsPath("notes.md"), "an unrelated note\n")
	ws.git("add", "docs/notes.md")
	ws.git("commit", "-m", "docs: an unrelated note")
	requireContains(t, "unrelated commit trailers", ws.headMessage(), "docs-base: "+preMerge)
	requireNotContains(t, "unrelated commit trailers", ws.headMessage(), theirs)

	// And the target reaches the base file only through the explicit act.
	ws.sanho("sync", "--continue")
	requireEqual(t, "base file after --continue", recordedBase(t, ws), theirs)
}

// --- C3 -----------------------------------------------------------------

// TestCorruptNoteAbortWithAnAdvancedBaseCannotFastForward is the
// review's third path.
//
// The corrupt-note abort skipped the base on the premise that a
// conflicted sync leaves it alone. Two states break the premise: a crash
// between the base write and the note clear, and a note left by a build
// that advanced the base at conflict time. In both, the abort left a
// base sitting on canonical head with pre-merge documents beneath it and
// the next push fast-forwarded over upstream, at exit 0.
//
// An abort that cannot read the note cannot know what the base should
// be, so it takes the older value the invariant demands: none at all.
// Publication then refuses for a reason it can name, and `sanho sync`
// works from there.
func TestCorruptNoteAbortWithAnAdvancedBaseCannotFastForward(t *testing.T) {
	t.Parallel()

	w := newWorld(t, defaultCanonicalDocs())
	ws := w.setup("corrupt-advanced-base")

	ws.commitDocs("docs: my edit", map[string]string{"api.md": "line one\nMINE\n"})
	theirs := w.advanceCanonical(map[string]string{"api.md": "line one\nTHEIRS\n"}, "canonical: their edit")
	ws.sanho("sync")

	// The crash: the base reached the target, the note was never cleared,
	// and what is left of it cannot be parsed.
	forgeBaseFile(t, ws, theirs, canonicalHeadTree(w))
	writeFile(t, ws.path(".git", "sanho", "sync.json"), "{ this file is not JSON\n")

	abort := ws.run("sync", "--abort")
	requireExit(t, "abort over a corrupt note", abort, 0)
	requireContains(t, "abort output", abort.combined(), "sync aborted; docs restored to HEAD")
	if syncNoteExists(t, ws) {
		t.Error("abort left the corrupt sync note behind")
	}
	if baseFileExists(t, ws) {
		t.Fatalf("abort kept a base it could not vouch for: %s", readFile(t, ws.basePath()))
	}
	requireEqual(t, "docs/api.md after abort", ws.readDocs("api.md"), "line one\nMINE\n")

	push := ws.push()
	requireExit(t, "push after the degraded abort", push, 1)
	requireContains(t, "rejection", push.combined(), "docs must be reconciled before publishing")
	requireContains(t, "rejection", push.combined(), "no remote ref was changed")
	requireEqual(t, "canonical head", w.canonicalHead(), theirs)
	requireEqual(t, "canonical api.md",
		w.canonicalFile(w.canonicalHead(), "api.md"), "line one\nTHEIRS\n")

	// Guidance closure: the rejection names `sanho sync`, and it works
	// from a workspace with no base at all.
	resync := ws.run("sync")
	requireExit(t, "the advised sync", resync, 0)
	requireContains(t, "sync output", resync.combined(), "have conflicts")
}
