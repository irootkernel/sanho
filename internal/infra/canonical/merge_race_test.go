package canonical

// F-C2: the §5.4 merge uses FIXED ref names, and fixed names are only
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
