package docsync

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// These wrappers expose no mutation methods: assessment cannot reach a writer.
type completionReads struct{ CompletionReader }
type noteReads struct{ SyncNoteReader }

func TestAssessmentOrderedBlockers(t *testing.T) {
	tests := []struct {
		name    string
		arrange func(*fixture)
		kind    BlockerKind
		check   int
		cause   error
	}{
		{"none", func(f *fixture) { f.state.note = nil }, BlockerNoSync, 0, ErrNoSyncInProgress},
		{"corrupt", func(f *fixture) { f.state.noteErr = ErrSyncNoteCorrupt }, BlockerCorruptNote, 0, ErrSyncNoteCorrupt},
		{"target", func(f *fixture) { f.state.note.Target.Commit = "invalid" }, BlockerInvalidTarget, 0, ErrSyncNoteCorrupt},
		{"markers before dirty and history", func(f *fixture) {
			f.app.markerPaths = []string{"docs/outside.md"}
			f.app.docsClean = false
			f.app.headCommit = commitOID(8)
		}, BlockerMarkers, 1, ErrMarkersRemain},
		{"dirty before history", func(f *fixture) { f.app.docsClean = false; f.app.headCommit = commitOID(8) }, BlockerUncommitted, 2, ErrResolutionUncommitted},
		{"history before missing tree", func(f *fixture) { f.app.headCommit = commitOID(8); f.state.note.MergedTree = "" }, BlockerForeignHistory, 3, ErrContinueForeignHistory},
		{"missing tree", func(f *fixture) { f.state.note.MergedTree = "" }, BlockerMissingMergeTree, 4, ErrResolutionUnverifiable},
		{"unexpected paths", func(f *fixture) { f.app.treeChangedPaths = []string{"docs/outside.md"} }, BlockerNonConflictChanges, 5, ErrResolutionChangedNonConflicts},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newFixture()
			f.state.note = liveNote()
			test.arrange(f)
			a, err := AssessCompletion(context.Background(), completionReads{f.app}, noteReads{f.state})
			if err != nil || a.Blocker == nil || a.Blocker.Kind != test.kind || !errors.Is(a.Blocker, test.cause) {
				t.Fatalf("assessment=%+v, error=%v", a, err)
			}
			for i, check := range a.Checks {
				want := CheckPassed
				if i == test.check {
					want = CheckBlocked
				}
				if i > test.check {
					want = CheckNotEvaluated
				}
				if check.State != want {
					t.Errorf("check %s=%s, want %s", check.Name, check.State, want)
				}
			}
			if len(f.state.savedBases) != 0 || f.state.noteCleared != 0 || len(f.app.commitMessages) != 0 {
				t.Fatal("assessment mutated state")
			}
		})
	}
}

func TestAssessmentPreservesLiteralEvidenceAndLegacyHistory(t *testing.T) {
	f := newFixture()
	f.state.note = liveNote()
	f.state.note.EntryHead = ""
	f.state.note.PreDatesEntryRecord = true
	allowed := []string{"docs/한글.md", "docs/comma, space.md", "docs/new\nline.md", "docs/[literal]*.md"}
	f.state.note.Conflicts = allowed
	f.app.treeChangedPaths = append(append([]string{}, allowed...), allowed[0])
	a, err := AssessCompletion(context.Background(), completionReads{f.app}, noteReads{f.state})
	if err != nil || a.Blocker != nil {
		t.Fatalf("assessment=%+v, error=%v", a, err)
	}
	if a.Checks[3].State != CheckNotApplicable {
		t.Fatal("unrecorded history claimed proven")
	}
	if !reflect.DeepEqual(a.Comparison.AllowedChanges, sortedPaths(allowed)) || len(a.Comparison.UnexpectedChanges) != 0 {
		t.Fatalf("comparison=%+v", a.Comparison)
	}
	if a.Comparison.ExpectedTree != f.state.note.MergedTree || a.Comparison.ActualTree != f.app.worktreeTree {
		t.Fatal("tree evidence changed")
	}
	if len(f.state.savedBases) != 0 || f.state.noteCleared != 0 {
		t.Fatal("assessment wrote state")
	}
}

type failedCompletionReader struct {
	CompletionReader
	cleanErr error
}

func (r failedCompletionReader) DocsClean(context.Context) (bool, error) { return false, r.cleanErr }

func TestAssessmentDoesNotConvertReadErrorsOrRunLaterReads(t *testing.T) {
	f := newFixture()
	f.state.note = liveNote()
	failure := errors.New("reader unavailable")
	reader := failedCompletionReader{f.app, failure}
	a, err := AssessCompletion(context.Background(), reader, f.state)
	if !errors.Is(err, failure) || a.Blocker != nil || a.Comparison != nil {
		t.Fatalf("assessment=%+v error=%v", a, err)
	}
	f.app.markerPaths = []string{"docs/outside-conflict.md"}
	a, err = AssessCompletion(context.Background(), reader, f.state)
	if err != nil || a.Blocker == nil || a.Blocker.Kind != BlockerMarkers {
		t.Fatalf("earlier marker lost: %+v %v", a, err)
	}
}

func TestAssessmentAncestryFailureRemainsAnOperationalError(t *testing.T) {
	f := newFixture()
	f.state.note = liveNote()
	f.state.note.MergedTree = ""
	f.app.headCommit = commitOID(8)
	failure := errors.New("entry commit cannot be read")
	f.app.ancestryErr = failure
	a, err := AssessCompletion(context.Background(), f.app, f.state)
	if !errors.Is(err, failure) || a.Blocker != nil || a.Comparison != nil || a.Note != nil {
		t.Fatalf("ancestry failure produced a verdict: %+v %v", a, err)
	}
	if f.state.noteCleared != 0 || len(f.state.savedBases) != 0 {
		t.Fatal("failed ancestry read changed persisted state")
	}
}

type failClearState struct {
	StatePort
	cause error
}

func (s failClearState) ClearSyncNote() error { return s.cause }

func TestContinueClearFailurePreservesNoteAndBase(t *testing.T) {
	f := newFixture()
	f.state.note = liveNote()
	before := f.state.base
	failure := errors.New("note cannot be removed")
	use := f.useCase()
	use.State = failClearState{f.state, failure}
	if _, err := use.Continue(context.Background()); !errors.Is(err, failure) {
		t.Fatalf("Continue=%v", err)
	}
	if f.state.note == nil || f.state.base != before || len(f.state.savedBases) != 0 {
		t.Fatal("failed note clear changed base or note")
	}
}
