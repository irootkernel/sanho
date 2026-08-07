package docsync_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/irootkernel/sanho/internal/domain/provenance"
	"github.com/irootkernel/sanho/internal/infra/canonical"
	"github.com/irootkernel/sanho/internal/usecase/docsync"
)

// TestSyncUpToDate: the base is canonical head and the docs already
// carry its tree, so sync reports and does nothing — in particular it
// creates no commit (§5.5 step 3).
func TestSyncUpToDate(t *testing.T) {
	files := map[string]string{"a.md": "alpha\n"}
	f := newFlow(t, files, files)
	base := f.adoptCanonicalHeadAsBase(t)
	before := f.head(t)

	result := f.sync(t, docsync.Options{})

	if result.Status != docsync.StatusUpToDate {
		t.Fatalf("status = %v, want up to date", result.Status)
	}
	if head := f.head(t); head != before {
		t.Fatalf("an up-to-date sync created a commit (%s -> %s)", before, head)
	}
	if got := f.base(t); got != base {
		t.Fatalf("base = %+v, want it left at %+v", got, base)
	}
	if status := f.status(t); status != "" {
		t.Fatalf("an up-to-date sync dirtied the workspace:\n%s", status)
	}
}

// TestSyncWithLocalCommitsAndUnmovedCanonical is the "sync before
// push" habit: the user has committed docs edits and canonical has not
// moved, so the merge re-adopts their own tree. Nothing needs doing and
// nothing is done — in particular no empty commit is attempted.
func TestSyncWithLocalCommitsAndUnmovedCanonical(t *testing.T) {
	f := newFlow(t,
		map[string]string{"a.md": "alpha\n"},
		map[string]string{"a.md": "alpha\n"},
	)
	base := f.adoptCanonicalHeadAsBase(t)

	f.writeDocs(t, map[string]string{"a.md": "alpha local\n"})
	before := f.commitAll(t, "docs: local edit")

	result := f.sync(t, docsync.Options{})

	if result.Status != docsync.StatusUpToDate {
		t.Fatalf("status = %v, want up to date", result.Status)
	}
	requireDocs(t, f, map[string]string{"a.md": "alpha local\n"})
	if head := f.head(t); head != before {
		t.Fatalf("sync committed (%s -> %s)", before, head)
	}
	if got := f.base(t); got != base {
		t.Fatalf("base = %+v, want it left at %+v", got, base)
	}
	if status := f.status(t); status != "" {
		t.Fatalf("sync dirtied the workspace:\n%s", status)
	}
}

// TestSyncAdoptsUpstreamWhenLocalDocsAreUnchanged is the everyday case:
// upstream moved, nothing local did, so the merge result is upstream's
// tree. It pins the whole §5.5 step 5 contract — docs updated, base
// file rewritten, exactly one commit with the exact subject, touching
// only docs — while the user's staged non-docs work stays staged.
func TestSyncAdoptsUpstreamWhenLocalDocsAreUnchanged(t *testing.T) {
	f := newFlow(t,
		map[string]string{"a.md": "alpha\n"},
		map[string]string{"a.md": "alpha\n"},
	)
	f.adoptCanonicalHeadAsBase(t)

	f.upstream(t, map[string]string{"a.md": "alpha upstream\n", "b.md": "beta\n"})
	target, targetTree := f.canonicalHead(t)

	// The user is in the middle of their own work.
	writeFile(t, f.appDir, "src/app.go", "package main // work in progress\n")
	gitRun(t, f.appDir, "add", "--", "src/app.go")
	stagedBefore := gitLine(t, f.appDir, "rev-parse", ":src/app.go")
	before := f.head(t)

	result := f.sync(t, docsync.Options{})

	if result.Status != docsync.StatusSynced {
		t.Fatalf("status = %v, want synced", result.Status)
	}
	requireDocs(t, f, map[string]string{"a.md": "alpha upstream\n", "b.md": "beta\n"})

	want := provenance.Base{Commit: target, Tree: targetTree}
	if got := f.base(t); got != want {
		t.Fatalf("base file = %+v, want %+v", got, want)
	}
	if result.NewBase != want {
		t.Fatalf("Result.NewBase = %+v, want %+v", result.NewBase, want)
	}

	if n := f.commitsSince(t, before); n != 1 {
		t.Fatalf("sync created %d commits, want exactly 1", n)
	}
	head := f.head(t)
	if result.CommitOID != head {
		t.Fatalf("Result.CommitOID = %s, want HEAD %s", result.CommitOID, head)
	}
	if subject := gitLine(t, f.appDir, "log", "-1", "--format=%s", head); subject != "docs: sync to "+target[:12] {
		t.Fatalf("subject = %q, want %q", subject, "docs: sync to "+target[:12])
	}
	for _, path := range f.changedPaths(t, head) {
		if !strings.HasPrefix(path, docsDir+"/") {
			t.Fatalf("the sync commit touched %s", path)
		}
	}

	// The user's staged work is untouched and still uncommitted.
	if status := f.status(t); status != "M  src/app.go" {
		t.Fatalf("status after sync = %q, want the staged file alone", status)
	}
	if got := gitLine(t, f.appDir, "rev-parse", ":src/app.go"); got != stagedBefore {
		t.Fatalf("staged src/app.go became %s, want %s", got, stagedBefore)
	}
}

// TestSyncThreeWayMerge: local docs edits are committed and upstream
// changed a different file. The merge combines both, and the sync
// commit carries only the upstream-side delta — the local edits are
// already in HEAD, which is exactly the diff-hygiene property D3 keeps
// from v0.1's [SANHO] commits.
func TestSyncThreeWayMerge(t *testing.T) {
	f := newFlow(t,
		map[string]string{"a.md": "alpha\n", "b.md": "beta\n"},
		map[string]string{"a.md": "alpha\n", "b.md": "beta\n"},
	)
	f.adoptCanonicalHeadAsBase(t)

	f.upstream(t, map[string]string{"a.md": "alpha\n", "b.md": "beta upstream\n"})
	target, _ := f.canonicalHead(t)

	f.writeDocs(t, map[string]string{"a.md": "alpha local\n", "b.md": "beta\n"})
	before := f.commitAll(t, "docs: edit alpha locally")

	result := f.sync(t, docsync.Options{})

	if result.Status != docsync.StatusSynced {
		t.Fatalf("status = %v, want synced", result.Status)
	}
	requireDocs(t, f, map[string]string{"a.md": "alpha local\n", "b.md": "beta upstream\n"})

	if n := f.commitsSince(t, before); n != 1 {
		t.Fatalf("sync created %d commits, want exactly 1", n)
	}
	changed := f.changedPaths(t, f.head(t))
	if strings.Join(changed, ",") != docsDir+"/b.md" {
		t.Fatalf("the sync commit changed %v, want only the upstream-side file", changed)
	}
	if got := f.base(t).Commit; got != target {
		t.Fatalf("base commit = %s, want the canonical head %s", got, target)
	}
}

// TestSyncConflictThenResolve walks §5.5 step 6 and the completion that
// follows it: markers land in the worktree with ref labels, the note
// records where the sync came from, where it is going and what it could
// not settle, **the base does not move**, nothing is committed, and no
// error is returned. Then the user resolves the standard git way and
// says so with `--continue`, which is the moment — and the only moment —
// the note clears and the base adopts the target.
func TestSyncConflictThenResolve(t *testing.T) {
	f := newFlow(t,
		map[string]string{"a.md": hunkFile("A", "B", "C"), "b.md": hunkFile("A", "B", "C")},
		map[string]string{"a.md": hunkFile("A", "B", "C"), "b.md": hunkFile("A", "B", "C")},
	)
	previous := f.adoptCanonicalHeadAsBase(t)

	// Two conflicting hunks in a.md and one in b.md.
	f.upstream(t, map[string]string{
		"a.md": hunkFile("A-upstream", "B-upstream", "C"),
		"b.md": hunkFile("A-upstream", "B", "C"),
	})
	target, targetTree := f.canonicalHead(t)

	f.writeDocs(t, map[string]string{
		"a.md": hunkFile("A-local", "B-local", "C"),
		"b.md": hunkFile("A-local", "B", "C"),
	})
	before := f.commitAll(t, "docs: local edits")

	result := f.sync(t, docsync.Options{})

	if result.Status != docsync.StatusConflicts {
		t.Fatalf("status = %v, want conflicts", result.Status)
	}
	if want := docsDir + "/a.md," + docsDir + "/b.md"; strings.Join(result.Conflicts, ",") != want {
		t.Fatalf("conflicts = %v, want %s", result.Conflicts, want)
	}

	conflicted := f.docsSnapshot(t)["a.md"]
	if n := countMarkerStarts(conflicted); n != 2 {
		t.Fatalf("a.md has %d conflict regions, want 2:\n%s", n, conflicted)
	}
	if !strings.Contains(conflicted, "<<<<<<< sanho-ours") || !strings.Contains(conflicted, ">>>>>>> sanho-upstream") {
		t.Fatalf("markers are not labeled by ref:\n%s", conflicted)
	}

	note, ok := f.note(t)
	if !ok {
		t.Fatal("a conflicted sync left no note")
	}
	if note.PrevBase != previous {
		t.Fatalf("note prev_base = %+v, want %+v", note.PrevBase, previous)
	}
	wantTarget := provenance.Base{Commit: target, Tree: targetTree}
	if note.Target != wantTarget {
		t.Fatalf("note target = %+v, want %+v", note.Target, wantTarget)
	}
	// The window's whole safety property: while the resolution is owed,
	// the base still answers for the docs the worktree derives from,
	// which are the PRE-merge ones. A base sitting on the merge target
	// here is the state that let a lost note become a fast-forward over
	// upstream's work.
	if got := f.base(t); got != previous {
		t.Fatalf("base file during the conflict window = %+v, want the previous base %+v", got, previous)
	}
	if head := f.head(t); head != before {
		t.Fatalf("a conflicted sync committed (%s -> %s)", before, head)
	}

	// The note records where the workspace stood, so that "was this
	// resolved?" can be answered by a commit having happened rather than
	// by the worktree merely looking tidy — and which paths that commit
	// has to touch for it to count.
	if note.EntryHead != before {
		t.Fatalf("note entry_head = %s, want the pre-sync HEAD %s", note.EntryHead, before)
	}
	if want := []string{docsDir + "/a.md", docsDir + "/b.md"}; strings.Join(note.Conflicts, ",") != strings.Join(want, ",") {
		t.Fatalf("note conflicts = %v, want %v", note.Conflicts, want)
	}

	// Not finished yet: markers are still there, and --continue says so
	// rather than recording a base for a file full of markers.
	if got, err := f.use.ResolutionState(context.Background()); err != nil || got != docsync.ResolutionPending {
		t.Fatalf("ResolutionState = (%v, %v) with markers present, want (pending, nil)", got, err)
	}
	if _, err := f.use.Continue(context.Background()); !errors.Is(err, docsync.ErrMarkersRemain) {
		t.Fatalf("Continue with markers present = %v, want ErrMarkersRemain", err)
	}

	// Resolve the standard git way.
	f.writeDocs(t, map[string]string{
		"a.md": hunkFile("A-resolved", "B-resolved", "C"),
		"b.md": hunkFile("A-resolved", "B", "C"),
	})
	gitRun(t, f.appDir, "add", "--", docsDir)

	// Still not finished: resolved but uncommitted — and the base has
	// still not moved, because nothing has recorded anything.
	if got, err := f.use.ResolutionState(context.Background()); err != nil || got != docsync.ResolutionPending {
		t.Fatalf("ResolutionState = (%v, %v) before the commit, want (pending, nil)", got, err)
	}
	if _, err := f.use.Continue(context.Background()); !errors.Is(err, docsync.ErrResolutionUncommitted) {
		t.Fatalf("Continue before the commit = %v, want ErrResolutionUncommitted", err)
	}
	if got := f.base(t); got != previous {
		t.Fatalf("base file before the resolution commit = %+v, want the previous base %+v", got, previous)
	}

	gitRun(t, f.appDir, "commit", "--quiet", "-m", "docs: resolve the sync conflict")

	// The commit is a commit. It is now visible as a resolution, and it
	// finishes nothing on its own: the note stands and the base is where
	// the sync left it until the user says otherwise.
	state, err := f.use.ResolutionState(context.Background())
	if err != nil {
		t.Fatalf("ResolutionState: %v", err)
	}
	if state != docsync.ResolutionResolved {
		t.Fatalf("ResolutionState = %v after a committed resolution, want resolved", state)
	}
	if _, ok := f.note(t); !ok {
		t.Fatal("the resolution commit cleared the sync note; only --continue may")
	}
	if got := f.base(t); got != previous {
		t.Fatalf("base file after the resolution commit = %+v, want the previous base %+v", got, previous)
	}

	done, err := f.use.Continue(context.Background())
	if err != nil {
		t.Fatalf("Continue: %v", err)
	}
	if done.Base != wantTarget {
		t.Fatalf("Continue adopted %+v, want the target %+v", done.Base, wantTarget)
	}
	if _, ok := f.note(t); ok {
		t.Fatal("the sync note survived --continue")
	}
	// And the base moves exactly here: completing the sync is the moment
	// the worktree docs start deriving from the target.
	if got := f.base(t); got != wantTarget {
		t.Fatalf("base file after --continue = %+v, want the target %+v", got, wantTarget)
	}
	// P3 holds: completing created nothing.
	if n := f.commitsSince(t, before); n != 1 {
		t.Fatalf("the resolution flow created %d commits, want exactly the user's own", n)
	}
}

// TestAnUnrelatedDocsCommitLeavesTheSyncOwed is the same window seen
// from the path the re-review found: a commit that moves HEAD and the
// docs tree without touching anything the sync conflicted on.
//
// The previous completion test asked only that HEAD and its docs tree
// had moved, which any docs commit does. The note records the conflicted
// paths precisely so that the question can be the one that matters — has
// the conflict been dealt with — and an unrelated document cannot answer
// it.
func TestAnUnrelatedDocsCommitLeavesTheSyncOwed(t *testing.T) {
	f := newFlow(t,
		map[string]string{"a.md": "canonical\n"},
		map[string]string{"a.md": "canonical\n"},
	)
	previous := f.adoptCanonicalHeadAsBase(t)

	f.upstream(t, map[string]string{"a.md": "upstream\n"})
	f.writeDocs(t, map[string]string{"a.md": "local\n"})
	f.commitAll(t, "docs: local edit")

	if result := f.sync(t, docsync.Options{}); result.Status != docsync.StatusConflicts {
		t.Fatalf("status = %v, want conflicts", result.Status)
	}

	// Put the conflict aside the way a stash does — worktree and index
	// back to HEAD — then commit an entirely unrelated document.
	gitRun(t, f.appDir, "checkout", "HEAD", "--", docsDir)
	writeFile(t, f.appDir, docsDir+"/notes.md", "an unrelated note\n")
	f.commitAll(t, "docs: an unrelated note")

	got, err := f.use.ResolutionState(context.Background())
	if err != nil {
		t.Fatalf("ResolutionState: %v", err)
	}
	if got != docsync.ResolutionNotCommitted {
		t.Fatalf("ResolutionState = %v after an unrelated docs commit, want not_committed", got)
	}
	if _, ok := f.note(t); !ok {
		t.Fatal("an unrelated docs commit cleared the sync note")
	}
	if base := f.base(t); base != previous {
		t.Fatalf("base file = %+v, want the previous base %+v", base, previous)
	}
}

// TestSyncConflictThenAbort: abort puts the docs back exactly as HEAD
// had them, restores the previous base, and drops the note — and doing
// it twice reports that there is nothing to abort rather than acting
// again.
func TestSyncConflictThenAbort(t *testing.T) {
	f := newFlow(t,
		map[string]string{"a.md": hunkFile("A", "B", "C"), "keep.md": "keep\n"},
		map[string]string{"a.md": hunkFile("A", "B", "C"), "keep.md": "keep\n"},
	)
	previous := f.adoptCanonicalHeadAsBase(t)

	// Upstream conflicts in a.md, adds a file and deletes another: the
	// abort has to undo all three kinds of change.
	f.upstream(t, map[string]string{
		"a.md":   hunkFile("A-upstream", "B", "C"),
		"new.md": "upstream only\n",
	})

	f.writeDocs(t, map[string]string{
		"a.md":    hunkFile("A-local", "B", "C"),
		"keep.md": "keep\n",
	})
	before := f.commitAll(t, "docs: local edits")
	docsBefore := f.docsSnapshot(t)
	statusBefore := f.status(t)

	if result := f.sync(t, docsync.Options{}); result.Status != docsync.StatusConflicts {
		t.Fatalf("status = %v, want conflicts", result.Status)
	}
	if _, ok := f.docsSnapshot(t)["new.md"]; !ok {
		t.Fatal("the conflicted merge did not materialize the upstream-only file")
	}
	if _, ok := f.docsSnapshot(t)["keep.md"]; ok {
		t.Fatal("the conflicted merge did not apply the upstream deletion")
	}

	if _, err := f.use.Abort(context.Background()); err != nil {
		t.Fatalf("Abort: %v", err)
	}

	requireDocs(t, f, docsBefore)
	if got := f.base(t); got != previous {
		t.Fatalf("base = %+v, want the previous %+v", got, previous)
	}
	if _, ok := f.note(t); ok {
		t.Fatal("the sync note survived the abort")
	}
	if head := f.head(t); head != before {
		t.Fatalf("abort moved HEAD to %s", head)
	}
	if status := f.status(t); status != statusBefore {
		t.Fatalf("status after abort = %q, want %q", status, statusBefore)
	}

	if _, err := f.use.Abort(context.Background()); !errors.Is(err, docsync.ErrNoSyncInProgress) {
		t.Fatalf("second Abort = %v, want ErrNoSyncInProgress", err)
	}
}

// TestSyncWithoutARecordedBase covers the missing-base rule: the merge
// base is the empty tree, so disjoint additions union cleanly and the
// same path added on both sides conflicts. This is the state
// publication directs users here from, so it has to work.
func TestSyncWithoutARecordedBase(t *testing.T) {
	t.Run("disjoint additions union cleanly", func(t *testing.T) {
		f := newFlow(t,
			map[string]string{"canonical.md": "from canonical\n"},
			map[string]string{"local.md": "from the workspace\n"},
		)
		if f.hasBase(t) {
			t.Fatal("the fixture recorded a base")
		}
		target, targetTree := f.canonicalHead(t)
		before := f.head(t)

		result := f.sync(t, docsync.Options{})

		if result.Status != docsync.StatusSynced {
			t.Fatalf("status = %v, want synced", result.Status)
		}
		requireDocs(t, f, map[string]string{
			"canonical.md": "from canonical\n",
			"local.md":     "from the workspace\n",
		})
		if want := (provenance.Base{Commit: target, Tree: targetTree}); f.base(t) != want {
			t.Fatalf("base = %+v, want %+v", f.base(t), want)
		}
		if n := f.commitsSince(t, before); n != 1 {
			t.Fatalf("sync created %d commits, want exactly 1", n)
		}
	})

	t.Run("the same path added twice conflicts", func(t *testing.T) {
		f := newFlow(t,
			map[string]string{"a.md": "canonical content\n"},
			map[string]string{"a.md": "workspace content\n"},
		)
		result := f.sync(t, docsync.Options{})

		if result.Status != docsync.StatusConflicts {
			t.Fatalf("status = %v, want conflicts", result.Status)
		}
		if strings.Join(result.Conflicts, ",") != docsDir+"/a.md" {
			t.Fatalf("conflicts = %v", result.Conflicts)
		}
		content := f.docsSnapshot(t)["a.md"]
		if !strings.Contains(content, "<<<<<<< sanho-ours") {
			t.Fatalf("a.md carries no markers:\n%s", content)
		}
		if _, ok := f.note(t); !ok {
			t.Fatal("no sync note was written")
		}

		// And the abort out of that state restores "no base recorded"
		// rather than writing an unreadable empty one.
		if _, err := f.use.Abort(context.Background()); err != nil {
			t.Fatalf("Abort: %v", err)
		}
		if f.hasBase(t) {
			t.Fatalf("abort left a base file: %+v", f.base(t))
		}
	})
}

// TestSyncWithALegacyBase: a base adopted from a v0.1 docs-version
// trailer records the commit but no tree, and the tree is resolved
// through canonical rather than treated as missing.
//
// The local deletion is the discriminator: only a correct merge base
// makes "we removed b.md, upstream left it alone" resolve to a
// deletion. Merging from the empty tree — the missing-base rule — would
// read b.md as an upstream addition and bring it back.
func TestSyncWithALegacyBase(t *testing.T) {
	f := newFlow(t,
		map[string]string{"a.md": "alpha\n", "b.md": "beta\n"},
		map[string]string{"a.md": "alpha\n", "b.md": "beta\n"},
	)
	legacy, _ := f.canonicalHead(t)
	f.setBase(t, provenance.Base{Commit: legacy})

	f.writeDocs(t, map[string]string{"a.md": "alpha\n"})
	f.commitAll(t, "docs: drop beta locally")

	f.upstream(t, map[string]string{"a.md": "alpha\n", "b.md": "beta\n", "c.md": "gamma\n"})
	target, targetTree := f.canonicalHead(t)

	result := f.sync(t, docsync.Options{})

	if result.Status != docsync.StatusSynced {
		t.Fatalf("status = %v, want synced", result.Status)
	}
	requireDocs(t, f, map[string]string{"a.md": "alpha\n", "c.md": "gamma\n"})
	if want := (provenance.Base{Commit: target, Tree: targetTree}); f.base(t) != want {
		t.Fatalf("base = %+v, want %+v", f.base(t), want)
	}
}

// TestSyncAfterAHistoryRewrite covers §5.3 case ④ as sync sees it: with
// a recorded docs-base-tree the base re-anchors by content and the sync
// proceeds; without one there is nothing to anchor to and the user is
// sent to --rebase-onto.
func TestSyncAfterAHistoryRewrite(t *testing.T) {
	t.Run("re-anchors by the recorded docs tree", func(t *testing.T) {
		f := newFlow(t,
			map[string]string{"a.md": "alpha\n", "b.md": "beta\n"},
			map[string]string{"a.md": "alpha\n", "b.md": "beta\n"},
		)
		stale := f.adoptCanonicalHeadAsBase(t)

		// A local deletion, so that only a correctly re-anchored merge
		// base produces the right answer: with the anchor's tree b.md is
		// ours-deleted and stays gone, without it b.md reads as an
		// upstream addition and comes back.
		f.writeDocs(t, map[string]string{"a.md": "alpha\n"})
		f.commitAll(t, "docs: drop beta locally")

		// A squash-like rewrite: same content, brand-new OIDs, plus one
		// commit on top.
		f.rewriteCanonical(t,
			map[string]string{"a.md": "alpha\n", "b.md": "beta\n"},
			map[string]string{"a.md": "alpha\n", "b.md": "beta\n", "c.md": "gamma\n"},
		)
		target, targetTree := f.canonicalHead(t)

		result := f.sync(t, docsync.Options{})

		if result.Status != docsync.StatusSynced {
			t.Fatalf("status = %v, want synced", result.Status)
		}
		requireDocs(t, f, map[string]string{"a.md": "alpha\n", "c.md": "gamma\n"})
		if want := (provenance.Base{Commit: target, Tree: targetTree}); f.base(t) != want {
			t.Fatalf("base = %+v, want %+v", f.base(t), want)
		}
		if f.base(t).Commit == stale.Commit {
			t.Fatal("the base never moved off the rewritten-away commit")
		}
	})

	t.Run("refuses when nothing carries the recorded tree", func(t *testing.T) {
		f := newFlow(t,
			map[string]string{"a.md": "alpha\n"},
			map[string]string{"a.md": "alpha\n"},
		)
		stale := f.adoptCanonicalHeadAsBase(t)
		f.rewriteCanonical(t, map[string]string{"a.md": "something else entirely\n"}, nil)
		before := f.head(t)

		err := f.syncErr(t, docsync.Options{})
		if !errors.Is(err, docsync.ErrUnknownBase) {
			t.Fatalf("error = %v, want ErrUnknownBase", err)
		}
		// Command-free: `sync --rebase-onto <commit>` is named by the CLI
		// catalog entry, which a closure fixture runs (F-H6).
		if strings.Contains(err.Error(), "sanho ") {
			t.Errorf("message = %q, want a command-free sentinel", err)
		}
		if got := f.base(t); got != stale {
			t.Fatalf("a refused sync moved the base to %+v", got)
		}
		if head := f.head(t); head != before {
			t.Fatalf("a refused sync committed")
		}
	})

	t.Run("refuses when the base records no tree to anchor by", func(t *testing.T) {
		f := newFlow(t,
			map[string]string{"a.md": "alpha\n"},
			map[string]string{"a.md": "alpha\n"},
		)
		stale, _ := f.canonicalHead(t)
		f.setBase(t, provenance.Base{Commit: stale}) // legacy shape
		f.rewriteCanonical(t, map[string]string{"a.md": "something else entirely\n"}, nil)

		if err := f.syncErr(t, docsync.Options{}); !errors.Is(err, docsync.ErrUnknownBase) {
			t.Fatalf("error = %v, want ErrUnknownBase", err)
		}
	})
}

// TestSyncRebaseOnto aims the merge at an older canonical commit, which
// is the manual escape from a rewrite (§5.5 step 8).
func TestSyncRebaseOnto(t *testing.T) {
	f := newFlow(t,
		map[string]string{"a.md": "alpha\n"},
		map[string]string{"a.md": "alpha\n"},
	)
	f.adoptCanonicalHeadAsBase(t)

	middle := f.upstream(t, map[string]string{"a.md": "alpha\n", "b.md": "beta\n"})
	f.upstream(t, map[string]string{"a.md": "alpha\n", "b.md": "beta\n", "c.md": "gamma\n"})
	head, _ := f.canonicalHead(t)
	if middle == head {
		t.Fatal("the fixture is wrong: the target is canonical head")
	}

	result := f.sync(t, docsync.Options{RebaseOnto: middle})

	if result.Status != docsync.StatusSynced {
		t.Fatalf("status = %v, want synced", result.Status)
	}
	requireDocs(t, f, map[string]string{"a.md": "alpha\n", "b.md": "beta\n"})
	if got := f.base(t).Commit; got != middle {
		t.Fatalf("base commit = %s, want the requested target %s", got, middle)
	}
	if subject := gitLine(t, f.appDir, "log", "-1", "--format=%s", "HEAD"); subject != "docs: sync to "+middle[:12] {
		t.Fatalf("subject = %q, want the target's short OID", subject)
	}

	t.Run("an unknown target is refused", func(t *testing.T) {
		unknown := strings.Repeat("0", 39) + "1"
		if err := f.syncErr(t, docsync.Options{RebaseOnto: unknown}); !errors.Is(err, docsync.ErrUnknownTarget) {
			t.Fatalf("error = %v, want ErrUnknownTarget", err)
		}
	})
}

// TestRebaseOntoRevivesDocsCanonicalDeleted runs the documented cost of
// the empty-tree fallback, rather than only asserting that the fallback
// happens.
//
// When the recorded base cannot be anchored anywhere in canonical
// history and the user names an explicit target, the merge base is the
// empty tree — the honest ancestor when the two histories share nothing.
// A merge on an empty base is the union of both sides, and a union has
// no way to tell "upstream deleted this" from "we added it". So a
// document canonical deleted, still present locally, comes back.
//
// docs/architecture.md states that plainly and tells the reader to
// delete and commit if the revival is unwanted. This test is what keeps
// the statement true: the behavior is a consequence of a deliberate
// choice, and a silent change to it would be a silent change to what the
// documentation promises.
func TestRebaseOntoRevivesDocsCanonicalDeleted(t *testing.T) {
	f := newFlow(t,
		map[string]string{"keep.md": "keep\n", "removed.md": "upstream removed this\n"},
		map[string]string{"keep.md": "keep\n", "removed.md": "upstream removed this\n"},
	)
	f.adoptCanonicalHeadAsBase(t)

	// Canonical is replaced wholesale, and the replacement no longer
	// carries removed.md. Nothing in the new history holds the recorded
	// docs tree, so there is nothing to re-anchor by.
	f.rewriteCanonical(t, map[string]string{"keep.md": "keep\n", "fresh.md": "a new document\n"}, nil)
	target, _ := f.canonicalHead(t)

	// Without a target the sync refuses, and its guidance names exactly
	// the command run below (D3).
	if err := f.syncErr(t, docsync.Options{}); !errors.Is(err, docsync.ErrUnknownBase) {
		t.Fatalf("error = %v, want ErrUnknownBase", err)
	}

	result := f.sync(t, docsync.Options{RebaseOnto: target})
	if result.Status != docsync.StatusSynced {
		t.Fatalf("status = %v, want synced", result.Status)
	}

	// The union: canonical's new document arrives, the local copy of the
	// deleted one survives, and nothing was lost in either direction.
	requireDocs(t, f, map[string]string{
		"keep.md":    "keep\n",
		"fresh.md":   "a new document\n",
		"removed.md": "upstream removed this\n",
	})
	if got := f.base(t).Commit; got != target {
		t.Fatalf("base commit = %s, want the requested target %s", got, target)
	}

	// And the documented way to finish the job: delete it and commit.
	f.writeDocs(t, map[string]string{"keep.md": "keep\n", "fresh.md": "a new document\n"})
	f.commitAll(t, "docs: drop the document canonical removed")
	requireDocs(t, f, map[string]string{"keep.md": "keep\n", "fresh.md": "a new document\n"})
}

// TestSyncAppliesUpstreamDeletions: a file upstream deleted has to
// leave the worktree *and* the index, so the sync commit records the
// deletion rather than resurrecting the file.
func TestSyncAppliesUpstreamDeletions(t *testing.T) {
	f := newFlow(t,
		map[string]string{"a.md": "alpha\n", "obsolete.md": "obsolete\n"},
		map[string]string{"a.md": "alpha\n", "obsolete.md": "obsolete\n"},
	)
	f.adoptCanonicalHeadAsBase(t)
	f.upstream(t, map[string]string{"a.md": "alpha\n"})
	before := f.head(t)

	result := f.sync(t, docsync.Options{})

	if result.Status != docsync.StatusSynced {
		t.Fatalf("status = %v, want synced", result.Status)
	}
	requireDocs(t, f, map[string]string{"a.md": "alpha\n"})
	if indexed := gitRun(t, f.appDir, "ls-files", "--", docsDir); strings.Contains(indexed, "obsolete.md") {
		t.Fatalf("the deleted file is still in the index:\n%s", indexed)
	}
	if n := f.commitsSince(t, before); n != 1 {
		t.Fatalf("sync created %d commits, want exactly 1", n)
	}
	if changed := f.changedPaths(t, f.head(t)); strings.Join(changed, ",") != docsDir+"/obsolete.md" {
		t.Fatalf("the sync commit changed %v, want the deletion alone", changed)
	}
	if status := f.status(t); status != "" {
		t.Fatalf("the workspace is dirty after sync:\n%s", status)
	}
}

// TestSyncRefusesDirtyDocs pins the §5.5 step 1 guard on both halves of
// "dirty", and pins that a refused sync is inert.
func TestSyncRefusesDirtyDocs(t *testing.T) {
	tests := []struct {
		name  string
		dirty func(t *testing.T, f *flow)
	}{
		{
			name: "unstaged docs edit",
			dirty: func(t *testing.T, f *flow) {
				writeFile(t, f.appDir, docsDir+"/a.md", "alpha edited\n")
			},
		},
		{
			name: "staged docs edit",
			dirty: func(t *testing.T, f *flow) {
				writeFile(t, f.appDir, docsDir+"/a.md", "alpha edited\n")
				gitRun(t, f.appDir, "add", "--", docsDir+"/a.md")
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newFlow(t,
				map[string]string{"a.md": "alpha\n"},
				map[string]string{"a.md": "alpha\n"},
			)
			base := f.adoptCanonicalHeadAsBase(t)
			f.upstream(t, map[string]string{"a.md": "alpha upstream\n"})

			test.dirty(t, f)
			docsBefore := f.docsSnapshot(t)
			statusBefore := f.status(t)
			before := f.head(t)

			err := f.syncErr(t, docsync.Options{})
			if !errors.Is(err, docsync.ErrDocsDirty) {
				t.Fatalf("error = %v, want ErrDocsDirty", err)
			}
			requireDocs(t, f, docsBefore)
			if got := f.base(t); got != base {
				t.Fatalf("a refused sync moved the base to %+v", got)
			}
			if head := f.head(t); head != before {
				t.Fatalf("a refused sync committed")
			}
			if status := f.status(t); status != statusBefore {
				t.Fatalf("status = %q, want %q", status, statusBefore)
			}
			if _, ok := f.note(t); ok {
				t.Fatal("a refused sync wrote a note")
			}
		})
	}
}

// TestSyncFailsClosedWhenCanonicalIsUnreachable: write paths require a
// successful fetch (§5.2), and a failed one must leave the workspace
// exactly as it was.
func TestSyncFailsClosedWhenCanonicalIsUnreachable(t *testing.T) {
	f := newFlow(t,
		map[string]string{"a.md": "alpha\n"},
		map[string]string{"a.md": "alpha\n"},
	)
	base := f.adoptCanonicalHeadAsBase(t)
	docsBefore := f.docsSnapshot(t)
	before := f.head(t)
	f.breakOrigin(t)

	err := f.syncErr(t, docsync.Options{})
	if !errors.Is(err, canonical.ErrUnreachable) {
		t.Fatalf("error = %v, want it to wrap canonical.ErrUnreachable", err)
	}
	requireDocs(t, f, docsBefore)
	if got := f.base(t); got != base {
		t.Fatalf("base = %+v, want it untouched at %+v", got, base)
	}
	if head := f.head(t); head != before {
		t.Fatalf("an offline sync committed")
	}
	if _, ok := f.note(t); ok {
		t.Fatal("an offline sync wrote a note")
	}
}
