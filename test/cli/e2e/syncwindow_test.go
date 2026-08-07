package e2e

// The second review wave: the conflicted-sync window.
//
// The first wave hardened the *resolution test* but left the dangerous
// state itself. A conflicted sync advanced the base file to the merge
// target immediately, so for as long as the sync was unresolved the
// workspace held `base == canonical head` while the docs worktree still
// carried pre-merge content. Anything that made the note stop counting
// in that window handed the next push a fast-forward over upstream's
// own work — exit 0, no message.
//
// Two paths reached it, and both are reproduced here end to end:
//
//	Path 1  the note survives but is misread. `git stash push -- docs`
//	        clears the markers; an unrelated docs commit then moves HEAD
//	        and its docs tree, which the old completion test accepted as
//	        the resolution.
//	Path 2  the note disappears. A `sync.json` nothing can parse is
//	        cleared by the degraded abort, which could not restore the
//	        base because the base lived inside the note.
//
// The fix removes the state rather than the two symptoms: the base stays
// where the sync found it until a resolution is confirmed. These tests
// assert the base file directly, because it is the fact both paths were
// really about.

import (
	"encoding/json"
	"testing"
)

// recordedBase reads the commit OID out of the workspace base file. The
// window is a claim about that file, so the assertions read it rather
// than inferring it from behavior.
func recordedBase(t *testing.T, ws *workspace) string {
	t.Helper()

	var base struct {
		Commit string `json:"commit"`
	}
	raw := readFile(t, ws.basePath())
	if err := json.Unmarshal([]byte(raw), &base); err != nil {
		t.Fatalf("parse the base file: %v\n%s", err, raw)
	}
	return base.Commit
}

func syncNoteExists(t *testing.T, ws *workspace) bool {
	t.Helper()
	return fileExists(t, ws.path(".git", "sanho", "sync.json"))
}

// --- Path 1 -------------------------------------------------------------

// TestAnUnrelatedDocsCommitDoesNotResolveAStashedSync is the re-review's
// first path, reproduced exactly as it was found.
//
// The sync conflicts; the user stashes the markers away and then commits
// an entirely unrelated document. That commit moves HEAD *and* the docs
// tree, which is all the previous completion test asked for, so the note
// was cleared — and with the base already sitting on canonical head, the
// next push republished the pre-merge tree over upstream's work.
//
// Every step of that chain is asserted: the base does not move while the
// sync is owed, the commit path says out loud that a resolution is still
// owed, the unrelated commit's provenance names the pre-merge base
// rather than the merge target, and the push is refused with canonical
// untouched.
func TestAnUnrelatedDocsCommitDoesNotResolveAStashedSync(t *testing.T) {
	t.Parallel()

	w := newWorld(t, defaultCanonicalDocs())
	ws := w.setup("unrelated-docs-commit")
	preMerge := w.canonicalHead()

	ws.commitDocs("docs: my edit", map[string]string{"api.md": "line one\nMINE\n"})
	theirs := w.advanceCanonical(map[string]string{"api.md": "line one\nTHEIRS\n"}, "canonical: their edit")

	conflicted := ws.sanho("sync")
	requireContains(t, "sync output", conflicted.combined(), "have conflicts")

	// The window itself: the base stays where the sync found it, so
	// nothing downstream can read "base == canonical head" while the
	// worktree still holds pre-merge content.
	requireEqual(t, "base file during the conflict window", recordedBase(t, ws), preMerge)

	// The escape: markers into the stash, worktree back to HEAD.
	ws.git("stash", "push", "--quiet", "--", "docs")
	requireNotContains(t, "docs/api.md after stashing", ws.readDocs("api.md"), "<<<<<<<")

	// An unrelated document, committed while the sync is still owed. This
	// is the commit that used to stand in for the resolution.
	writeFile(t, ws.docsPath("notes.md"), "an unrelated note\n")
	ws.git("add", "docs/notes.md")
	commit := ws.gitExit("commit", "-m", "docs: an unrelated note")
	requireExit(t, "an unrelated docs commit during the window", commit, 0)
	requireContains(t, "pre-commit notice", commit.combined(), "was never resolved by a commit")

	// Its provenance describes the content it actually carries: pre-merge
	// docs derive from the pre-merge base. Stamping the merge target here
	// would let a later checkout re-derive the base onto the target and
	// reopen this very path through the trailer.
	requireContains(t, "commit trailers", ws.headMessage(), "docs-base: "+preMerge)

	push := ws.push()
	requireExit(t, "push after an unrelated docs commit", push, 1)
	requireContains(t, "rejection", push.combined(), "was never resolved by a commit")
	requireContains(t, "rejection", push.combined(), "no remote ref was changed")

	if !syncNoteExists(t, ws) {
		t.Error("an unrelated docs commit cleared the sync note; a resolution is still owed and nothing records it")
	}
	requireEqual(t, "base file after the unrelated commit", recordedBase(t, ws), preMerge)
	requireEqual(t, "canonical head", w.canonicalHead(), theirs)
	requireEqual(t, "canonical api.md",
		w.canonicalFile(w.canonicalHead(), "api.md"), "line one\nTHEIRS\n")
}

// TestResolvingTheConflictAdvancesTheBase is Path 1's other half: the
// window closes on a real resolution, and the base moves then.
//
// It is the reason the base advance could be deferred at all — the
// resolution commit must still end up describing the merge target, both
// in the base file and in its own provenance trailer, or the next push
// would report a conflict that nobody created.
func TestResolvingTheConflictAdvancesTheBase(t *testing.T) {
	t.Parallel()

	w := newWorld(t, defaultCanonicalDocs())
	ws := w.setup("resolution-advances-base")
	preMerge := w.canonicalHead()

	ws.commitDocs("docs: my edit", map[string]string{"api.md": "line one\nMINE\n"})
	theirs := w.advanceCanonical(map[string]string{"api.md": "line one\nTHEIRS\n"}, "canonical: their edit")
	ws.sanho("sync")
	requireEqual(t, "base file during the conflict window", recordedBase(t, ws), preMerge)

	ws.writeDocs(map[string]string{"api.md": "line one\nRESOLVED\n"})
	ws.git("add", "docs/api.md")
	resolve := ws.gitExit("commit", "-m", "docs: resolve the conflict")
	requireExit(t, "the resolution commit", resolve, 0)

	// The resolution derives from the target, so that is what it stamps —
	// otherwise a re-derivation would wind the base back and the next
	// push would "merge" documents nobody reverted.
	requireContains(t, "resolution trailers", ws.headMessage(), "docs-base: "+theirs)
	// The commit that resolves the sync must not be told a sync is owed.
	requireNotContains(t, "resolution commit output", resolve.combined(), "was never resolved by a commit")

	push := ws.push()
	requireExit(t, "push after resolving", push, 0)
	requireContains(t, "push output", push.combined(), "published docs")
	if syncNoteExists(t, ws) {
		t.Error("the publishing push left the sync note behind")
	}
	requireEqual(t, "canonical api.md",
		w.canonicalFile(w.canonicalHead(), "api.md"), "line one\nRESOLVED\n")
}

// TestTheOwedSyncNoticeReplacesTheFreshnessWarning is the consequence of
// holding the base still, dealt with rather than tolerated.
//
// For the whole of the window the base really is behind canonical — that
// is the point of leaving it there — so the §5.6 freshness warning would
// otherwise fire on every commit, under a notice describing the same
// state more usefully, and would advise a `sanho sync` that refuses
// while a note exists. One line about one state.
func TestTheOwedSyncNoticeReplacesTheFreshnessWarning(t *testing.T) {
	t.Parallel()

	w := newWorld(t, defaultCanonicalDocs())
	ws := w.setup("one-line-per-state")

	ws.commitDocs("docs: my edit", map[string]string{"api.md": "line one\nMINE\n"})
	w.advanceCanonical(map[string]string{"api.md": "line one\nTHEIRS\n"}, "canonical: their edit")
	ws.sanho("sync")
	ws.git("stash", "push", "--quiet", "--", "docs")

	writeFile(t, ws.path("src", "main.go"), "package main\n")
	ws.git("add", "src/main.go")
	commit := ws.gitExit("commit", "-m", "feat: unrelated code")
	requireExit(t, "an unrelated code commit while a sync is owed", commit, 0)

	requireContains(t, "pre-commit output", commit.combined(), "was never resolved by a commit")
	requireNotContains(t, "pre-commit output", commit.combined(), "commits behind")

	// And the warning comes back the moment the sync is finished with.
	requireExit(t, "the advised abort", ws.run("sync", "--abort"), 0)
	writeFile(t, ws.path("src", "other.go"), "package main\n")
	ws.git("add", "src/other.go")
	after := ws.gitExit("commit", "-m", "feat: more unrelated code")
	requireExit(t, "an unrelated code commit after the abort", after, 0)
	requireContains(t, "pre-commit output after the abort", after.combined(), "commits behind")
}

// TestACheckoutDoesNotReDeriveTheBaseWhileASyncIsOwed is the other half
// of holding the base still: nothing else may write it either.
//
// Base re-derivation adopts whatever the newest stamped commit names,
// and a commit made while a conflict was set aside can perfectly well
// carry a trailer pointing at the merge target — an older build stamped
// every commit in the window that way, and `--no-verify` puts any
// trailer anywhere. Adopting it would put the base on canonical head
// with pre-merge documents beneath it, which is exactly the state this
// wave removes, arrived at through the trailer instead of through the
// sync.
//
// So the three HEAD-moved hooks stand down for the duration. The note
// survives every checkout, and the hook that settles it writes the base.
func TestACheckoutDoesNotReDeriveTheBaseWhileASyncIsOwed(t *testing.T) {
	t.Parallel()

	w := newWorld(t, defaultCanonicalDocs())
	ws := w.setup("checkout-during-sync")
	preMerge := w.canonicalHead()

	ws.commitDocs("docs: my edit", map[string]string{"api.md": "line one\nMINE\n"})
	theirs := w.advanceCanonical(map[string]string{"api.md": "line one\nTHEIRS\n"}, "canonical: their edit")
	ws.sanho("sync")

	// Put the conflict aside, so the docs worktree matches HEAD and the
	// §5.10 step-1 guard is not what stops the re-derivation.
	ws.git("stash", "push", "--quiet", "--", "docs")

	// A commit carrying the merge target as its docs base while the docs
	// are still pre-merge. --no-verify keeps sanho's own stamping out of
	// it: the point is a trailer from somewhere else.
	ws.git("commit", "--allow-empty", "--no-verify", "-m", "chore: a stray trailer\n\ndocs-base: "+theirs)

	checkout := ws.gitExit("checkout", "-b", "probe")
	requireExit(t, "a checkout while a sync is owed", checkout, 0)
	requireNotContains(t, "post-checkout output", checkout.combined(), "docs base re-derived")
	requireEqual(t, "base file after the checkout", recordedBase(t, ws), preMerge)

	// And the abort still finds the state it left behind.
	requireExit(t, "abort after the checkout", ws.run("sync", "--abort"), 0)
	requireEqual(t, "base file after the abort", recordedBase(t, ws), preMerge)
}

// --- Path 2 -------------------------------------------------------------

// TestAbortOverACorruptNoteLeavesASafeBase is the re-review's second
// path.
//
// A `sync.json` nothing can parse is cleared by `sanho sync --abort` on
// its existence alone — that is the abort's contract and it is right.
// What made it dangerous was the base: the conflicted sync had already
// moved it to the merge target, the previous value lived inside the note
// that could not be read, and the abort therefore left a workspace whose
// base claimed canonical head while its docs were pre-merge. The next
// push fast-forwarded over upstream.
//
// With the base left alone at conflict time there is nothing to restore:
// the abort is lossless, the follow-up repair line is gone, and the push
// is refused as the ordinary case-③ conflict it always was.
func TestAbortOverACorruptNoteLeavesASafeBase(t *testing.T) {
	t.Parallel()

	w := newWorld(t, defaultCanonicalDocs())
	ws := w.setup("corrupt-note-safe-base")
	preMerge := w.canonicalHead()

	ws.commitDocs("docs: my edit", map[string]string{"api.md": "line one\nMINE\n"})
	theirs := w.advanceCanonical(map[string]string{"api.md": "line one\nTHEIRS\n"}, "canonical: their edit")
	ws.sanho("sync")
	requireEqual(t, "base file during the conflict window", recordedBase(t, ws), preMerge)

	writeFile(t, ws.path(".git", "sanho", "sync.json"), "{ this file is not JSON\n")

	abort := ws.run("sync", "--abort")
	requireExit(t, "abort over a corrupt note", abort, 0)
	requireContains(t, "abort output", abort.combined(), "sync aborted; docs restored to HEAD")
	// Nothing was lost, so nothing is owed: the degraded follow-up line
	// described a base the abort no longer has to guess at.
	requireNotContains(t, "abort output", abort.combined(), "the docs base was left as the sync set it")
	if syncNoteExists(t, ws) {
		t.Error("abort left the corrupt sync note behind")
	}
	requireEqual(t, "docs/api.md after abort", ws.readDocs("api.md"), "line one\nMINE\n")
	requireEqual(t, "base file after the degraded abort", recordedBase(t, ws), preMerge)

	push := ws.push()
	requireExit(t, "push after the degraded abort", push, 1)
	requireContains(t, "rejection", push.combined(), "your docs changes conflict with upstream")
	requireContains(t, "rejection", push.combined(), "\n  docs/api.md\n")
	requireEqual(t, "canonical head", w.canonicalHead(), theirs)
	requireEqual(t, "canonical api.md",
		w.canonicalFile(w.canonicalHead(), "api.md"), "line one\nTHEIRS\n")
}

// --- the gates, re-confirmed --------------------------------------------

// TestEveryUnfinishedSyncNoteStillRefusesThePush pins the invariant the
// rest of this wave leans on: whatever shape an unfinished sync is in,
// the push boundary refuses it and canonical is untouched.
//
// Deferring the base advance makes the *consequence* of a missed refusal
// far smaller, and that is exactly why the refusals themselves are
// re-asserted rather than assumed: they are the layer that was never
// supposed to be load-bearing alone.
func TestEveryUnfinishedSyncNoteStillRefusesThePush(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// derange puts the conflicted workspace into one of the three
		// shapes an unfinished sync can have.
		derange func(t *testing.T, ws *workspace)
		want    string
	}{
		{
			name:    "markers still in the worktree",
			derange: func(t *testing.T, ws *workspace) {},
			want:    "finish the sync first",
		},
		{
			name:    "put aside without a resolution commit",
			derange: func(t *testing.T, ws *workspace) { ws.git("stash", "push", "--quiet", "--", "docs") },
			want:    "was never resolved by a commit",
		},
		{
			name: "a note nothing can parse",
			derange: func(t *testing.T, ws *workspace) {
				writeFile(t, ws.path(".git", "sanho", "sync.json"), "{ this file is not JSON\n")
			},
			want: "record of the sync in progress is unreadable",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			w := newWorld(t, defaultCanonicalDocs())
			ws := w.setup("gate")
			ws.commitDocs("docs: my edit", map[string]string{"api.md": "line one\nMINE\n"})
			theirs := w.advanceCanonical(
				map[string]string{"api.md": "line one\nTHEIRS\n"}, "canonical: their edit")
			ws.sanho("sync")
			test.derange(t, ws)

			push := ws.push()
			requireExit(t, "push with an unfinished sync", push, 1)
			requireContains(t, "rejection", push.combined(), test.want)
			requireContains(t, "rejection", push.combined(), "no remote ref was changed")
			if !syncNoteExists(t, ws) {
				t.Error("the refused push cleared the sync note")
			}
			requireEqual(t, "canonical head", w.canonicalHead(), theirs)
		})
	}
}
