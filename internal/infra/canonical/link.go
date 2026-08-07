package canonical

import (
	"context"
	"fmt"

	"github.com/irootkernel/sanho/internal/infra/gitx"
)

// Link binds a Store to one application repository and exposes the
// composite operations the use cases ask for, so that the flow layer
// never has to carry a git-dir path around: it satisfies
// usecase/publish.CanonicalPort and the canonical half of
// usecase/docsync.CanonicalPort by shape.
//
// The adapter lives here rather than in the use case because the
// architecture rules forbid a usecase package from importing infra; the
// wiring happens in interface/cli, which may see both.
type Link struct {
	store     *Store
	appGitDir string
}

// NewLink binds store to the application repository whose git dir is
// appGitDir (`git rev-parse --git-dir`, i.e. the worktree-private dir,
// not the common dir — object exchange targets either equivalently).
func NewLink(store *Store, appGitDir string) *Link {
	return &Link{store: store, appGitDir: appGitDir}
}

// Store exposes the underlying clone for the read-only rendering paths
// (data age, branch, directory) that status and doctor need.
func (l *Link) Store() *Store { return l.store }

func (l *Link) Fetch(ctx context.Context) error { return l.store.Fetch(ctx) }

func (l *Link) Head(ctx context.Context) (commit, tree string, err error) {
	return l.store.Head(ctx)
}

func (l *Link) ResolveCommit(ctx context.Context, oid string) (bool, error) {
	return l.store.ResolveCommit(ctx, oid)
}

func (l *Link) IsAncestor(ctx context.Context, a, b string) (bool, error) {
	return l.store.IsAncestor(ctx, a, b)
}

func (l *Link) FindCommitByDocsTree(ctx context.Context, tree string) (string, bool, error) {
	return l.store.FindCommitByDocsTree(ctx, tree)
}

// FetchFromApp imports the pushed tip so its docs tree is addressable
// clone-side (§5.3 step 5).
func (l *Link) FetchFromApp(ctx context.Context, tipOID string) error {
	return l.store.FetchFromApp(ctx, l.appGitDir, tipOID)
}

// FetchIntoApp imports canonical head into the app repo's object
// database and returns its commit OID (§5.5 step 2).
func (l *Link) FetchIntoApp(ctx context.Context) (string, error) {
	return l.store.FetchIntoApp(ctx, l.appGitDir)
}

// MergeDocs runs the §5.4 merge clone-side over the docs trees of
// baseCommit and theirsCommit and the already-resolved oursTree (the
// pushed tip's docs tree, imported by FetchFromApp).
func (l *Link) MergeDocs(ctx context.Context, baseCommit, oursTree, theirsCommit string) (tree string, conflicts []string, clean bool, err error) {
	baseTree, err := l.store.docsTreeOf(ctx, baseCommit)
	if err != nil {
		return "", nil, false, err
	}
	theirsTree, err := l.store.docsTreeOf(ctx, theirsCommit)
	if err != nil {
		return "", nil, false, err
	}
	return l.MergeDocsTrees(ctx, baseTree, oursTree, theirsTree)
}

// MergeDocsTrees is the §5.4 merge over three already-resolved docs
// trees.
//
// It exists because publication's evaluation pass chains: the second
// tip of a multi-ref push merges against the tree the first tip *would*
// publish, and that tree has no commit yet. An empty baseTree means "no
// shared history" and resolves to the empty tree, which is the honest
// ancestor when canonical has never been published into.
func (l *Link) MergeDocsTrees(ctx context.Context, baseTree, oursTree, theirsTree string) (tree string, conflicts []string, clean bool, err error) {
	if baseTree == "" {
		if baseTree, err = l.store.EmptyTree(ctx); err != nil {
			return "", nil, false, err
		}
	}
	result, err := MergeTree(ctx, l.store.dir, baseTree, oursTree, theirsTree)
	if err != nil {
		return "", nil, false, err
	}
	return result.Tree, result.Conflicts, result.Clean, nil
}

// DocsTreeOfCommit resolves a canonical commit's docs tree (its root
// tree — canonical is docs-only).
func (l *Link) DocsTreeOfCommit(ctx context.Context, commit string) (string, error) {
	return l.store.docsTreeOf(ctx, commit)
}

// DocsFileCount reports how many files a canonical commit publishes.
func (l *Link) DocsFileCount(ctx context.Context, commit string) (int, error) {
	return l.store.DocsFileCount(ctx, commit)
}

func (l *Link) CommitDocsTree(ctx context.Context, tree, parent, authorName, authorEmail, message string) (string, error) {
	return l.store.CommitDocsTree(ctx, tree, parent, authorName, authorEmail, message)
}

func (l *Link) PushHead(ctx context.Context, newHead, expectedOld string) error {
	return l.store.PushHead(ctx, newHead, expectedOld)
}

// docsTreeOf resolves a canonical commit's docs tree. The canonical
// repository is docs-only, so that is its root tree (§5.3 case ②).
func (s *Store) docsTreeOf(ctx context.Context, commit string) (string, error) {
	tree, err := gitx.New(s.dir).Line(ctx, "rev-parse", "--verify", commit+"^{tree}")
	if err != nil {
		return "", fmt.Errorf("canonical: resolve docs tree of %s: %w", commit, err)
	}
	return tree, nil
}
