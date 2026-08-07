// Package docsync orchestrates the consume/reconcile flows of sanho
// v0.2 (sanho-v0.2.md §5.5): `sanho sync`, `sanho sync --continue`,
// `sanho sync --abort`, `sanho sync --rebase-onto`, and `sanho pull`.
// Mechanics live behind ports implemented by infra (canonical, wsstate,
// app-repo git); decisions live in domain.
//
// One invariant governs every base write in this package, and it is the
// reason the flow looks the way it does:
//
//	A recorded base may never be ahead of the docs the worktree
//	carries; where the two cannot both be established, the older
//	value wins.
//
// A base that is too old costs a merge — publication reconciles against
// real history and, at worst, reports a conflict. A base that is too new
// costs upstream's work: the next push is evaluated as a fast-forward
// and republishes whatever the worktree happens to hold. The two
// failures are not comparable, so every decision here is resolved
// toward the older value.
package docsync

import (
	"context"
	"errors"
	"fmt"
	"strings"

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
	// Sync records it when it materializes conflicts, so that the
	// reporting can tell "a commit happened here" from "the worktree
	// merely looks tidy".
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
	// DocsPathsChangedBetween reports whether any of paths (docs paths as
	// the merge reported them) differs between two docs trees. It is how
	// the *reporting* tells "a commit went near this conflict" from "the
	// conflict was put aside" — never how a sync is completed, which is
	// an explicit act (see Continue).
	DocsPathsChangedBetween(ctx context.Context, fromTree, toTree string, paths []string) (bool, error)
	// DocsTreeDifferences counts the paths differing between two docs
	// trees. `--continue` uses it to say how far the worktree has drifted
	// from the merge result it is completing; nothing gates on it.
	DocsTreeDifferences(ctx context.Context, fromTree, toTree string) (int, error)
	// IsAncestor reports whether commit a is b or an ancestor of it in
	// THIS repository's history. It is local and network-free, which is
	// what lets `--continue` insist on standing where the sync began
	// without reaching for the canonical clone.
	IsAncestor(ctx context.Context, a, b string) (bool, error)
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
//
// Both writers take a context because neither is a plain file write any
// more: every base that reaches disk goes through the guard described on
// SaveBase, and the guard runs git.
type StatePort interface {
	LoadBase() (provenance.Base, bool, error)
	// SaveBase records a base only when the adapter can show, locally,
	// that it is not ahead of the docs the worktree carries. A candidate
	// it cannot vouch for is refused with an error satisfying
	// errors.Is(err, ErrBaseNotCorroborated) and nothing is written.
	SaveBase(ctx context.Context, base provenance.Base) error
	// SaveSyncTargetBase records the merge target a `--continue` adopts.
	//
	// It exists because that one write cannot be corroborated from trees.
	// A resolution may legitimately be "keep every one of my lines",
	// which leaves the worktree byte-identical to the pre-merge state, so
	// no comparison of docs trees can tell it from a sync that was never
	// reconciled at all. entryHead — where the app repo stood when the
	// markers were written — is the fact that can: the adapter verifies
	// with real git that HEAD descends from it, which is the same
	// precondition Continue enforces and is what makes completing a sync
	// from unrelated history impossible from either side.
	SaveSyncTargetBase(ctx context.Context, base provenance.Base, entryHead string) error
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
// restore on abort, what the resolution derives from, where the worktree
// stood when the markers were written, and what the merge could not
// settle.
//
// Its existence is the whole of "a sync is unfinished". Two commands
// delete it — `sanho sync --continue` and `sanho sync --abort` — and
// nothing else, including every hook, may write or clear it.
//
// It is also, for as long as it exists, the *only* record of the merge
// target: the base file is deliberately left where the sync found it
// until the user completes the sync (see Run's conflict branch), so the
// note carries the value the base will eventually take.
type SyncNote struct {
	// PrevBase is the base the sync found, restored by abort.
	PrevBase provenance.Base
	// Target is the canonical state the resolution derives from, and the
	// base the workspace adopts when `--continue` completes the sync.
	Target provenance.Base
	// EntryHead and EntryDocsTree pin the app repo at materialize time.
	// A resolution is a commit, and a commit moves both; a stash, a
	// revert, or `git checkout HEAD -- docs` moves neither.
	//
	// They describe the window rather than decide it. Nothing completes a
	// sync but `--continue`; these two fields let the *reporting* say
	// which unfinished state a workspace is in, which is a different job
	// and one that may be answered wrongly without costing anything.
	EntryHead     string
	EntryDocsTree string
	// MergedTree is the docs tree the conflicted merge produced — markers
	// and all. It is recorded for one purpose: `--continue` compares the
	// worktree against it and says how far the completion drifted from
	// the merge it is completing. Nothing gates on it, and a note written
	// before the field existed simply carries "".
	MergedTree string
	// Conflicts are the docs paths the merge left conflicted,
	// repository-relative.
	Conflicts []string
	// PreDatesEntryRecord marks a note written before the entry fields
	// existed. It cannot say whether a commit settled anything, so the
	// state it describes is reported as "unknown" rather than as "not
	// resolved" — and `--continue` completes it like any other note.
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
	//
	// On StatusConflicts it names the merge TARGET, which the base file
	// has deliberately not adopted yet: the conflicted run records it in
	// the sync note — along with the merge result and where HEAD stood —
	// and the base moves only when `sanho sync --continue` completes the
	// sync, from that same history. Renderers of that status say "merged
	// with upstream", not "the base is now X", so the distinction stays
	// visible to a reader.
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
	// ErrMarkersRemain and ErrResolutionUncommitted are `--continue`'s
	// two refusals. They are separate sentinels rather than reuses of
	// ErrDocsDirty because the CLI has to tell them apart from the same
	// states met by `sanho sync` itself, which are answered by different
	// guidance: there, dirty docs mean "commit or stash before
	// reconciling"; here they mean "your resolution is not committed
	// yet".
	ErrMarkersRemain         = errors.New("the docs worktree still contains conflict markers")
	ErrResolutionUncommitted = errors.New("the resolution has not been committed")
	// ErrContinueForeignHistory refuses a `--continue` run from history
	// the sync never stood on.
	//
	// The three preconditions before it are all about the *worktree*, and
	// a branch switch satisfies every one of them while changing what the
	// worktree contains completely: `git stash push -- docs` followed by
	// `git checkout other` leaves no markers, clean docs, and a note that
	// still names a merge target — so the sync completed on a branch whose
	// documents never took part in it, and the base landed ahead of them.
	// Standing on the sync's own history is the fourth precondition, and
	// it is answered locally by one `merge-base --is-ancestor`.
	ErrContinueForeignHistory = errors.New("the sync was started on a different history")
	ErrUnknownBase            = errors.New("the recorded docs base is unknown to the canonical repository")
	ErrUnknownTarget          = errors.New("the requested target is not a canonical commit")
	ErrPullNeedsSync          = errors.New("local docs have changes a fast-forward cannot carry")
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
	// ErrBaseNotCorroborated is the StatePort's refusal to record a base
	// it cannot show is safe. Like ErrSyncNoteCorrupt it is declared here
	// rather than imported from the adapter, because a use case may not
	// see infra or interface; the CLI raises the condition under this
	// sentinel so both sides can route on it.
	ErrBaseNotCorroborated = errors.New("the docs base could not be recorded")
)

// Resolution classifies the state of an unfinished sync for *reporting*.
//
// None of these values completes anything. A sync ends when the user
// runs `sanho sync --continue` or `sanho sync --abort`; what the classes
// below decide is which sentence a hook prints and whether the push
// boundary explains itself as "still resolving", "put aside", or
// "resolved but not recorded". Reading a state wrongly costs a
// misdirected sentence, never a base write.
type Resolution int

const (
	// ResolutionNoSync: no note; nothing is owed.
	ResolutionNoSync Resolution = iota
	// ResolutionPending: markers remain, or the resolution is edited but
	// not committed. The ordinary mid-resolution state.
	ResolutionPending
	// ResolutionNotCommitted: no markers and clean docs, but nothing has
	// been committed that touches a path the merge conflicted on — the
	// conflict was put aside (stashed, reverted, checked out) rather than
	// resolved.
	ResolutionNotCommitted
	// ResolutionResolved: a commit has settled at least one conflicted
	// path. The work looks finished and the sync is not: `--continue`
	// records it.
	ResolutionResolved
	// ResolutionUnknown: the note cannot answer the question — it was
	// written by an older build (PreDatesEntryRecord) or records no
	// conflicted paths. Reported as such rather than as "not resolved",
	// which would state a reason that is not known to be true.
	ResolutionUnknown
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
	case ResolutionResolved:
		return "resolved"
	case ResolutionUnknown:
		return "unknown"
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
	// The markers go into the worktree, then the note is written, and
	// **the base is not touched at all.**
	//
	// Moving the base to the merge target here was the structural defect
	// of the first cut. For as long as the resolution was owed, the
	// workspace held base == canonical head while the docs worktree still
	// carried pre-merge content — so anything that made the note stop
	// counting (a stash the completion test misread, a damaged note the
	// abort could only delete) handed the next push a fast-forward and it
	// republished the pre-merge tree over upstream's work, at exit 0.
	//
	// Nothing is lost by waiting: the note carries Target, so the value
	// is still recorded, and the base keeps answering the question it is
	// defined by (§5.7) — which canonical state the *worktree* docs
	// derive from — which during the window is still the previous base.
	// `sanho sync --continue` adopts Target, and nothing else does.
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
		// The note is written BEFORE the markers land, and the order is
		// the crash contract. The note is the only record that a sync
		// owns the docs worktree; markers in the worktree with no note
		// are a state nothing can abort, complete, or even name, and an
		// interruption between the two writes has to leave the
		// recoverable half. A note with no markers yet is exactly that —
		// `sanho sync --abort` restores docs from HEAD and clears it, and
		// re-running `sanho sync` after the abort lays the merge out
		// again. Nothing reads the note as proof that markers exist.
		note := SyncNote{
			PrevBase:      base,
			Target:        newBase,
			EntryHead:     entryHead,
			EntryDocsTree: oursTree,
			MergedTree:    mergedTree,
			Conflicts:     conflicts,
		}
		if err := u.State.SaveSyncNote(note); err != nil {
			return Result{}, fmt.Errorf("record the sync in progress: %w", err)
		}
		if err := u.App.CheckoutDocsTree(ctx, mergedTree); err != nil {
			return Result{}, fmt.Errorf("write the merged docs: %w", err)
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
		if err := u.State.SaveBase(ctx, newBase); err != nil {
			return Result{}, fmt.Errorf("record new base: %w", err)
		}
		return Result{Status: StatusSynced, NewBase: newBase}, nil
	}

	if err := u.App.CheckoutDocsTree(ctx, mergedTree); err != nil {
		return Result{}, fmt.Errorf("write the merged docs: %w", err)
	}
	if err := u.State.SaveBase(ctx, newBase); err != nil {
		return Result{}, fmt.Errorf("record new base: %w", err)
	}
	commit, err := u.App.CommitDocs(ctx, syncCommitMessage(target))
	if err != nil {
		return Result{}, fmt.Errorf("commit the docs sync: %w", err)
	}
	return Result{Status: StatusSynced, NewBase: newBase, CommitOID: commit}, nil
}

// ContinueResult reports one `sanho sync --continue`: the base the
// workspace has adopted, which is the sync's merge target.
type ContinueResult struct {
	Base provenance.Base
	// MergeDrift counts the docs paths that differ between the tree the
	// conflicted merge produced and the tree being completed.
	//
	// It is information, never a refusal. Completing a sync whose clean
	// half was reverted along with its conflicts — the shape a blanket
	// `git checkout HEAD -- docs` or a `git stash push -- docs` leaves —
	// is legitimate and is exactly the "keep my own lines" reading
	// `--continue` exists to express. But it silently drops upstream
	// content the user never saw a conflict for, so the count is
	// reported. Zero means the worktree is the merge result, and a note
	// written before the merged tree was recorded reports zero too,
	// because nothing is known rather than because nothing drifted.
	MergeDrift int
}

// AbortResult reports one `sanho sync --abort`. It carries nothing:
// abort restores the docs, puts the base back, and drops the note, and
// there is no partial outcome left to describe.
//
// It used to carry a Degraded flag for the unreadable-note case, because
// the previous base lived inside the note and a conflicted sync had
// already moved the base file to the merge target — so an abort over a
// damaged note left the base on the target with pre-merge docs beneath
// it, and had to say so. A conflicted sync no longer moves the base, and
// an abort that cannot read its note now clears the base rather than
// leaving one it cannot vouch for, so there is no degraded outcome left
// to report.
type AbortResult struct{}

// Abort executes §5.5 step 7. Valid whenever a sync note exists; by
// construction it cannot fail after its precondition passes (guidance
// closure, D3) — it moves no ref, creates no commit, and touches only
// the docs worktree/index and two state files.
//
// "Whenever a sync note exists" is meant literally, including when the
// note is unreadable. Abort is the way *out* of a broken sync state, so
// a damaged note is precisely the case it must survive: refusing there
// left a workspace with markers in docs/, a file nothing could parse,
// and no command that could clear either.
//
// An unreadable note now clears the base instead of leaving it alone.
// Skipping the base rested on a premise — "a conflicted sync never moved
// it" — that two states break: a crash between a base write and the note
// clear, and a note left by a build that advanced the base at conflict
// time. In both, the abort walked away from a base sitting on the merge
// target with pre-merge documents beneath it, and the next push
// fast-forwarded over upstream at exit 0. An abort that cannot read its
// note cannot know which value is right, and the invariant says that
// where the base cannot be established the older value wins: no base at
// all is the oldest there is. Publication then refuses with `no_base`
// and names `sanho sync`, which establishes one.
//
// The order is restore docs → settle the base → clear the note, and it
// is chosen so that a crash anywhere in the middle leaves a re-runnable
// state: the note is the proof that an abort is still owed, so it is
// deleted last, and every step before it is idempotent (a checkout of
// HEAD's docs, and a base write that only ever moves backwards). Running
// `sanho sync --abort` again after an interruption simply redoes them.
func (u *UseCase) Abort(ctx context.Context) (AbortResult, error) {
	note, exists, err := u.State.LoadSyncNote()
	unreadable := errors.Is(err, ErrSyncNoteCorrupt)
	switch {
	case err != nil && !unreadable:
		return AbortResult{}, fmt.Errorf("read sync state: %w", err)
	case !exists:
		return AbortResult{}, ErrNoSyncInProgress
	}

	if err := u.App.RestoreDocsFromHead(ctx); err != nil {
		return AbortResult{}, fmt.Errorf("restore the docs worktree: %w", err)
	}
	if unreadable {
		if err := u.State.ClearBase(); err != nil {
			return AbortResult{}, fmt.Errorf("remove the base file: %w", err)
		}
	} else if err := u.restoreBase(ctx, note); err != nil {
		return AbortResult{}, err
	}
	if err := u.State.ClearSyncNote(); err != nil {
		return AbortResult{}, fmt.Errorf("clear the sync note: %w", err)
	}
	return AbortResult{}, nil
}

// restoreBase puts the base file back the way the aborted sync found it.
//
// A sync written by *this* version left the base alone, so this is
// ordinarily an idempotent rewrite of the value already on disk. It is
// kept for the two cases where it is not:
//
//   - A note written before the base advance was deferred
//     (PreDatesEntryRecord). That binary moved the base to the merge
//     target when it materialized the markers, so a workspace left
//     mid-sync across the upgrade really does need PrevBase written
//     back. This is the one asymmetry between old and new notes, and it
//     runs in the only direction that cannot lose data.
//   - A sync entered with no base at all. PrevBase is then the zero
//     value, which is not a writable one — the base file schema has no
//     representation for an empty commit OID and reading one back is a
//     corruption error — so "forget the base" has to be a removal.
func (u *UseCase) restoreBase(ctx context.Context, note SyncNote) error {
	prev := note.PrevBase
	if prev.IsZero() {
		if err := u.State.ClearBase(); err != nil {
			return fmt.Errorf("remove the base file: %w", err)
		}
		return nil
	}
	switch err := u.State.SaveBase(ctx, prev); {
	case err == nil:
		return nil
	case errors.Is(err, ErrBaseNotCorroborated):
		// The guard could not vouch for the value the sync found, which
		// only a note from an older build can produce (one that moved
		// the base at conflict time, so PrevBase is genuinely behind the
		// recorded value and the ancestry cannot be shown offline). The
		// invariant resolves toward the older value, and no base at all
		// is the oldest there is — and abort's contract is that it
		// cannot fail once a note exists, so this may not become an
		// error. Publication then refuses with `no_base` and names
		// `sanho sync`, which establishes one.
		if clearErr := u.State.ClearBase(); clearErr != nil {
			return fmt.Errorf("remove the base file: %w", clearErr)
		}
		return nil
	default:
		return fmt.Errorf("restore the base file: %w", err)
	}
}

// Continue completes a conflicted sync (§5.5 step 6b). It is the only
// path by which a workspace adopts a merge target, and it exists because
// the alternative does not work.
//
// Three review waves tried to *infer* completion from the state a
// resolution leaves behind, and each narrowing left a smaller version of
// the same door. The last one is the argument: after escaping the
// markers with `git stash push -- docs`, the most natural next action is
// to keep editing the same document — and that commit moves HEAD, moves
// the docs tree, and changes a path the merge conflicted on. Every
// question after-the-fact tree evidence can ask answers "resolved",
// while the merge stands exactly where it was. A predicate narrow enough
// to reject it would start rejecting genuine resolutions.
//
// So completion is an act, in the shape `git rebase --continue` already
// taught: the user says when the reconciliation is done, and sanho
// records it. Four preconditions, each of which names what remains:
//
//   - A sync note exists. Without one there is nothing to complete.
//   - No docs file still carries conflict markers. Recording a base for
//     content full of markers describes a resolution of nothing.
//   - The docs are clean relative to HEAD, so what the base will
//     describe is committed content rather than an edit in progress.
//   - HEAD stands on the history the sync began on — it IS the note's
//     entry head, or descends from it.
//
// The fourth is the fix for the fourth review's C1, and the first three
// are the reason it was needed: every one of them is about the worktree,
// and a branch switch satisfies all three while replacing the documents
// entirely. `git stash push -- docs` clears the markers and the dirt;
// `git checkout other` moves to history the merge never touched; and the
// completion then recorded canonical head as the base of documents that
// had never been reconciled with it. The next push was evaluated as a
// fast-forward and republished them over upstream, at exit 0.
//
// Ancestry rather than identity is the right test, and deliberately so:
// a resolution is ordinarily several commits, sometimes on a branch made
// from the resolution, and all of those still descend from where the
// sync began. What cannot descend from it is a branch that was never
// part of it. One local `merge-base --is-ancestor` decides it; nothing
// is fetched.
//
// It still deliberately does NOT ask whether a commit settled the
// conflicted paths. Taking "ours" wholesale, byte for byte, is a
// legitimate resolution that leaves no trace at all — the dead end the
// previous design had no exit from — and the user asserting it, from
// the sync's own history, is exactly the evidence that was missing.
//
// No commit is created (P3), no ref moves, and nothing is fetched.
//
// The two writes are ordered note-first, and the order is the invariant
// in miniature: a crash between them leaves the note gone and the base
// at its older value, which publication reconciles as an ordinary
// divergence. The reverse order fails toward a base ahead of the
// worktree — a fast-forward over upstream's work. The base write is
// still guarded: SaveSyncTargetBase re-proves the same ancestry from the
// adapter side, so the invariant does not rest on this function alone.
func (u *UseCase) Continue(ctx context.Context) (ContinueResult, error) {
	note, exists, err := u.State.LoadSyncNote()
	switch {
	case errors.Is(err, ErrSyncNoteCorrupt):
		return ContinueResult{}, err
	case err != nil:
		return ContinueResult{}, fmt.Errorf("read sync state: %w", err)
	case !exists:
		return ContinueResult{}, ErrNoSyncInProgress
	}
	if !note.Target.Valid() {
		// A note that cannot say what to adopt is unusable for exactly
		// the reason an unparseable one is, so it is reported the same
		// way and routed to the abort, which needs nothing from it.
		return ContinueResult{}, fmt.Errorf("%w: the sync note records no usable merge target", ErrSyncNoteCorrupt)
	}

	conflicted, err := u.App.ScanWorktreeDocsForMarkers(ctx)
	if err != nil {
		return ContinueResult{}, fmt.Errorf("scan docs for conflict markers: %w", err)
	}
	if len(conflicted) > 0 {
		return ContinueResult{}, fmt.Errorf("%w: %s", ErrMarkersRemain, strings.Join(conflicted, ", "))
	}
	clean, err := u.App.DocsClean(ctx)
	if err != nil {
		return ContinueResult{}, fmt.Errorf("read docs status: %w", err)
	}
	if !clean {
		return ContinueResult{}, ErrResolutionUncommitted
	}
	if err := u.requireSyncHistory(ctx, note); err != nil {
		return ContinueResult{}, err
	}

	drift, err := u.mergeDrift(ctx, note)
	if err != nil {
		return ContinueResult{}, err
	}

	if err := u.State.ClearSyncNote(); err != nil {
		return ContinueResult{}, fmt.Errorf("clear the sync note: %w", err)
	}
	if err := u.State.SaveSyncTargetBase(ctx, note.Target, note.EntryHead); err != nil {
		return ContinueResult{}, fmt.Errorf("record new base: %w", err)
	}
	return ContinueResult{Base: note.Target, MergeDrift: drift}, nil
}

// requireSyncHistory is `--continue`'s fourth precondition.
//
// An empty EntryHead is not a failure to check: it means the sync began
// on an unborn HEAD, or on a note written before the field existed
// (PreDatesEntryRecord). Neither can say which history the sync belongs
// to, so neither can be violated — and refusing there would strand a
// workspace left mid-sync across an upgrade with no way to finish it,
// which is a worse answer than the one this precondition prevents.
func (u *UseCase) requireSyncHistory(ctx context.Context, note SyncNote) error {
	if note.EntryHead == "" {
		return nil
	}
	head, err := u.App.HeadCommit(ctx)
	if err != nil {
		return fmt.Errorf("read HEAD: %w", err)
	}
	if head == note.EntryHead {
		return nil
	}
	descends, err := u.App.IsAncestor(ctx, note.EntryHead, head)
	if err != nil {
		return fmt.Errorf("check whether HEAD descends from where the sync began: %w", err)
	}
	if descends {
		return nil
	}
	return fmt.Errorf("%w: it began at %s, and HEAD is %s",
		ErrContinueForeignHistory, shortOID(note.EntryHead), shortOID(head))
}

// mergeDrift counts how far the tree being completed has moved from the
// tree the conflicted merge produced. A note with no recorded merged
// tree reports zero: the fact is unknown, and inventing a number would
// be worse than saying nothing.
func (u *UseCase) mergeDrift(ctx context.Context, note SyncNote) (int, error) {
	if note.MergedTree == "" {
		return 0, nil
	}
	worktree, err := u.App.WorktreeDocsTree(ctx)
	if err != nil {
		return 0, fmt.Errorf("hash worktree docs: %w", err)
	}
	drift, err := u.App.DocsTreeDifferences(ctx, note.MergedTree, worktree)
	if err != nil {
		return 0, fmt.Errorf("compare the worktree with the merge result: %w", err)
	}
	return drift, nil
}

// ResolutionState reports where an unfinished sync stands. It is a
// query: it writes nothing, and in particular it neither moves the base
// nor clears the note.
//
// It used to do both, under the name CompleteIfResolved, and the hooks
// called it — so the read paths of the tool mutated the one file the
// sync window is defined by, on evidence that could not tell a
// resolution from a discarded merge. Completion moved to `--continue`;
// what is left here is the reporting that tells a user which unfinished
// state they are in.
//
// The classes are ordered by what can be observed, not by severity:
// markers in the worktree or uncommitted docs mean the resolution is
// still being made; otherwise a note that can say whether a commit
// touched a conflicted path says so, and one that cannot says that
// instead of guessing.
func (u *UseCase) ResolutionState(ctx context.Context) (Resolution, error) {
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

	return u.classifyCommittedWork(ctx, note)
}

// classifyCommittedWork separates "a commit settled one of the paths the
// merge conflicted on" from "nothing committed touched them" — and from
// "this note cannot say".
//
// HEAD moving is necessary but not sufficient: a stash, a revert and a
// `git checkout HEAD -- docs` all leave HEAD alone, while an unrelated
// docs commit moves it without going near the conflict. The
// discriminating question is whether one of the conflicted paths differs
// between the docs tree HEAD carried when the markers landed and the one
// it carries now.
//
// A note with no entry record and a note with no conflicted paths are
// both reported as ResolutionUnknown. Saying "no commit has changed the
// files it conflicted on" about a note that never recorded those files
// would be stating a reason nothing knows to be true — the false
// explanation the third review found in the legacy-note path.
func (u *UseCase) classifyCommittedWork(ctx context.Context, note SyncNote) (Resolution, error) {
	if note.PreDatesEntryRecord || len(note.Conflicts) == 0 {
		return ResolutionUnknown, nil
	}
	head, err := u.App.HeadCommit(ctx)
	if err != nil {
		return ResolutionNoSync, fmt.Errorf("read HEAD: %w", err)
	}
	if head == note.EntryHead {
		return ResolutionNotCommitted, nil
	}
	headTree, err := u.App.HeadDocsTree(ctx)
	if err != nil {
		return ResolutionNoSync, fmt.Errorf("read the docs tree of HEAD: %w", err)
	}
	settled, err := u.App.DocsPathsChangedBetween(ctx, note.EntryDocsTree, headTree, note.Conflicts)
	if err != nil {
		return ResolutionNoSync, fmt.Errorf("compare the conflicted docs paths: %w", err)
	}
	if settled {
		return ResolutionResolved, nil
	}
	return ResolutionNotCommitted, nil
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
	if err := u.State.SaveBase(ctx, newBase); err != nil {
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
