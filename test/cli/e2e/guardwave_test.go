package e2e

// The fourth review wave's regressions: the two remaining ways a base
// could end up ahead of the docs the worktree carries, and the push that
// then deleted upstream's work at exit 0.
//
// Both were reproduced against the previous binary before anything here
// was written, and both are black-box for the same reason every earlier
// wave was: the failure produced a plausible success message and showed
// up only in what canonical afterwards contained.

import (
	"reflect"
	"strings"
	"testing"
)

// --- C1: `--continue` may not complete a sync from foreign history ------

// TestContinueRefusesFromAForeignBranch is C1's reproduction.
//
// A conflicted sync on `main`, escaped with `git stash push -- docs`,
// then `git checkout other` — a branch whose docs never took part in the
// merge. The previous binary completed the sync there ("sync completed;
// docs base is now <canonical head>"), which left `other` holding a base
// equal to canonical head over pre-merge documents. The next push on
// that branch was decided as a fast-forward and republished the
// pre-merge tree over upstream's, deleting a document canonical had.
func TestContinueRefusesFromAForeignBranch(t *testing.T) {
	t.Parallel()

	w := newWorld(t, map[string]string{"api.md": "line one\nline two\n", "old.md": "ancient\n"})
	ws := w.setup("foreign-continue")

	// `other` is cut BEFORE the docs edit the sync is about, so its
	// documents took no part in the merge and it descends from nothing
	// the sync touched.
	ws.git("branch", "other")
	ws.commitDocs("docs: my edit", map[string]string{"api.md": "line one\nMINE\n"})
	// Canonical edits the same line AND adds a document of its own — the
	// one a fast-forward from `other` would delete.
	w.advanceCanonical(
		map[string]string{"api.md": "line one\nTHEIRS\n", "old.md": "ancient\n", "new.md": "upstream addition\n"},
		"canonical: their edit and an addition")

	requireContains(t, "sync", ws.sanho("sync").combined(), "have conflicts")
	baseDuringWindow := recordedBase(t, ws)

	// Escape the markers and move to the branch that never participated.
	ws.git("stash", "push", "--quiet", "--", "docs")
	ws.git("checkout", "--quiet", "other")

	completed := ws.run("sync", "--continue")
	requireExit(t, "sync --continue on a branch the sync never touched", completed, 1)
	requireContains(t, "refusal", completed.combined(), "this sync cannot be completed here")
	requireContains(t, "refusal", completed.combined(), "sanho sync --abort")
	requireNotContains(t, "refusal", completed.combined(), "sync completed")

	// The base did not move, so the sync is still owed and the note is
	// still there.
	requireEqual(t, "base file", recordedBase(t, ws), baseDuringWindow)
	if !syncNoteExists(t, ws) {
		t.Fatal("a refused --continue deleted the sync note")
	}

	// And the push that used to publish the pre-merge tree is refused
	// with canonical untouched.
	before := w.canonicalHead()
	writeFile(t, ws.path("src", "main.go"), "package main\n")
	ws.git("add", "-A")
	ws.gitExit("commit", "-m", "feat: unrelated code")

	push := ws.gitExit("push", "--quiet", "origin", "other")
	requireExit(t, "push from the foreign branch", push, 1)
	requireEqual(t, "canonical head", w.canonicalHead(), before)

	want := []string{"api.md", "new.md", "old.md"}
	if got := w.canonicalPaths(w.canonicalHead()); !reflect.DeepEqual(got, want) {
		t.Fatalf("canonical holds %v, want %v — the push deleted an upstream document", got, want)
	}
	requireEqual(t, "canonical api.md",
		w.canonicalFile(w.canonicalHead(), "api.md"), "line one\nTHEIRS\n")
}

// TestContinueStillCompletesOnTheSyncsOwnHistory is the other half of
// C1's fix, and the one the auditor warned about explicitly: the
// precondition is *ancestry*, not identity, so a resolution that made
// commits — or none at all — completes exactly as before.
func TestContinueStillCompletesOnTheSyncsOwnHistory(t *testing.T) {
	t.Parallel()

	t.Run("head_unmoved", func(t *testing.T) {
		t.Parallel()

		w := newWorld(t, defaultCanonicalDocs())
		ws := conflictedSync(t, w)
		// The take-ours-wholesale exit the closure suite depends on.
		ws.git("stash", "push", "--quiet", "--", "docs")

		out := ws.sanho("sync", "--continue")
		requireContains(t, "completion", out.combined(), "sync completed")
		requireEqual(t, "base file", recordedBase(t, ws), w.canonicalHead())
	})

	t.Run("head_moved_forward", func(t *testing.T) {
		t.Parallel()

		w := newWorld(t, defaultCanonicalDocs())
		ws := conflictedSync(t, w)

		ws.writeDocs(map[string]string{"api.md": "line one\nRESOLVED\n"})
		ws.git("add", "docs")
		ws.git("commit", "-m", "docs: resolve the conflict")
		// An unrelated commit on top: HEAD is further along the same
		// history, which is still the sync's own history.
		writeFile(t, ws.path("src", "main.go"), "package main\n")
		ws.git("add", "-A")
		ws.git("commit", "-m", "feat: unrelated code")

		out := ws.sanho("sync", "--continue")
		requireContains(t, "completion", out.combined(), "sync completed")
		requireEqual(t, "base file", recordedBase(t, ws), w.canonicalHead())
	})

	t.Run("branch_created_from_the_syncs_head", func(t *testing.T) {
		t.Parallel()

		w := newWorld(t, defaultCanonicalDocs())
		ws := conflictedSync(t, w)

		ws.writeDocs(map[string]string{"api.md": "line one\nRESOLVED\n"})
		ws.git("add", "docs")
		ws.git("commit", "-m", "docs: resolve the conflict")
		// A branch made from the resolution still descends from the entry
		// head, so the sync it completes is its own.
		ws.git("checkout", "--quiet", "-b", "resolution")

		out := ws.sanho("sync", "--continue")
		requireContains(t, "completion", out.combined(), "sync completed")
	})
}

// TestContinueReportsDriftFromTheMergeResult is W2's semantic warning:
// a worktree reverted past the clean part of the merge completes anyway,
// and says how far it drifted rather than pretending the merge result is
// what was adopted.
func TestContinueReportsDriftFromTheMergeResult(t *testing.T) {
	t.Parallel()

	w := newWorld(t, map[string]string{"api.md": "line one\nline two\n"})
	ws := w.setup("merge-drift")

	ws.commitDocs("docs: my edit", map[string]string{"api.md": "line one\nMINE\n"})
	// Canonical changes the conflicting file AND adds a clean one, so the
	// merge result carries content the user never sees after a stash.
	w.advanceCanonical(
		map[string]string{"api.md": "line one\nTHEIRS\n", "guide.md": "clean upstream addition\n"},
		"canonical: their edit plus an addition")

	requireContains(t, "sync", ws.sanho("sync").combined(), "have conflicts")
	ws.git("stash", "push", "--quiet", "--", "docs")

	out := ws.sanho("sync", "--continue")
	requireContains(t, "completion", out.combined(), "sync completed")
	requireContains(t, "drift line", out.combined(), "differ from the merge result")
}

// --- C2: an uncorroborated base may not survive a branch switch --------

// TestPreAdoptionBranchCannotDeleteCanonicalDocs is C2's reproduction,
// with the corrected fixture: a branch that HAS docs and carries no
// provenance at all.
//
// The workspace adopts sanho on `main` with `init --force`, so the base
// is canonical head. Checking out the long-lived `legacy` branch used to
// find no trailer, print nothing, and KEEP the base — leaving base ==
// canonical head over one stale document. The push was then decided as a
// fast-forward and replaced canonical's six documents with that one.
func TestPreAdoptionBranchCannotDeleteCanonicalDocs(t *testing.T) {
	t.Parallel()

	canonicalDocs := map[string]string{
		"a.md": "canonical a\n", "b.md": "canonical b\n", "c.md": "canonical c\n",
		"d.md": "canonical d\n", "e.md": "canonical e\n", "f.md": "canonical f\n",
	}
	w := newWorld(t, canonicalDocs)
	ws := w.newWorkspace("pre-adoption")

	// Docs existed before sanho did, and a long-lived branch was cut
	// then: it has documents and no provenance whatsoever.
	ws.writeDocs(map[string]string{"legacy-only.md": "pre-adoption doc\n"})
	ws.git("add", "-A")
	ws.git("commit", "-m", "chore: pre-sanho work with docs")
	ws.git("branch", "legacy")

	ws.sanho("init",
		"--project", projectName, "--docs-repo-url", w.origin, "--actor-email", actorEmail,
		"--force", "-y")
	ws.git("add", "-A")
	ws.git("commit", "-m", "docs: adopt canonical docs")

	before := w.canonicalHead()
	requireEqual(t, "base after adoption", recordedBase(t, ws), before)

	checkout := ws.gitExit("checkout", "--quiet", "legacy")
	requireExit(t, "checkout the pre-adoption branch", checkout, 0)
	requireContains(t, "checkout output", checkout.combined(),
		"docs base was cleared")

	// No base survives a checkout that cannot corroborate one.
	if fileExists(t, ws.basePath()) {
		t.Fatalf("the base file survived a checkout onto history that cannot vouch for it: %s",
			readFile(t, ws.basePath()))
	}

	// Doctor names the state rather than reporting `[ok] base`.
	doctor := ws.run("doctor")
	requireContains(t, "doctor", doctor.combined(), "no docs base is recorded")

	writeFile(t, ws.path("src", "main.go"), "package main\n")
	ws.git("add", "-A")
	ws.git("commit", "-m", "feat: work on the long-lived branch")

	push := ws.gitExit("push", "--quiet", "origin", "legacy")
	requireExit(t, "push the pre-adoption branch", push, 1)
	requireContains(t, "rejection", push.combined(), "no remote ref was changed")

	requireEqual(t, "canonical head", w.canonicalHead(), before)
	want := []string{"a.md", "b.md", "c.md", "d.md", "e.md", "f.md"}
	if got := w.canonicalPaths(w.canonicalHead()); !reflect.DeepEqual(got, want) {
		t.Fatalf("canonical holds %v, want %v — the push deleted every upstream document", got, want)
	}
}

// TestPublicationRefusesAnUncorroboratedFastForward is C2's other end:
// even with a base file the workspace still holds, publication will not
// fast-forward over canonical unless the pushed tip's own history
// vouches for that base.
//
// The base is restored behind sanho's back — the shape any future path
// that forgets to clear one would produce — and the push must still
// refuse.
func TestPublicationRefusesAnUncorroboratedFastForward(t *testing.T) {
	t.Parallel()

	w := newWorld(t, map[string]string{"api.md": "line one\n", "old.md": "ancient\n"})
	ws := w.newWorkspace("uncorroborated")

	ws.writeDocs(map[string]string{"legacy-only.md": "pre-adoption doc\n"})
	ws.git("add", "-A")
	ws.git("commit", "-m", "chore: pre-sanho work with docs")
	ws.git("branch", "legacy")

	ws.sanho("init",
		"--project", projectName, "--docs-repo-url", w.origin, "--actor-email", actorEmail,
		"--force", "-y")
	ws.git("add", "-A")
	ws.git("commit", "-m", "docs: adopt canonical docs")

	before := w.canonicalHead()
	baseFile := readFile(t, ws.basePath())

	ws.git("checkout", "--quiet", "legacy")
	// Put the base back by hand: this is the state W4's guard prevents
	// sanho itself from creating, and the state publication must survive
	// anyway.
	writeFile(t, ws.basePath(), baseFile)

	push := ws.gitExit("push", "--quiet", "origin", "legacy")
	requireExit(t, "push with a base the branch cannot vouch for", push, 1)
	requireContains(t, "rejection", push.combined(), "docs must be reconciled before publishing")

	requireEqual(t, "canonical head", w.canonicalHead(), before)
	want := []string{"api.md", "old.md"}
	if got := w.canonicalPaths(w.canonicalHead()); !reflect.DeepEqual(got, want) {
		t.Fatalf("canonical holds %v, want %v — an uncorroborated fast-forward landed", got, want)
	}
}

// TestOrdinaryFastForwardStillPublishes is the no-regression half of
// W3(b): a workspace whose own commits carry the provenance publishes
// exactly as before, including when the base has already advanced past
// the newest trailer (which is the state of every workspace that has
// just published).
func TestOrdinaryFastForwardStillPublishes(t *testing.T) {
	t.Parallel()

	w := newWorld(t, defaultCanonicalDocs())
	ws := w.setup("ordinary-ff")

	ws.commitDocs("docs: first note", map[string]string{"guide.md": "one\n"})
	first := ws.push()
	requireExit(t, "first push", first, 0)
	requireContains(t, "first push", first.combined(), "(fast_forward)")

	// The base has now advanced past the commit the trailer names; the
	// next push must still be a fast-forward.
	ws.commitDocs("docs: second note", map[string]string{"guide.md": "one\ntwo\n"})
	second := ws.push()
	requireExit(t, "second push", second, 0)
	requireContains(t, "second push", second.combined(), "(fast_forward)")
	requireEqual(t, "canonical guide.md",
		w.canonicalFile(w.canonicalHead(), "guide.md"), "one\ntwo\n")
}

// --- W4: the base writer's guard ---------------------------------------

// TestNoWriterRecordsABaseAheadOfTheWorktree drives every command that
// records a base and asserts the invariant after each one: whatever the
// base file names, the docs the worktree carries must be able to account
// for it.
//
// The check is the same one publication makes — the tip's own history
// must vouch for the recorded base, or the base must be exactly the
// worktree's docs — so a writer that produced an "ahead" value would be
// caught here whichever path it took.
func TestNoWriterRecordsABaseAheadOfTheWorktree(t *testing.T) {
	t.Parallel()

	w := newWorld(t, defaultCanonicalDocs())
	ws := w.setup("every-writer")

	steps := []struct {
		name string
		run  func()
	}{
		{"init", func() {}},
		{"sync_up_to_date", func() { ws.sanho("sync") }},
		{"publish_advance", func() {
			ws.commitDocs("docs: local note", map[string]string{"guide.md": "one\n"})
			requireExit(t, "push", ws.push(), 0)
		}},
		{"pull", func() {
			w.advanceCanonical(
				map[string]string{"api.md": "line one\nline two\n", "guide.md": "one\ntwo\n"},
				"canonical: upstream edit")
			ws.sanho("pull", "--commit")
		}},
		{"sync_merge", func() {
			ws.commitDocs("docs: another note", map[string]string{"notes.md": "mine\n"})
			w.advanceCanonical(
				map[string]string{"api.md": "line one\nline two\n", "guide.md": "one\ntwo\nthree\n"},
				"canonical: another upstream edit")
			ws.sanho("sync")
		}},
		{"sync_continue", func() {
			ws.commitDocs("docs: conflicting note", map[string]string{"api.md": "line one\nMINE\n"})
			w.advanceCanonical(
				map[string]string{"api.md": "line one\nTHEIRS\n"}, "canonical: conflicting edit")
			requireContains(t, "sync", ws.sanho("sync").combined(), "have conflicts")
			ws.writeDocs(map[string]string{"api.md": "line one\nRESOLVED\n"})
			ws.git("add", "docs")
			ws.git("commit", "-m", "docs: resolve")
			ws.sanho("sync", "--continue")
		}},
		{"doctor_fix", func() { ws.sanho("doctor", "--fix") }},
		{"post_checkout_rederivation", func() {
			ws.git("checkout", "--quiet", "-b", "side")
			ws.git("checkout", "--quiet", "main")
		}},
	}

	for _, step := range steps {
		step.run()
		requireBaseNotAheadOfWorktree(t, ws, step.name)
	}
}

// requireBaseNotAheadOfWorktree asserts the W4 invariant from outside:
// either the base file is absent, or the recorded base is one the docs
// the worktree carries can account for — its own docs tree, or a base
// the tip's history stamps.
func requireBaseNotAheadOfWorktree(t *testing.T, ws *workspace, label string) {
	t.Helper()

	if !fileExists(t, ws.basePath()) {
		return
	}
	base := recordedBase(t, ws)
	if base == "" {
		t.Fatalf("%s: the base file records no commit", label)
	}

	// The recorded base's docs tree, read from the private clone.
	inClone := func(args ...string) result {
		return execute(t, ws.cloneDir(), ws.w.env(), "git", args...)
	}
	tree := strings.TrimSpace(inClone("rev-parse", "--verify", base+"^{tree}").stdout)
	worktree := strings.TrimSpace(ws.git("rev-parse", "HEAD:docs").stdout)
	if tree != "" && tree == worktree {
		return
	}

	// Otherwise the tip's history must name it, or a commit it descends
	// from in canonical history.
	trailers := ws.git("log", "--format=%(trailers:key=docs-base,valueonly)", "HEAD").stdout
	for _, line := range strings.Split(trailers, "\n") {
		stamped := strings.TrimSpace(line)
		if stamped == "" {
			continue
		}
		if stamped == base {
			return
		}
		if inClone("merge-base", "--is-ancestor", stamped, base).exitCode == 0 {
			return
		}
		break
	}
	t.Fatalf("%s: the base file names %s, which neither the worktree docs nor the tip's provenance accounts for",
		label, base)
}
