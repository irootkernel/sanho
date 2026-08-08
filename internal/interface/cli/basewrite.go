package cli

// The guarded base writer (docs/architecture.md "State and persistence" invariant).
//
// This file is the ONLY place in the codebase that may call
// wsstate.SaveBase, and `internal/architecture` fails the build for any
// other caller. Everything that records a base — a clean sync, a
// `--continue`, an abort's restore, a pull, publication's advance, base
// re-derivation, `doctor --fix`, `init`, `migrate` — comes through
// writeBase below.
//
// Why a single door rather than nine correct callers.
//
// The failure this wave answers has now been found four times, and each
// time in a different path. Every one of them was a *correct-looking*
// base write: the value came from somewhere real, the caller had a good
// reason, and the result was a base file naming a canonical state the
// documents beside it had never derived from. The next push was then
// evaluated as a fast-forward — publish the worktree's tree directly
// over canonical head — and everything upstream held and the worktree
// did not was deleted, at exit 0, under the word "published".
//
// Three waves patched three callers. What none of them changed is that
// `wsstate.SaveBase` is an unconditional writer: it takes an OID pair and
// puts it on disk, and the invariant that governs it —
//
//	a recorded base may never be ahead of the docs the worktree carries
//
// — was enforced by nothing at runtime, only by every caller separately
// getting it right. A tenth caller written next year inherits none of
// the three fixes. So the invariant gets an enforcement point, and the
// enforcement point gets a test that fails when a caller goes around it.
//
// What the guard can and cannot prove.
//
// "Ahead" is a claim about *derivation*, not about content: a base may
// legitimately name a canonical state whose documents the worktree
// lacks (a deletion is an ordinary publication), and it may legitimately
// name one whose documents the worktree has long since moved past. So
// the guard does not ask "does the worktree contain this?" — it asks
// "can this workspace show, locally, that its documents came from here?"
// and admits only on a proof it can check itself. The proofs are
// enumerated in warrants below; there is no "the caller says so".
//
// It is deliberately not the last line of defence. Publication makes the
// same demand at the point of use (usecase/publish.requireCorroboratedBase),
// because a base file can also be edited, restored from a backup, or
// carried in by a tool that never ran this code.

import (
	"context"
	"errors"
	"fmt"

	"github.com/irootkernel/sanho/internal/domain/provenance"
	"github.com/irootkernel/sanho/internal/infra/canonical"
	"github.com/irootkernel/sanho/internal/infra/wsstate"
	"github.com/irootkernel/sanho/internal/usecase/docsync"
)

// warrant names the local proof that admitted a base write. It exists
// to be reported: a refusal says which proofs were tried, and the
// diagnostics under --verbose say which one succeeded.
type warrant string

const (
	// warrantWorktreeTree: the candidate's docs tree IS the tree the
	// worktree currently hashes to. The base cannot be ahead of documents
	// it is byte-identical to. (publication's advance, pull, init in fresh
	// mode, a sync whose merge result is the target.)
	warrantWorktreeTree warrant = "the worktree docs are exactly this base's docs"
	// warrantSameCommit: the recorded base already names this commit, so
	// the write moves nothing. It is what lets `migrate` fill in a tree
	// beside a commit v0.1 recorded, and what makes every re-derivation
	// that re-confirms the current value a no-op rather than a claim.
	warrantSameCommit warrant = "the recorded base already names this commit"
	// warrantStampedHistory: the newest docs-base trailer reachable from
	// HEAD names this commit. The trailer is written by the commit that
	// carries the documents, so it is the documents' own account of where
	// they came from — and unlike the base file it cannot be left behind
	// by a branch switch. (re-derivation, `doctor --fix`, `init` reuse.)
	warrantStampedHistory warrant = "the tip's own provenance names this base"
	// warrantOlderBase: the candidate is an ancestor of the recorded base
	// in canonical history. The invariant is one-directional — a base
	// that is too old costs a merge, a base that is too new costs
	// upstream's work — so moving backwards is always admissible.
	// (`sync --abort`'s restore, `--rebase-onto`.)
	warrantOlderBase warrant = "this base precedes the one already recorded"
	// warrantAbsorbed: three-way merging the candidate's docs into the
	// worktree's, over the previously recorded base, is clean and changes
	// nothing. The worktree already contains everything this base
	// publishes, which is what a completed reconciliation means.
	// (`sanho sync`'s clean path, including --rebase-onto.)
	warrantAbsorbed warrant = "the worktree docs already contain this base's docs"
	// warrantUnresolvableTree: the candidate's docs tree is not in this
	// repository at all, so no comparison can be made in either
	// direction. Admitted because nothing can be shown to be at risk, and
	// refusing would strand a legacy adoption whose canonical commit this
	// checkout has never fetched. Publication's own corroboration is what
	// stands behind it.
	warrantUnresolvableTree warrant = "this base's docs are not in this repository to compare"
	// warrantSyncCompletion: the sync being completed began on the
	// history HEAD stands on, and the docs are committed. It is the one
	// warrant offered by a caller rather than discovered, because the one
	// fact that separates a genuine "keep my own lines" resolution from a
	// sync completed on a branch that never took part in it — where the
	// merge started — is not in any tree. The ANCESTRY is still verified
	// here against real git; only the entry commit is passed in, by the
	// one caller that has just read it out of the note it is clearing.
	warrantSyncCompletion warrant = "this completes a sync begun on the current history"
)

// errBaseNotCorroborated is the refusal. It reports itself as
// docsync.ErrBaseNotCorroborated so the use cases — which may not see
// this package — can route on it, and carries the candidate for the
// message.
type errBaseNotCorroborated struct {
	candidate provenance.Base
	detail    string
}

func (e *errBaseNotCorroborated) Error() string {
	return fmt.Sprintf("%s: %s is not supported by anything this workspace can check locally (%s)",
		docsync.ErrBaseNotCorroborated, shortOID(e.candidate.Commit), e.detail)
}

func (e *errBaseNotCorroborated) Is(target error) bool {
	return target == docsync.ErrBaseNotCorroborated
}

// syncCompletion is the sync-completion warrant's evidence, offered by
// the one caller that has just read it out of the note it is clearing.
// A nil value means "this is not a completion".
type syncCompletion struct {
	// entryHead is where the app repo stood when the markers were
	// written. It is empty for a note that predates the field, or one
	// written on an unborn HEAD.
	entryHead string
}

// writeBase is the guard. It records candidate only after establishing
// one of the warrants above, and writes nothing otherwise.
func (s statePort) writeBase(ctx context.Context, candidate provenance.Base, completion *syncCompletion) error {
	if !candidate.Valid() {
		return &errBaseNotCorroborated{candidate: candidate, detail: "it is not a well-formed OID pair"}
	}

	granted, err := s.corroborate(ctx, candidate, completion)
	if err != nil {
		return err
	}
	if granted == "" {
		return &errBaseNotCorroborated{
			candidate: candidate,
			detail: "its docs are not the worktree's, the tip's provenance does not name it, " +
				"and it does not precede the recorded base",
		}
	}
	if err := wsstate.SaveBase(s.workDir, candidate); err != nil {
		return err
	}
	return nil
}

// corroborate tries every warrant, cheapest first, and returns the one
// that held (or "" for none). A warrant whose evidence cannot be
// gathered is simply not granted: an unreadable clone, a missing object
// or a merge that will not run are all failures to PROVE, and the
// invariant resolves a failed proof toward not writing.
func (s statePort) corroborate(ctx context.Context, candidate provenance.Base, completion *syncCompletion) (warrant, error) {
	if completion != nil {
		granted, err := s.completesSyncOnThisHistory(ctx, completion.entryHead)
		if err != nil || granted != "" {
			return granted, err
		}
		// A completion whose ancestry does not hold falls through to the
		// ordinary warrants rather than short-circuiting: the write is
		// still admissible if the tree evidence supports it on its own.
	}

	recorded, hasRecorded, loadErr := wsstate.LoadBase(s.workDir)
	if loadErr != nil {
		// A corrupt base file is a reason to replace it, not a reason to
		// refuse — but it can vouch for nothing, so the warrants that
		// consult it are unavailable.
		hasRecorded = false
	}
	if hasRecorded && recorded.Commit == candidate.Commit {
		return warrantSameCommit, nil
	}

	candidateTree, resolved, err := s.resolveDocsTree(ctx, candidate)
	if err != nil {
		return "", err
	}
	if resolved {
		worktree, treeErr := s.ws.repo.WorktreeDocsTree(ctx)
		if treeErr != nil {
			return "", fmt.Errorf("hash worktree docs: %w", treeErr)
		}
		if worktree == candidateTree {
			return warrantWorktreeTree, nil
		}
		if s.stampedHistoryNames(ctx, candidate.Commit) {
			return warrantStampedHistory, nil
		}
		if hasRecorded && s.precedesRecordedBase(ctx, candidate.Commit, recorded.Commit) {
			return warrantOlderBase, nil
		}
		if s.absorbedByWorktree(ctx, recorded, hasRecorded, worktree, candidateTree) {
			return warrantAbsorbed, nil
		}
		return "", nil
	}

	if s.stampedHistoryNames(ctx, candidate.Commit) {
		return warrantStampedHistory, nil
	}
	return warrantUnresolvableTree, nil
}

// completesSyncOnThisHistory verifies the sync-completion warrant: HEAD
// is the commit the sync began at, or descends from it.
//
// An empty entry head is granted without a check, and that is a real
// decision rather than an oversight. It means the note records no
// history — written on an unborn HEAD, or by a build from before the
// field existed — so there is no ancestry to verify and no version of
// this check that could ever pass. Refusing would leave a workspace left
// mid-sync across an upgrade unable to complete OR to keep the
// reconciliation it had already made: `sanho sync --abort` would be the
// only exit, and it throws that work away. `Continue` reaches the same
// conclusion for the same reason, and publication's own corroboration
// (usecase/publish.requireCorroboratedBase) is what stands behind both.
func (s statePort) completesSyncOnThisHistory(ctx context.Context, entryHead string) (warrant, error) {
	if entryHead == "" {
		return warrantSyncCompletion, nil
	}
	head, err := s.ws.repo.HeadCommit(ctx)
	if err != nil {
		return "", fmt.Errorf("read HEAD: %w", err)
	}
	if head == entryHead {
		return warrantSyncCompletion, nil
	}
	descends, err := s.ws.repo.IsAncestor(ctx, entryHead, head)
	if err != nil {
		return "", fmt.Errorf("check whether HEAD descends from where the sync began: %w", err)
	}
	if descends {
		return warrantSyncCompletion, nil
	}
	return "", nil
}

// resolveDocsTree answers what docs tree the candidate publishes.
//
// The recorded tree field is preferred but not trusted: the OID has to
// be present in this repository for any comparison to mean anything, so
// an unresolvable one is reported as unresolved and the commit is asked
// instead. Canonical commits are docs-only, so their root tree IS the
// docs tree — which is why the app repository can answer at all, once
// sync's object import has run.
func (s statePort) resolveDocsTree(ctx context.Context, candidate provenance.Base) (tree string, resolved bool, err error) {
	if candidate.Tree != "" {
		present, err := s.ws.repo.TreeExists(ctx, candidate.Tree)
		if err != nil {
			return "", false, err
		}
		if present {
			return candidate.Tree, true, nil
		}
	}
	if tree, err := s.ws.repo.CommitTree(ctx, candidate.Commit); err == nil {
		return tree, true, nil
	}
	if store := canonicalOrNil(s.ws); store != nil {
		if tree, err := s.ws.link(store).DocsTreeOfCommit(ctx, candidate.Commit); err == nil {
			return tree, true, nil
		}
	}
	return "", false, nil
}

// stampedHistoryNames reports whether the newest provenance trailer
// reachable from HEAD names this commit — the documents' own account of
// where they came from.
func (s statePort) stampedHistoryNames(ctx context.Context, commit string) bool {
	stamped, found, err := deriveBase(ctx, s.ws)
	return err == nil && found && stamped.Commit == commit
}

// precedesRecordedBase reports whether the candidate is an ancestor of
// the base already recorded, which makes it strictly older and therefore
// always admissible. The clone is the only place both commits are
// reliably present; without one the question is simply unanswered.
func (s statePort) precedesRecordedBase(ctx context.Context, candidate, recorded string) bool {
	store := canonicalOrNil(s.ws)
	if store == nil {
		return false
	}
	ancestor, err := store.IsAncestor(ctx, candidate, recorded)
	return err == nil && ancestor
}

// absorbedByWorktree reports whether the worktree docs already contain
// everything the candidate publishes: three-way merging the candidate
// into the worktree, over the recorded base, is clean and changes
// nothing.
//
// That is the exact shape of a completed reconcile. `sanho sync` writes
// the merge result into the worktree and then records the target as the
// base, and re-running the merge against the result is a no-op — which
// is what makes it checkable after the fact instead of taken on trust.
//
// A merge that cannot run is not a grant. The lock, the temp refs and
// the object lookups can all fail, and every one of those is a failure
// to prove.
//
// TWO ancestors are tried, and both answers are sound proofs of the same
// thing:
//
//   - The recorded base's docs tree. "Merging the candidate in changes
//     nothing" then means the worktree already carries the base→candidate
//     changes, which is what a completed reconcile looks like.
//   - The EMPTY tree. With no shared history the merge is the union of
//     both sides, so it equals the worktree only when the worktree holds
//     every path the candidate does, at identical content — a strictly
//     stronger statement than the first.
//
// The second is not a relaxation; it is the ancestor `sanho sync` itself
// used. A recorded base that canonical no longer knows (history was
// rewritten) is unusable as a merge base for the FLOW even when its tree
// object is still lying around locally, so the flow merged against the
// empty tree — and a guard that insisted on the local leftover judged a
// different merge from the one that was actually performed, and refused
// `sanho sync --rebase-onto`: the tool's own rewrite-recovery advice.
func (s statePort) absorbedByWorktree(ctx context.Context, recorded provenance.Base, hasRecorded bool, worktree, candidateTree string) bool {
	for _, baseTree := range s.absorptionAncestors(ctx, recorded, hasRecorded) {
		result, err := canonical.MergeTree(ctx, s.ws.root, baseTree, worktree, candidateTree)
		if err == nil && result.Clean && result.Tree == worktree {
			return true
		}
	}
	return false
}

// absorptionAncestors lists the merge bases the absorption test tries,
// most precise first.
func (s statePort) absorptionAncestors(ctx context.Context, recorded provenance.Base, hasRecorded bool) []string {
	var ancestors []string
	if hasRecorded {
		if tree, resolved, err := s.resolveDocsTree(ctx, recorded); err == nil && resolved {
			ancestors = append(ancestors, tree)
		}
	}
	if empty, err := s.ws.repo.EmptyTree(ctx); err == nil {
		ancestors = append(ancestors, empty)
	}
	return ancestors
}

// causeOfBaseRefusal renders a guard refusal for a user-facing line,
// stripped of the sentinel's own text.
func causeOfBaseRefusal(err error) string {
	var refusal *errBaseNotCorroborated
	if errors.As(err, &refusal) {
		return refusal.detail
	}
	return causeOf(err)
}
