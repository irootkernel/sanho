package docsync

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/irootkernel/sanho/internal/domain/provenance"
	pubdom "github.com/irootkernel/sanho/internal/domain/publish"
)

// TestRunGuardsRefuseBeforeTouchingAnything walks §5.5 step 1 in order.
// The order is the subject: a conflicted sync makes the docs dirty by
// construction, so the note has to be consulted first or the user is
// told to commit changes that are not theirs.
func TestRunGuardsRefuseBeforeTouchingAnything(t *testing.T) {
	tests := []struct {
		name    string
		arrange func(f *fixture)
		want    error
		// wantTrace is the exact sequence of side effects allowed before
		// the refusal.
		wantTrace string
		// wantMessage is a fragment the guidance must name.
		wantMessage string
	}{
		{
			name: "a sync in progress outranks dirty docs",
			arrange: func(f *fixture) {
				f.state.note = &SyncNote{
					PrevBase: provenance.Base{Commit: commitOID(0), Tree: treeOID(0)},
					Target:   provenance.Base{Commit: commitOID(1), Tree: treeOID(1)},
				}
				f.app.docsClean = false
			},
			want:        ErrSyncInProgress,
			wantTrace:   "",
			wantMessage: "syncing",
		},
		{
			name:        "dirty docs refuse before the network",
			arrange:     func(f *fixture) { f.app.docsClean = false },
			want:        ErrDocsDirty,
			wantTrace:   "docs-clean",
			wantMessage: "commit or stash",
		},
		{
			name:      "an unreachable canonical fails closed",
			arrange:   func(f *fixture) { f.canonical.fetchErr = pubdom.ErrUnreachable },
			want:      pubdom.ErrUnreachable,
			wantTrace: "docs-clean fetch",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newFixture()
			test.arrange(f)

			_, err := f.useCase().Run(context.Background(), Options{})
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
			if test.wantMessage != "" && !strings.Contains(err.Error(), test.wantMessage) {
				t.Errorf("message = %q, want it to name %q", err, test.wantMessage)
			}
			if got := f.shared.trace(); got != test.wantTrace {
				t.Fatalf("side effects = %q, want %q", got, test.wantTrace)
			}
			if len(f.state.savedBases) != 0 || len(f.state.savedNotes) != 0 {
				t.Fatalf("a refused sync wrote state: bases=%v notes=%v", f.state.savedBases, f.state.savedNotes)
			}
		})
	}
}

// TestRunShortCircuitsWhenUpToDate: base is canonical head and the docs
// already carry its tree, so nothing at all happens (§5.5 step 3).
func TestRunShortCircuitsWhenUpToDate(t *testing.T) {
	f := newFixture()
	f.state.base = provenance.Base{Commit: commitOID(1), Tree: treeOID(1)}
	f.app.headTree = treeOID(1)

	result, err := f.useCase().Run(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Status != StatusUpToDate {
		t.Fatalf("status = %v, want up to date", result.Status)
	}
	if want := (provenance.Base{Commit: commitOID(1), Tree: treeOID(1)}); result.NewBase != want {
		t.Fatalf("NewBase = %+v, want %+v", result.NewBase, want)
	}
	if got := f.shared.trace(); got != "docs-clean fetch import" {
		t.Fatalf("side effects = %q, want only the fetch", got)
	}
}

// TestRunResolvesTheMergeBase is the routing table of resolveBaseTree:
// three recorded states, three merge bases.
func TestRunResolvesTheMergeBase(t *testing.T) {
	tests := []struct {
		name     string
		arrange  func(f *fixture)
		wantBase string
	}{
		{
			name: "a recorded base resolves to its own tree",
			arrange: func(f *fixture) {
				f.state.base = provenance.Base{Commit: commitOID(0), Tree: treeOID(0)}
			},
			wantBase: treeOID(0),
		},
		{
			name: "a legacy base without a tree resolves through canonical",
			arrange: func(f *fixture) {
				f.state.base = provenance.Base{Commit: commitOID(0)}
			},
			wantBase: treeOID(0),
		},
		{
			name: "no base at all merges from the empty tree",
			arrange: func(f *fixture) {
				f.state.base, f.state.hasBase = provenance.Base{}, false
			},
			wantBase: emptyTreeOID,
		},
		{
			name: "an unknown base re-anchors by its docs tree",
			arrange: func(f *fixture) {
				// Known to the clone but rewritten off canonical's line
				// of history, so nothing imported it app-side.
				f.state.base = provenance.Base{Commit: commitOID(7), Tree: treeOID(0)}
				f.canonical.known[commitOID(7)] = true
			},
			wantBase: treeOID(0),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newFixture()
			test.arrange(f)

			if _, err := f.useCase().Run(context.Background(), Options{}); err != nil {
				t.Fatalf("Run: %v", err)
			}
			if len(f.app.mergeCalls) != 1 {
				t.Fatalf("merge calls = %v, want exactly one", f.app.mergeCalls)
			}
			got := f.app.mergeCalls[0]
			want := mergeCall{base: test.wantBase, ours: treeOID(0), theirs: treeOID(1)}
			if got != want {
				t.Fatalf("merge = %+v, want %+v", got, want)
			}
		})
	}
}

// TestRunRejectsAnUnanchorableBase covers the two rewrite states in
// which no merge base can be found; both must point at --rebase-onto,
// which is a command that works there (guidance closure, D3).
func TestRunRejectsAnUnanchorableBase(t *testing.T) {
	tests := []struct {
		name    string
		arrange func(f *fixture)
	}{
		{
			name: "unknown base with no recorded tree",
			arrange: func(f *fixture) {
				f.state.base = provenance.Base{Commit: commitOID(7)}
			},
		},
		{
			name: "unknown base whose tree is nowhere in canonical",
			arrange: func(f *fixture) {
				f.state.base = provenance.Base{Commit: commitOID(7), Tree: treeOID(7)}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newFixture()
			test.arrange(f)

			_, err := f.useCase().Run(context.Background(), Options{})
			if !errors.Is(err, ErrUnknownBase) {
				t.Fatalf("error = %v, want ErrUnknownBase", err)
			}
			// The sentinel states the fact; the recovery command is the
			// CLI catalog's job (F-H6), so the message must NOT name one.
			if strings.Contains(err.Error(), "sanho ") {
				t.Errorf("message = %q, want a command-free sentinel", err)
			}
			if len(f.app.mergeCalls) != 0 || len(f.state.savedBases) != 0 {
				t.Fatalf("a rejected sync acted: merges=%v bases=%v", f.app.mergeCalls, f.state.savedBases)
			}
		})
	}
}

func TestRunRebaseOnto(t *testing.T) {
	t.Run("an unknown target is refused", func(t *testing.T) {
		f := newFixture()

		_, err := f.useCase().Run(context.Background(), Options{RebaseOnto: commitOID(7)})
		if !errors.Is(err, ErrUnknownTarget) {
			t.Fatalf("error = %v, want ErrUnknownTarget", err)
		}
		if len(f.app.mergeCalls) != 0 {
			t.Fatalf("a refused target still merged: %v", f.app.mergeCalls)
		}
	})

	// The explicit target overrides an unusable base: instead of the
	// ErrUnknownBase refusal (whose own message advises --rebase-onto —
	// refusing here was the D3 closure violation the e2e suite caught),
	// the merge falls back to the empty tree as its base, exactly as in
	// the no-base state.
	t.Run("an unusable base falls back to the empty tree", func(t *testing.T) {
		for name, base := range map[string]provenance.Base{
			"unknown base with no recorded tree":   {Commit: commitOID(7)},
			"unknown base with an unanchored tree": {Commit: commitOID(7), Tree: treeOID(7)},
		} {
			t.Run(name, func(t *testing.T) {
				f := newFixture()
				f.state.base = base
				f.canonical.known[commitOID(2)] = true
				f.app.commitTrees[commitOID(2)] = treeOID(2)

				result, err := f.useCase().Run(context.Background(), Options{RebaseOnto: commitOID(2)})
				if err != nil {
					t.Fatalf("Run: %v", err)
				}
				if len(f.app.mergeCalls) != 1 || f.app.mergeCalls[0].base != emptyTreeOID {
					t.Fatalf("merge base = %v, want the empty tree", f.app.mergeCalls)
				}
				want := provenance.Base{Commit: commitOID(2), Tree: treeOID(2)}
				if result.NewBase != want {
					t.Fatalf("NewBase = %+v, want %+v", result.NewBase, want)
				}
			})
		}
	})

	t.Run("a known target replaces canonical head", func(t *testing.T) {
		f := newFixture()
		// Canonical head is commit 1; aim at the older commit 2 instead.
		f.canonical.known[commitOID(2)] = true
		f.app.commitTrees[commitOID(2)] = treeOID(2)

		result, err := f.useCase().Run(context.Background(), Options{RebaseOnto: commitOID(2)})
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if len(f.app.mergeCalls) != 1 || f.app.mergeCalls[0].theirs != treeOID(2) {
			t.Fatalf("merged toward %v, want the requested target tree %s", f.app.mergeCalls, treeOID(2))
		}
		want := provenance.Base{Commit: commitOID(2), Tree: treeOID(2)}
		if result.NewBase != want {
			t.Fatalf("NewBase = %+v, want %+v", result.NewBase, want)
		}
		if got := f.app.commitMessages; len(got) != 1 || got[0] != "docs: sync to "+commitOID(2)[:12] {
			t.Fatalf("commit messages = %v, want the target's short OID", got)
		}
	})
}

func TestRunCleanMerge(t *testing.T) {
	f := newFixture()

	result, err := f.useCase().Run(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Status != StatusSynced {
		t.Fatalf("status = %v, want synced", result.Status)
	}
	if result.CommitOID != commitOID(9) {
		t.Fatalf("CommitOID = %s, want the created commit", result.CommitOID)
	}
	if want := (provenance.Base{Commit: commitOID(1), Tree: treeOID(1)}); result.NewBase != want {
		t.Fatalf("NewBase = %+v, want %+v", result.NewBase, want)
	}

	// The merged tree reaches the worktree, the base is recorded, and
	// only then is the commit made: a crash before the commit leaves a
	// state the next run can still describe correctly.
	if got := f.shared.trace(); got != "docs-clean fetch import merge checkout save-base commit" {
		t.Fatalf("sequence = %q", got)
	}
	if got := f.app.checkedOut; len(got) != 1 || got[0] != treeOID(9) {
		t.Fatalf("checked out %v, want the merge result", got)
	}
	if got := f.app.commitMessages; len(got) != 1 || got[0] != "docs: sync to "+commitOID(1)[:12] {
		t.Fatalf("commit messages = %v", got)
	}
	if len(f.state.savedNotes) != 0 {
		t.Fatalf("a clean sync wrote a note: %v", f.state.savedNotes)
	}
}

// TestRunCleanMergeThatChangesNothing: the merge is clean but its
// result is already HEAD's docs tree (upstream's edits were present
// locally). There is nothing to check out and nothing git could commit,
// so at most the base moves — and when the base is already the target,
// not even that.
func TestRunCleanMergeThatChangesNothing(t *testing.T) {
	t.Run("the base still advances", func(t *testing.T) {
		f := newFixture()
		f.app.mergeTree = treeOID(0) // == HEAD docs tree

		result, err := f.useCase().Run(context.Background(), Options{})
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if result.Status != StatusSynced {
			t.Fatalf("status = %v, want synced", result.Status)
		}
		if result.CommitOID != "" {
			t.Fatalf("CommitOID = %s, want none", result.CommitOID)
		}
		if got := f.shared.trace(); got != "docs-clean fetch import merge save-base" {
			t.Fatalf("sequence = %q, want no checkout and no commit", got)
		}
	})

	t.Run("nothing at all moved", func(t *testing.T) {
		// Local docs commits against an unmoved canonical: the merge
		// re-adopts the user's own tree and the base already names the
		// target, so the honest answer is "up to date".
		f := newFixture()
		f.state.base = provenance.Base{Commit: commitOID(1), Tree: treeOID(1)}
		f.app.mergeTree = treeOID(0) // == HEAD docs tree, != canonical's

		result, err := f.useCase().Run(context.Background(), Options{})
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if result.Status != StatusUpToDate {
			t.Fatalf("status = %v, want up to date", result.Status)
		}
		if got := f.shared.trace(); got != "docs-clean fetch import merge" {
			t.Fatalf("sequence = %q, want nothing written", got)
		}
	})
}

// TestRunConflictIsNotAnError pins the §5.5 step 6 contract: markers in
// the worktree, a note recording everything the resolution will be
// judged against, **the base left exactly where the sync found it**, no
// commit, and a nil error so the CLI can render template 2 from the
// Result.
//
// The base is the load-bearing assertion. Advancing it here put the
// workspace in a state where base == canonical head while the worktree
// still held pre-merge docs, so any path that lost the note — a stash
// the completion test misread, a damaged note the abort could only
// delete — turned the next push into a fast-forward that republished the
// pre-merge tree over upstream's work.
func TestRunConflictIsNotAnError(t *testing.T) {
	f := newFixture()
	f.app.mergeConflicts = []string{"docs/api.md", "docs/schema.md"}

	result, err := f.useCase().Run(context.Background(), Options{})
	if err != nil {
		t.Fatalf("a conflicted merge returned an error: %v", err)
	}
	if result.Status != StatusConflicts {
		t.Fatalf("status = %v, want conflicts", result.Status)
	}
	if strings.Join(result.Conflicts, ",") != "docs/api.md,docs/schema.md" {
		t.Fatalf("conflicts = %v", result.Conflicts)
	}

	if got := f.shared.trace(); got != "docs-clean fetch import merge checkout save-note" {
		t.Fatalf("sequence = %q, want the note written and the base left alone", got)
	}
	if got := f.app.checkedOut; len(got) != 1 || got[0] != treeOID(9) {
		t.Fatalf("checked out %v, want the conflicted merge result", got)
	}
	if len(f.app.commitMessages) != 0 {
		t.Fatalf("a conflicted sync committed: %v", f.app.commitMessages)
	}
	if got := f.state.savedBases; len(got) != 0 {
		t.Fatalf("a conflicted sync moved the base to %v; the merge target lives in the note until the resolution is confirmed", got)
	}
	if got := f.state.base; got != (provenance.Base{Commit: commitOID(0), Tree: treeOID(0)}) {
		t.Fatalf("base = %+v, want the pre-sync base", got)
	}

	wantNote := SyncNote{
		PrevBase: provenance.Base{Commit: commitOID(0), Tree: treeOID(0)},
		Target:   provenance.Base{Commit: commitOID(1), Tree: treeOID(1)},
		// Where the workspace stood when the markers landed, and what the
		// merge could not settle: together they make "was this resolved?"
		// answerable, and answerable by the right question.
		EntryHead:     commitOID(7),
		EntryDocsTree: treeOID(0),
		Conflicts:     []string{"docs/api.md", "docs/schema.md"},
	}
	if len(f.state.savedNotes) != 1 || !reflect.DeepEqual(f.state.savedNotes[0], wantNote) {
		t.Fatalf("note = %+v, want %+v", f.state.savedNotes, wantNote)
	}
}

func TestAbort(t *testing.T) {
	t.Run("without a sync in progress", func(t *testing.T) {
		f := newFixture()

		if _, err := f.useCase().Abort(context.Background()); !errors.Is(err, ErrNoSyncInProgress) {
			t.Fatalf("error = %v, want ErrNoSyncInProgress", err)
		}
		if f.app.restores != 0 {
			t.Fatal("an abort with nothing to abort touched the worktree")
		}
	})

	t.Run("restores the docs, then the base, then drops the note", func(t *testing.T) {
		f := newFixture()
		previous := provenance.Base{Commit: commitOID(0), Tree: treeOID(0)}
		f.state.note = liveNote()

		if _, err := f.useCase().Abort(context.Background()); err != nil {
			t.Fatalf("Abort: %v", err)
		}
		if got := f.shared.trace(); got != "restore save-base clear-note" {
			t.Fatalf("sequence = %q, want the note dropped last", got)
		}
		if got := f.state.savedBases; len(got) != 1 || got[0] != previous {
			t.Fatalf("saved bases = %v, want the previous base %+v", got, previous)
		}
	})

	// A note written by the build that advanced the base at conflict time
	// is the one case where the restore is not a rewrite of the value
	// already on disk. Aborting it has to put the pre-sync base back.
	t.Run("restores a base a pre-upgrade note left on the target", func(t *testing.T) {
		f := newFixture()
		previous := provenance.Base{Commit: commitOID(0), Tree: treeOID(0)}
		f.state.note = &SyncNote{PrevBase: previous, Target: provenance.Base{Commit: commitOID(1), Tree: treeOID(1)}}
		f.state.base = provenance.Base{Commit: commitOID(1), Tree: treeOID(1)}

		if _, err := f.useCase().Abort(context.Background()); err != nil {
			t.Fatalf("Abort: %v", err)
		}
		if got := f.state.base; got != previous {
			t.Fatalf("base after abort = %+v, want the previous base %+v", got, previous)
		}
	})

	t.Run("restores the absence of a base", func(t *testing.T) {
		f := newFixture()
		f.state.note = &SyncNote{Target: provenance.Base{Commit: commitOID(1), Tree: treeOID(1)}, EntryHead: commitOID(7), EntryDocsTree: treeOID(0)}

		if _, err := f.useCase().Abort(context.Background()); err != nil {
			t.Fatalf("Abort: %v", err)
		}
		if got := f.shared.trace(); got != "restore clear-base clear-note" {
			t.Fatalf("sequence = %q, want the base file removed rather than zeroed", got)
		}
		if len(f.state.savedBases) != 0 {
			t.Fatalf("an empty base was written: %v", f.state.savedBases)
		}
	})

	t.Run("is idempotent", func(t *testing.T) {
		f := newFixture()
		f.state.note = liveNote()
		use := f.useCase()

		if _, err := use.Abort(context.Background()); err != nil {
			t.Fatalf("first Abort: %v", err)
		}
		if _, err := use.Abort(context.Background()); !errors.Is(err, ErrNoSyncInProgress) {
			t.Fatalf("second Abort = %v, want ErrNoSyncInProgress", err)
		}
	})
}

// TestResolutionStateWritesNothing is the demotion, asserted as a
// property rather than per case: whatever the state, asking about it
// changes nothing.
//
// This is the audit lineage's "a read path must not mutate" applied to
// the one file the sync window is defined by. The routine this replaced
// cleared the note and moved the base whenever the state looked
// resolved, from inside `pre-commit` and `pre-push` — so a stash
// followed by more work on the conflicted file was enough to make two
// hooks finish a sync nobody had finished.
func TestResolutionStateWritesNothing(t *testing.T) {
	arrangements := map[string]func(f *fixture){
		"no sync": func(f *fixture) {},
		"markers remain": func(f *fixture) {
			f.state.note = liveNote()
			f.app.markerPaths = []string{"docs/api.md"}
		},
		"resolved and committed": func(f *fixture) {
			f.state.note = liveNote()
			f.app.headCommit = commitOID(8)
			f.app.headTree = treeOID(9)
			f.app.changedPaths = map[string]bool{"docs/api.md": true}
		},
		"put aside": func(f *fixture) { f.state.note = liveNote() },
		"a legacy note": func(f *fixture) {
			f.state.note = &SyncNote{
				Target:              provenance.Base{Commit: commitOID(1)},
				PreDatesEntryRecord: true,
			}
		},
	}

	for name, arrange := range arrangements {
		t.Run(name, func(t *testing.T) {
			f := newFixture()
			arrange(f)
			before := f.state.base

			if _, err := f.useCase().ResolutionState(context.Background()); err != nil {
				t.Fatalf("ResolutionState: %v", err)
			}
			if len(f.state.savedBases) != 0 || len(f.state.savedNotes) != 0 || f.state.noteCleared != 0 {
				t.Fatalf("a query wrote state: bases=%v notes=%v cleared=%d",
					f.state.savedBases, f.state.savedNotes, f.state.noteCleared)
			}
			if f.state.base != before {
				t.Fatalf("base = %+v, want it untouched at %+v", f.state.base, before)
			}
		})
	}
}

// TestResolutionState covers the whole classification. Nothing here
// completes a sync — that is `Continue`'s job — so each case is about
// which sentence a hook is entitled to print.
func TestResolutionState(t *testing.T) {
	// committed is a commit having happened: HEAD has moved and carries a
	// different docs tree. On its own that is any docs commit at all.
	committed := func(f *fixture) {
		f.app.headCommit = commitOID(8)
		f.app.headTree = treeOID(9)
	}
	// settled is what makes it a resolution: one of the conflicted paths
	// changed with it.
	settled := func(f *fixture) {
		committed(f)
		f.app.changedPaths = map[string]bool{"docs/api.md": true}
	}

	tests := []struct {
		name    string
		arrange func(f *fixture)
		want    Resolution
	}{
		{
			name:    "no sync in progress",
			arrange: func(f *fixture) {},
			want:    ResolutionNoSync,
		},
		{
			name: "markers still in the worktree",
			arrange: func(f *fixture) {
				f.state.note = liveNote()
				f.app.markerPaths = []string{"docs/api.md"}
			},
			want: ResolutionPending,
		},
		{
			name: "resolved but not committed",
			arrange: func(f *fixture) {
				f.state.note = liveNote()
				f.app.docsClean = false
			},
			want: ResolutionPending,
		},
		{
			name: "a commit that changed a conflicted path",
			arrange: func(f *fixture) {
				f.state.note = liveNote()
				settled(f)
			},
			want: ResolutionResolved,
		},
		{
			name: "clean, but HEAD never moved (stash, revert, checkout)",
			arrange: func(f *fixture) {
				f.state.note = liveNote()
			},
			want: ResolutionNotCommitted,
		},
		{
			name: "an unrelated commit that left the docs tree alone",
			arrange: func(f *fixture) {
				f.state.note = liveNote()
				// HEAD moved; the docs tree did not.
				f.app.headCommit = commitOID(8)
			},
			want: ResolutionNotCommitted,
		},
		{
			name: "an unrelated DOCS commit: HEAD and the docs tree moved, the conflict did not",
			arrange: func(f *fixture) {
				f.state.note = liveNote()
				committed(f)
			},
			want: ResolutionNotCommitted,
		},
		{
			// It cannot say "no commit changed the files it conflicted
			// on" — it does not know which files those were — so it says
			// so, rather than borrowing a reason that happens to route
			// the user to the same two commands.
			name: "a note that records no conflicted paths says it cannot tell",
			arrange: func(f *fixture) {
				note := liveNote()
				note.Conflicts = nil
				f.state.note = note
				settled(f)
			},
			want: ResolutionUnknown,
		},
		{
			name: "a note written before entry_head existed says it cannot tell",
			arrange: func(f *fixture) {
				f.state.note = &SyncNote{
					Target:              provenance.Base{Commit: commitOID(1)},
					PreDatesEntryRecord: true,
				}
				settled(f)
			},
			want: ResolutionUnknown,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newFixture()
			test.arrange(f)

			got, err := f.useCase().ResolutionState(context.Background())
			if err != nil {
				t.Fatalf("ResolutionState: %v", err)
			}
			if got != test.want {
				t.Fatalf("ResolutionState = %v, want %v", got, test.want)
			}
		})
	}
}

// TestResolutionStateComparesOnlyTheConflictedPaths pins what the
// classification actually asks git, since the answer is the whole
// difference between the "resolved" sentence and the "put aside" one.
func TestResolutionStateComparesOnlyTheConflictedPaths(t *testing.T) {
	f := newFixture()
	f.state.note = liveNote()
	f.app.headCommit = commitOID(8)
	f.app.headTree = treeOID(9)

	if _, err := f.useCase().ResolutionState(context.Background()); err != nil {
		t.Fatalf("ResolutionState: %v", err)
	}
	want := diffCall{from: treeOID(0), to: treeOID(9), paths: "docs/api.md"}
	if len(f.app.diffCalls) != 1 || f.app.diffCalls[0] != want {
		t.Fatalf("diff calls = %+v, want one %+v", f.app.diffCalls, want)
	}
}

// --- `sanho sync --continue`: completion as an act ---------------------

// TestContinueRefusesWhatIsNotReady walks the three preconditions. Each
// refusal names what remains, and none of them writes anything: a
// refused completion must leave the workspace exactly as unfinished as
// it found it, or the next attempt is answering a different question.
func TestContinueRefusesWhatIsNotReady(t *testing.T) {
	tests := []struct {
		name    string
		arrange func(f *fixture)
		want    error
	}{
		{
			name:    "no sync is in progress",
			arrange: func(f *fixture) {},
			want:    ErrNoSyncInProgress,
		},
		{
			name: "markers are still in the worktree",
			arrange: func(f *fixture) {
				f.state.note = liveNote()
				f.app.markerPaths = []string{"docs/api.md"}
			},
			want: ErrMarkersRemain,
		},
		{
			name: "the resolution is edited but not committed",
			arrange: func(f *fixture) {
				f.state.note = liveNote()
				f.app.docsClean = false
			},
			want: ErrResolutionUncommitted,
		},
		{
			name: "the note cannot be read",
			arrange: func(f *fixture) {
				f.state.noteErr = fmt.Errorf("%w: unexpected end of JSON input", ErrSyncNoteCorrupt)
			},
			want: ErrSyncNoteCorrupt,
		},
		{
			// A parseable note with no usable target cannot say what to
			// adopt, and guessing is the one thing the invariant forbids.
			name: "the note records no usable merge target",
			arrange: func(f *fixture) {
				note := liveNote()
				note.Target = provenance.Base{}
				f.state.note = note
			},
			want: ErrSyncNoteCorrupt,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newFixture()
			test.arrange(f)
			before := f.state.base

			_, err := f.useCase().Continue(context.Background())
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
			if len(f.state.savedBases) != 0 || f.state.noteCleared != 0 {
				t.Fatalf("a refused --continue acted: bases=%v cleared=%d",
					f.state.savedBases, f.state.noteCleared)
			}
			if f.state.base != before {
				t.Fatalf("base = %+v, want it untouched at %+v", f.state.base, before)
			}
			if len(f.app.commitMessages) != 0 || len(f.app.checkedOut) != 0 || f.app.restores != 0 {
				t.Fatalf("a refused --continue touched the worktree: commits=%v checkouts=%v restores=%d",
					f.app.commitMessages, f.app.checkedOut, f.app.restores)
			}
			// The sentinels state facts; the commands belong to the CLI
			// catalog, where a closure fixture can prove them runnable.
			if err != nil && strings.Contains(err.Error(), "sanho ") {
				t.Errorf("message = %q, want a command-free sentinel", err)
			}
		})
	}
}

// TestContinueCompletesTheSync is the whole of the new verb: the note
// goes, the base adopts the target, and nothing else happens.
//
// The write order is asserted directly, because it is the invariant in
// miniature. Note first, base second: an interruption between them
// leaves the base at its OLDER value, which publication reconciles as an
// ordinary divergence. The reverse order fails toward a base ahead of
// the worktree, which is a fast-forward over upstream's work.
func TestContinueCompletesTheSync(t *testing.T) {
	f := newFixture()
	f.state.note = liveNote()

	result, err := f.useCase().Continue(context.Background())
	if err != nil {
		t.Fatalf("Continue: %v", err)
	}

	target := provenance.Base{Commit: commitOID(1), Tree: treeOID(1)}
	if result.Base != target {
		t.Fatalf("adopted base = %+v, want the note's target %+v", result.Base, target)
	}
	if got := f.shared.trace(); got != "scan docs-clean clear-note save-base" {
		t.Fatalf("sequence = %q, want the note dropped BEFORE the base moved", got)
	}
	if got := f.state.savedBases; len(got) != 1 || got[0] != target {
		t.Fatalf("saved bases = %v, want exactly the target %+v", got, target)
	}
	// P3: completing a sync creates nothing.
	if len(f.app.commitMessages) != 0 || len(f.app.checkedOut) != 0 || f.app.restores != 0 {
		t.Fatalf("--continue touched the repository: commits=%v checkouts=%v restores=%d",
			f.app.commitMessages, f.app.checkedOut, f.app.restores)
	}
}

// TestContinueInterruptedBeforeTheBaseWriteFailsOld simulates the crash
// the ordering exists for: the note is gone, the base write never
// happened, and what the workspace is left holding is the OLDER base.
func TestContinueInterruptedBeforeTheBaseWriteFailsOld(t *testing.T) {
	f := newFixture()
	f.state.note = liveNote()
	f.state.saveBaseErr = errors.New("the disk went away")

	if _, err := f.useCase().Continue(context.Background()); err == nil {
		t.Fatal("Continue reported success after the base write failed")
	}
	if f.state.noteCleared != 1 {
		t.Fatalf("note cleared %d times, want 1 — the note is dropped first", f.state.noteCleared)
	}
	previous := provenance.Base{Commit: commitOID(0), Tree: treeOID(0)}
	if f.state.base != previous {
		t.Fatalf("base after the interruption = %+v, want the older value %+v", f.state.base, previous)
	}
}

// TestContinueIsIdempotent: completing twice is not an error the second
// time round wearing a different name — there is simply nothing left to
// complete.
func TestContinueIsIdempotent(t *testing.T) {
	f := newFixture()
	f.state.note = liveNote()
	use := f.useCase()

	if _, err := use.Continue(context.Background()); err != nil {
		t.Fatalf("first Continue: %v", err)
	}
	if _, err := use.Continue(context.Background()); !errors.Is(err, ErrNoSyncInProgress) {
		t.Fatalf("second Continue = %v, want ErrNoSyncInProgress", err)
	}
	if len(f.state.savedBases) != 1 {
		t.Fatalf("saved bases = %v, want exactly one write", f.state.savedBases)
	}
}

// TestContinueCompletesATakeOursResolution is the dead end the previous
// design had no exit from: a resolution that keeps every conflicted path
// byte for byte leaves no evidence at all, so no inference could ever
// confirm it. The user asserting it is the evidence.
func TestContinueCompletesATakeOursResolution(t *testing.T) {
	f := newFixture()
	f.state.note = liveNote()
	// Nothing was committed: HEAD is exactly where the sync left it, and
	// the docs tree with it.
	f.app.headCommit = commitOID(7)
	f.app.headTree = treeOID(0)

	state, err := f.useCase().ResolutionState(context.Background())
	if err != nil || state != ResolutionNotCommitted {
		t.Fatalf("ResolutionState = (%v, %v), want (not_committed, nil)", state, err)
	}
	if _, err := f.useCase().Continue(context.Background()); err != nil {
		t.Fatalf("Continue over a take-ours resolution: %v", err)
	}
	if target := (provenance.Base{Commit: commitOID(1), Tree: treeOID(1)}); f.state.base != target {
		t.Fatalf("base = %+v, want the target %+v", f.state.base, target)
	}
}

// TestContinueCompletesALegacyNote is the other escape the verb opens.
// A note written before the entry fields existed cannot prove anything,
// so the previous design could only ever abort it — and the message it
// printed explained the refusal with a reason nothing knew to be true.
func TestContinueCompletesALegacyNote(t *testing.T) {
	f := newFixture()
	f.state.note = &SyncNote{
		PrevBase:            provenance.Base{Commit: commitOID(0), Tree: treeOID(0)},
		Target:              provenance.Base{Commit: commitOID(1), Tree: treeOID(1)},
		PreDatesEntryRecord: true,
	}

	state, err := f.useCase().ResolutionState(context.Background())
	if err != nil || state != ResolutionUnknown {
		t.Fatalf("ResolutionState = (%v, %v), want (unknown, nil)", state, err)
	}
	if _, err := f.useCase().Continue(context.Background()); err != nil {
		t.Fatalf("Continue over a legacy note: %v", err)
	}
	if _, ok, _ := f.state.LoadSyncNote(); ok {
		t.Error("--continue left the legacy note behind")
	}
}

// liveNote is a note as a conflicted sync writes it, pinned to the
// fixture's pre-resolution HEAD and to what the merge could not settle.
func liveNote() *SyncNote {
	return &SyncNote{
		PrevBase:      provenance.Base{Commit: commitOID(0), Tree: treeOID(0)},
		Target:        provenance.Base{Commit: commitOID(1), Tree: treeOID(1)},
		EntryHead:     commitOID(7),
		EntryDocsTree: treeOID(0),
		Conflicts:     []string{"docs/api.md"},
	}
}

// TestACorruptNoteStillYieldsToAbort is the review's second finding: a
// `sync.json` nothing can parse used to make every path fail, including
// the one operation whose contract is that it cannot.
//
// The base assertion is the third review's C3, and it is why this test
// is not the one it was. Leaving the base alone rested on "a conflicted
// sync never moved it", and the fixture below is the state that breaks
// the premise: the base already names the merge target (a crash between
// the base write and the note clear, or a note from a build that
// advanced the base at conflict time) and the note can no longer say
// what it was. An abort that walked away from that left a base ahead of
// the worktree, which the next push spends as a fast-forward.
func TestACorruptNoteStillYieldsToAbort(t *testing.T) {
	corrupt := func() *fixture {
		f := newFixture()
		f.state.noteErr = fmt.Errorf("%w: /repo/.git/sanho/sync.json: unexpected end of JSON input", ErrSyncNoteCorrupt)
		// The dangerous shape: the base sits on canonical head while the
		// worktree docs are the pre-merge ones (tree 0).
		f.state.base = provenance.Base{Commit: commitOID(1), Tree: treeOID(1)}
		return f
	}

	t.Run("abort restores the docs, forgets the base, and clears the note", func(t *testing.T) {
		f := corrupt()

		if _, err := f.useCase().Abort(context.Background()); err != nil {
			t.Fatalf("Abort over a corrupt note: %v", err)
		}
		if got := f.shared.trace(); got != "restore clear-base clear-note" {
			t.Fatalf("sequence = %q, want the docs restored, the base cleared, and the note dropped last", got)
		}
		// The previous base lived inside the note, so nothing can be
		// written in its place. The invariant decides what happens then:
		// where the base cannot be established, the older value wins, and
		// no base at all is the oldest there is.
		if len(f.state.savedBases) != 0 {
			t.Fatalf("an abort over an unreadable note guessed at a base: %v", f.state.savedBases)
		}
		if f.state.hasBase {
			t.Fatalf("base after the abort = %+v, want none recorded", f.state.base)
		}
	})

	t.Run("every other path refuses with the sentinel", func(t *testing.T) {
		for name, call := range map[string]func(*UseCase) error{
			"run": func(u *UseCase) error {
				_, err := u.Run(context.Background(), Options{})
				return err
			},
			"pull": func(u *UseCase) error {
				_, err := u.Pull(context.Background(), false)
				return err
			},
			"resolution state": func(u *UseCase) error {
				_, err := u.ResolutionState(context.Background())
				return err
			},
			"continue": func(u *UseCase) error {
				_, err := u.Continue(context.Background())
				return err
			},
		} {
			t.Run(name, func(t *testing.T) {
				f := corrupt()
				if err := call(f.useCase()); !errors.Is(err, ErrSyncNoteCorrupt) {
					t.Fatalf("error = %v, want ErrSyncNoteCorrupt", err)
				}
			})
		}
	})
}

func TestPullRefusals(t *testing.T) {
	tests := []struct {
		name    string
		arrange func(f *fixture)
		want    error
	}{
		{
			name: "a sync in progress",
			arrange: func(f *fixture) {
				f.state.note = &SyncNote{Target: provenance.Base{Commit: commitOID(1)}}
			},
			want: ErrSyncInProgress,
		},
		{
			name: "no recorded base",
			arrange: func(f *fixture) {
				f.state.base, f.state.hasBase = provenance.Base{}, false
			},
			want: ErrPullNeedsSync,
		},
		{
			name: "local docs edits",
			arrange: func(f *fixture) {
				f.app.worktreeTree = treeOID(5)
			},
			want: ErrPullNeedsSync,
		},
		{
			name:    "an unreachable canonical",
			arrange: func(f *fixture) { f.canonical.fetchErr = pubdom.ErrUnreachable },
			want:    pubdom.ErrUnreachable,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newFixture()
			test.arrange(f)

			_, err := f.useCase().Pull(context.Background(), false)
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
			if len(f.app.checkedOut) != 0 || len(f.state.savedBases) != 0 {
				t.Fatalf("a refused pull acted: checkouts=%v bases=%v", f.app.checkedOut, f.state.savedBases)
			}
		})
	}
}

func TestPull(t *testing.T) {
	t.Run("up to date", func(t *testing.T) {
		f := newFixture()
		f.state.base = provenance.Base{Commit: commitOID(1), Tree: treeOID(1)}
		f.app.worktreeTree = treeOID(1)

		result, err := f.useCase().Pull(context.Background(), false)
		if err != nil {
			t.Fatalf("Pull: %v", err)
		}
		if result.Status != StatusUpToDate {
			t.Fatalf("status = %v, want up to date", result.Status)
		}
		if len(f.app.checkedOut) != 0 {
			t.Fatalf("an up-to-date pull checked out %v", f.app.checkedOut)
		}
	})

	t.Run("fast-forwards the docs", func(t *testing.T) {
		f := newFixture()

		result, err := f.useCase().Pull(context.Background(), false)
		if err != nil {
			t.Fatalf("Pull: %v", err)
		}
		if result.Status != StatusSynced {
			t.Fatalf("status = %v, want synced", result.Status)
		}
		if got := f.app.checkedOut; len(got) != 1 || got[0] != treeOID(1) {
			t.Fatalf("checked out %v, want the canonical tree", got)
		}
		if want := (provenance.Base{Commit: commitOID(1), Tree: treeOID(1)}); result.NewBase != want {
			t.Fatalf("NewBase = %+v, want %+v", result.NewBase, want)
		}
		if len(f.app.commitMessages) != 0 {
			t.Fatalf("a plain pull committed: %v", f.app.commitMessages)
		}
		if result.CommitOID != "" {
			t.Fatalf("CommitOID = %s, want none", result.CommitOID)
		}
	})

	t.Run("--commit records the update", func(t *testing.T) {
		f := newFixture()

		result, err := f.useCase().Pull(context.Background(), true)
		if err != nil {
			t.Fatalf("Pull: %v", err)
		}
		if got := f.app.commitMessages; len(got) != 1 || got[0] != "docs: sync to "+commitOID(1)[:12] {
			t.Fatalf("commit messages = %v, want the sync convention", got)
		}
		if result.CommitOID != commitOID(9) {
			t.Fatalf("CommitOID = %s, want the created commit", result.CommitOID)
		}
		if got := f.shared.trace(); got != "docs-clean fetch import checkout save-base commit" {
			t.Fatalf("sequence = %q", got)
		}
	})

	t.Run("--commit records nothing when HEAD already carries the tree", func(t *testing.T) {
		f := newFixture()
		// HEAD's docs already equal canonical; only the worktree and the
		// base are behind (the state a case-③ publish leaves, §5.3).
		f.app.headTree = treeOID(1)

		if _, err := f.useCase().Pull(context.Background(), true); err != nil {
			t.Fatalf("Pull: %v", err)
		}
		if len(f.app.commitMessages) != 0 {
			t.Fatalf("committed with nothing to commit: %v", f.app.commitMessages)
		}
		if got := f.state.savedBases; len(got) != 1 {
			t.Fatalf("saved bases = %v, want the base advanced once", got)
		}
	})
}

func TestStatusString(t *testing.T) {
	for status, want := range map[Status]string{
		StatusUpToDate:  "up_to_date",
		StatusSynced:    "synced",
		StatusConflicts: "conflicts",
		Status(9):       "status(9)",
	} {
		if got := status.String(); got != want {
			t.Errorf("Status(%d).String() = %q, want %q", status, got, want)
		}
	}
}

// --- F-H5: `sanho pull` must not discard staged docs -------------------

// TestPullRefusesWhenDocsAreStaged is R1-G1's repro. The pre-fix Pull
// tested only the worktree tree against the base tree, so a docs edit
// that was `git add`ed and then restored in the worktree — or any staged
// edit at all, since CheckoutDocsTree rewrites index entries — passed the
// guard and was overwritten. There is no undo for a discarded index.
func TestPullRefusesWhenDocsAreStaged(t *testing.T) {
	f := newFixture()
	f.app.docsClean = false

	_, err := f.useCase().Pull(context.Background(), false)
	if !errors.Is(err, ErrPullNeedsSync) {
		t.Fatalf("error = %v, want ErrPullNeedsSync", err)
	}
	if !strings.Contains(err.Error(), "uncommitted changes") {
		t.Errorf("message = %q, want it to name the uncommitted changes", err)
	}
	// And it refused before anything could be written: no checkout, no
	// base write, not even a fetch.
	if got := f.shared.trace(); got != "docs-clean" {
		t.Fatalf("side effects = %q, want the refusal to precede every write", got)
	}
	if len(f.app.checkedOut) != 0 || len(f.state.savedBases) != 0 {
		t.Fatalf("a refused pull acted: checkouts=%v bases=%v", f.app.checkedOut, f.state.savedBases)
	}
}

// --- F-M4: --rebase-onto is rewrite recovery, not time travel ----------

func TestRebaseOntoRefusesAnAncestorOfAHealthyBase(t *testing.T) {
	f := newFixture()
	// The recorded base is canonical head itself and perfectly
	// reachable; commitOID(0) precedes it.
	f.state.base = provenance.Base{Commit: commitOID(1), Tree: treeOID(1)}

	_, err := f.useCase().Run(context.Background(), Options{RebaseOnto: commitOID(0)})
	if !errors.Is(err, ErrRebaseOntoHealthy) {
		t.Fatalf("error = %v, want ErrRebaseOntoHealthy", err)
	}
	if len(f.app.mergeCalls) != 0 || len(f.state.savedBases) != 0 {
		t.Fatalf("the refused rebase acted: merges=%v bases=%v", f.app.mergeCalls, f.state.savedBases)
	}
}

// TestRebaseOntoAllowsRecoveryWhenTheBaseIsUnreachable keeps the guard
// from swallowing the flag's actual purpose.
func TestRebaseOntoAllowsRecoveryWhenTheBaseIsUnreachable(t *testing.T) {
	f := newFixture()
	f.state.base = provenance.Base{Commit: commitOID(77), Tree: treeOID(77)}

	if _, err := f.useCase().Run(context.Background(), Options{RebaseOnto: commitOID(1)}); err != nil {
		t.Fatalf("Run with --rebase-onto against an unreachable base: %v", err)
	}
}
