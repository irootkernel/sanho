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
	// AbsorbedByTip proves the narrow content warrant used when a tip's
	// provenance trailer no longer resolves after a canonical rewrite:
	// merging canonical head into the tip over the empty tree is clean
	// and yields the tip tree unchanged.
	AbsorbedByTip(ctx context.Context, tipTree, head string) (bool, error)
	// GcAuto asks Git to maintain the private clone after publication.
	// It is best-effort and must never change the publication verdict.
	GcAuto(ctx context.Context) error
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
	// ScanDocsBlobsAgainst scans the docs blobs a publication would
	// INTRODUCE into canonical (§5.4 detector); returns conflicted paths.
	// Unreadable blobs error.
	//
	// publishedDocsTree is canonical head's docs tree — the state this
	// gate has already passed, by induction over every publication that
	// built it. Only content differing from it is scanned, which keeps
	// the gate proportional to the publication instead of to the docs
	// directory (F-H4b) without leaving a way in behind it. An empty
	// tree argument means "nothing published yet" and scans everything.
	ScanDocsBlobsAgainst(ctx context.Context, publishedDocsTree, commit string) ([]string, error)
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
	// NewestDocsBase returns the base named by the newest docs-base (or
	// legacy docs-version) trailer reachable from tip, which is the
	// pushed branch's own account of where its docs came from.
	//
	// It is the corroboration a fast-forward needs. The base FILE is
	// workspace state: one file, shared by every branch of the checkout,
	// and a branch switch can leave it describing a canonical state the
	// branch now standing there never derived from. The branch's own
	// history cannot be swapped that way.
	NewestDocsBase(ctx context.Context, tip string) (base provenance.Base, found bool, err error)
}

// StatePort is the workspace state publication needs.
type StatePort interface {
	LoadBase() (provenance.Base, bool, error)
	// SaveBase records a base only when the adapter can show, locally,
	// that it is not ahead of the docs the worktree carries; a candidate
	// it cannot vouch for is refused and nothing is written. Publication
	// only ever offers one it has just proved identical to the worktree,
	// so the guard is a second opinion here rather than a constraint.
	SaveBase(ctx context.Context, base provenance.Base) error
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
	// ReasonUncorroboratedBase: the recorded base would license a
	// fast-forward, and the pushed branch's own provenance does not
	// support it. See requireCorroboratedBase.
	ReasonUncorroboratedBase = "uncorroborated_base"
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
	// PublishedCases is the case decided for each entry of PublishedOIDs,
	// positionally. Reporting Case beside every OID was a reuse of the
	// LAST tip's label for all of them, so a two-branch push whose first
	// publication was a fast-forward and whose second was a merge
	// reported both as merges.
	PublishedCases []pubdom.Case
	// BaseAdvanceError reports a §5.3 step 6 base advance that could not
	// be completed after the publication itself succeeded.
	//
	// It is carried rather than returned. The push HAS happened, canonical
	// HAS moved, and failing the hook at that point would report a
	// publication that landed as a rejected push — the one message that is
	// simply false. The base is workspace state that `sanho pull` and the
	// re-derivation hooks correct on their own, so the honest outcome is
	// the success plus a line saying the pointer did not move.
	BaseAdvanceError error
	// MaintenanceError reports a best-effort private-clone gc failure
	// after publication. Like BaseAdvanceError, it is outcome detail and
	// never turns an already-landed publication into a rejected push.
	MaintenanceError error
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
	// The hook has normally applied this already; it is repeated here so
	// the use case's own contract does not depend on its caller.
	candidates := Publishable(updates)
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

	// Step 3 — fetch. Write paths fail closed on an unreachable
	// canonical (§5.2); the port's ErrUnreachable travels up intact.
	if err := u.Canonical.Fetch(ctx); err != nil {
		return Outcome{}, fmt.Errorf("refresh canonical repository: %w", err)
	}

	// Step 4 — marker gate on every pushed tip.
	//
	// It runs after the fetch, which reverses §5.3's listed order, and
	// the reason is the gate's baseline: what a push introduces is
	// measured against the docs canonical *already publishes*, so the
	// gate needs a current canonical head to be sound at all. The
	// cheap-rejection principle the old order served is kept where it
	// belongs — the sync-note refusal, which needs nothing but a local
	// file, is made at the hook boundary before the clone is even
	// opened.
	gateSnapshot, err := u.canonicalSnapshot(ctx)
	if err != nil {
		return Outcome{}, err
	}
	if err := u.gateMarkers(ctx, candidates, gateSnapshot.headTree); err != nil {
		return Outcome{}, err
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
		for i := range published {
			outcome.PublishedCases = append(outcome.PublishedCases, plan.publications[i].decided)
		}
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
		//
		// A failure here is NOT fatal, and that is the fix for M2. The
		// publication has landed: canonical moved, other workspaces can
		// already see it, and returning an error would make the hook print
		// the §5.9 rejection template — whose last line promises that no
		// remote ref was changed — over a push that changed one. The base
		// is local state with two independent repairs (`sanho pull` and
		// the re-derivation hooks), so the truthful outcome is the
		// publication plus a line saying the pointer stayed put.
		final := plan.publications[len(plan.publications)-1]
		advanced, err := u.advanceBase(ctx, outcome.Published, final.tree)
		outcome.BaseAdvanced = advanced
		outcome.BaseAdvanceError = err
		outcome.MaintenanceError = u.Canonical.GcAuto(ctx)
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
		imported := false
		if decided == pubdom.CaseFastForward && !snap.bootstrap {
			// The absorption warrant operates clone-side, so import the tip
			// before corroboration. This changes only the private object
			// database; a rejected push still changes no ref or remote.
			if err := u.Canonical.FetchFromApp(ctx, t.oid); err != nil {
				return pushPlan{}, fmt.Errorf("import %s into the canonical clone: %w", shortOID(t.oid), err)
			}
			imported = true
			if err := u.requireCorroboratedBase(ctx, t, *base, snap.head); err != nil {
				return pushPlan{}, err
			}
		}

		// Importing the tip writes objects into the workspace-private
		// clone. It changes no ref there and nothing at all on origin, so
		// it is not a canonical mutation in the sense F-H1 is about.
		if !imported {
			if err := u.Canonical.FetchFromApp(ctx, t.oid); err != nil {
				return pushPlan{}, fmt.Errorf("import %s into the canonical clone: %w", shortOID(t.oid), err)
			}
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

// Publishable applies §5.3 step 1: branch updates only, deletions
// (all-zero local OID) excluded.
//
// It is exported because the hook applies it before anything else: a
// push carrying only tags or only branch deletions is not publication's
// business at all, and deciding that at the boundary is what keeps such
// a push out of the sync gate and out of `canonical.Ensure` — which
// creates and fetches a clone, so an offline tag push used to fail for a
// reason that had nothing to do with it (M1).
func Publishable(updates []RefUpdate) []RefUpdate {
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

// gateMarkers refuses any pushed tip that would introduce docs carrying
// unresolved conflict markers into canonical (§5.3 step 3).
//
// The baseline is canonical head's docs tree, one value for the whole
// push, so a tip is scanned exactly once however many refs point at it —
// unlike the old per-(remote tip, tip) keying, the question no longer
// depends on which remote the ref is going to.
func (u *UseCase) gateMarkers(ctx context.Context, updates []RefUpdate, publishedDocsTree string) error {
	scanned := make(map[string]bool, len(updates))
	for _, update := range updates {
		if scanned[update.LocalOID] {
			continue
		}
		scanned[update.LocalOID] = true

		paths, err := u.App.ScanDocsBlobsAgainst(ctx, publishedDocsTree, update.LocalOID)
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

// requireCorroboratedBase is the second half of the fourth review's C2.
//
// A fast-forward is the one case that publishes the tip's docs tree
// *directly over* canonical head, with no merge — so everything canonical
// holds and the tip does not is deleted. The whole justification for
// that is the recorded base: base == head means nothing has landed
// upstream since these docs derived from it.
//
// The base file cannot carry that justification alone. It is workspace
// state — one file at the checkout root, shared by every branch — while
// the thing being published is a BRANCH. `git checkout` a branch whose
// docs predate sanho, or one whose provenance was never stamped, and the
// file still names canonical head while the documents under it derive
// from nothing of the sort. That is the reproduction: a long-lived
// branch with one stale document replaced all six of canonical's, at
// exit 0, reported as `(fast_forward)`.
//
// So the tip must vouch for the base out of its OWN history: the newest
// docs-base trailer reachable from it must name the recorded base, or a
// canonical ancestor of it. The ancestor half is not a loophole but the
// ordinary state — publication's own base advance moves the file past
// the commit the trailers name, so every workspace that has just pushed
// is in it.
//
// A trailer invalidated by rewritten history gets one narrower content
// warrant: the tip must absorb canonical head exactly. If neither proof
// holds, the CLI routes the base==head state to an explicit provenance
// restamp; `sanho sync` would be a no-op there. Nothing else about the
// push changes: an auto-merge combines both sides and never needed this,
// and a docs-identical tip short-circuits before the base is consulted.
func (u *UseCase) requireCorroboratedBase(ctx context.Context, t *tip, base provenance.Base, head string) error {
	stamped, found, err := u.App.NewestDocsBase(ctx, t.oid)
	if err != nil {
		return fmt.Errorf("read the docs provenance of %s: %w", shortOID(t.oid), err)
	}
	if found {
		if stamped.Commit == base.Commit {
			return nil
		}
		// A trailer naming a canonical ancestor of the recorded base is
		// the post-publication state, not a mismatch.
		ancestor, err := u.Canonical.IsAncestor(ctx, stamped.Commit, base.Commit)
		if err != nil {
			return fmt.Errorf("check whether %s precedes the recorded base: %w", shortOID(stamped.Commit), err)
		}
		if ancestor {
			return nil
		}
	}
	// A canonical rewrite can legitimately erase the commit stamped by
	// the resolution tip. When the tip already absorbs every entry at
	// the new head, publishing it deletes nothing from canonical, which
	// is the exact safety property the historical warrant stood for.
	// Merge failure or conflict is merely failure to prove that property;
	// keep the existing fail-closed rejection.
	if absorbed, absorbErr := u.Canonical.AbsorbedByTip(ctx, t.docsTree, head); absorbErr == nil && absorbed {
		return nil
	}
	return &SyncRequiredError{Base: base.Commit, Head: head, Reason: ReasonUncorroboratedBase}
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
	if err := u.State.SaveBase(ctx, provenance.Base{Commit: published, Tree: publishedTree}); err != nil {
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
