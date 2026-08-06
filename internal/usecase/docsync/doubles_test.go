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
	headTree     string
	worktreeTree string
	emptyTree    string
	// commitTrees is the app-side object database: which commits have
	// been imported and what their root trees are.
	commitTrees map[string]string

	mergeTree      string
	mergeConflicts []string

	markerPaths []string

	commitOID string

	mergeCalls     []mergeCall
	checkedOut     []string
	restores       int
	commitMessages []string
}

func (a *fakeApp) DocsClean(ctx context.Context) (bool, error) {
	a.record("docs-clean")
	return a.docsClean, nil
}

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

// noteRecord is one saved sync note.
type noteRecord struct{ prev, target provenance.Base }

type fakeState struct {
	*journal

	base    provenance.Base
	hasBase bool
	note    *noteRecord

	savedBases  []provenance.Base
	savedNotes  []noteRecord
	noteCleared int
}

func (s *fakeState) LoadBase() (provenance.Base, bool, error) {
	return s.base, s.hasBase, nil
}

func (s *fakeState) SaveBase(base provenance.Base) error {
	s.record("save-base")
	s.savedBases = append(s.savedBases, base)
	s.base, s.hasBase = base, true
	return nil
}

func (s *fakeState) ClearBase() error {
	s.record("clear-base")
	s.base, s.hasBase = provenance.Base{}, false
	return nil
}

func (s *fakeState) LoadSyncNote() (provenance.Base, provenance.Base, bool, error) {
	if s.note == nil {
		return provenance.Base{}, provenance.Base{}, false, nil
	}
	return s.note.prev, s.note.target, true, nil
}

func (s *fakeState) SaveSyncNote(prev, target provenance.Base) error {
	s.record("save-note")
	record := noteRecord{prev: prev, target: target}
	s.savedNotes = append(s.savedNotes, record)
	s.note = &record
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
