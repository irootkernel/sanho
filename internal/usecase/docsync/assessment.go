package docsync

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// CompletionReader supplies only the local facts needed to finish a sync.
// Inspection and Continue bind readers with different execution policies.
// Execution, capability, and missing-object failures must remain errors;
// they never prove absent history, an empty tree, clean docs, or zero drift.
type CompletionReader interface {
	ScanWorktreeDocsForMarkers(context.Context) ([]string, error)
	DocsClean(context.Context) (bool, error)
	HeadCommit(context.Context) (string, error)
	IsAncestor(context.Context, string, string) (bool, error)
	WorktreeDocsTree(context.Context) (string, error)
	DocsTreeChangedPaths(context.Context, string, string) ([]string, error)
}

// SyncNoteReader cannot write the note or the adopted base.
type SyncNoteReader interface {
	LoadSyncNote() (SyncNote, bool, error)
}

type BlockerKind int

const (
	BlockerNoSync BlockerKind = iota + 1
	BlockerCorruptNote
	BlockerInvalidTarget
	BlockerMarkers
	BlockerUncommitted
	BlockerForeignHistory
	BlockerMissingMergeTree
	BlockerNonConflictChanges
)

// CompletionBlocker preserves both the existing error identity and exact paths.
// Public reasons, recovery identities, and guidance belong to the CLI.
type CompletionBlocker struct {
	Kind  BlockerKind
	Paths []string
	Cause error
}

func (b *CompletionBlocker) Error() string { return b.Cause.Error() }
func (b *CompletionBlocker) Unwrap() error { return b.Cause }

type CompletionCheckState string

const (
	CheckPassed        CompletionCheckState = "passed"
	CheckBlocked       CompletionCheckState = "blocked"
	CheckNotEvaluated  CompletionCheckState = "not_evaluated"
	CheckNotApplicable CompletionCheckState = "not_applicable"
)

type CompletionCheck struct {
	Name  string
	State CompletionCheckState
}

type CompletionComparison struct {
	ExpectedTree      string
	ActualTree        string
	AllowedChanges    []string
	UnexpectedChanges []string
}

// CompletionAssessment is evidence from one read, never permission to mutate.
// An operational error invalidates the assessment instead of becoming a blocker.
type CompletionAssessment struct {
	Note       *SyncNote
	Head       string
	Checks     []CompletionCheck
	Comparison *CompletionComparison
	Blocker    *CompletionBlocker
}

// AssessCompletion stops at the first actual Continue guard. In particular, the
// whole-docs marker scan runs before any filter-capable status or tree read.
func AssessCompletion(ctx context.Context, app CompletionReader, state SyncNoteReader) (CompletionAssessment, error) {
	a := CompletionAssessment{Checks: []CompletionCheck{
		{Name: "sync_note", State: CheckNotEvaluated},
		{Name: "markers", State: CheckNotEvaluated},
		{Name: "docs_clean", State: CheckNotEvaluated},
		{Name: "entry_history", State: CheckNotEvaluated},
		{Name: "merge_tree", State: CheckNotEvaluated},
		{Name: "non_conflict_preservation", State: CheckNotEvaluated},
	}}
	block := func(index int, kind BlockerKind, paths []string, cause error) (CompletionAssessment, error) {
		a.Checks[index].State = CheckBlocked
		a.Blocker = &CompletionBlocker{Kind: kind, Paths: sortedPaths(paths), Cause: cause}
		return a, nil
	}
	note, exists, err := state.LoadSyncNote()
	switch {
	case errors.Is(err, ErrSyncNoteCorrupt):
		return block(0, BlockerCorruptNote, nil, err)
	case err != nil:
		return CompletionAssessment{}, fmt.Errorf("read sync state: %w", err)
	case !exists:
		return block(0, BlockerNoSync, nil, ErrNoSyncInProgress)
	case !note.Target.Valid():
		return block(0, BlockerInvalidTarget, nil, fmt.Errorf("%w: the sync note records no usable merge target", ErrSyncNoteCorrupt))
	}
	note.Conflicts = sortedPaths(note.Conflicts)
	a.Note = &note
	a.Checks[0].State = CheckPassed
	paths, err := app.ScanWorktreeDocsForMarkers(ctx)
	if err != nil {
		return CompletionAssessment{}, fmt.Errorf("scan docs for conflict markers: %w", err)
	}
	if len(paths) > 0 {
		paths = sortedPaths(paths)
		return block(1, BlockerMarkers, paths, fmt.Errorf("%w: %s", ErrMarkersRemain, strings.Join(paths, ", ")))
	}
	a.Checks[1].State = CheckPassed
	clean, err := app.DocsClean(ctx)
	if err != nil {
		return CompletionAssessment{}, fmt.Errorf("read docs status: %w", err)
	}
	if !clean {
		return block(2, BlockerUncommitted, nil, ErrResolutionUncommitted)
	}
	a.Checks[2].State = CheckPassed
	a.Head, err = app.HeadCommit(ctx)
	if err != nil {
		return CompletionAssessment{}, fmt.Errorf("read HEAD: %w", err)
	}
	if note.EntryHead == "" {
		a.Checks[3].State = CheckNotApplicable
	} else {
		if a.Head != note.EntryHead {
			descends, historyErr := app.IsAncestor(ctx, note.EntryHead, a.Head)
			if historyErr != nil {
				return CompletionAssessment{}, fmt.Errorf("check whether HEAD descends from where the sync began: %w", historyErr)
			}
			if !descends {
				return block(3, BlockerForeignHistory, nil, fmt.Errorf("%w: it began at %s, and HEAD is %s", ErrContinueForeignHistory, shortOID(note.EntryHead), shortOID(a.Head)))
			}
		}
		a.Checks[3].State = CheckPassed
	}
	if note.MergedTree == "" {
		return block(4, BlockerMissingMergeTree, nil, ErrResolutionUnverifiable)
	}
	a.Checks[4].State = CheckPassed
	actual, err := app.WorktreeDocsTree(ctx)
	if err != nil {
		return CompletionAssessment{}, fmt.Errorf("hash worktree docs: %w", err)
	}
	changed, err := app.DocsTreeChangedPaths(ctx, note.MergedTree, actual)
	if err != nil {
		return CompletionAssessment{}, fmt.Errorf("compare the worktree with the merge result: %w", err)
	}
	comparison := &CompletionComparison{
		ExpectedTree: note.MergedTree, ActualTree: actual,
		AllowedChanges: []string{}, UnexpectedChanges: []string{},
	}
	for _, path := range sortedPaths(changed) {
		if _, allowed := slices.BinarySearch(note.Conflicts, path); allowed {
			comparison.AllowedChanges = append(comparison.AllowedChanges, path)
		} else {
			comparison.UnexpectedChanges = append(comparison.UnexpectedChanges, path)
		}
	}
	a.Comparison = comparison
	if len(comparison.UnexpectedChanges) > 0 {
		return block(5, BlockerNonConflictChanges, comparison.UnexpectedChanges,
			fmt.Errorf("%w: %s", ErrResolutionChangedNonConflicts, strings.Join(comparison.UnexpectedChanges, ", ")))
	}
	a.Checks[5].State = CheckPassed
	return a, nil
}

func sortedPaths(paths []string) []string {
	result := append([]string{}, paths...)
	slices.Sort(result)
	return slices.Compact(result)
}
