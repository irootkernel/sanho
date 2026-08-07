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
	// MergeDocsTrees runs the §5.4 merge clone-side over three docs
	// TREES. Trees rather than commits is what lets the evaluation pass
	// chain several tips together without writing anything: the second
	// tip merges against the tree the first one would publish, which has
	// no commit yet. An empty baseTree means "no common history", i.e.
	// the empty tree.
	MergeDocsTrees(ctx context.Context, baseTree, oursTree, theirsTree string) (tree string, conflicts []string, clean bool, err error)
	// DocsTreeOfCommit resolves a canonical commit's docs tree.
	DocsTreeOfCommit(ctx context.Context, commit string) (string, error)
	// DocsFileCount counts the files a canonical commit publishes. It
	// exists for one message: the refusal that names how many docs an
	// empty-tree publication would delete (§5.3, F-H2).
	DocsFileCount(ctx context.Context, commit string) (int, error)
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
	// ScanDocsBlobsSince scans the docs blobs a push would INTRODUCE
	// (§5.4 detector); returns conflicted paths. Unreadable blobs error.
	//
	// since is the branch's previous remote tip, or "" for a branch the
	// remote has never seen — in which case the whole tree is scanned.
	// Scoping to the diff is what keeps the gate proportional to the
	// push instead of to the docs directory (F-H4b).
	ScanDocsBlobsSince(ctx context.Context, since, commit string) ([]string, error)
	// DocsCommitSubjects lists subjects of commits since base that
	// touched docs, oldest first (canonical commit body).
	DocsCommitSubjects(ctx context.Context, base, tip string) ([]string, error)
	// RepoIdentity returns the app repo short name and current branch
	// for the canonical commit subject.
	RepoIdentity(ctx context.Context) (repoName, branch string, err error)
	// EmptyTree returns this repository's empty-tree OID. Publication
	// needs it for exactly one state: an empty canonical repository has
	// no head tree, and "no docs published yet" is the empty tree.
	EmptyTree(ctx context.Context) (string, error)
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
//	ErrEmptyPublish     → docs_would_be_deleted
//
// The three that carry detail are raised as the struct errors below,
// which report themselves as these sentinels under errors.Is and hand
// over their payload under errors.As. Canonical-unreachable is not
// redefined here: the port surfaces domain/publish.ErrUnreachable and
// this package wraps it unchanged.
//
// None of these strings names a command. Guidance is the CLI's job
// (§5.9 catalog): a sentinel that spelled a next step would be a second,
// uncataloged copy of it — which is exactly what the widened closure
// gate now forbids.
var (
	ErrSyncInProgress   = errors.New("a conflicted sync is in progress")
	ErrMarkersPresent   = errors.New("docs contain unresolved conflict markers")
	ErrSyncRequired     = errors.New("docs changes must be reconciled before publishing")
	ErrHistoryRewritten = errors.New("canonical history was rewritten and the recorded base is unreachable")
	ErrEmptyPublish     = errors.New("publishing this branch would delete every canonical doc")
)

// EmptyPublishError rejects a tip whose docs tree is empty while
// canonical still holds documents (§5.3, F-H2).
//
// The state is ordinary and the intent is ambiguous, which is why it is
// a refusal rather than a publication: pushing a branch created before
// the docs directory existed, or one where `git rm -r docs` was the
// point, look identical from here. Fail closed, name the branch, and let
// SANHO_ALLOW_DOCS_DELETION say which it was.
type EmptyPublishError struct {
	// Branch and Tip identify the push that would empty canonical.
	Branch string
	Tip    string
	// Head is the canonical head whose content would be deleted.
	Head string
	// DocsCount is how many files that head publishes, or -1 when it
	// could not be counted (the refusal stands either way).
	DocsCount int
}

func (e *EmptyPublishError) Error() string {
	return fmt.Sprintf("%s: %s carries no docs (canonical head %s)", ErrEmptyPublish, e.Branch, shortOID(e.Head))
}

func (e *EmptyPublishError) Is(target error) bool { return target == ErrEmptyPublish }

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
	// Published is the FINAL canonical head when a publication happened
	// — the last link of the chain, which is what the success line
	// names.
	Published string
	// PublishedOIDs lists every canonical commit this push created, in
	// order. A multi-ref push publishes once per distinct docs tree, and
	// reporting only the last one is how the multi-ref clobber (F-C1)
	// stayed invisible: the caller must be able to see the whole chain.
	PublishedOIDs []string
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
	// AllowEmptyPublish disarms the §5.3 empty-docs refusal (F-H2) for
	// one push. The CLI sets it from SANHO_ALLOW_DOCS_DELETION=1: the
	// deletion of every canonical doc is a legitimate operation, it is
	// just never one to infer from a branch that happens to carry no
	// docs.
	AllowEmptyPublish bool
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

// snapshot is the canonical state one evaluation is decided against. It
// is read once per attempt and then held still: every tip in a push is
// judged against the same head, which is what stops a later tip from
// being decided against a head its own siblings just moved (F-C1).
type snapshot struct {
	head      string
	headTree  string
	bootstrap bool
}

// publication is one validated write: the tip that motivates it and the
// exact docs tree it will carry. The tree is computed in the evaluation
// pass, so the publication pass has nothing left to decide.
type publication struct {
	tip     *tip
	tree    string
	decided pubdom.Case
}

// pushPlan is a whole push, validated before anything is written.
type pushPlan struct {
	publications []publication
	// decided is the case of the last publication — "the" case for the
	// ordinary single-branch push.
	decided pubdom.Case
}

// Run evaluates the hook's ref updates per §5.3 steps 1–6. A nil error
// means the push may proceed; a returned error carries the §5.9
// guidance (sync required / markers present / canonical unreachable /
// history rewritten / empty publication) and the hook exits non-zero.
//
// The shape is **evaluate then publish**, and it is the fix for F-C1 and
// F-H1 both.
//
// The first pass decides every tip against ONE frozen (head, base)
// snapshot and computes, with tree-level merges only, the exact tree
// each publication would write — chaining, so tip 2 is evaluated against
// the tree tip 1 would leave rather than against the tree canonical
// happens to hold. Nothing in that pass mutates canonical: it writes
// objects into the private clone and no ref anywhere. So a push in which
// any tip conflicts, lacks a base, would empty canonical, or sits on
// rewritten history is rejected whole, with canonical untouched — which
// is what makes the template's "no remote ref was changed" true by
// construction.
//
// The second pass then commits and CAS-pushes the precomputed trees in
// order, each one parented on the previous publication. A later tip is
// never fast-forwarded past an earlier one: the chain is why pushing two
// branches with different docs no longer silently deletes one of them.
//
// The base advances once, at the end, against the final publication.
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
	for attempt := 0; attempt < MaxCASRetries; attempt++ {
		snap, err := u.canonicalSnapshot(ctx)
		if err != nil {
			return outcome, err
		}

		// Re-anchoring corrects the base in place, so each attempt starts
		// from the recorded value rather than from the previous attempt's
		// correction.
		anchored := base
		plan, err := u.evaluate(ctx, tips, &anchored, hasBase, snap)
		if err != nil {
			var syncErr *SyncRequiredError
			if errors.As(err, &syncErr) {
				outcome.Conflicts = syncErr.Conflicts
			}
			return outcome, err
		}
		outcome.Case = plan.decided
		if len(plan.publications) == 0 {
			return outcome, nil
		}

		published, publishErr := u.publishPlan(ctx, plan, snap)
		outcome.PublishedOIDs = append(outcome.PublishedOIDs, published...)
		if len(outcome.PublishedOIDs) > 0 {
			outcome.Published = outcome.PublishedOIDs[len(outcome.PublishedOIDs)-1]
		}

		switch {
		case publishErr == nil:
		case errors.Is(publishErr, pubdom.ErrNonFastForward):
			// A racing publisher won. Refetch and re-enter the evaluation
			// from scratch; the merge must be recomputed against the new
			// head, never replayed.
			if fetchErr := u.Canonical.Fetch(ctx); fetchErr != nil {
				return outcome, fmt.Errorf("refresh canonical repository: %w", fetchErr)
			}
			continue
		default:
			return outcome, publishErr
		}

		// Step 6 — base-advance rule, once, against the final publication.
		final := plan.publications[len(plan.publications)-1]
		advanced, err := u.advanceBase(ctx, outcome.Published, final.tree)
		if err != nil {
			return outcome, err
		}
		outcome.BaseAdvanced = advanced
		return outcome, nil
	}

	head, _, _, err := u.canonicalHead(ctx)
	if err != nil {
		return outcome, err
	}
	return outcome, &SyncRequiredError{Base: base.Commit, Head: head, Reason: ReasonCASExhausted}
}

// evaluate is pass 1: decide and compute, never write a ref.
//
// accTree is the tree the chain has reached — canonical's frozen head
// tree before the first publication, then each computed publication in
// turn. Every tip is merged against it rather than against the frozen
// head tree, which is precisely what a fast-forward would have thrown
// away.
func (u *UseCase) evaluate(ctx context.Context, tips []tip, base *provenance.Base, hasBase bool, snap snapshot) (pushPlan, error) {
	emptyTree, err := u.App.EmptyTree(ctx)
	if err != nil {
		return pushPlan{}, fmt.Errorf("resolve the empty tree: %w", err)
	}

	plan := pushPlan{decided: pubdom.CaseUpToDate}
	accTree := snap.headTree

	for i := range tips {
		t := &tips[i]

		// Case ① short-circuits before the base is consulted at all, so a
		// workspace with a missing or orphaned base can still push
		// docs-identical (or docs-free) commits.
		if t.docsTree == accTree {
			continue
		}
		if !hasBase && !snap.bootstrap {
			// No merge base means no safe way to combine this tip with
			// canonical. `sanho sync` establishes one and succeeds in this
			// state, so the guidance stays closed (D3).
			return pushPlan{}, &SyncRequiredError{Head: snap.head, Reason: ReasonNoBase}
		}
		if err := u.refuseEmptyPublication(ctx, t, emptyTree, accTree, snap); err != nil {
			return pushPlan{}, err
		}

		var decided pubdom.Case
		if snap.bootstrap {
			decided = decideBootstrap(t.docsTree, snap.headTree)
		} else if decided, err = u.decideWithReanchor(ctx, t, base, snap.head, snap.headTree); err != nil {
			return pushPlan{}, err
		}

		// Importing the tip writes objects into the workspace-private
		// clone. It changes no ref there and nothing at all on origin, so
		// it is not a canonical mutation in the sense F-H1 is about.
		if err := u.Canonical.FetchFromApp(ctx, t.oid); err != nil {
			return pushPlan{}, fmt.Errorf("import %s into the canonical clone: %w", shortOID(t.oid), err)
		}

		tree := t.docsTree
		chained := len(plan.publications) > 0
		if decided == pubdom.CaseAutoMerge || chained {
			// A second publication in the same push is always a merge,
			// whatever the case analysis said against the frozen head: the
			// content the first one added is not in this tip, and
			// fast-forwarding over it is the data loss F-C1 names.
			decided = pubdom.CaseAutoMerge
			if tree, err = u.mergeOnto(ctx, t, *base, hasBase, snap, accTree); err != nil {
				return pushPlan{}, err
			}
		}
		if tree == accTree {
			// The merge added nothing: this tip's content is already in
			// what the chain will publish.
			continue
		}

		plan.publications = append(plan.publications, publication{tip: t, tree: tree, decided: decided})
		plan.decided = decided
		accTree = tree
	}
	return plan, nil
}

// refuseEmptyPublication is the F-H2 gate: a tip with no docs at all,
// pushed at a canonical that has them, would publish the empty tree and
// delete every document. Fail closed and name the branch.
func (u *UseCase) refuseEmptyPublication(ctx context.Context, t *tip, emptyTree, accTree string, snap snapshot) error {
	if u.AllowEmptyPublish || t.docsTree != emptyTree || accTree == emptyTree {
		return nil
	}
	count, err := u.Canonical.DocsFileCount(ctx, snap.head)
	if err != nil {
		// The refusal does not depend on the count; only its wording does.
		count = -1
	}
	return &EmptyPublishError{Branch: t.branch, Tip: t.oid, Head: snap.head, DocsCount: count}
}

// mergeOnto three-way merges the tip's docs onto the tree the chain has
// reached, taking the frozen base as the common ancestor.
func (u *UseCase) mergeOnto(ctx context.Context, t *tip, base provenance.Base, hasBase bool, snap snapshot, ontoTree string) (string, error) {
	baseTree, err := u.mergeBaseTree(ctx, base, hasBase, snap)
	if err != nil {
		return "", err
	}
	tree, conflicts, clean, err := u.Canonical.MergeDocsTrees(ctx, baseTree, t.docsTree, ontoTree)
	if err != nil {
		return "", fmt.Errorf("merge docs against canonical head: %w", err)
	}
	if !clean {
		return "", &SyncRequiredError{
			Base:      base.Commit,
			Head:      snap.head,
			Conflicts: conflicts,
			Reason:    ReasonConflicts,
		}
	}
	return tree, nil
}

// mergeBaseTree resolves the common ancestor of every merge in one push:
// the docs tree of the frozen (possibly re-anchored) base. An empty
// string means "no shared history", which MergeDocsTrees reads as the
// empty tree — the honest ancestor when canonical has no commits.
func (u *UseCase) mergeBaseTree(ctx context.Context, base provenance.Base, hasBase bool, snap snapshot) (string, error) {
	if snap.bootstrap || !hasBase {
		return "", nil
	}
	tree, err := u.Canonical.DocsTreeOfCommit(ctx, base.Commit)
	if err != nil {
		return "", fmt.Errorf("resolve the docs tree of base %s: %w", shortOID(base.Commit), err)
	}
	return tree, nil
}

// publishPlan is pass 2: commit and CAS-push the precomputed trees in
// order, each parented on the one before. Nothing here decides anything.
func (u *UseCase) publishPlan(ctx context.Context, plan pushPlan, snap snapshot) ([]string, error) {
	parent := snap.head
	var published []string

	for i := range plan.publications {
		step := &plan.publications[i]

		newHead, err := u.commitPublication(ctx, step.tip, step.tree, parent)
		if err != nil {
			return published, err
		}
		switch err := u.Canonical.PushHead(ctx, newHead, parent); {
		case err == nil:
		case errors.Is(err, pubdom.ErrNonFastForward):
			return published, err
		default:
			return published, fmt.Errorf("publish to canonical: %w", err)
		}

		step.tip.publishedTree = step.tree
		published = append(published, newHead)
		parent = newHead
	}
	return published, nil
}

// canonicalSnapshot freezes the canonical facts one evaluation pass is
// decided against.
func (u *UseCase) canonicalSnapshot(ctx context.Context) (snapshot, error) {
	head, headTree, bootstrap, err := u.canonicalHead(ctx)
	if err != nil {
		return snapshot{}, err
	}
	return snapshot{head: head, headTree: headTree, bootstrap: bootstrap}, nil
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
		since := update.RemoteOID
		if isZeroOID(since) {
			since = ""
		}
		// One scan per (previous remote tip, tip) pair: the same tip
		// pushed to two remotes with different previous states is two
		// different diffs, and only re-scanning both keeps the gate
		// honest.
		key := since + ".." + update.LocalOID
		if scanned[key] {
			continue
		}
		scanned[key] = true

		paths, err := u.App.ScanDocsBlobsSince(ctx, since, update.LocalOID)
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
//
// Identical tip OIDs are dropped first (F-M6). `git push --all` on a
// repository where two branches point at the same commit sends the same
// OID twice; resolving its docs tree twice is a wasted git invocation
// per duplicate before the tree-level dedup notices.
func (u *UseCase) resolveTips(ctx context.Context, updates []RefUpdate) ([]tip, error) {
	var tips []tip
	seen := make(map[string]bool, len(updates))
	seenOID := make(map[string]bool, len(updates))

	for _, update := range updates {
		if seenOID[update.LocalOID] {
			continue
		}
		seenOID[update.LocalOID] = true

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

// canonicalHead reads canonical head, translating the "nothing has ever
// been published" state into the facts the case analysis needs: no head
// commit, and the empty tree as the head's docs tree.
//
// bootstrap is the flag that tells the rest of publishTip it is looking
// at a repository with no history rather than at one whose head merely
// happens to be unreachable.
func (u *UseCase) canonicalHead(ctx context.Context) (head, headTree string, bootstrap bool, err error) {
	head, headTree, err = u.Canonical.Head(ctx)
	switch {
	case err == nil:
		return head, headTree, false, nil
	case !errors.Is(err, pubdom.ErrEmptyBranch):
		return "", "", false, fmt.Errorf("read canonical head: %w", err)
	}

	empty, err := u.App.EmptyTree(ctx)
	if err != nil {
		return "", "", false, fmt.Errorf("resolve the empty tree: %w", err)
	}
	return "", empty, true, nil
}

// decideBootstrap is the §5.3 case analysis against an empty canonical
// repository, and it deliberately disregards the recorded base.
//
// An empty canonical has no history, so there is nothing for a base to
// be an ancestor of and nothing for it to be unknown *to*: BaseKnown and
// BaseIsAncestor are vacuously true, and the effective base is "no
// commit", which equals the (absent) head. Decide then returns
// CaseUpToDate when the tip carries no docs either, and otherwise
// CaseFastForward — publishing the tip's docs tree as a root commit with
// no parent and no lease.
//
// Treating a *recorded* base the same way is the point rather than an
// oversight. Feeding a stale base into the ordinary analysis would yield
// CaseUnknownBase and a "canonical history was rewritten" rejection,
// which is a false diagnosis: canonical was never written at all. The
// state is reachable in practice — a workspace whose canonical
// repository was emptied or replaced — and the honest response is to
// bootstrap it, which loses nothing because there is no upstream content
// to merge with.
func decideBootstrap(tipDocsTree, emptyTree string) pubdom.Case {
	return pubdom.Decide(pubdom.Inputs{
		Base:           provenance.Base{},
		TipDocsTree:    tipDocsTree,
		Head:           "",
		HeadDocsTree:   emptyTree,
		BaseKnown:      true,
		BaseIsAncestor: true,
	})
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
