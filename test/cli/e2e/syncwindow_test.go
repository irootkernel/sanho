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
// where the sync found it until the sync is completed. These tests
// assert the base file directly, because it is the fact both paths were
// really about.
//
// The THIRD wave then changed what "completed" means — it is
// `sanho sync --continue`, not an inference — and two claims in this
// file were inverted by that rather than reworded (see
// TestCompletingTheSyncAdvancesTheBase and
// TestAbortOverACorruptNoteLeavesASafeBase). Both say why in their own
// comments; continuewave_test.go carries the reproductions that forced
// the change.

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
	// The commit being prepared makes the docs dirty, so the notice is
	// the one that asserts nothing about what has been committed. What it
	// must not do is stay silent: the previous version suppressed exactly
	// this line whenever the staged tree touched a conflicted path, which
	// is the moment a user is most likely to think the sync ended.
	requireContains(t, "pre-commit notice", commit.combined(), "is not completed")
	requireContains(t, "pre-commit notice", commit.combined(), "sanho sync --continue")

	// Its provenance describes the content it actually carries: pre-merge
	// docs derive from the pre-merge base. That is now true of EVERY
	// commit in the window, not only of the ones that leave the conflict
	// alone — the third review found the exception (a commit that touched
	// a conflicted path was stamped with the merge target) surviving the
	// abort that the tool itself advises.
	requireContains(t, "commit trailers", ws.headMessage(), "docs-base: "+preMerge)
	requireNotContains(t, "commit trailers", ws.headMessage(), theirs)

	push := ws.push()
	requireExit(t, "push after an unrelated docs commit", push, 1)
	requireContains(t, "rejection", push.combined(), "no commit has changed the files it conflicted on")
	requireContains(t, "rejection", push.combined(), "no remote ref was changed")

	if !syncNoteExists(t, ws) {
		t.Error("an unrelated docs commit cleared the sync note; a resolution is still owed and nothing records it")
	}
	requireEqual(t, "base file after the unrelated commit", recordedBase(t, ws), preMerge)
	requireEqual(t, "canonical head", w.canonicalHead(), theirs)
	requireEqual(t, "canonical api.md",
		w.canonicalFile(w.canonicalHead(), "api.md"), "line one\nTHEIRS\n")
}

// TestCompletingTheSyncAdvancesTheBase is Path 1's other half, restated
// for the contract that replaced the inference.
//
// The wave-2 version asserted two things this wave deliberately breaks:
// that the resolution commit stamps the merge TARGET, and that the base
// advances when the commit lands. Both were the inference in disguise —
// the trailer outlived aborts (C2) and the advance rested on evidence a
// stash could forge (C1). What survives is the property they were really
// protecting: after the sync is completed, the base and the docs
// describe the same canonical state, so the next push is not a conflict
// nobody created.
func TestCompletingTheSyncAdvancesTheBase(t *testing.T) {
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

	// The resolution stamps the base file's value, like every other
	// commit in the window. A later re-derivation that adopts it lands on
	// a base that is at worst too OLD, which publication reconciles as an
	// ordinary divergence — the direction the invariant chooses.
	requireContains(t, "resolution trailers", ws.headMessage(), "docs-base: "+preMerge)
	requireNotContains(t, "resolution trailers", ws.headMessage(), theirs)
	// It is not accused of having abandoned the sync, either: the notice
	// it gets names the one step that is actually outstanding.
	requireNotContains(t, "resolution commit output", resolve.combined(), "no commit has changed the files it conflicted on")
	requireContains(t, "resolution commit output", resolve.combined(), "sanho sync --continue")

	requireEqual(t, "base file after the resolution commit", recordedBase(t, ws), preMerge)
	ws.sanho("sync", "--continue")
	requireEqual(t, "base file after the completion", recordedBase(t, ws), theirs)
	if syncNoteExists(t, ws) {
		t.Error("--continue left the sync note behind")
	}

	push := ws.push()
	requireExit(t, "push after completing the sync", push, 0)
	requireContains(t, "push output", push.combined(), "published docs")
	requireEqual(t, "canonical api.md",
		w.canonicalFile(w.canonicalHead(), "api.md"), "line one\nRESOLVED\n")

	// And the workspace is coherent: nothing owed, nothing behind.
	doctor := ws.run("doctor")
	requireExit(t, "doctor after the flow", doctor, 0)
	requireContains(t, "doctor", doctor.combined(), "no problems found")
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

	requireContains(t, "pre-commit output", commit.combined(), "no commit has changed the files it conflicted on")
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
// survives every checkout, and the base is written when the user
// completes the sync.
//
// Kept, with its stakes re-stated. When it was written, this guard was
// the last line of defense: sanho's own resolution commits carried the
// merge target, so a re-derivation that ran inside the window would
// adopt one. It is no longer load-bearing — every commit in the window
// now stamps the base file's own value, and
// TestAbortThenBranchSwitchDoesNotAdoptAPoisonedTrailer proves the
// dangerous trailer cannot exist at all — but the guard stays, and so
// does this test, because a third party writing the file a sync is
// holding still is noise nobody needs. The `--no-verify` trailer below
// is what keeps the test honest about that: it is the one way such a
// trailer can still reach history.
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
// path, and the third review's C3 on the same fixture.
//
// A `sync.json` nothing can parse is cleared by `sanho sync --abort` on
// its existence alone — that is the abort's contract and it is right.
// What made it dangerous was the base: the previous value lived inside
// the note that could not be read, so the abort had to decide what to do
// with a file it could not vouch for. Wave 2 answered "leave it", on the
// premise that a conflicted sync never moves it; wave 3 found two states
// where the premise is false and the leftover base is the merge target.
//
// The answer now is the invariant: where the base cannot be established,
// take the older value, and none at all is the oldest there is. The
// assertion below is therefore *not* the wave-2 one reworded — it is the
// opposite claim about the same file, and the push refusal it produces
// is `no_base` rather than a conflict.
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
	// The old degraded follow-up line described a base the abort had left
	// on the merge target and asked the user to repair. There is nothing
	// left behind to repair now.
	requireNotContains(t, "abort output", abort.combined(), "the docs base was left as the sync set it")
	if syncNoteExists(t, ws) {
		t.Error("abort left the corrupt sync note behind")
	}
	requireEqual(t, "docs/api.md after abort", ws.readDocs("api.md"), "line one\nMINE\n")
	if fileExists(t, ws.basePath()) {
		t.Errorf("abort kept a base it could not vouch for: %s", readFile(t, ws.basePath()))
	}

	push := ws.push()
	requireExit(t, "push after the degraded abort", push, 1)
	requireContains(t, "rejection", push.combined(), "docs must be reconciled before publishing")
	requireContains(t, "rejection", push.combined(), "no remote ref was changed")
	requireEqual(t, "canonical head", w.canonicalHead(), theirs)
	requireEqual(t, "canonical api.md",
		w.canonicalFile(w.canonicalHead(), "api.md"), "line one\nTHEIRS\n")

	// And the advised recovery works from there, which is what makes
	// forgetting the base a closed answer rather than a wedge.
	resync := ws.run("sync")
	requireExit(t, "the advised sync", resync, 0)
	requireContains(t, "sync output", resync.combined(), "have conflicts")
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
		// derange puts the conflicted workspace into one of the shapes an
		// unfinished sync can have.
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
			want:    "no commit has changed the files it conflicted on",
		},
		{
			// The shape the explicit-completion contract adds, and the one
			// a user is most likely to believe is finished.
			name: "resolved and committed, but never completed",
			derange: func(t *testing.T, ws *workspace) {
				ws.writeDocs(map[string]string{"api.md": "line one\nRESOLVED\n"})
				ws.git("add", "docs/api.md")
				ws.git("commit", "-m", "docs: resolve the conflict")
			},
			want: "is not completed — ",
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
