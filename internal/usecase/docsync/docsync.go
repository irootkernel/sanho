// Package docsync orchestrates the consume/reconcile flows of sanho
// v0.2 (sanho-v0.2.md §5.5): `sanho sync`, `sanho sync --abort`,
// `sanho sync --rebase-onto`, and `sanho pull`. Mechanics live behind
// ports implemented by infra (canonical, wsstate, app-repo git);
// decisions live in domain.
package docsync

import (
	"context"
	"errors"
	"fmt"

	"github.com/irootkernel/sanho/internal/domain/provenance"
)

// CanonicalPort is the slice of canonical-clone behavior sync needs.
type CanonicalPort interface {
	Fetch(ctx context.Context) error
	Head(ctx context.Context) (commit, tree string, err error)
	// FetchIntoApp imports canonical head into the app repo's object
	// database and returns its commit OID. Everything reachable from
	// canonical head comes with it, which is what lets the app repo
	// resolve older canonical commits (a --rebase-onto target, the
	// recorded base) without a second import.
	FetchIntoApp(ctx context.Context) (headCommit string, err error)
	ResolveCommit(ctx context.Context, oid string) (bool, error)
	// IsAncestor reports whether a is b or an ancestor of it. Sync uses
	// it against canonical head, which is exactly the set FetchIntoApp
	// imports: a base off that line of history has no objects app-side
	// and must be re-anchored rather than merged from.
	IsAncestor(ctx context.Context, a, b string) (bool, error)
	FindCommitByDocsTree(ctx context.Context, tree string) (commit string, found bool, err error)
}

// AppRepoPort is the slice of app-repository behavior sync needs. All
// mutating operations are docs-pathspec-scoped; nothing outside the
// docs dir is ever touched (§5.5 step 1 requirement).
type AppRepoPort interface {
	// DocsClean reports whether worktree and index are clean for the
	// docs paths relative to HEAD.
	DocsClean(ctx context.Context) (bool, error)
	// HeadDocsTree returns HEAD's docs tree OID (the empty tree for an
	// unborn HEAD or an absent docs dir).
	HeadDocsTree(ctx context.Context) (string, error)
	// WorktreeDocsTree returns the docs tree the working files currently
	// hash to — `sanho pull`'s "are there local docs edits" test.
	WorktreeDocsTree(ctx context.Context) (string, error)
	// EmptyTree returns this repository's empty-tree OID, the merge base
	// used when no base is recorded (see resolveBaseTree).
	EmptyTree(ctx context.Context) (string, error)
	// CommitTree returns the root tree of a commit in the app repo's
	// object database. Canonical commits are docs-only, so after
	// FetchIntoApp their root tree is the docs tree sync merges with.
	CommitTree(ctx context.Context, commit string) (string, error)
	// MergeDocs runs the §5.4 tree merge in the app repo and returns the
	// result tree, conflict list, and cleanliness.
	MergeDocs(ctx context.Context, baseTree, oursTree, theirsTree string) (tree string, conflicts []string, clean bool, err error)
	// CheckoutDocsTree materializes tree into the docs worktree and
	// index (docs paths only).
	CheckoutDocsTree(ctx context.Context, tree string) error
	// RestoreDocsFromHead resets docs worktree+index to HEAD (abort).
	RestoreDocsFromHead(ctx context.Context) error
	// CommitDocs creates the user's `docs: sync to <oid>` commit
	// restricted to the docs pathspec; returns the new commit OID.
	CommitDocs(ctx context.Context, message string) (string, error)
	// ScanWorktreeDocsForMarkers reports docs worktree files that still
	// carry conflict markers (§5.4 detector).
	ScanWorktreeDocsForMarkers(ctx context.Context) ([]string, error)
}

// StatePort is workspace-local state (wsstate) as sync needs it.
type StatePort interface {
	LoadBase() (provenance.Base, bool, error)
	SaveBase(provenance.Base) error
	// ClearBase removes the base file, restoring the "no base recorded"
	// state. Abort needs it because a workspace can enter a sync with no
	// base at all, and a zero Base is not a writable value: the base
	// file schema (§5.7) has no representation for an empty commit OID
	// and reading one back is a corruption error.
	ClearBase() error
	LoadSyncNote() (prev provenance.Base, target provenance.Base, exists bool, err error)
	SaveSyncNote(prev, target provenance.Base) error
	ClearSyncNote() error
}

// Status is the outcome class of a sync run.
type Status int

const (
	StatusUpToDate  Status = iota
	StatusSynced           // docs reconciled; base advanced
	StatusConflicts        // markers materialized; resolution pending
)

// String renders the status for diagnostics and JSON output.
func (s Status) String() string {
	switch s {
	case StatusUpToDate:
		return "up_to_date"
	case StatusSynced:
		return "synced"
	case StatusConflicts:
		return "conflicts"
	default:
		return "status(" + fmt.Sprint(int(s)) + ")"
	}
}

// Result reports one sync, pull, or abort-free run.
type Result struct {
	Status Status
	// NewBase is the adopted base (canonical head, or --rebase-onto
	// target). It is also set for StatusUpToDate, where it repeats the
	// base already recorded, so renderers always have an OID to name.
	NewBase provenance.Base
	// Conflicts lists conflicted docs paths when StatusConflicts.
	Conflicts []string
	// CommitOID is the created sync commit when one was made. It is
	// empty on a StatusSynced run whose merge result equals HEAD's docs
	// tree — the base moved but the docs did not, so there is nothing to
	// commit and `git commit` would fail rather than record an empty
	// change.
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

// Guidance sentinels. Each maps to one §5.8 machine error code so the
// CLI can route on errors.Is without reading messages:
//
//	ErrSyncInProgress   → sync_in_progress
//	ErrNoSyncInProgress → sync_in_progress (the absent half)
//	ErrDocsDirty        → sync_required
//	ErrPullNeedsSync    → sync_required
//	ErrUnknownBase      → history_rewritten
//	ErrUnknownTarget    → history_rewritten
//
// Canonical-unreachable is not redefined here: the port surfaces
// domain/publish.ErrUnreachable and this package wraps it unchanged, so
// errors.Is still recognizes it after the added context.
var (
	ErrSyncInProgress   = errors.New("a conflicted sync is in progress")
	ErrNoSyncInProgress = errors.New("no sync is in progress")
	ErrDocsDirty        = errors.New("docs have uncommitted changes")
	ErrUnknownBase      = errors.New("the recorded docs base is unknown to the canonical repository")
	ErrUnknownTarget    = errors.New("the requested target is not a canonical commit")
	ErrPullNeedsSync    = errors.New("local docs have changes that 'sanho pull' cannot fast-forward")
)

// syncCommitPrefix is the fixed subject of the commit sync and
// `pull --commit` create (§5.5 steps 5 and the pull contract). It is a
// machine-readable convention, so the OID width is fixed too.
const syncCommitPrefix = "docs: sync to "

// shortOIDWidth is §5.9's "OIDs shortened to 12 chars".
const shortOIDWidth = 12

// Run executes §5.5 steps 1–6.
//
// The guards run in a fixed order, and the order is load-bearing: a
// conflicted sync leaves the docs worktree dirty by construction, so
// checking the sync note first is what makes the state report itself as
// "finish or abort the sync" instead of the useless "commit your docs
// changes".
//
// A conflicted merge is **not** an error. It returns
// (Result{Status: StatusConflicts, Conflicts: …}, nil): the run did
// exactly what it was asked to do, the markers are in the worktree, and
// the CLI renders §5.9 template 2 from the Result. Errors are reserved
// for states in which sync did nothing.
func (u *UseCase) Run(ctx context.Context, opts Options) (Result, error) {
	// Step 1 — a sync already owns the docs worktree.
	notePrev, noteTarget, noteExists, err := u.State.LoadSyncNote()
	if err != nil {
		return Result{}, fmt.Errorf("read sync state: %w", err)
	}
	if noteExists {
		return Result{}, fmt.Errorf("%w: syncing %s to %s; resolve the markers and commit, or run 'sanho sync --abort'",
			ErrSyncInProgress, shortOID(notePrev.Commit), shortOID(noteTarget.Commit))
	}

	// Step 1 (continued) — docs must be clean. Sync runs at user pace,
	// so asking is acceptable; this requirement is what lets v0.2 delete
	// the layer-preservation machinery entirely.
	clean, err := u.App.DocsClean(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("read docs status: %w", err)
	}
	if !clean {
		return Result{}, fmt.Errorf("%w: commit or stash your docs changes first", ErrDocsDirty)
	}

	// Step 2 — fetch, then import canonical objects into the app repo so
	// the merge and the checkout can both run app-side. Write paths fail
	// closed on an unreachable canonical (§5.2); the port's
	// ErrUnreachable travels up intact.
	if err := u.Canonical.Fetch(ctx); err != nil {
		return Result{}, fmt.Errorf("refresh canonical repository: %w", err)
	}
	if _, err := u.Canonical.FetchIntoApp(ctx); err != nil {
		return Result{}, fmt.Errorf("import canonical objects: %w", err)
	}

	head, target, targetTree, err := u.resolveTarget(ctx, opts.RebaseOnto)
	if err != nil {
		return Result{}, err
	}
	newBase := provenance.Base{Commit: target, Tree: targetTree}

	base, hasBase, err := u.State.LoadBase()
	if err != nil {
		return Result{}, fmt.Errorf("read base file: %w", err)
	}
	oursTree, err := u.App.HeadDocsTree(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("read the docs tree of HEAD: %w", err)
	}

	// Step 3 — nothing to reconcile and nothing to record.
	if hasBase && base.Commit == target && oursTree == targetTree {
		return Result{Status: StatusUpToDate, NewBase: newBase}, nil
	}

	// Step 4 — the three-way merge, in the app repo.
	baseTree, err := u.resolveBaseTree(ctx, base, hasBase, head)
	if err != nil {
		return Result{}, err
	}
	mergedTree, conflicts, mergeClean, err := u.App.MergeDocs(ctx, baseTree, oursTree, targetTree)
	if err != nil {
		return Result{}, fmt.Errorf("merge docs with canonical: %w", err)
	}

	// Step 6 — conflicted.
	//
	// Ordering: the markers go into the worktree first, then the note,
	// then the base. The note is the record that a resolution is owed,
	// and the base moves to the target because the resolution — once
	// made — derives from the target (§5.5 step 6), which is what makes
	// the eventual resolution commit stamp the right docs-base. Writing
	// the note before the base means a crash between them leaves the
	// abort path fully armed with the still-current previous base.
	if !mergeClean {
		if err := u.App.CheckoutDocsTree(ctx, mergedTree); err != nil {
			return Result{}, fmt.Errorf("write the merged docs: %w", err)
		}
		if err := u.State.SaveSyncNote(base, newBase); err != nil {
			return Result{}, fmt.Errorf("record the sync in progress: %w", err)
		}
		if err := u.State.SaveBase(newBase); err != nil {
			return Result{}, fmt.Errorf("record new base: %w", err)
		}
		return Result{Status: StatusConflicts, NewBase: newBase, Conflicts: conflicts}, nil
	}

	// Step 5 — clean.
	//
	// The merge can be clean and still change nothing: upstream's edits
	// were already present locally, or — the everyday case — the user
	// has local docs commits and canonical has not moved, so the merge
	// only re-adopts their own tree. There is then nothing for git to
	// commit (a pathspec commit of an unchanged tree fails rather than
	// records an empty change), so the docs are left alone and at most
	// the base pointer moves. When the base already names the target
	// too, nothing whatsoever happened and saying "synced" would be a
	// lie.
	if mergedTree == oursTree {
		if hasBase && base == newBase {
			return Result{Status: StatusUpToDate, NewBase: newBase}, nil
		}
		if err := u.State.SaveBase(newBase); err != nil {
			return Result{}, fmt.Errorf("record new base: %w", err)
		}
		return Result{Status: StatusSynced, NewBase: newBase}, nil
	}

	if err := u.App.CheckoutDocsTree(ctx, mergedTree); err != nil {
		return Result{}, fmt.Errorf("write the merged docs: %w", err)
	}
	if err := u.State.SaveBase(newBase); err != nil {
		return Result{}, fmt.Errorf("record new base: %w", err)
	}
	commit, err := u.App.CommitDocs(ctx, syncCommitMessage(target))
	if err != nil {
		return Result{}, fmt.Errorf("commit the docs sync: %w", err)
	}
	return Result{Status: StatusSynced, NewBase: newBase, CommitOID: commit}, nil
}

// Abort executes §5.5 step 7. Valid whenever a sync note exists; by
// construction it cannot fail after its precondition passes (guidance
// closure, D3) — it moves no ref, creates no commit, and touches only
// the docs worktree/index and two state files.
//
// The order is restore docs → restore base → clear the note, and it is
// chosen so that a crash anywhere in the middle leaves a re-runnable
// state: the note is the proof that an abort is still owed, so it is
// deleted last, and every step before it is idempotent (a checkout of
// HEAD's docs, and a write of a value read out of the note). Running
// `sanho sync --abort` again after an interruption simply redoes them.
func (u *UseCase) Abort(ctx context.Context) error {
	prev, _, exists, err := u.State.LoadSyncNote()
	if err != nil {
		return fmt.Errorf("read sync state: %w", err)
	}
	if !exists {
		return ErrNoSyncInProgress
	}

	if err := u.App.RestoreDocsFromHead(ctx); err != nil {
		return fmt.Errorf("restore the docs worktree: %w", err)
	}
	if err := u.restoreBase(prev); err != nil {
		return err
	}
	if err := u.State.ClearSyncNote(); err != nil {
		return fmt.Errorf("clear the sync note: %w", err)
	}
	return nil
}

// restoreBase puts the base file back the way the aborted sync found
// it, including the case where it found nothing at all.
func (u *UseCase) restoreBase(prev provenance.Base) error {
	if prev.IsZero() {
		if err := u.State.ClearBase(); err != nil {
			return fmt.Errorf("remove the base file: %w", err)
		}
		return nil
	}
	if err := u.State.SaveBase(prev); err != nil {
		return fmt.Errorf("restore the base file: %w", err)
	}
	return nil
}

// CompleteIfResolved clears the sync note once a conflicted sync has
// actually been resolved, and reports whether it did.
//
// Resolution is the standard git idiom — edit, `git add`, `git commit`
// — so nothing in this package observes it happening. The hooks call
// this afterwards (P3b) and it decides from the state alone: a note
// exists, no docs worktree file still carries markers, and the docs are
// clean relative to HEAD, i.e. the resolution has been committed rather
// than merely edited. Anything short of that returns false with no
// error and no state change, because "not finished yet" is not a
// failure.
func (u *UseCase) CompleteIfResolved(ctx context.Context) (bool, error) {
	_, _, exists, err := u.State.LoadSyncNote()
	if err != nil {
		return false, fmt.Errorf("read sync state: %w", err)
	}
	if !exists {
		return false, nil
	}

	conflicted, err := u.App.ScanWorktreeDocsForMarkers(ctx)
	if err != nil {
		return false, fmt.Errorf("scan docs for conflict markers: %w", err)
	}
	if len(conflicted) > 0 {
		return false, nil
	}

	clean, err := u.App.DocsClean(ctx)
	if err != nil {
		return false, fmt.Errorf("read docs status: %w", err)
	}
	if !clean {
		return false, nil
	}

	if err := u.State.ClearSyncNote(); err != nil {
		return false, fmt.Errorf("clear the sync note: %w", err)
	}
	return true, nil
}

// Pull executes `sanho pull` (§5.5): fast-forward-only consume. It
// refuses when local docs are edited relative to the base and points at
// sync; withCommit records the update as a sync-style commit.
func (u *UseCase) Pull(ctx context.Context, withCommit bool) (Result, error) {
	notePrev, noteTarget, noteExists, err := u.State.LoadSyncNote()
	if err != nil {
		return Result{}, fmt.Errorf("read sync state: %w", err)
	}
	if noteExists {
		return Result{}, fmt.Errorf("%w: syncing %s to %s; resolve the markers and commit, or run 'sanho sync --abort'",
			ErrSyncInProgress, shortOID(notePrev.Commit), shortOID(noteTarget.Commit))
	}

	if err := u.Canonical.Fetch(ctx); err != nil {
		return Result{}, fmt.Errorf("refresh canonical repository: %w", err)
	}
	if _, err := u.Canonical.FetchIntoApp(ctx); err != nil {
		return Result{}, fmt.Errorf("import canonical objects: %w", err)
	}

	head, target, targetTree, err := u.resolveTarget(ctx, "")
	if err != nil {
		return Result{}, err
	}

	base, hasBase, err := u.State.LoadBase()
	if err != nil {
		return Result{}, fmt.Errorf("read base file: %w", err)
	}
	if !hasBase {
		// Pull consumes from a known point; establishing one is exactly
		// what sync does, and it succeeds in this state (D3 guidance
		// closure).
		return Result{}, fmt.Errorf("%w: no docs base is recorded; run 'sanho sync' first", ErrPullNeedsSync)
	}
	baseTree, err := u.resolveBaseTree(ctx, base, true, head)
	if err != nil {
		return Result{}, err
	}

	// The base file answers "which canonical state do the worktree docs
	// derive from" (§5.7), so comparing the worktree tree against the
	// base tree is the whole of pull's precondition: equal means the
	// local docs are canonical content, unchanged, and may simply be
	// replaced.
	worktreeTree, err := u.App.WorktreeDocsTree(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("hash worktree docs: %w", err)
	}
	if worktreeTree != baseTree {
		return Result{}, fmt.Errorf("%w: local docs differ from base %s; run 'sanho sync' to reconcile them",
			ErrPullNeedsSync, shortOID(base.Commit))
	}

	newBase := provenance.Base{Commit: target, Tree: targetTree}
	if base.Commit == target && baseTree == targetTree {
		return Result{Status: StatusUpToDate, NewBase: newBase}, nil
	}

	headDocsTree, err := u.App.HeadDocsTree(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("read the docs tree of HEAD: %w", err)
	}
	if targetTree != worktreeTree {
		if err := u.App.CheckoutDocsTree(ctx, targetTree); err != nil {
			return Result{}, fmt.Errorf("write the canonical docs: %w", err)
		}
	}
	if err := u.State.SaveBase(newBase); err != nil {
		return Result{}, fmt.Errorf("record new base: %w", err)
	}

	// Committing is only possible when the pulled tree differs from
	// HEAD's; otherwise the pull moved the base past content HEAD
	// already carries and there is no change to record.
	if withCommit && targetTree != headDocsTree {
		commit, err := u.App.CommitDocs(ctx, syncCommitMessage(target))
		if err != nil {
			return Result{}, fmt.Errorf("commit the docs update: %w", err)
		}
		return Result{Status: StatusSynced, NewBase: newBase, CommitOID: commit}, nil
	}
	return Result{Status: StatusSynced, NewBase: newBase}, nil
}

// resolveTarget picks the canonical state to merge toward and reports
// canonical head alongside it. Head is always read, even when an
// explicit target overrides it, because it is what defines the set of
// objects FetchIntoApp imported — the fact resolveBaseTree decides on.
//
// The explicit --rebase-onto target must be a canonical commit: that is
// the check that matters, since merging toward something canonical has
// never published would record a base the next push cannot use. Its
// tree is resolved app-side, which also verifies the commit actually
// came over (it does for anything reachable from canonical head).
func (u *UseCase) resolveTarget(ctx context.Context, rebaseOnto string) (head, target, targetTree string, err error) {
	head, headTree, err := u.Canonical.Head(ctx)
	if err != nil {
		return "", "", "", fmt.Errorf("read canonical head: %w", err)
	}
	if rebaseOnto == "" {
		return head, head, headTree, nil
	}

	known, err := u.Canonical.ResolveCommit(ctx, rebaseOnto)
	if err != nil {
		return "", "", "", fmt.Errorf("resolve %s in canonical: %w", shortOID(rebaseOnto), err)
	}
	if !known {
		return "", "", "", fmt.Errorf("%w: %s", ErrUnknownTarget, shortOID(rebaseOnto))
	}
	tree, err := u.App.CommitTree(ctx, rebaseOnto)
	if err != nil {
		return "", "", "", fmt.Errorf("resolve the docs tree of %s: %w", shortOID(rebaseOnto), err)
	}
	return head, rebaseOnto, tree, nil
}

// resolveBaseTree answers "which canonical tree do the local docs
// derive from" — the merge base of §5.5 step 4.
//
// Three states, three answers:
//
//   - No base recorded at all: the **empty tree**. A three-way merge
//     against an empty base is the union of both sides' additions, with
//     a conflict only where both sides added the same path differently.
//     That is the right meaning of "these docs and canonical share no
//     recorded history", and it is what makes `sanho sync` genuinely
//     succeed in the no-base state that publication directs users to it
//     from (usecase/publish's ReasonNoBase) — guidance closure, D3.
//     The alternative, refusing, would leave that advice pointing at a
//     command that cannot work.
//
//   - Base commit reachable from canonical head: its tree, resolved
//     app-side. This covers the legacy `{commit, tree: ""}` base
//     (adopted from a v0.1 docs-version trailer) with no special case,
//     and it is deliberately preferred over the recorded tree field:
//     the merge runs in the app repository, so resolving there proves
//     the object is present where the merge will look for it, and a
//     stale tree field cannot poison the merge base. Reachability is
//     the right test rather than mere existence — the clone can still
//     hold the objects of a rewritten-away commit locally, but nothing
//     off canonical head's history was imported app-side.
//
//   - Base commit not reachable (history was rewritten): the recorded
//     docs-base-tree is the disaster anchor (D2). Finding a canonical
//     commit that still carries that tree both re-anchors the base and
//     proves the tree is reachable from canonical head — hence imported
//     by FetchIntoApp and usable as a merge base. With no recorded
//     tree, or no commit carrying it, there is nothing to anchor to and
//     the user is directed to `--rebase-onto`.
func (u *UseCase) resolveBaseTree(ctx context.Context, base provenance.Base, hasBase bool, head string) (string, error) {
	if !hasBase {
		tree, err := u.App.EmptyTree(ctx)
		if err != nil {
			return "", fmt.Errorf("resolve the empty tree: %w", err)
		}
		return tree, nil
	}

	reachable, err := u.baseIsReachable(ctx, base.Commit, head)
	if err != nil {
		return "", err
	}
	if reachable {
		tree, err := u.App.CommitTree(ctx, base.Commit)
		if err != nil {
			return "", fmt.Errorf("resolve the docs tree of base %s: %w", shortOID(base.Commit), err)
		}
		return tree, nil
	}

	if base.Tree == "" {
		return "", fmt.Errorf("%w: %s carries no docs-base-tree to re-anchor by; pick a canonical commit and run 'sanho sync --rebase-onto <commit>'",
			ErrUnknownBase, shortOID(base.Commit))
	}
	anchor, found, err := u.Canonical.FindCommitByDocsTree(ctx, base.Tree)
	if err != nil {
		return "", fmt.Errorf("search canonical history for docs tree %s: %w", shortOID(base.Tree), err)
	}
	if !found {
		return "", fmt.Errorf("%w: neither %s nor its docs tree %s is in canonical history; pick a canonical commit and run 'sanho sync --rebase-onto <commit>'",
			ErrUnknownBase, shortOID(base.Commit), shortOID(base.Tree))
	}
	tree, err := u.App.CommitTree(ctx, anchor)
	if err != nil {
		return "", fmt.Errorf("resolve the docs tree of re-anchored base %s: %w", shortOID(anchor), err)
	}
	return tree, nil
}

// baseIsReachable reports whether the recorded base commit is part of
// the history canonical head names, which is both "canonical still has
// it" and "its objects came over with FetchIntoApp".
func (u *UseCase) baseIsReachable(ctx context.Context, base, head string) (bool, error) {
	known, err := u.Canonical.ResolveCommit(ctx, base)
	if err != nil {
		return false, fmt.Errorf("resolve base %s in canonical: %w", shortOID(base), err)
	}
	if !known {
		return false, nil
	}
	ancestor, err := u.Canonical.IsAncestor(ctx, base, head)
	if err != nil {
		return false, fmt.Errorf("check whether base %s precedes canonical head: %w", shortOID(base), err)
	}
	return ancestor, nil
}

func syncCommitMessage(target string) string { return syncCommitPrefix + shortOID(target) }

// shortOID renders an OID for user-facing messages at the §5.9 width.
func shortOID(oid string) string {
	if oid == "" {
		return "(none)"
	}
	if len(oid) <= shortOIDWidth {
		return oid
	}
	return oid[:shortOIDWidth]
}
