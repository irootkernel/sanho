package publish

import (
	"context"
	"fmt"

	"github.com/irootkernel/sanho/internal/domain/provenance"
	pubdom "github.com/irootkernel/sanho/internal/domain/publish"
)

// Port doubles for the orchestration tests.
//
// Everything below the git boundary — merges, trees, CAS pushes, marker
// detection — is covered against real git in infra/canonical and
// infra/appgit (sanho-v0.2.md §9 rule 1). What these doubles stand in
// for is only the *shape* of those answers, so that this package's own
// subject — gate ordering, case dispatch, the retry budget, the
// base-advance rule and the error vocabulary — can be driven through
// states that real repositories reach rarely or only by racing.
//
// The canonical double is a model, not a stub: it keeps a branch, honors
// the compare-and-swap contract on push, and only ever accepts a commit
// it was asked to create.

func commitOID(n int) string { return fmt.Sprintf("%040x", 0x1000+n) }
func treeOID(n int) string   { return fmt.Sprintf("%040x", 0x2000+n) }

// commitPair is one canonical commit and the docs tree it carries.
type commitPair struct {
	commit string
	tree   string
}

type createdCommit struct {
	tree       string
	parent     string
	authorName string
	authorMail string
	message    string
}

type fakeCanonical struct {
	// branch is the published history, oldest first.
	branch []commitPair
	// detached commits resolve but are not reachable from the branch —
	// the "known base, wrong line of history" shape of case ④.
	detached []commitPair

	// fetchErr, when set, fails every Fetch (unreachable canonical).
	fetchErr error
	// onPush runs before each push (1-based count) so a test can model a
	// racing publisher landing first.
	onPush func(c *fakeCanonical, attempt int)

	// merge results returned by MergeDocs / MergeDocsTrees.
	mergeTree      string
	mergeResults   []string
	mergeConflicts []string
	mergeErr       error

	// docsCount answers DocsFileCount (the empty-publish refusal).
	docsCount    int
	docsCountErr error

	// recorded traffic
	fetches    int
	pushes     int
	published  []string
	imported   []string
	created    []createdCommit
	mergeCalls []string
	pushLeases []string
	nextOID    int
}

func (c *fakeCanonical) head() commitPair {
	if len(c.branch) == 0 {
		return commitPair{}
	}
	return c.branch[len(c.branch)-1]
}

// appendBranch lands a commit on the canonical branch, the way another
// publisher's successful push would.
func (c *fakeCanonical) appendBranch(commit, tree string) {
	c.branch = append(c.branch, commitPair{commit: commit, tree: tree})
}

func (c *fakeCanonical) Fetch(ctx context.Context) error {
	c.fetches++
	return c.fetchErr
}

// Head models infra/canonical's contract, including its distinguishable
// answer for a repository nothing has ever published into.
func (c *fakeCanonical) Head(ctx context.Context) (string, string, error) {
	head := c.head()
	if head.commit == "" {
		return "", "", fmt.Errorf("%w: main", pubdom.ErrEmptyBranch)
	}
	return head.commit, head.tree, nil
}

func (c *fakeCanonical) ResolveCommit(ctx context.Context, oid string) (bool, error) {
	if oid == "" {
		return false, nil
	}
	for _, pair := range append(append([]commitPair{}, c.branch...), c.detached...) {
		if pair.commit == oid {
			return true, nil
		}
	}
	return false, nil
}

func (c *fakeCanonical) IsAncestor(ctx context.Context, a, b string) (bool, error) {
	indexA, indexB := -1, -1
	for i, pair := range c.branch {
		if pair.commit == a {
			indexA = i
		}
		if pair.commit == b {
			indexB = i
		}
	}
	return indexA >= 0 && indexB >= 0 && indexA <= indexB, nil
}

func (c *fakeCanonical) FindCommitByDocsTree(ctx context.Context, tree string) (string, bool, error) {
	if tree == "" {
		return "", false, nil
	}
	for i := len(c.branch) - 1; i >= 0; i-- {
		if c.branch[i].tree == tree {
			return c.branch[i].commit, true, nil
		}
	}
	for i := len(c.detached) - 1; i >= 0; i-- {
		if c.detached[i].tree == tree {
			return c.detached[i].commit, true, nil
		}
	}
	return "", false, nil
}

func (c *fakeCanonical) FetchFromApp(ctx context.Context, tipOID string) error {
	c.imported = append(c.imported, tipOID)
	return nil
}

func (c *fakeCanonical) MergeDocs(ctx context.Context, baseCommit, oursTree, theirsCommit string) (string, []string, bool, error) {
	c.mergeCalls = append(c.mergeCalls, baseCommit+"|"+oursTree+"|"+theirsCommit)
	if c.mergeErr != nil {
		return "", nil, false, c.mergeErr
	}
	if len(c.mergeConflicts) > 0 {
		return c.mergeTree, c.mergeConflicts, false, nil
	}
	return c.mergeTree, nil, true, nil
}

// MergeDocsTrees models the tree-level merge the evaluation pass uses.
// Recorded calls carry trees on every position, which is what lets a
// test assert that the chain merged against the *accumulated* tree
// rather than against canonical's frozen head tree (F-C1).
//
// mergeResults, when populated, answers successive calls in order, so a
// multi-tip push can be given a distinct union tree per merge; otherwise
// mergeTree answers every call, as before.
func (c *fakeCanonical) MergeDocsTrees(ctx context.Context, baseTree, oursTree, theirsTree string) (string, []string, bool, error) {
	c.mergeCalls = append(c.mergeCalls, baseTree+"|"+oursTree+"|"+theirsTree)
	if c.mergeErr != nil {
		return "", nil, false, c.mergeErr
	}
	if len(c.mergeConflicts) > 0 {
		return c.mergeTree, c.mergeConflicts, false, nil
	}
	if len(c.mergeResults) > 0 {
		tree := c.mergeResults[0]
		c.mergeResults = c.mergeResults[1:]
		return tree, nil, true, nil
	}
	return c.mergeTree, nil, true, nil
}

// DocsTreeOfCommit answers from the modeled branch and detached commits.
func (c *fakeCanonical) DocsTreeOfCommit(ctx context.Context, commit string) (string, error) {
	if commit == "" {
		return "", fmt.Errorf("no commit given")
	}
	for _, pair := range append(append([]commitPair{}, c.branch...), c.detached...) {
		if pair.commit == commit {
			return pair.tree, nil
		}
	}
	return "", fmt.Errorf("no tree recorded for %s", commit)
}

// DocsFileCount stands in for the canonical inventory the empty-publish
// refusal quotes (F-H2).
func (c *fakeCanonical) DocsFileCount(ctx context.Context, commit string) (int, error) {
	if c.docsCountErr != nil {
		return 0, c.docsCountErr
	}
	if commit == "" {
		return 0, nil
	}
	return c.docsCount, nil
}

func (c *fakeCanonical) CommitDocsTree(ctx context.Context, tree, parent, authorName, authorEmail, message string) (string, error) {
	c.created = append(c.created, createdCommit{
		tree:       tree,
		parent:     parent,
		authorName: authorName,
		authorMail: authorEmail,
		message:    message,
	})
	c.nextOID++
	return commitOID(0x900 + c.nextOID), nil
}

func (c *fakeCanonical) PushHead(ctx context.Context, newHead, expectedOld string) error {
	c.pushes++
	c.pushLeases = append(c.pushLeases, expectedOld)
	if c.onPush != nil {
		c.onPush(c, c.pushes)
	}
	if expectedOld != c.head().commit {
		return fmt.Errorf("%w: expected %s, canonical is at %s",
			pubdom.ErrNonFastForward, expectedOld, c.head().commit)
	}

	// Only a commit this canonical was asked to create may land, which
	// catches an orchestration bug that pushes a stale OID.
	for _, made := range c.created {
		if made.parent == expectedOld {
			c.appendBranch(newHead, made.tree)
			c.published = append(c.published, newHead)
			return nil
		}
	}
	return fmt.Errorf("push of an uncreated commit %s", newHead)
}

type fakeApp struct {
	docsTrees   map[string]string
	markerPaths map[string][]string
	markerErr   error
	subjects    map[string][]string
	repoName    string
	branch      string
	identityErr error
	worktree    string
	worktreeErr error
	emptyTree   string

	scanned       []string
	scanRanges    []string
	subjectCalls  []string
	worktreeCalls int
}

func (a *fakeApp) DocsTreeOf(ctx context.Context, commit string) (string, error) {
	tree, ok := a.docsTrees[commit]
	if !ok {
		return "", fmt.Errorf("no docs tree recorded for %s", commit)
	}
	return tree, nil
}

func (a *fakeApp) ScanDocsBlobsAgainst(ctx context.Context, publishedDocsTree, commit string) ([]string, error) {
	a.scanned = append(a.scanned, commit)
	a.scanRanges = append(a.scanRanges, publishedDocsTree+".."+commit)
	if a.markerErr != nil {
		return nil, a.markerErr
	}
	return a.markerPaths[commit], nil
}

func (a *fakeApp) DocsCommitSubjects(ctx context.Context, base, tip string) ([]string, error) {
	a.subjectCalls = append(a.subjectCalls, base+".."+tip)
	return a.subjects[base+".."+tip], nil
}

func (a *fakeApp) RepoIdentity(ctx context.Context) (string, string, error) {
	if a.identityErr != nil {
		return "", "", a.identityErr
	}
	return a.repoName, a.branch, nil
}

// EmptyTree stands in for the app repository's empty-tree OID. The
// default is a recognizable sentinel so a test that never sets it can
// still tell "the empty tree" apart from any tree it did register.
func (a *fakeApp) EmptyTree(ctx context.Context) (string, error) {
	if a.emptyTree == "" {
		return "tree-empty", nil
	}
	return a.emptyTree, nil
}

func (a *fakeApp) WorktreeDocsTree(ctx context.Context) (string, error) {
	a.worktreeCalls++
	if a.worktreeErr != nil {
		return "", a.worktreeErr
	}
	return a.worktree, nil
}

type fakeState struct {
	base     provenance.Base
	hasBase  bool
	loadErr  error
	saveErr  error
	inSync   bool
	syncErr  error
	saved    []provenance.Base
	loadCall int
}

func (s *fakeState) LoadBase() (provenance.Base, bool, error) {
	s.loadCall++
	return s.base, s.hasBase, s.loadErr
}

func (s *fakeState) SaveBase(base provenance.Base) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	s.saved = append(s.saved, base)
	return nil
}

func (s *fakeState) SyncInProgress() (bool, error) { return s.inSync, s.syncErr }
