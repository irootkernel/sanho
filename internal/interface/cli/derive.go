package cli

// Base re-derivation (docs/architecture.md "Git hooks") and the sync preview shared
// by the pre-commit warning and `sanho status` (the commit-hook contract step 2).

import (
	"context"

	"github.com/irootkernel/sanho/internal/domain/provenance"
	"github.com/irootkernel/sanho/internal/infra/canonical"
)

// deriveScanDepth bounds the history walk. The base is carried by the
// newest stamped commit, so a deep scan only matters for a workspace
// whose recent history is entirely docs-free; 500 commits is far past
// the point where an unstamped run means the trailers are simply not
// there.
const deriveScanDepth = 500

// deriveBase re-derives the base from the newest reachable stamped
// commit (the hook contract): walk history, collect each commit's trailer values,
// and let domain/provenance pick.
//
// Both trailer keys are accepted. A legacy `docs-version: X` commit
// asserted that its docs tree *equaled* canonical X, which makes X a
// correct base for edits made on top of it — so mixed histories need no
// rewrite (the provenance contract "legacy coexistence").
//
// The scan itself lives in appgit, which is also what the publication
// gate asks about a pushed tip. One implementation is the point: a
// branch's provenance is now consulted at two boundaries — here, to
// decide what this checkout's base is, and at push time, to decide
// whether a fast-forward is licensed — and the two must never be able
// to read the same history differently.
func deriveBase(ctx context.Context, ws *workspace) (provenance.Base, bool, error) {
	commits, err := ws.repo.CommitTrailers(ctx, "HEAD", deriveScanDepth)
	if err != nil {
		return provenance.Base{}, false, err
	}
	base, ok := provenance.SelectBase(commits)
	return base, ok, nil
}

// rederivation is what a HEAD-moved hook did to the base file.
type rederivation int

const (
	// rederivedNothing: the base file is exactly as it was.
	rederivedNothing rederivation = iota
	// rederivedAdopted: a new base was recorded from history.
	rederivedAdopted
	// rederivedCleared: the recorded base could not be corroborated by
	// the new HEAD and was removed.
	rederivedCleared
)

// rederiveBaseAfterHeadMoved is the post-checkout / post-merge /
// post-rewrite body (the hook contract). It reports what it did, so the hooks stay
// silent in the ordinary case and speak in the two that matter.
//
// Two states leave the base untouched, and both are spec rules rather
// than caution. A docs worktree that differs from HEAD's docs carries
// uncommitted edits that survived the checkout — the base answers "which
// canonical state do the *worktree* docs derive from" (the state contract invariant),
// so nothing derived from HEAD's history describes it; `sanho doctor`
// flags any resulting inconsistency.
//
// And an unfinished sync owns the base outright. It leaves the base at
// the pre-sync value on purpose and adopts the merge target only when
// the resolution is confirmed, so a re-derivation in that window would
// be a third party writing the one file the sync is holding still.
// Concretely it would adopt whatever the newest stamped commit says —
// including the target stamped by a resolution the note has not
// confirmed yet, or by a commit made while the conflict was set aside —
// and put the base on canonical head with pre-merge docs beneath it.
// Nothing is lost by standing down: the note survives every checkout,
// and the next hook that settles it writes the base.
//
// **History with no stamped commit no longer leaves the base alone**,
// and that change is the fourth review's C2. Keeping it was the quiet
// reading of "there is nothing to adopt" — but the base file is one file
// at the checkout root while the thing that moved is HEAD, so what
// "keeping" actually did was hand the branch now standing there a base
// belonging to a different branch. A repository that had `docs/` before
// it had sanho has such branches by construction: check one out, and the
// workspace held base == canonical head over one stale document. The
// push was a fast-forward and canonical's six documents became that one.
//
// So a base that the new HEAD cannot account for is REMOVED. The bar is
// deliberately low — either the recorded base's docs ARE the worktree's
// docs, or history names it — because the point is not to second-guess a
// working workspace but to refuse to carry an unexamined pointer across
// a branch switch. No base at all is a state everything downstream
// handles: publication refuses with `no_base` and names `sanho sync`,
// which merges against the empty tree (the union of both sides) and
// establishes one.
func rederiveBaseAfterHeadMoved(ctx context.Context, ws *workspace) (provenance.Base, rederivation, error) {
	if _, syncing, err := ws.statePort().LoadSyncNote(); syncing || err != nil {
		// Existence is the fact that matters, and an unreadable note has
		// it. A note that could not even be looked at is the same answer
		// for a weaker reason: the sync state is unknown, and writing the
		// base is not something to do while it is.
		return provenance.Base{}, rederivedNothing, nil //nolint:nilerr // an unfinished sync owns the base; a hook is not the place to argue
	}

	worktreeTree, err := ws.repo.WorktreeDocsTree(ctx)
	if err != nil {
		return provenance.Base{}, rederivedNothing, err
	}
	headTree, err := ws.repo.HeadDocsTree(ctx)
	if err != nil {
		return provenance.Base{}, rederivedNothing, err
	}
	if worktreeTree != headTree {
		return provenance.Base{}, rederivedNothing, nil
	}

	state := ws.statePort()
	current, hasCurrent, loadErr := state.LoadBase()
	if loadErr != nil {
		// A corrupt base file is exactly what re-derivation repairs, so
		// an unreadable one is a reason to write, not to stop.
		hasCurrent = false
	}

	derived, found, err := deriveBase(ctx, ws)
	if err != nil {
		return provenance.Base{}, rederivedNothing, err
	}
	if !found {
		return provenance.Base{}, dropUncorroboratedBase(ctx, state, current, hasCurrent, worktreeTree), nil
	}

	if hasCurrent && current.Commit == derived.Commit {
		// Same commit: keep what is recorded. A legacy `docs-version`
		// adoption carries no tree, and overwriting a recorded tree with
		// nothing would discard the rewrite anchor (D2) for no gain.
		return current, rederivedNothing, nil
	}
	// M8: a base the worktree docs already ARE, or one that has advanced
	// past the commit history names, is not improved by rewriting it.
	// Publication's own base advance produces the second state on every
	// successful push, so re-deriving there would drag the base backwards
	// on the next branch switch and make the following push re-merge
	// content nobody changed.
	if hasCurrent && baseNeedsNoRederivation(ctx, ws, current, worktreeTree) {
		return current, rederivedNothing, nil
	}
	if err := state.SaveBase(ctx, derived); err != nil {
		return provenance.Base{}, rederivedNothing, err
	}
	return derived, rederivedAdopted, nil
}

// dropUncorroboratedBase removes a base the new HEAD cannot account for.
//
// "Cannot account for" is one comparison: the recorded base's docs are
// not the docs the worktree carries. History has already been asked and
// had nothing to say (this is the !found path), so there is no second
// opinion to consult — and a base whose tree the checkout does not even
// hold is the least corroborated of all.
func dropUncorroboratedBase(ctx context.Context, state statePort, current provenance.Base, hasCurrent bool, worktreeTree string) rederivation {
	if !hasCurrent {
		return rederivedNothing
	}
	if tree, resolved, err := state.resolveDocsTree(ctx, current); err == nil && resolved && tree == worktreeTree {
		return rederivedNothing
	}
	if err := state.ClearBase(); err != nil {
		// A hook may not fail a checkout (P2). The base stays where it
		// was, and `sanho doctor` reports the same disagreement.
		return rederivedNothing
	}
	return rederivedCleared
}

// baseNeedsNoRederivation is M8: the recorded base's docs tree IS the
// tree the worktree carries, so nothing about the documents is in
// question and rewriting the pointer is pure churn.
//
// That is the state of every workspace that has just published or
// pulled: the advance moved the base past the commit the trailers name,
// while the documents are exactly what it names. Re-deriving there
// dragged the base backwards on the next HEAD move and made the
// following push re-merge content nobody had changed.
//
// The audit also proposed a second shape — "the recorded base is a
// DESCENDANT of the derived one in canonical history" — and it is
// deliberately not implemented. It subsumes the tree test above and
// admits one case the tree test excludes: a HEAD whose documents differ
// from what the recorded base names. That case is a branch switch onto
// older docs, and keeping the descendant base there is precisely the
// shape this wave exists to prevent — base == canonical head over
// documents that never derived from it. The narrow test costs a
// re-derivation nobody notices; the wide one costs upstream's work.
func baseNeedsNoRederivation(ctx context.Context, ws *workspace, current provenance.Base, worktreeTree string) bool {
	tree, resolved, err := ws.statePort().resolveDocsTree(ctx, current)
	return err == nil && resolved && tree == worktreeTree
}

// syncPreview is the clean/conflict prediction of the commit-hook contract step 2, shared by
// the commit warning and `sanho status`.
type syncPreview struct {
	// Known is false when the prediction could not be computed; callers
	// then degrade to a behind-count-only message, which the guidance
	// contract permits explicitly.
	Known     bool
	Clean     bool
	Conflicts []string
}

// previewSync predicts what `sanho sync` would do, without touching the
// network, the app worktree, the index, or any app ref.
//
// It runs clone-side rather than app-side, which is the significant
// choice. The merge needs three trees in one object database; app-side
// would mean fetching canonical objects *into the application
// repository* on every commit, writing FETCH_HEAD and growing the user's
// object store as a side effect of a read-only check. Importing the app
// tip into sanho's own private clone instead keeps the churn entirely
// inside `.git/sanho/canonical`, which sanho owns and `sanho clean`
// deletes.
// oursTree is the local side of the prediction, and the caller chooses
// it (M7). `sanho status` reports on the checkout, so it asks about
// HEAD; the pre-commit warning reports on the commit being made, so it
// asks about the INDEX. Predicting a commit's conflicts from HEAD is
// answering about the previous commit — the one state in which the
// warning is guaranteed to describe something the user is no longer
// doing.
func previewSync(ctx context.Context, ws *workspace, store *canonical.Store, base provenance.Base, head, headTree, oursTree string) syncPreview {
	if oursTree == headTree {
		// Nothing to merge: the local docs already are canonical's.
		return syncPreview{Known: true, Clean: true}
	}

	port := ws.canonicalPort(store)
	if err := port.FetchFromApp(ctx, "HEAD"); err != nil {
		return syncPreview{}
	}
	_, conflicts, clean, err := port.MergeDocs(ctx, base.Commit, oursTree, head)
	if err != nil {
		return syncPreview{}
	}
	return syncPreview{Known: true, Clean: clean, Conflicts: conflicts}
}
