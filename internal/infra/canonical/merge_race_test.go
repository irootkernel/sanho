package canonical

// F-C2: the merge contract merge uses FIXED ref names, and fixed names are only
// safe under a lock.
//
// The reviewer's repro was two merges in one ref store at the same time
// — the pre-commit freshness preview merging in the private clone while
// a `git push` publishes through it, or two linked worktrees sharing one
// common ref store. `refs/sanho-ours` is a common ref, so the second
// writer overwrote the first's input between its update-ref and its
// merge-tree, and the first published a tree built from somebody else's
// content, with exit 0.

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// TestMergeTreeIsSerializedWithinOneRepository runs two merges with
// distinguishable inputs concurrently, many times over, and requires
// each to get back its OWN result.
//
// Without the flock this fails within a handful of iterations: the
// losing goroutine's result tree carries the other's content, which is
// exactly the "wrong tree published, exit 0" outcome.
func TestMergeTreeIsSerializedWithinOneRepository(t *testing.T) {
	const iterations = 20

	factory := newTreeFactory(t)
	ctx := context.Background()

	base := factory.tree(t, map[string]entry{"a.md": text("base\n")})
	// Two disjoint edits, so each merge has exactly one right answer and
	// the two answers are different trees.
	oursA := factory.tree(t, map[string]entry{"a.md": text("base\n"), "alpha.md": text("alpha\n")})
	theirsA := factory.tree(t, map[string]entry{"a.md": text("base\n"), "one.md": text("one\n")})
	oursB := factory.tree(t, map[string]entry{"a.md": text("base\n"), "bravo.md": text("bravo\n")})
	theirsB := factory.tree(t, map[string]entry{"a.md": text("base\n"), "two.md": text("two\n")})

	// The expected results, computed one at a time with nothing racing.
	wantA := mustMerge(t, ctx, factory.dir, base, oursA, theirsA)
	wantB := mustMerge(t, ctx, factory.dir, base, oursB, theirsB)
	if wantA == wantB {
		t.Fatal("the two merges must produce distinguishable trees for this test to mean anything")
	}

	for i := 0; i < iterations; i++ {
		var wg sync.WaitGroup
		var gotA, gotB string
		var errA, errB error

		wg.Add(2)
		go func() {
			defer wg.Done()
			var res MergeResult
			res, errA = MergeTree(ctx, factory.dir, base, oursA, theirsA)
			gotA = res.Tree
		}()
		go func() {
			defer wg.Done()
			var res MergeResult
			res, errB = MergeTree(ctx, factory.dir, base, oursB, theirsB)
			gotB = res.Tree
		}()
		wg.Wait()

		if errA != nil || errB != nil {
			t.Fatalf("iteration %d: merges failed: A=%v B=%v", i, errA, errB)
		}
		if gotA != wantA {
			t.Fatalf("iteration %d: merge A produced %s, want its own result %s (it read the other merge's refs)",
				i, gotA, wantA)
		}
		if gotB != wantB {
			t.Fatalf("iteration %d: merge B produced %s, want its own result %s (it read the other merge's refs)",
				i, gotB, wantB)
		}
	}
}

// TestMergeTreeRecoversFromStaleRefs: a crashed or killed merge leaves
// refs/sanho-ours behind. Refusing on it would wedge the workspace
// permanently — every later merge, sync and push would fail — so the
// first thing done under the lock is to clear the leftovers.
// TestMergeTreeIsSerializedAcrossLinkedWorktrees is the same race, in
// the topology it was actually reported from.
//
// The previous test proves the lock works for two callers naming the
// SAME directory. A linked worktree is a different directory with a
// different `--git-dir` — and `refs/sanho-ours` is a common ref, shared
// by every worktree of the repository, so two merges run from two
// worktrees contend on exactly one pair of ref names. The lock therefore
// has to resolve to `--git-common-dir`, not `--git-dir`, and this is the
// test that can tell those two apart: with a per-worktree lock both
// callers acquire instantly, both write `refs/sanho-ours`, and one
// merges the other's content.
//
// It uses real `git worktree add` rather than a simulation, because the
// thing under test is git's own ref-namespace behavior.
func TestMergeTreeIsSerializedAcrossLinkedWorktrees(t *testing.T) {
	const iterations = 20

	factory := newTreeFactory(t)
	ctx := context.Background()

	// A commit is required before `git worktree add` will run.
	gitRun(t, factory.dir, "commit", "--quiet", "--allow-empty", "-m", "seed")
	linked := filepath.Join(t.TempDir(), "linked")
	gitRun(t, factory.dir, "worktree", "add", "--quiet", "--detach", linked)
	t.Cleanup(func() { gitRun(t, factory.dir, "worktree", "remove", "--force", linked) })

	base := factory.tree(t, map[string]entry{"a.md": text("base\n")})
	oursA := factory.tree(t, map[string]entry{"a.md": text("base\n"), "alpha.md": text("alpha\n")})
	theirsA := factory.tree(t, map[string]entry{"a.md": text("base\n"), "one.md": text("one\n")})
	oursB := factory.tree(t, map[string]entry{"a.md": text("base\n"), "bravo.md": text("bravo\n")})
	theirsB := factory.tree(t, map[string]entry{"a.md": text("base\n"), "two.md": text("two\n")})

	wantA := mustMerge(t, ctx, factory.dir, base, oursA, theirsA)
	wantB := mustMerge(t, ctx, linked, base, oursB, theirsB)
	if wantA == wantB {
		t.Fatal("the two merges must produce distinguishable trees for this test to mean anything")
	}

	for i := 0; i < iterations; i++ {
		var wg sync.WaitGroup
		var gotA, gotB string
		var errA, errB error

		wg.Add(2)
		go func() {
			defer wg.Done()
			var res MergeResult
			res, errA = MergeTree(ctx, factory.dir, base, oursA, theirsA)
			gotA = res.Tree
		}()
		go func() {
			defer wg.Done()
			var res MergeResult
			res, errB = MergeTree(ctx, linked, base, oursB, theirsB)
			gotB = res.Tree
		}()
		wg.Wait()

		for _, check := range []struct {
			label     string
			err       error
			got, want string
		}{
			{"main worktree", errA, gotA, wantA},
			{"linked worktree", errB, gotB, wantB},
		} {
			if check.err != nil {
				// A lock timeout is a legitimate outcome under contention
				// and is reported as such; a merge that RAN and produced
				// the other side's tree is the failure this test exists
				// for.
				if strings.Contains(check.err.Error(), "another sanho process holds") {
					continue
				}
				t.Fatalf("iteration %d: %s: MergeTree: %v", i, check.label, check.err)
			}
			if check.got != check.want {
				t.Fatalf("iteration %d: %s merged into %s, want %s — two worktrees shared one ref store "+
					"and one merge read the other's inputs", i, check.label, check.got, check.want)
			}
		}
	}
}

func TestMergeTreeRecoversFromStaleRefs(t *testing.T) {
	factory := newTreeFactory(t)
	ctx := context.Background()

	base := factory.tree(t, map[string]entry{"a.md": text("base\n")})
	ours := factory.tree(t, map[string]entry{"a.md": text("base\n"), "ours.md": text("ours\n")})
	theirs := factory.tree(t, map[string]entry{"a.md": text("base\n"), "theirs.md": text("theirs\n")})
	want := mustMerge(t, ctx, factory.dir, base, ours, theirs)

	// Leave both temp refs pointing at unrelated content, exactly as an
	// interrupted merge would.
	stale := factory.tree(t, map[string]entry{"stale.md": text("stale\n")})
	staleCommit := gitLine(t, factory.dir, "commit-tree", stale, "-m", "stale")
	gitRun(t, factory.dir, "update-ref", refOurs, staleCommit)
	gitRun(t, factory.dir, "update-ref", refUpstream, staleCommit)

	got, err := MergeTree(ctx, factory.dir, base, ours, theirs)
	if err != nil {
		t.Fatalf("MergeTree over stale refs: %v", err)
	}
	if got.Tree != want {
		t.Fatalf("result = %s, want %s: the stale refs were reused instead of cleared", got.Tree, want)
	}

	// And it cleaned up after itself.
	for _, ref := range []string{refOurs, refUpstream} {
		if out := gitRun(t, factory.dir, "for-each-ref", "--format=%(refname)", ref); strings.TrimSpace(out) != "" {
			t.Errorf("%s survived the merge", ref)
		}
	}
}

func mustMerge(t *testing.T, ctx context.Context, dir, base, ours, theirs string) string {
	t.Helper()
	res, err := MergeTree(ctx, dir, base, ours, theirs)
	if err != nil {
		t.Fatalf("MergeTree: %v", err)
	}
	if !res.Clean {
		t.Fatalf("MergeTree reported conflicts %v; this fixture must merge cleanly", res.Conflicts)
	}
	return res.Tree
}
