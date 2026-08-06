// Package docsync orchestrates the consume/reconcile flows of sanho
// v0.2 (sanho-v0.2.md §5.5): `sanho sync`, `sanho sync --abort`,
// `sanho sync --rebase-onto`, and `sanho pull`. Mechanics live behind
// ports implemented by infra (canonical, wsstate, app-repo git);
// decisions live in domain.
package docsync

import (
	"context"

	"github.com/irootkernel/sanho/internal/domain/provenance"
)

// CanonicalPort is the slice of canonical-clone behavior sync needs.
type CanonicalPort interface {
	Fetch(ctx context.Context) error
	Head(ctx context.Context) (commit, tree string, err error)
	// FetchIntoApp imports canonical head into the app repo's object
	// database and returns its commit OID.
	FetchIntoApp(ctx context.Context) (headCommit string, err error)
	ResolveCommit(ctx context.Context, oid string) (bool, error)
	FindCommitByDocsTree(ctx context.Context, tree string) (commit string, found bool, err error)
}

// AppRepoPort is the slice of app-repository behavior sync needs. All
// operations are docs-pathspec-scoped; nothing outside the docs dir is
// ever touched (§5.5 step 1 requirement).
type AppRepoPort interface {
	// DocsClean reports whether worktree and index are clean for the
	// docs paths relative to HEAD.
	DocsClean(ctx context.Context) (bool, error)
	// HeadDocsTree returns HEAD's docs tree OID ("" for unborn HEAD).
	HeadDocsTree(ctx context.Context) (string, error)
	// MergeDocs runs the §5.4 tree merge in the app repo and returns the
	// result tree, conflict list, and cleanliness.
	MergeDocs(ctx context.Context, baseCommit, oursTree, theirsCommit string) (tree string, conflicts []string, clean bool, err error)
	// CheckoutDocsTree materializes tree into the docs worktree and
	// index (docs paths only).
	CheckoutDocsTree(ctx context.Context, tree string) error
	// RestoreDocsFromHead resets docs worktree+index to HEAD (abort).
	RestoreDocsFromHead(ctx context.Context) error
	// CommitDocs creates the user's `docs: sync to <oid>` commit
	// restricted to the docs pathspec; returns the new commit OID.
	CommitDocs(ctx context.Context, message string) (string, error)
}

// StatePort is workspace-local state (wsstate) as sync needs it.
type StatePort interface {
	LoadBase() (provenance.Base, bool, error)
	SaveBase(provenance.Base) error
	LoadSyncNote() (prev provenance.Base, target provenance.Base, exists bool, err error)
	SaveSyncNote(prev, target provenance.Base) error
	ClearSyncNote() error
}

// Status is the outcome class of a sync run.
type Status int

const (
	StatusUpToDate  Status = iota
	StatusSynced           // clean merge committed
	StatusConflicts        // markers materialized; resolution pending
)

// Result reports one sync run.
type Result struct {
	Status Status
	// NewBase is the adopted base (canonical head, or --rebase-onto
	// target) when Status != StatusUpToDate.
	NewBase provenance.Base
	// Conflicts lists conflicted docs paths when StatusConflicts.
	Conflicts []string
	// CommitOID is the created sync commit when StatusSynced.
	CommitOID string
}

// UseCase wires the ports.
type UseCase struct {
	Canonical CanonicalPort
	App       AppRepoPort
	State     StatePort
}

// Options control a run.
type Options struct {
	// RebaseOnto overrides the merge target with an explicit canonical
	// commit (rewrite recovery, §5.5 step 8). Empty = canonical head.
	RebaseOnto string
}

// Run executes §5.5 steps 1–6. Fail-closed guards (dirty docs, sync
// already in progress, canonical unreachable) return errors carrying
// the §5.9 guidance codes.
func (u *UseCase) Run(ctx context.Context, opts Options) (Result, error) {
	panic("unimplemented (sanho v0.2 P3)")
}

// Abort executes §5.5 step 7. Valid whenever a sync note exists; by
// construction it cannot fail after its preconditions pass (guidance
// closure, D3).
func (u *UseCase) Abort(ctx context.Context) error {
	panic("unimplemented (sanho v0.2 P3)")
}

// Pull executes `sanho pull` (§5.5): fast-forward-only consume.
// Refuses when local docs are edited relative to the base (directs to
// sync). withCommit records the update as a sync-style commit.
func (u *UseCase) Pull(ctx context.Context, withCommit bool) (Result, error) {
	panic("unimplemented (sanho v0.2 P3)")
}
