package docsync

import (
	"context"
	"fmt"
	"strings"

	"github.com/irootkernel/sanho/internal/domain/provenance"
)

// Port doubles for the orchestration tests.
//
// Everything below the git boundary — the merge itself, the docs
// checkout, marker detection, the base and note files — is covered
// against real git elsewhere (infra/appgit, and the flow suite in
// test/docsync). What these doubles stand in for is the *shape* of
// those answers, so that this package's own subject can be driven
// through every branch: guard ordering, the three base-resolution
// routes, --rebase-onto, the note lifecycle, and which writes happen in
// which order.
//
// All three doubles share one journal, so a test can assert not just
// that a write happened but that it happened after the write it depends
// on.

func commitOID(n int) string { return fmt.Sprintf("%040x", 0x1000+n) }
func treeOID(n int) string   { return fmt.Sprintf("%040x", 0x2000+n) }

// emptyTreeOID stands for the repository's empty tree, the merge base
// of a workspace with no recorded base.
var emptyTreeOID = fmt.Sprintf("%040x", 0)

type journal struct{ steps []string }

func (j *journal) record(step string) { j.steps = append(j.steps, step) }
func (j *journal) trace() string      { return strings.Join(j.steps, " ") }

type mergeCall struct{ base, ours, theirs string }

type fakeCanonical struct {
	*journal

	headCommit string
	headTree   string
	fetchErr   error
	// known lists commits that resolve in canonical, reachable those
	// that are also part of head's history, and byTree maps a docs tree
	// to the commit carrying it (re-anchoring).
	known     map[string]bool
	reachable map[string]bool
	byTree    map[string]string
}

func (c *fakeCanonical) Fetch(ctx context.Context) error {
	c.record("fetch")
	return c.fetchErr
}

func (c *fakeCanonical) Head(ctx context.Context) (string, string, error) {
	return c.headCommit, c.headTree, nil
}

func (c *fakeCanonical) FetchIntoApp(ctx context.Context) (string, error) {
	c.record("import")
	return c.headCommit, nil
}

func (c *fakeCanonical) ResolveCommit(ctx context.Context, oid string) (bool, error) {
	return c.known[oid], nil
}

func (c *fakeCanonical) IsAncestor(ctx context.Context, a, b string) (bool, error) {
	return c.reachable[a] && b == c.headCommit, nil
}

func (c *fakeCanonical) FindCommitByDocsTree(ctx context.Context, tree string) (string, bool, error) {
	commit, ok := c.byTree[tree]
	return commit, ok, nil
}

type fakeApp struct {
	*journal

	docsClean    bool
	headCommit   string
	headTree     string
	worktreeTree string
	emptyTree    string
	// commitTrees is the app-side object database: which commits have
	// been imported and what their root trees are.
	commitTrees map[string]string

	mergeTree      string
	mergeConflicts []string

	markerPaths []string

	// changedPaths are the docs paths DocsPathsChangedBetween reports as
	// different; diffErr makes the comparison itself fail.
	changedPaths map[string]bool
	diffErr      error
	// treeDiffs is the count DocsTreeDifferences reports for two
	// different trees, and ancestors is IsAncestor's answer table keyed
	// "a->b".
	treeDiffs   int
	ancestors   map[string]bool
	ancestryErr error

	commitOID string

	mergeCalls     []mergeCall
	checkedOut     []string
	restores       int
	commitMessages []string
	diffCalls      []diffCall
}

// diffCall records one DocsPathsChangedBetween invocation, so a test can
// assert which trees were compared over which paths.
type diffCall struct{ from, to, paths string }

func (a *fakeApp) DocsClean(ctx context.Context) (bool, error) {
	a.record("docs-clean")
	return a.docsClean, nil
}

func (a *fakeApp) HeadCommit(ctx context.Context) (string, error) { return a.headCommit, nil }

func (a *fakeApp) HeadDocsTree(ctx context.Context) (string, error) { return a.headTree, nil }

func (a *fakeApp) WorktreeDocsTree(ctx context.Context) (string, error) {
	return a.worktreeTree, nil
}

func (a *fakeApp) EmptyTree(ctx context.Context) (string, error) { return a.emptyTree, nil }

func (a *fakeApp) CommitTree(ctx context.Context, commit string) (string, error) {
	tree, ok := a.commitTrees[commit]
	if !ok {
		return "", fmt.Errorf("commit %s was never imported into the app repository", commit)
	}
	return tree, nil
}

func (a *fakeApp) MergeDocs(ctx context.Context, baseTree, oursTree, theirsTree string) (string, []string, bool, error) {
	a.record("merge")
	a.mergeCalls = append(a.mergeCalls, mergeCall{base: baseTree, ours: oursTree, theirs: theirsTree})
	if len(a.mergeConflicts) > 0 {
		return a.mergeTree, a.mergeConflicts, false, nil
	}
	return a.mergeTree, nil, true, nil
}

func (a *fakeApp) CheckoutDocsTree(ctx context.Context, tree string) error {
	a.record("checkout")
	a.checkedOut = append(a.checkedOut, tree)
	return nil
}

func (a *fakeApp) RestoreDocsFromHead(ctx context.Context) error {
	a.record("restore")
	a.restores++
	return nil
}

func (a *fakeApp) CommitDocs(ctx context.Context, message string) (string, error) {
	a.record("commit")
	a.commitMessages = append(a.commitMessages, message)
	return a.commitOID, nil
}

func (a *fakeApp) ScanWorktreeDocsForMarkers(ctx context.Context) ([]string, error) {
	a.record("scan")
	return a.markerPaths, nil
}

// DocsPathsChangedBetween stands in for `git diff-tree` limited to the
// note's conflicted paths. changedPaths is the set of paths this fake
// considers different between ANY two distinct trees, which is all the
// orchestration under test needs: whether the answer is yes or no, not
// how git arrives at it.
func (a *fakeApp) DocsPathsChangedBetween(ctx context.Context, fromTree, toTree string, paths []string) (bool, error) {
	a.record("diff-paths")
	a.diffCalls = append(a.diffCalls, diffCall{from: fromTree, to: toTree, paths: strings.Join(paths, ",")})
	if a.diffErr != nil {
		return false, a.diffErr
	}
	if fromTree == "" || toTree == "" || fromTree == toTree {
		return false, nil
	}
	for _, path := range paths {
		if a.changedPaths[path] {
			return true, nil
		}
	}
	return false, nil
}

// DocsTreeDifferences counts differing paths for the `--continue` drift
// report. The doubles model trees as opaque OIDs, so "different" is
// simply "not the same tree"; treeDiffs lets a test name a count.
func (a *fakeApp) DocsTreeDifferences(ctx context.Context, fromTree, toTree string) (int, error) {
	a.record("tree-diff")
	if fromTree == "" || toTree == "" || fromTree == toTree {
		return 0, nil
	}
	if a.treeDiffs != 0 {
		return a.treeDiffs, nil
	}
	return 1, nil
}

// IsAncestor answers `--continue`'s history precondition. ancestors maps
// "a->b" to the answer; anything unlisted is "not an ancestor", which is
// the conservative reading and the one the refusal tests want.
func (a *fakeApp) IsAncestor(ctx context.Context, first, second string) (bool, error) {
	a.record("is-ancestor")
	if a.ancestryErr != nil {
		return false, a.ancestryErr
	}
	return a.ancestors[first+"->"+second], nil
}

type fakeState struct {
	*journal

	base    provenance.Base
	hasBase bool
	note    *SyncNote
	// noteErr is returned alongside the note, for the corrupt-file case
	// the StatePort contract reports as exists=true plus an error.
	noteErr error
	// saveBaseErr makes the base write fail, which is how the ordering
	// tests simulate a crash between the two writes of a completion.
	saveBaseErr error

	savedBases      []provenance.Base
	savedNotes      []SyncNote
	completionHeads []string
	noteCleared     int
}

func (s *fakeState) LoadBase() (provenance.Base, bool, error) {
	return s.base, s.hasBase, nil
}

func (s *fakeState) SaveBase(ctx context.Context, base provenance.Base) error {
	s.record("save-base")
	if s.saveBaseErr != nil {
		return s.saveBaseErr
	}
	s.savedBases = append(s.savedBases, base)
	s.base, s.hasBase = base, true
	return nil
}

// SaveSyncTargetBase is the completion writer. The double records the
// entry head it was offered, so a test can assert that Continue hands
// the guard the note's own evidence rather than nothing.
func (s *fakeState) SaveSyncTargetBase(ctx context.Context, base provenance.Base, entryHead string) error {
	s.record("save-sync-target-base")
	s.completionHeads = append(s.completionHeads, entryHead)
	if s.saveBaseErr != nil {
		return s.saveBaseErr
	}
	s.savedBases = append(s.savedBases, base)
	s.base, s.hasBase = base, true
	return nil
}

func (s *fakeState) ClearBase() error {
	s.record("clear-base")
	s.base, s.hasBase = provenance.Base{}, false
	return nil
}

func (s *fakeState) LoadSyncNote() (SyncNote, bool, error) {
	if s.noteErr != nil {
		return SyncNote{}, true, s.noteErr
	}
	if s.note == nil {
		return SyncNote{}, false, nil
	}
	return *s.note, true, nil
}

func (s *fakeState) SaveSyncNote(note SyncNote) error {
	s.record("save-note")
	s.savedNotes = append(s.savedNotes, note)
	s.note = &note
	return nil
}

func (s *fakeState) ClearSyncNote() error {
	s.record("clear-note")
	s.noteCleared++
	s.note = nil
	return nil
}

// fixture wires one scenario: canonical head is commit 1 / tree 1, the
// app repo has imported it, and the recorded base is commit 0 / tree 0.
type fixture struct {
	canonical *fakeCanonical
	app       *fakeApp
	state     *fakeState
	shared    *journal
}

func newFixture() *fixture {
	shared := &journal{}
	f := &fixture{
		shared: shared,
		canonical: &fakeCanonical{
			journal:    shared,
			headCommit: commitOID(1),
			headTree:   treeOID(1),
			known:      map[string]bool{commitOID(0): true, commitOID(1): true},
			reachable:  map[string]bool{commitOID(0): true, commitOID(1): true},
			byTree:     map[string]string{treeOID(0): commitOID(0), treeOID(1): commitOID(1)},
		},
		app: &fakeApp{
			journal:      shared,
			docsClean:    true,
			headCommit:   commitOID(7),
			headTree:     treeOID(0),
			worktreeTree: treeOID(0),
			emptyTree:    emptyTreeOID,
			commitTrees: map[string]string{
				commitOID(0): treeOID(0),
				commitOID(1): treeOID(1),
			},
			mergeTree: treeOID(9),
			commitOID: commitOID(9),
		},
		state: &fakeState{
			journal: shared,
			base:    provenance.Base{Commit: commitOID(0), Tree: treeOID(0)},
			hasBase: true,
		},
	}
	return f
}

func (f *fixture) useCase() *UseCase {
	return &UseCase{Canonical: f.canonical, App: f.app, State: f.state}
}
