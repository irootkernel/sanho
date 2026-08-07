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
	pubdom "github.com/irootkernel/sanho/internal/domain/publish"
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
	// HeadCommit returns HEAD's commit OID, or "" for an unborn HEAD.
	// Sync records it when it materializes conflicts, so that "has this
	// been resolved?" can be answered by asking whether a commit
	// happened rather than only by looking at the worktree.
	HeadCommit(ctx context.Context) (string, error)
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
	// LoadSyncNote reports the note and whether one exists. A note that
	// exists but cannot be read is reported as exists=true with an error
	// satisfying errors.Is(err, ErrSyncNoteCorrupt); that pairing is what
	// lets Abort stay infallible over a damaged file.
	LoadSyncNote() (note SyncNote, exists bool, err error)
	SaveSyncNote(note SyncNote) error
	ClearSyncNote() error
}

// SyncNote is the record a conflicted sync leaves behind: what to
// restore on abort, what the resolution derives from, and where the
// worktree stood when the markers were written.
type SyncNote struct {
	// PrevBase is the base the sync found, restored by abort.
	PrevBase provenance.Base
	// Target is the canonical state the resolution derives from.
	Target provenance.Base
	// EntryHead and EntryDocsTree pin the app repo at materialize time.
	// A resolution is a commit, and a commit moves both; a stash, a
	// revert, or `git checkout HEAD -- docs` moves neither, which is
	// exactly the difference CompleteIfResolved has to see.
	EntryHead     string
	EntryDocsTree string
	// PreDatesEntryRecord marks a note written before the two fields
	// above existed. It cannot prove a resolution happened, so it is
	// read as unresolved.
	PreDatesEntryRecord bool
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
	// base already recorded, so renderers always have an OID to name —
	// with one exception: an empty canonical repository has no commit to
	// name, so a StatusUpToDate run against one leaves NewBase zero.
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
	ErrPullNeedsSync    = errors.New("local docs have changes a fast-forward cannot carry")
	// ErrRebaseOntoHealthy refuses --rebase-onto when the recorded base
	// is perfectly reachable and the target merely precedes it (F-M4).
	ErrRebaseOntoHealthy = errors.New("--rebase-onto targets an ancestor of a healthy base")
	// ErrSyncNoteCorrupt is the StatePort's contract for "a sync note is
	// there, and it cannot be read". It is declared here rather than
	// imported from infra because a use case may not see infra; the CLI
	// adapter translates the wsstate sentinel into this one.
	//
	// Every guard treats it as "a sync is in progress" — the note's
	// existence is the fact, not its contents — and routes the user to
	// `sanho sync --abort`, which succeeds over it.
	ErrSyncNoteCorrupt = errors.New("the record of the sync in progress is unreadable")
)

// Resolution classifies what CompleteIfResolved found, so that callers
// can tell "nothing to do" from "still owed" from "owed, and the user
// probably thinks it is finished".
type Resolution int

const (
	// ResolutionNoSync: no note; nothing was owed.
	ResolutionNoSync Resolution = iota
	// ResolutionPending: markers remain, or the resolution is edited but
	// not committed. The ordinary mid-resolution state.
	ResolutionPending
	// ResolutionNotCommitted: no markers and clean docs, but HEAD is
	// exactly where the sync left it — the conflict was put aside
	// (stashed, reverted, checked out) rather than resolved. The note is
	// kept and the state gets its own guidance.
	ResolutionNotCommitted
	// ResolutionCompleted: the note was cleared by this call.
	ResolutionCompleted
)

// String renders the resolution for diagnostics.
func (r Resolution) String() string {
	switch r {
	case ResolutionNoSync:
		return "no_sync"
	case ResolutionPending:
		return "pending"
	case ResolutionNotCommitted:
		return "not_committed"
	case ResolutionCompleted:
		return "completed"
	default:
		return "resolution(" + fmt.Sprint(int(r)) + ")"
	}
}

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
	if err := u.refuseWhileSyncing(); err != nil {
		return Result{}, err
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
	empty, err := u.canonicalEmpty(ctx)
	if err != nil {
		return Result{}, err
	}
	if empty {
		return noUpstreamYet(opts.RebaseOnto)
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
	baseTree, err := u.resolveBaseTree(ctx, base, hasBase, head, opts.RebaseOnto != "")
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
		// HEAD is read *before* the markers land so the note describes
		// the state the resolution has to move away from. Nothing here
		// moves HEAD, so reading it after would give the same answer —
		// but the note's meaning is "where this sync began", and taking
		// the reading at that point is what makes it so.
		entryHead, err := u.App.HeadCommit(ctx)
		if err != nil {
			return Result{}, fmt.Errorf("read HEAD: %w", err)
		}
		if err := u.App.CheckoutDocsTree(ctx, mergedTree); err != nil {
			return Result{}, fmt.Errorf("write the merged docs: %w", err)
		}
		note := SyncNote{
			PrevBase:      base,
			Target:        newBase,
			EntryHead:     entryHead,
			EntryDocsTree: oursTree,
		}
		if err := u.State.SaveSyncNote(note); err != nil {
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

// AbortResult reports one `sanho sync --abort`.
type AbortResult struct {
	// Degraded is set when the note existed but could not be read. The
	// abort still restored the docs and cleared the note — those need
	// only the note's existence — but the previous base lives *inside*
	// the note, so the base file was left where the conflicted sync put
	// it. `sanho doctor --fix` re-derives it from commit history.
	Degraded bool
}

// Abort executes §5.5 step 7. Valid whenever a sync note exists; by
// construction it cannot fail after its precondition passes (guidance
// closure, D3) — it moves no ref, creates no commit, and touches only
// the docs worktree/index and two state files.
//
// "Whenever a sync note exists" is meant literally, including when the
// note is unreadable. Abort is the way *out* of a broken sync state, so
// a damaged note is precisely the case it must survive: refusing there
// left a workspace with markers in docs/, a file nothing could parse,
// and no command that could clear either. Only the two facts the note
// carries are lost, so the base is left alone and the caller is told.
//
// The order is restore docs → restore base → clear the note, and it is
// chosen so that a crash anywhere in the middle leaves a re-runnable
// state: the note is the proof that an abort is still owed, so it is
// deleted last, and every step before it is idempotent (a checkout of
// HEAD's docs, and a write of a value read out of the note). Running
// `sanho sync --abort` again after an interruption simply redoes them.
func (u *UseCase) Abort(ctx context.Context) (AbortResult, error) {
	note, exists, err := u.State.LoadSyncNote()
	degraded := errors.Is(err, ErrSyncNoteCorrupt)
	switch {
	case err != nil && !degraded:
		return AbortResult{}, fmt.Errorf("read sync state: %w", err)
	case !exists:
		return AbortResult{}, ErrNoSyncInProgress
	}

	if err := u.App.RestoreDocsFromHead(ctx); err != nil {
		return AbortResult{}, fmt.Errorf("restore the docs worktree: %w", err)
	}
	if !degraded {
		if err := u.restoreBase(note.PrevBase); err != nil {
			return AbortResult{}, err
		}
	}
	if err := u.State.ClearSyncNote(); err != nil {
		return AbortResult{}, fmt.Errorf("clear the sync note: %w", err)
	}
	return AbortResult{Degraded: degraded}, nil
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
// actually been resolved, and reports what it found.
//
// Resolution is the standard git idiom — edit, `git add`, `git commit`
// — so nothing in this package observes it happening. The hooks call
// this afterwards (P3b) and it decides from the state alone.
//
// Three conditions, and the third is the one that matters. No worktree
// file still carries markers; the docs are clean relative to HEAD, so
// the resolution was committed rather than merely edited; and **a
// commit really happened** — HEAD has moved off where the sync left it,
// carrying a different docs tree.
//
// Without that third condition the test is passed by doing nothing at
// all. `git stash push -- docs` leaves no markers and clean docs, and so
// does `git checkout HEAD -- docs`; the note was then cleared while the
// conflict stood unresolved. That was not merely untidy: a conflicted
// sync has already advanced the base to the merge target, so the very
// next push saw base == canonical head, took it for a fast-forward, and
// republished the pre-merge tree — upstream's work reverted, exit 0, no
// message. The state that produces it is reported as
// ResolutionNotCommitted, the note is kept, and the caller says so.
//
// Requiring the docs *tree* to have moved as well as HEAD is what keeps
// an unrelated commit from standing in for the resolution. A genuine
// resolution cannot leave the docs tree unchanged: the markers are in
// the worktree, so an unchanged tree means nothing was staged and git
// refuses the commit outright.
func (u *UseCase) CompleteIfResolved(ctx context.Context) (Resolution, error) {
	note, exists, err := u.State.LoadSyncNote()
	if err != nil {
		return ResolutionNoSync, fmt.Errorf("read sync state: %w", err)
	}
	if !exists {
		return ResolutionNoSync, nil
	}

	conflicted, err := u.App.ScanWorktreeDocsForMarkers(ctx)
	if err != nil {
		return ResolutionNoSync, fmt.Errorf("scan docs for conflict markers: %w", err)
	}
	if len(conflicted) > 0 {
		return ResolutionPending, nil
	}

	clean, err := u.App.DocsClean(ctx)
	if err != nil {
		return ResolutionNoSync, fmt.Errorf("read docs status: %w", err)
	}
	if !clean {
		return ResolutionPending, nil
	}

	committed, err := u.resolutionWasCommitted(ctx, note)
	if err != nil {
		return ResolutionNoSync, err
	}
	if !committed {
		return ResolutionNotCommitted, nil
	}

	if err := u.State.ClearSyncNote(); err != nil {
		return ResolutionNoSync, fmt.Errorf("clear the sync note: %w", err)
	}
	return ResolutionCompleted, nil
}

// resolutionWasCommitted reports whether HEAD has moved off the state
// the sync recorded when it wrote the markers.
func (u *UseCase) resolutionWasCommitted(ctx context.Context, note SyncNote) (bool, error) {
	if note.PreDatesEntryRecord {
		return false, nil
	}
	head, err := u.App.HeadCommit(ctx)
	if err != nil {
		return false, fmt.Errorf("read HEAD: %w", err)
	}
	if head == note.EntryHead {
		return false, nil
	}
	headTree, err := u.App.HeadDocsTree(ctx)
	if err != nil {
		return false, fmt.Errorf("read the docs tree of HEAD: %w", err)
	}
	return headTree != note.EntryDocsTree, nil
}

// refuseWhileSyncing is the shared step-1 guard of Run and Pull: an
// unfinished sync owns the docs worktree, and a note that cannot be read
// owns it just as much as one that can.
func (u *UseCase) refuseWhileSyncing() error {
	note, exists, err := u.State.LoadSyncNote()
	switch {
	case errors.Is(err, ErrSyncNoteCorrupt):
		return err
	case err != nil:
		return fmt.Errorf("read sync state: %w", err)
	case exists:
		return fmt.Errorf("%w: syncing %s to %s",
			ErrSyncInProgress, shortOID(note.PrevBase.Commit), shortOID(note.Target.Commit))
	}
	return nil
}

// Pull executes `sanho pull` (§5.5): fast-forward-only consume. It
// refuses when local docs are edited relative to the base and points at
// sync; withCommit records the update as a sync-style commit.
func (u *UseCase) Pull(ctx context.Context, withCommit bool) (Result, error) {
	if err := u.refuseWhileSyncing(); err != nil {
		return Result{}, err
	}

	// `pull` replaces the docs worktree AND the docs index entries, so
	// staged docs changes are content it would silently discard. The
	// worktree-versus-base test below cannot see them — a staged edit
	// whose worktree copy was then restored hashes back to the base tree
	// — which is exactly how F-H5 destroyed staged work. Ask git.
	clean, err := u.App.DocsClean(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("read docs status: %w", err)
	}
	if !clean {
		return Result{}, fmt.Errorf("%w: docs have uncommitted changes", ErrPullNeedsSync)
	}

	if err := u.Canonical.Fetch(ctx); err != nil {
		return Result{}, fmt.Errorf("refresh canonical repository: %w", err)
	}
	empty, err := u.canonicalEmpty(ctx)
	if err != nil {
		return Result{}, err
	}
	if empty {
		return noUpstreamYet("")
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
		return Result{}, fmt.Errorf("%w: no docs base is recorded", ErrPullNeedsSync)
	}
	baseTree, err := u.resolveBaseTree(ctx, base, true, head, false)
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
		return Result{}, fmt.Errorf("%w: local docs differ from base %s",
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
// It is only reached once canonicalEmpty has ruled out the no-commits
// state, so Head is guaranteed to resolve here.
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
	if err := u.refuseHealthyRebase(ctx, rebaseOnto, head); err != nil {
		return "", "", "", err
	}
	tree, err := u.App.CommitTree(ctx, rebaseOnto)
	if err != nil {
		return "", "", "", fmt.Errorf("resolve the docs tree of %s: %w", shortOID(rebaseOnto), err)
	}
	return head, rebaseOnto, tree, nil
}

// refuseHealthyRebase is the F-M4 guard.
//
// `--rebase-onto` is rewrite recovery: it exists so a workspace whose
// recorded base vanished from canonical history can name a surviving
// commit to reconcile against. Pointing it at an ancestor of a base that
// is perfectly reachable is a different request — "adopt this older
// canonical state as my base" — and honoring it would record a base
// behind content the workspace already has, so the next push would
// "merge" documents nobody reverted. Refuse, and describe the ordinary
// route instead.
//
// A target that is NOT an ancestor of the base (a sibling line of
// history, or something newer) is left alone: that is a genuine
// re-anchor and the flag's purpose.
func (u *UseCase) refuseHealthyRebase(ctx context.Context, target, head string) error {
	base, hasBase, err := u.State.LoadBase()
	if err != nil || !hasBase {
		// No base, or an unreadable one: nothing healthy to protect.
		return nil //nolint:nilerr // an unreadable base is precisely the state --rebase-onto repairs
	}
	healthy, err := u.baseIsReachable(ctx, base.Commit, head)
	if err != nil || !healthy {
		return nil //nolint:nilerr // an unreachable base is what the flag is for
	}
	if target == base.Commit {
		return nil
	}
	ancestor, err := u.Canonical.IsAncestor(ctx, target, base.Commit)
	if err != nil {
		return fmt.Errorf("check whether %s precedes the recorded base: %w", shortOID(target), err)
	}
	if !ancestor {
		return nil
	}
	return fmt.Errorf("%w: %s precedes base %s", ErrRebaseOntoHealthy, shortOID(target), shortOID(base.Commit))
}

// canonicalEmpty reports whether canonical carries no commits at all.
//
// It is asked immediately after the fetch and *before* any object
// import, because importing from a branch that does not exist is itself
// a git error ("couldn't find remote ref"): the emptiness question has
// to be settled before anything downstream assumes a head exists.
func (u *UseCase) canonicalEmpty(ctx context.Context) (bool, error) {
	_, _, err := u.Canonical.Head(ctx)
	switch {
	case err == nil:
		return false, nil
	case errors.Is(err, pubdom.ErrEmptyBranch):
		return true, nil
	default:
		return false, fmt.Errorf("read canonical head: %w", err)
	}
}

// noUpstreamYet is the answer both Run and Pull give for a canonical
// repository nothing has ever published into (§5.3 bootstrap). There is
// no upstream content to consume and no commit to record as a base, so
// the truthful outcome is "up to date" with an empty NewBase; the first
// `git push` creates canonical's root commit.
//
// An explicit --rebase-onto target is the one thing that cannot be
// answered this way: naming a commit in a repository that has none is a
// mistake, and reporting it as "up to date" would hide it.
func noUpstreamYet(rebaseOnto string) (Result, error) {
	if rebaseOnto != "" {
		return Result{}, fmt.Errorf("%w: %s (the canonical repository has no commits yet)",
			ErrUnknownTarget, shortOID(rebaseOnto))
	}
	return Result{Status: StatusUpToDate}, nil
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
//
//   - Base unusable but the caller passed an explicit `--rebase-onto`
//     target (explicitTarget): fall back to the **empty tree**, exactly
//     as in the no-base state. The user has designated the canonical
//     commit to merge toward; refusing here would make the tool's own
//     rewrite guidance advise a command that cannot succeed (the D3
//     closure violation the e2e closure suite caught). An empty base is
//     the honest ancestor when recorded history and canonical history
//     genuinely share nothing.
func (u *UseCase) resolveBaseTree(ctx context.Context, base provenance.Base, hasBase bool, head string, explicitTarget bool) (string, error) {
	if !hasBase {
		return u.emptyBaseTree(ctx)
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
		if explicitTarget {
			return u.emptyBaseTree(ctx)
		}
		return "", fmt.Errorf("%w: %s carries no docs-base-tree to re-anchor by",
			ErrUnknownBase, shortOID(base.Commit))
	}
	anchor, found, err := u.Canonical.FindCommitByDocsTree(ctx, base.Tree)
	if err != nil {
		return "", fmt.Errorf("search canonical history for docs tree %s: %w", shortOID(base.Tree), err)
	}
	if !found {
		if explicitTarget {
			return u.emptyBaseTree(ctx)
		}
		return "", fmt.Errorf("%w: neither %s nor its docs tree %s is in canonical history",
			ErrUnknownBase, shortOID(base.Commit), shortOID(base.Tree))
	}
	tree, err := u.App.CommitTree(ctx, anchor)
	if err != nil {
		return "", fmt.Errorf("resolve the docs tree of re-anchored base %s: %w", shortOID(anchor), err)
	}
	return tree, nil
}

// emptyBaseTree resolves the empty tree as a merge base (the no-base
// and explicit-target fallbacks of resolveBaseTree).
func (u *UseCase) emptyBaseTree(ctx context.Context) (string, error) {
	tree, err := u.App.EmptyTree(ctx)
	if err != nil {
		return "", fmt.Errorf("resolve the empty tree: %w", err)
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
