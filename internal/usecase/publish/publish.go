// Package publish orchestrates the pre-push publication flow of sanho
// v0.2 (sanho-v0.2.md §5.3): gate checks, the four-case analysis
// (decided in domain/publish), canonical-side auto-merge, CAS push with
// bounded retry, and the base-advance rule. It never touches the app
// worktree, index, or refs (worktree inviolability).
package publish

import (
	"context"
	"errors"
	"fmt"
	"strings"

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

// headsPrefix is the only ref namespace publication considers; tag
// pushes pass through untouched (§5.3 step 1).
const headsPrefix = "refs/heads/"

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

// Rejection sentinels. Each maps to one §5.9 machine error code, so the
// CLI can route on errors.Is without reading messages:
//
//	ErrSyncInProgress   → sync_in_progress
//	ErrMarkersPresent   → markers_present
//	ErrSyncRequired     → sync_required
//	ErrHistoryRewritten → history_rewritten
//
// The two that carry file lists are raised as the struct errors below,
// which report themselves as these sentinels under errors.Is and hand
// over their paths under errors.As. Canonical-unreachable is not
// redefined here: the port surfaces domain/publish.ErrUnreachable and
// this package wraps it unchanged.
var (
	ErrSyncInProgress   = errors.New("a conflicted sync is in progress")
	ErrMarkersPresent   = errors.New("docs contain unresolved conflict markers")
	ErrSyncRequired     = errors.New("docs changes must be reconciled with 'sanho sync' before publishing")
	ErrHistoryRewritten = errors.New("canonical history was rewritten and the recorded base is unreachable")
)

// MarkersPresentError names the committed docs files carrying conflict
// markers (§5.3 step 3).
type MarkersPresentError struct {
	// Tip is the pushed tip the markers were found in.
	Tip string
	// Paths are the offending docs files, repository-relative.
	Paths []string
}

func (e *MarkersPresentError) Error() string {
	return fmt.Sprintf("%s: %s in %s", ErrMarkersPresent, strings.Join(e.Paths, ", "), shortOID(e.Tip))
}

func (e *MarkersPresentError) Is(target error) bool { return target == ErrMarkersPresent }

// SyncRequiredError rejects a push that cannot be published without the
// user reconciling first (§5.3 case ③-conflict, and the exhausted CAS
// retry budget). Conflicts is empty when there is nothing file-specific
// to report.
type SyncRequiredError struct {
	// Base and Head are the OIDs the §5.9 push-rejection message quotes.
	Base string
	Head string
	// Conflicts lists conflicted docs paths, if any.
	Conflicts []string
	// Reason is a short machine-stable cause: "conflicts", "cas_retry_exhausted",
	// or "no_base".
	Reason string
}

func (e *SyncRequiredError) Error() string {
	detail := e.Reason
	if len(e.Conflicts) > 0 {
		detail = strings.Join(e.Conflicts, ", ")
	}
	return fmt.Sprintf("%s (base %s → %s): %s", ErrSyncRequired, shortOID(e.Base), shortOID(e.Head), detail)
}

func (e *SyncRequiredError) Is(target error) bool { return target == ErrSyncRequired }

// Reason values for SyncRequiredError.
const (
	// ReasonConflicts: the three-way merge produced conflicts.
	ReasonConflicts = "conflicts"
	// ReasonCASExhausted: MaxCASRetries publishers won the race in a row.
	ReasonCASExhausted = "cas_retry_exhausted"
	// ReasonNoBase: the workspace has no recorded base, so no merge base
	// exists to publish from. `sanho sync` establishes one.
	ReasonNoBase = "no_base"
)

// Outcome reports one pre-push evaluation.
type Outcome struct {
	// Published is the new canonical head when a publication happened.
	Published string
	// Case records the decided case for the (deduplicated) tips: the
	// case of the last tip processed, which for the ordinary
	// single-branch push is simply "the" case.
	Case pubdom.Case
	// Conflicts lists conflicted paths when the push is rejected with
	// sync guidance.
	Conflicts []string
	// BaseAdvanced reports whether §5.3 step 6 moved the base file.
	BaseAdvanced bool
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

// tip is one deduplicated publication candidate: the pushed commit, its
// docs tree, and the ref facts the canonical commit message quotes.
type tip struct {
	oid       string
	docsTree  string
	branch    string
	remoteOID string
	// publishedTree is the tree actually written for this tip — docsTree
	// for a fast-forward, the merge result for an auto-merge. The
	// base-advance rule compares the worktree against what was
	// published, not against what was pushed.
	publishedTree string
}

// Run evaluates the hook's ref updates per §5.3 steps 1–6. A nil error
// means the push may proceed; a returned error carries the §5.9
// guidance (sync required / markers present / canonical unreachable /
// history rewritten) and the hook exits non-zero without any remote
// ref changed.
func (u *UseCase) Run(ctx context.Context, updates []RefUpdate) (Outcome, error) {
	// Step 1 — filter. Tag pushes and branch deletions are not ours.
	candidates := publishable(updates)
	if len(candidates) == 0 {
		return Outcome{Case: pubdom.CaseUpToDate}, nil
	}

	// Step 2 — a conflicted sync owns the docs worktree until it is
	// finished; publishing over it would push a half-resolved state.
	inProgress, err := u.State.SyncInProgress()
	if err != nil {
		return Outcome{}, fmt.Errorf("check sync state: %w", err)
	}
	if inProgress {
		return Outcome{}, ErrSyncInProgress
	}

	// Step 3 — marker gate on every pushed tip, before any network work,
	// so a rejected push costs nothing.
	if err := u.gateMarkers(ctx, candidates); err != nil {
		return Outcome{}, err
	}

	// Step 4 — fetch. Write paths fail closed on an unreachable
	// canonical (§5.2); the port's ErrUnreachable travels up intact.
	if err := u.Canonical.Fetch(ctx); err != nil {
		return Outcome{}, fmt.Errorf("refresh canonical repository: %w", err)
	}

	// Step 5 — resolve docs trees and deduplicate. Identical trees
	// publish once; stdin order decides which ref describes them.
	tips, err := u.resolveTips(ctx, candidates)
	if err != nil {
		return Outcome{}, err
	}

	base, hasBase, err := u.State.LoadBase()
	if err != nil {
		return Outcome{}, fmt.Errorf("read base file: %w", err)
	}

	outcome := Outcome{Case: pubdom.CaseUpToDate}
	for i := range tips {
		t := &tips[i]
		published, decided, err := u.publishTip(ctx, t, &base, hasBase)
		if err != nil {
			var syncErr *SyncRequiredError
			if errors.As(err, &syncErr) {
				outcome.Conflicts = syncErr.Conflicts
			}
			return outcome, err
		}
		outcome.Case = decided
		if published == "" {
			continue
		}
		outcome.Published = published

		// Step 6 — base-advance rule.
		advanced, err := u.advanceBase(ctx, published, t.publishedTree)
		if err != nil {
			return outcome, err
		}
		if advanced {
			base = provenance.Base{Commit: published, Tree: t.publishedTree}
			hasBase = true
			outcome.BaseAdvanced = true
		}
	}
	return outcome, nil
}

// publishable applies §5.3 step 1: branch updates only, deletions
// (all-zero local OID) excluded.
func publishable(updates []RefUpdate) []RefUpdate {
	var kept []RefUpdate
	for _, u := range updates {
		if !strings.HasPrefix(u.LocalRef, headsPrefix) {
			continue
		}
		if u.LocalOID == "" || isZeroOID(u.LocalOID) {
			continue
		}
		kept = append(kept, u)
	}
	return kept
}

// isZeroOID recognizes git's deletion sentinel in both hash sizes.
func isZeroOID(oid string) bool {
	return strings.Trim(oid, "0") == ""
}

func (u *UseCase) gateMarkers(ctx context.Context, updates []RefUpdate) error {
	scanned := make(map[string]bool, len(updates))
	for _, update := range updates {
		if scanned[update.LocalOID] {
			continue
		}
		scanned[update.LocalOID] = true

		paths, err := u.App.ScanDocsBlobsForMarkers(ctx, update.LocalOID)
		if err != nil {
			return fmt.Errorf("scan docs of %s for conflict markers: %w", shortOID(update.LocalOID), err)
		}
		if len(paths) > 0 {
			return &MarkersPresentError{Tip: update.LocalOID, Paths: paths}
		}
	}
	return nil
}

// resolveTips maps each pushed ref to its docs tree and drops later
// duplicates of a tree already queued (§5.3 step 5 "deduplicate
// identical Ts", in stdin order).
func (u *UseCase) resolveTips(ctx context.Context, updates []RefUpdate) ([]tip, error) {
	var tips []tip
	seen := make(map[string]bool, len(updates))

	for _, update := range updates {
		docsTree, err := u.App.DocsTreeOf(ctx, update.LocalOID)
		if err != nil {
			return nil, fmt.Errorf("resolve docs tree of %s: %w", shortOID(update.LocalOID), err)
		}
		if seen[docsTree] {
			continue
		}
		seen[docsTree] = true

		remoteOID := update.RemoteOID
		if isZeroOID(remoteOID) {
			remoteOID = ""
		}
		tips = append(tips, tip{
			oid:       update.LocalOID,
			docsTree:  docsTree,
			branch:    strings.TrimPrefix(update.LocalRef, headsPrefix),
			remoteOID: remoteOID,
		})
	}
	return tips, nil
}

// publishTip runs the §5.3 case analysis for one tip, with the bounded
// CAS retry loop around it. It returns the published canonical commit
// ("" when nothing needed publishing) and the case that decided the
// outcome.
func (u *UseCase) publishTip(ctx context.Context, t *tip, base *provenance.Base, hasBase bool) (string, pubdom.Case, error) {
	decided := pubdom.CaseUpToDate

	for attempt := 0; attempt < MaxCASRetries; attempt++ {
		head, headTree, err := u.Canonical.Head(ctx)
		if err != nil {
			return "", decided, fmt.Errorf("read canonical head: %w", err)
		}

		// Case ① short-circuits before the base is consulted at all, so
		// a workspace with a missing or orphaned base can still push
		// docs-identical (or docs-free) commits.
		if t.docsTree == headTree {
			return "", pubdom.CaseUpToDate, nil
		}
		if !hasBase {
			// No merge base means no safe way to combine this tip with
			// canonical. `sanho sync` establishes one and succeeds in
			// this state, so the guidance stays closed (D3).
			return "", pubdom.CaseUnknownBase, &SyncRequiredError{Head: head, Reason: ReasonNoBase}
		}

		decided, err = u.decideWithReanchor(ctx, t, base, head, headTree)
		if err != nil {
			return "", decided, err
		}

		publishedTree, err := u.treeToPublish(ctx, t, decided, *base, head)
		if err != nil {
			return "", decided, err
		}

		newHead, err := u.commitPublication(ctx, t, publishedTree, head)
		if err != nil {
			return "", decided, err
		}

		switch err := u.Canonical.PushHead(ctx, newHead, head); {
		case err == nil:
			t.publishedTree = publishedTree
			return newHead, decided, nil
		case errors.Is(err, pubdom.ErrNonFastForward):
			// A racing publisher won. Refetch and re-enter the case
			// analysis from scratch; the merge must be recomputed
			// against the new head, never replayed.
			if err := u.Canonical.Fetch(ctx); err != nil {
				return "", decided, fmt.Errorf("refresh canonical repository: %w", err)
			}
		default:
			return "", decided, fmt.Errorf("publish to canonical: %w", err)
		}
	}

	head, _, err := u.Canonical.Head(ctx)
	if err != nil {
		return "", decided, fmt.Errorf("read canonical head: %w", err)
	}
	return "", decided, &SyncRequiredError{Base: base.Commit, Head: head, Reason: ReasonCASExhausted}
}

// decideWithReanchor runs domain Decide and, on case ④, attempts the
// docs-base-tree re-anchoring of §5.3 before giving up. Re-anchoring
// does not consume a CAS attempt: it is a correction of local state, not
// a lost race.
func (u *UseCase) decideWithReanchor(ctx context.Context, t *tip, base *provenance.Base, head, headTree string) (pubdom.Case, error) {
	decided, err := u.decide(ctx, t, *base, head, headTree)
	if err != nil {
		return decided, err
	}
	if decided != pubdom.CaseUnknownBase {
		return decided, nil
	}

	anchor, found, err := u.Canonical.FindCommitByDocsTree(ctx, base.Tree)
	if err != nil {
		return decided, fmt.Errorf("search canonical history for docs tree %s: %w", shortOID(base.Tree), err)
	}
	if !found {
		return decided, fmt.Errorf("%w: base %s is unknown to canonical head %s",
			ErrHistoryRewritten, shortOID(base.Commit), shortOID(head))
	}

	*base = provenance.Base{Commit: anchor, Tree: base.Tree}
	decided, err = u.decide(ctx, t, *base, head, headTree)
	if err != nil {
		return decided, err
	}
	if decided == pubdom.CaseUnknownBase {
		// The re-anchored commit exists but is not on the published
		// branch: the rewrite moved the content off the line of history
		// publication targets, which only `sanho sync --rebase-onto` can
		// straighten out.
		return decided, fmt.Errorf("%w: re-anchored base %s is not an ancestor of canonical head %s",
			ErrHistoryRewritten, shortOID(anchor), shortOID(head))
	}
	return decided, nil
}

func (u *UseCase) decide(ctx context.Context, t *tip, base provenance.Base, head, headTree string) (pubdom.Case, error) {
	known, err := u.Canonical.ResolveCommit(ctx, base.Commit)
	if err != nil {
		return pubdom.CaseUnknownBase, fmt.Errorf("resolve base %s in canonical: %w", shortOID(base.Commit), err)
	}

	ancestor := false
	if known {
		if ancestor, err = u.Canonical.IsAncestor(ctx, base.Commit, head); err != nil {
			return pubdom.CaseUnknownBase, fmt.Errorf("check whether base %s precedes canonical head: %w", shortOID(base.Commit), err)
		}
	}

	return pubdom.Decide(pubdom.Inputs{
		Base:           base,
		TipDocsTree:    t.docsTree,
		Head:           head,
		HeadDocsTree:   headTree,
		BaseKnown:      known,
		BaseIsAncestor: ancestor,
	}), nil
}

// treeToPublish produces the docs tree that will become the canonical
// commit: the tip's own tree for a fast-forward, the merge result for an
// auto-merge. Both first import the tip so its trees are addressable
// clone-side.
func (u *UseCase) treeToPublish(ctx context.Context, t *tip, decided pubdom.Case, base provenance.Base, head string) (string, error) {
	if err := u.Canonical.FetchFromApp(ctx, t.oid); err != nil {
		return "", fmt.Errorf("import %s into the canonical clone: %w", shortOID(t.oid), err)
	}

	if decided == pubdom.CaseFastForward {
		return t.docsTree, nil
	}

	tree, conflicts, clean, err := u.Canonical.MergeDocs(ctx, base.Commit, t.docsTree, head)
	if err != nil {
		return "", fmt.Errorf("merge docs against canonical head: %w", err)
	}
	if !clean {
		return "", &SyncRequiredError{
			Base:      base.Commit,
			Head:      head,
			Conflicts: conflicts,
			Reason:    ReasonConflicts,
		}
	}
	return tree, nil
}

func (u *UseCase) commitPublication(ctx context.Context, t *tip, tree, parent string) (string, error) {
	repoName, currentBranch, err := u.App.RepoIdentity(ctx)
	if err != nil {
		return "", fmt.Errorf("read app repository identity: %w", err)
	}
	branch := t.branch
	if branch == "" {
		branch = currentBranch
	}

	subjects, err := u.App.DocsCommitSubjects(ctx, t.remoteOID, t.oid)
	if err != nil {
		return "", fmt.Errorf("collect docs commit subjects: %w", err)
	}

	message := pubdom.CommitMeta{
		RepoName:    repoName,
		Branch:      branch,
		WorkspaceID: u.WorkspaceID,
		TipOID:      t.oid,
		Subjects:    subjects,
	}.Message()

	newHead, err := u.Canonical.CommitDocsTree(ctx, tree, parent, u.ActorName, u.ActorEmail, message)
	if err != nil {
		return "", fmt.Errorf("create canonical commit: %w", err)
	}
	return newHead, nil
}

// advanceBase implements §5.3 step 6: the base file records which
// canonical state the *worktree* docs derive from, so it may only move
// when the worktree is byte-identical to what was just published.
// Anything else (a case-③ merge the worktree has not seen, uncommitted
// edits) leaves it alone — `sanho pull` moves it later.
func (u *UseCase) advanceBase(ctx context.Context, published, publishedTree string) (bool, error) {
	worktree, err := u.App.WorktreeDocsTree(ctx)
	if err != nil {
		return false, fmt.Errorf("hash worktree docs: %w", err)
	}
	if worktree != publishedTree {
		return false, nil
	}
	if err := u.State.SaveBase(provenance.Base{Commit: published, Tree: publishedTree}); err != nil {
		return false, fmt.Errorf("record new base: %w", err)
	}
	return true, nil
}

// shortOID renders an OID for user-facing messages at the §5.9 width.
func shortOID(oid string) string {
	const width = 12
	if len(oid) <= width {
		if oid == "" {
			return "(none)"
		}
		return oid
	}
	return oid[:width]
}
