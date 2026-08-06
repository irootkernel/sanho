// Package publish orchestrates the pre-push publication flow of sanho
// v0.2 (sanho-v0.2.md §5.3): gate checks, the four-case analysis
// (decided in domain/publish), canonical-side auto-merge, CAS push with
// bounded retry, and the base-advance rule. It never touches the app
// worktree, index, or refs (worktree inviolability).
package publish

import (
	"context"

	"github.com/irootkernel/sanho/internal/domain/provenance"
	pubdom "github.com/irootkernel/sanho/internal/domain/publish"
)

// RefUpdate is one line of the pre-push hook's stdin.
type RefUpdate struct {
	LocalRef  string
	LocalOID  string
	RemoteRef string
	RemoteOID string
}

// MaxCASRetries bounds the refetch-and-retry loop on lost races (§5.3
// step 5, case ②/③).
const MaxCASRetries = 3

// CanonicalPort is the canonical-clone behavior publication needs.
type CanonicalPort interface {
	Fetch(ctx context.Context) error
	Head(ctx context.Context) (commit, tree string, err error)
	ResolveCommit(ctx context.Context, oid string) (bool, error)
	IsAncestor(ctx context.Context, a, b string) (bool, error)
	FindCommitByDocsTree(ctx context.Context, tree string) (string, bool, error)
	// FetchFromApp imports the pushed tip so its docs tree is
	// addressable clone-side.
	FetchFromApp(ctx context.Context, tipOID string) error
	// MergeDocs runs the §5.4 merge clone-side.
	MergeDocs(ctx context.Context, baseCommit, oursTree, theirsCommit string) (tree string, conflicts []string, clean bool, err error)
	CommitDocsTree(ctx context.Context, tree, parent, authorName, authorEmail, message string) (string, error)
	// PushHead CAS-publishes; returns canonical.ErrNonFastForward on a
	// lost race.
	PushHead(ctx context.Context, newHead, expectedOld string) error
}

// AppRepoPort is the read-only app-repository access publication needs.
type AppRepoPort interface {
	// DocsTreeOf returns the docs tree OID of a commit (empty-tree OID
	// when the docs dir is absent).
	DocsTreeOf(ctx context.Context, commit string) (string, error)
	// ScanDocsBlobsForMarkers scans a commit's docs blobs (§5.4
	// detector); returns conflicted paths. Unreadable blobs error.
	ScanDocsBlobsForMarkers(ctx context.Context, commit string) ([]string, error)
	// DocsCommitSubjects lists subjects of commits since base that
	// touched docs, oldest first (canonical commit body).
	DocsCommitSubjects(ctx context.Context, base, tip string) ([]string, error)
	// RepoIdentity returns the app repo short name and current branch
	// for the canonical commit subject.
	RepoIdentity(ctx context.Context) (repoName, branch string, err error)
	// WorktreeDocsTree returns the current worktree docs tree OID (for
	// the base-advance rule).
	WorktreeDocsTree(ctx context.Context) (string, error)
}

// StatePort is the workspace state publication needs.
type StatePort interface {
	LoadBase() (provenance.Base, bool, error)
	SaveBase(provenance.Base) error
	SyncInProgress() (bool, error)
}

// Outcome reports one pre-push evaluation.
type Outcome struct {
	// Published is the new canonical head when a publication happened.
	Published string
	// Case records the decided case for the (deduplicated) tips.
	Case pubdom.Case
	// Conflicts lists conflicted paths when the push is rejected with
	// sync guidance.
	Conflicts []string
}

// UseCase wires the ports.
type UseCase struct {
	Canonical CanonicalPort
	App       AppRepoPort
	State     StatePort
	// Actor identity for canonical commits.
	ActorName   string
	ActorEmail  string
	WorkspaceID string
}

// Run evaluates the hook's ref updates per §5.3 steps 1–6. A nil error
// means the push may proceed; a returned error carries the §5.9
// guidance (sync required / markers present / canonical unreachable /
// history rewritten) and the hook exits non-zero without any remote
// ref changed.
func (u *UseCase) Run(ctx context.Context, updates []RefUpdate) (Outcome, error) {
	panic("unimplemented (sanho v0.2 P3)")
}
