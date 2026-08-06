// Package canonical manages the workspace-private clone of the
// canonical docs repository and all git operations against it
// (sanho-v0.2.md §5.2–§5.4): fetch policy and data-age, object exchange
// with the app repository, tree-level 3-way merges via
// `git merge-tree --write-tree`, and low-level publication (commit-tree
// + compare-and-swap push).
//
// Case decisions live in domain/publish; flow orchestration in
// usecase/publish and usecase/docsync. This package owns mechanics.
package canonical

import (
	"context"
	"errors"
	"time"
)

// ErrNonFastForward is the CAS-loss sentinel: origin rejected the push
// because its head moved. Callers refetch and retry (§5.3, ≤3 attempts).
var ErrNonFastForward = errors.New("canonical push rejected: non-fast-forward")

// ErrUnreachable wraps network failures reaching origin; write paths
// fail closed on it with the §5.9 message.
var ErrUnreachable = errors.New("canonical repository unreachable")

// Store is a handle on the private bare clone at
// <git-common-dir>/sanho/canonical.
type Store struct {
	dir    string // clone directory
	url    string // origin URL (from workspace config)
	branch string // resolved publication branch (main|master)
}

// Open returns a Store for an existing clone; Ensure creates the clone
// (and resolves the publication branch) when absent. appCommonDir is
// the app repo's `git rev-parse --git-common-dir`.
func Open(appCommonDir, url string) (*Store, error) {
	panic("unimplemented (sanho v0.2 P2)")
}

func Ensure(ctx context.Context, appCommonDir, url string) (*Store, error) {
	panic("unimplemented (sanho v0.2 P2)")
}

// Branch returns the resolved publication branch name; Dir the clone
// directory; URL the origin URL (all used by doctor/status rendering).
func (s *Store) Branch() string { return s.branch }
func (s *Store) Dir() string    { return s.dir }
func (s *Store) URL() string    { return s.url }

// Fetch updates the clone from origin (network runner). Records the
// fetch time for Age.
func (s *Store) Fetch(ctx context.Context) error {
	panic("unimplemented (sanho v0.2 P2)")
}

// Age returns how old the last successful fetch is; ok=false when the
// clone has never fetched.
func (s *Store) Age() (age time.Duration, ok bool) {
	panic("unimplemented (sanho v0.2 P2)")
}

// Head returns the last-fetched canonical head commit and its docs
// tree OID (for a docs-only canonical repo, the root tree).
func (s *Store) Head(ctx context.Context) (commit, tree string, err error) {
	panic("unimplemented (sanho v0.2 P2)")
}

// ResolveCommit reports whether oid exists in the clone, and
// IsAncestor whether a is an ancestor of (or equal to) b.
func (s *Store) ResolveCommit(ctx context.Context, oid string) (bool, error) {
	panic("unimplemented (sanho v0.2 P2)")
}

func (s *Store) IsAncestor(ctx context.Context, a, b string) (bool, error) {
	panic("unimplemented (sanho v0.2 P2)")
}

// Distance returns (behind, ahead) counts between a and b via
// rev-list --left-right --count, for status rendering.
func (s *Store) Distance(ctx context.Context, a, b string) (behind, ahead int, err error) {
	panic("unimplemented (sanho v0.2 P2)")
}

// FindCommitByDocsTree searches canonical history (newest first) for a
// commit whose docs tree equals tree — the docs-base-tree re-anchoring
// used for rewrite recovery (§5.3 case ④, D2).
func (s *Store) FindCommitByDocsTree(ctx context.Context, tree string) (commit string, found bool, err error) {
	panic("unimplemented (sanho v0.2 P2)")
}

// FetchFromApp imports ref (an OID or ref name) from the app repository
// into the clone's object database via local transport, making the app
// tip's docs trees addressable here (§5.2 object exchange).
func (s *Store) FetchFromApp(ctx context.Context, appGitDir, ref string) error {
	panic("unimplemented (sanho v0.2 P2)")
}

// FetchIntoApp imports the last-fetched canonical head into the app
// repository's object database and returns its OID, so merges and
// checkouts can run app-side (used by sync/pull).
func (s *Store) FetchIntoApp(ctx context.Context, appGitDir string) (headCommit string, err error) {
	panic("unimplemented (sanho v0.2 P2)")
}

// MergeResult is the outcome of a tree-level 3-way merge.
type MergeResult struct {
	// Tree is the result tree OID (contains conflict markers when not
	// Clean).
	Tree  string
	Clean bool
	// Conflicts lists conflicted paths relative to the docs root.
	Conflicts []string
}

// MergeTree runs `git merge-tree --write-tree` over three docs trees,
// wrapping them in synthetic parentless commits and labeling the sides
// sanho-ours / sanho-upstream (§5.4). repoDir chooses where to run
// (clone for publication, app repo for sync); all three trees must be
// present in that repo's object database.
func MergeTree(ctx context.Context, repoDir string, baseTree, oursTree, theirsTree string) (MergeResult, error) {
	panic("unimplemented (sanho v0.2 P2)")
}

// CommitDocsTree creates a canonical commit with the given docs tree,
// parent, author identity, and message; returns its OID. No push.
func (s *Store) CommitDocsTree(ctx context.Context, tree, parent, authorName, authorEmail, message string) (string, error) {
	panic("unimplemented (sanho v0.2 P2)")
}

// PushHead compare-and-swap publishes newHead to origin's publication
// branch, expecting the remote to still be at expectedOld. A lost race
// returns ErrNonFastForward; network failure wraps ErrUnreachable.
// Never force-pushes.
func (s *Store) PushHead(ctx context.Context, newHead, expectedOld string) error {
	panic("unimplemented (sanho v0.2 P2)")
}
