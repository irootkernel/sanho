package publish

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/irootkernel/sanho/internal/domain/provenance"
	pubdom "github.com/irootkernel/sanho/internal/domain/publish"
)

// Named fixture OIDs. Canonical starts one commit deep at (canonHead,
// canonTree); the workspace's tip carries tipTree.
var (
	canonRoot = commitOID(1)
	canonHead = commitOID(2)
	canonTree = treeOID(1)
	rootTree  = treeOID(0)

	appTip    = commitOID(10)
	appTipAlt = commitOID(11)
	tipTree   = treeOID(10)
	tipTreeB  = treeOID(11)

	mergedTree = treeOID(20)
)

// scenario builds a use case whose canonical history is
// canonRoot → canonHead, with one pushed branch tip.
type scenario struct {
	canonical *fakeCanonical
	app       *fakeApp
	state     *fakeState
	useCase   *UseCase
	updates   []RefUpdate
}

func newScenario(t *testing.T) *scenario {
	t.Helper()

	canonical := &fakeCanonical{
		branch: []commitPair{
			{commit: canonRoot, tree: rootTree},
			{commit: canonHead, tree: canonTree},
		},
		mergeTree: mergedTree,
	}
	app := &fakeApp{
		docsTrees:   map[string]string{appTip: tipTree, appTipAlt: tipTreeB},
		markerPaths: map[string][]string{},
		subjects:    map[string][]string{".." + appTip: {"docs: local edit"}},
		repoName:    "product",
		branch:      "main",
		worktree:    tipTree,
	}
	state := &fakeState{base: provenance.Base{Commit: canonHead, Tree: canonTree}, hasBase: true}

	return &scenario{
		canonical: canonical,
		app:       app,
		state:     state,
		useCase: &UseCase{
			Canonical:   canonical,
			App:         app,
			State:       state,
			ActorName:   "Publisher",
			ActorEmail:  "publisher@example.test",
			WorkspaceID: "product:/home/u/product",
		},
		updates: []RefUpdate{{
			LocalRef:  "refs/heads/main",
			LocalOID:  appTip,
			RemoteRef: "refs/heads/main",
			RemoteOID: strings.Repeat("0", 40),
		}},
	}
}

func (s *scenario) run(t *testing.T) (Outcome, error) {
	t.Helper()
	return s.useCase.Run(context.Background(), s.updates)
}

// --- Step 1: filtering ------------------------------------------------

func TestRunIgnoresNonBranchAndDeletedRefs(t *testing.T) {
	zero := strings.Repeat("0", 40)

	tests := []struct {
		name    string
		updates []RefUpdate
	}{
		{name: "no updates at all"},
		{
			name:    "tag push",
			updates: []RefUpdate{{LocalRef: "refs/tags/v1", LocalOID: appTip, RemoteRef: "refs/tags/v1"}},
		},
		{
			name:    "branch deletion with a 40-zero OID",
			updates: []RefUpdate{{LocalRef: "refs/heads/gone", LocalOID: zero, RemoteRef: "refs/heads/gone", RemoteOID: canonHead}},
		},
		{
			name:    "branch deletion with a 64-zero OID",
			updates: []RefUpdate{{LocalRef: "refs/heads/gone", LocalOID: strings.Repeat("0", 64), RemoteRef: "refs/heads/gone"}},
		},
		{
			name:    "empty local OID",
			updates: []RefUpdate{{LocalRef: "refs/heads/gone", LocalOID: "", RemoteRef: "refs/heads/gone"}},
		},
		{
			name:    "note ref",
			updates: []RefUpdate{{LocalRef: "refs/notes/commits", LocalOID: appTip}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := newScenario(t)
			s.updates = test.updates

			outcome, err := s.run(t)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if outcome.Case != pubdom.CaseUpToDate || outcome.Published != "" {
				t.Fatalf("outcome = %+v, want an untouched no-op", outcome)
			}
			// Nothing may be evaluated at all: no gate, no fetch.
			if s.state.loadCall != 0 || s.canonical.fetches != 0 || len(s.app.scanned) != 0 {
				t.Fatalf("a filtered-out push did work: loads=%d fetches=%d scans=%v",
					s.state.loadCall, s.canonical.fetches, s.app.scanned)
			}
		})
	}
}

// --- Step 2: sync-in-progress gate -----------------------------------

func TestRunRejectsWhileASyncIsInProgress(t *testing.T) {
	s := newScenario(t)
	s.state.inSync = true

	_, err := s.run(t)
	if !errors.Is(err, ErrSyncInProgress) {
		t.Fatalf("error = %v, want ErrSyncInProgress", err)
	}
	if s.canonical.fetches != 0 {
		t.Errorf("the gate ran after the network: fetches = %d", s.canonical.fetches)
	}
	if len(s.app.scanned) != 0 {
		t.Errorf("the sync gate must precede the marker scan; scanned %v", s.app.scanned)
	}
}

func TestRunPropagatesSyncStateErrors(t *testing.T) {
	s := newScenario(t)
	s.state.syncErr = errors.New("boom")

	if _, err := s.run(t); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error = %v, want the sync-state failure", err)
	}
}

// --- Step 3: marker gate ---------------------------------------------

func TestRunRejectsCommittedConflictMarkers(t *testing.T) {
	s := newScenario(t)
	s.app.markerPaths[appTip] = []string{"docs/api.md", "docs/schema.md"}

	_, err := s.run(t)
	if !errors.Is(err, ErrMarkersPresent) {
		t.Fatalf("error = %v, want ErrMarkersPresent", err)
	}

	var markersErr *MarkersPresentError
	if !errors.As(err, &markersErr) {
		t.Fatalf("error = %v, want a *MarkersPresentError carrying the paths", err)
	}
	want := []string{"docs/api.md", "docs/schema.md"}
	if !reflect.DeepEqual(markersErr.Paths, want) {
		t.Fatalf("paths = %v, want %v", markersErr.Paths, want)
	}
	if markersErr.Tip != appTip {
		t.Errorf("tip = %s, want %s", markersErr.Tip, appTip)
	}
	for _, path := range want {
		if !strings.Contains(err.Error(), path) {
			t.Errorf("message %q does not name %s", err.Error(), path)
		}
	}
	if s.canonical.fetches != 0 {
		t.Errorf("the marker gate must precede the fetch; fetches = %d", s.canonical.fetches)
	}
}

func TestRunScansEachTipOnce(t *testing.T) {
	s := newScenario(t)
	s.updates = append(s.updates, RefUpdate{
		LocalRef: "refs/heads/topic", LocalOID: appTip, RemoteRef: "refs/heads/topic",
	})

	if _, err := s.run(t); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(s.app.scanned) != 1 {
		t.Fatalf("scanned = %v, want the shared tip scanned once", s.app.scanned)
	}
}

func TestRunFailsClosedWhenTheMarkerScanFails(t *testing.T) {
	s := newScenario(t)
	s.app.markerErr = errors.New("blob unreadable")

	_, err := s.run(t)
	if err == nil || !strings.Contains(err.Error(), "blob unreadable") {
		t.Fatalf("error = %v, want the scan failure to fail the gate closed", err)
	}
	if s.canonical.fetches != 0 {
		t.Errorf("a failed gate still reached the network: fetches = %d", s.canonical.fetches)
	}
}

// --- Step 4: fetch ----------------------------------------------------

func TestRunFailsClosedWhenCanonicalIsUnreachable(t *testing.T) {
	s := newScenario(t)
	s.canonical.fetchErr = pubdom.ErrUnreachable

	_, err := s.run(t)
	if !errors.Is(err, pubdom.ErrUnreachable) {
		t.Fatalf("error = %v, want it to wrap ErrUnreachable", err)
	}
	if len(s.canonical.published) != 0 {
		t.Errorf("published %v despite an unreachable canonical", s.canonical.published)
	}
}

// --- Step 5: case analysis -------------------------------------------

func TestRunCaseUpToDateIsANoOp(t *testing.T) {
	s := newScenario(t)
	s.app.docsTrees[appTip] = canonTree

	outcome, err := s.run(t)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if outcome.Case != pubdom.CaseUpToDate {
		t.Errorf("case = %v, want up to date", outcome.Case)
	}
	if outcome.Published != "" {
		t.Errorf("published %s in case ①", outcome.Published)
	}
	if len(s.canonical.created) != 0 || s.canonical.pushes != 0 {
		t.Errorf("case ① created %d commits and pushed %d times", len(s.canonical.created), s.canonical.pushes)
	}
	if len(s.state.saved) != 0 {
		t.Errorf("case ① moved the base to %v", s.state.saved)
	}
}

// TestRunCaseUpToDateSurvivesAnOrphanedBase pins the precedence rule:
// nothing to publish means nothing can go wrong, whatever the base says.
func TestRunCaseUpToDateSurvivesAnOrphanedBase(t *testing.T) {
	s := newScenario(t)
	s.app.docsTrees[appTip] = canonTree
	s.state.base = provenance.Base{Commit: commitOID(999), Tree: treeOID(999)}

	outcome, err := s.run(t)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if outcome.Case != pubdom.CaseUpToDate {
		t.Fatalf("case = %v, want up to date", outcome.Case)
	}
}

func TestRunCaseFastForwardPublishes(t *testing.T) {
	s := newScenario(t)
	s.app.subjects[".."+appTip] = []string{"docs: add guide", "docs: fix typo"}

	outcome, err := s.run(t)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if outcome.Case != pubdom.CaseFastForward {
		t.Fatalf("case = %v, want fast forward", outcome.Case)
	}
	if outcome.Published == "" {
		t.Fatal("Run published nothing")
	}
	if got := s.canonical.head().commit; got != outcome.Published {
		t.Fatalf("canonical head = %s, want the published %s", got, outcome.Published)
	}
	if !reflect.DeepEqual(s.canonical.imported, []string{appTip}) {
		t.Errorf("imported = %v, want the tip imported once", s.canonical.imported)
	}
	if len(s.canonical.mergeCalls) != 0 {
		t.Errorf("a fast forward merged: %v", s.canonical.mergeCalls)
	}

	if len(s.canonical.created) != 1 {
		t.Fatalf("created %d commits, want 1", len(s.canonical.created))
	}
	created := s.canonical.created[0]
	if created.tree != tipTree {
		t.Errorf("published tree = %s, want the tip docs tree %s", created.tree, tipTree)
	}
	if created.parent != canonHead {
		t.Errorf("parent = %s, want the canonical head %s", created.parent, canonHead)
	}
	if created.authorName != "Publisher" || created.authorMail != "publisher@example.test" {
		t.Errorf("author = %s <%s>, want the actor", created.authorName, created.authorMail)
	}

	want := "docs: product/main (2 app commits)\n" +
		"\n" +
		"source: product:/home/u/product @ " + appTip + "\n" +
		"commits:\n" +
		"  - docs: add guide\n" +
		"  - docs: fix typo\n"
	if created.message != want {
		t.Fatalf("message =\n%q\nwant\n%q", created.message, want)
	}
}

// TestRunUsesThePushedBranchName asserts the canonical subject names the
// ref being pushed, not whatever branch HEAD happens to sit on.
func TestRunUsesThePushedBranchName(t *testing.T) {
	s := newScenario(t)
	s.updates[0].LocalRef = "refs/heads/feature/docs-rewrite"
	s.app.branch = "some-other-branch"

	if _, err := s.run(t); err != nil {
		t.Fatalf("Run: %v", err)
	}
	subject := strings.SplitN(s.canonical.created[0].message, "\n", 2)[0]
	if want := "docs: product/feature/docs-rewrite (1 app commits)"; subject != want {
		t.Fatalf("subject = %q, want %q", subject, want)
	}
}

// TestRunPassesThePreviousRemoteOIDAsTheSubjectRange asserts the commit
// body summarizes exactly the commits this push adds.
func TestRunPassesThePreviousRemoteOIDAsTheSubjectRange(t *testing.T) {
	s := newScenario(t)
	s.updates[0].RemoteOID = appTipAlt

	if _, err := s.run(t); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if want := []string{appTipAlt + ".." + appTip}; !reflect.DeepEqual(s.app.subjectCalls, want) {
		t.Fatalf("subject range = %v, want %v", s.app.subjectCalls, want)
	}
}

func TestRunCaseAutoMergePublishesTheMergedTree(t *testing.T) {
	s := newScenario(t)
	s.state.base = provenance.Base{Commit: canonRoot, Tree: rootTree}
	s.app.worktree = treeOID(99) // worktree differs from the merge result

	outcome, err := s.run(t)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if outcome.Case != pubdom.CaseAutoMerge {
		t.Fatalf("case = %v, want auto merge", outcome.Case)
	}
	if want := []string{canonRoot + "|" + tipTree + "|" + canonHead}; !reflect.DeepEqual(s.canonical.mergeCalls, want) {
		t.Fatalf("merge called with %v, want %v", s.canonical.mergeCalls, want)
	}
	if s.canonical.created[0].tree != mergedTree {
		t.Fatalf("published tree = %s, want the merged tree %s", s.canonical.created[0].tree, mergedTree)
	}
	if outcome.BaseAdvanced {
		t.Error("the base advanced although the worktree differs from the published tree")
	}
	if len(s.state.saved) != 0 {
		t.Errorf("base was saved as %v", s.state.saved)
	}
}

func TestRunCaseAutoMergeConflictedRequiresSync(t *testing.T) {
	s := newScenario(t)
	s.state.base = provenance.Base{Commit: canonRoot, Tree: rootTree}
	s.canonical.mergeConflicts = []string{"docs/api.md", "docs/schema.md"}

	outcome, err := s.run(t)
	if !errors.Is(err, ErrSyncRequired) {
		t.Fatalf("error = %v, want ErrSyncRequired", err)
	}

	var syncErr *SyncRequiredError
	if !errors.As(err, &syncErr) {
		t.Fatalf("error = %v, want a *SyncRequiredError", err)
	}
	want := []string{"docs/api.md", "docs/schema.md"}
	if !reflect.DeepEqual(syncErr.Conflicts, want) {
		t.Fatalf("conflicts = %v, want %v", syncErr.Conflicts, want)
	}
	if syncErr.Reason != ReasonConflicts {
		t.Errorf("reason = %q, want %q", syncErr.Reason, ReasonConflicts)
	}
	if syncErr.Base != canonRoot || syncErr.Head != canonHead {
		t.Errorf("error quotes base %s → head %s, want %s → %s", syncErr.Base, syncErr.Head, canonRoot, canonHead)
	}
	if !reflect.DeepEqual(outcome.Conflicts, want) {
		t.Errorf("outcome conflicts = %v, want %v", outcome.Conflicts, want)
	}

	// A rejected push changes nothing remote (§5.9).
	if s.canonical.pushes != 0 || len(s.canonical.created) != 0 {
		t.Errorf("a conflicted merge still created %d commits and pushed %d times",
			len(s.canonical.created), s.canonical.pushes)
	}
	if s.canonical.head().commit != canonHead {
		t.Errorf("canonical head moved to %s", s.canonical.head().commit)
	}
}

func TestRunCaseUnknownBaseReanchorsByDocsTree(t *testing.T) {
	s := newScenario(t)
	// The recorded base commit is gone, but its tree survives on the
	// branch root: exactly what a squash or rebase leaves behind.
	s.state.base = provenance.Base{Commit: commitOID(777), Tree: rootTree}

	outcome, err := s.run(t)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if outcome.Case != pubdom.CaseAutoMerge {
		t.Fatalf("case = %v, want auto merge after re-anchoring", outcome.Case)
	}
	if want := []string{canonRoot + "|" + tipTree + "|" + canonHead}; !reflect.DeepEqual(s.canonical.mergeCalls, want) {
		t.Fatalf("merge called with %v, want the re-anchored base %v", s.canonical.mergeCalls, want)
	}
	if outcome.Published == "" {
		t.Fatal("re-anchored publication produced nothing")
	}
}

// TestRunCaseUnknownBaseReanchorsToHeadAsAFastForward covers the anchor
// landing exactly on canonical head.
func TestRunCaseUnknownBaseReanchorsToHeadAsAFastForward(t *testing.T) {
	s := newScenario(t)
	s.state.base = provenance.Base{Commit: commitOID(777), Tree: canonTree}

	outcome, err := s.run(t)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if outcome.Case != pubdom.CaseFastForward {
		t.Fatalf("case = %v, want fast forward after re-anchoring to head", outcome.Case)
	}
}

func TestRunCaseUnknownBaseWithoutAnAnchorReportsRewrittenHistory(t *testing.T) {
	s := newScenario(t)
	s.state.base = provenance.Base{Commit: commitOID(777), Tree: treeOID(777)}

	_, err := s.run(t)
	if !errors.Is(err, ErrHistoryRewritten) {
		t.Fatalf("error = %v, want ErrHistoryRewritten", err)
	}
	if s.canonical.pushes != 0 {
		t.Errorf("a rewritten-history rejection still pushed %d times", s.canonical.pushes)
	}
}

// TestRunReanchorToADetachedCommitStillReportsRewrittenHistory: the tree
// exists, but on a line of history the publication branch no longer
// contains, which only `sanho sync --rebase-onto` can straighten out.
func TestRunReanchorToADetachedCommitStillReportsRewrittenHistory(t *testing.T) {
	s := newScenario(t)
	orphanTree := treeOID(555)
	s.canonical.detached = []commitPair{{commit: commitOID(555), tree: orphanTree}}
	s.state.base = provenance.Base{Commit: commitOID(777), Tree: orphanTree}

	_, err := s.run(t)
	if !errors.Is(err, ErrHistoryRewritten) {
		t.Fatalf("error = %v, want ErrHistoryRewritten", err)
	}
	if !strings.Contains(err.Error(), "re-anchored") {
		t.Errorf("message %q does not explain that re-anchoring landed off-branch", err.Error())
	}
}

func TestRunWithoutARecordedBaseAsksForSync(t *testing.T) {
	s := newScenario(t)
	s.state.hasBase = false
	s.state.base = provenance.Base{}

	_, err := s.run(t)
	if !errors.Is(err, ErrSyncRequired) {
		t.Fatalf("error = %v, want ErrSyncRequired", err)
	}
	var syncErr *SyncRequiredError
	if !errors.As(err, &syncErr) || syncErr.Reason != ReasonNoBase {
		t.Fatalf("error = %v, want reason %q", err, ReasonNoBase)
	}
	if s.canonical.pushes != 0 {
		t.Errorf("published without a base")
	}
}

// TestRunWithoutARecordedBaseStillAllowsUpToDatePushes: a workspace that
// never synced must still be able to push code-only commits.
func TestRunWithoutARecordedBaseStillAllowsUpToDatePushes(t *testing.T) {
	s := newScenario(t)
	s.state.hasBase = false
	s.state.base = provenance.Base{}
	s.app.docsTrees[appTip] = canonTree

	outcome, err := s.run(t)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if outcome.Case != pubdom.CaseUpToDate {
		t.Fatalf("case = %v, want up to date", outcome.Case)
	}
}

func TestRunPropagatesBaseLoadErrors(t *testing.T) {
	s := newScenario(t)
	s.state.loadErr = errors.New("base file is corrupt")

	if _, err := s.run(t); err == nil || !strings.Contains(err.Error(), "corrupt") {
		t.Fatalf("error = %v, want the base-file failure", err)
	}
}

// --- Deduplication ----------------------------------------------------

func TestRunDeduplicatesIdenticalDocsTrees(t *testing.T) {
	s := newScenario(t)
	s.app.docsTrees[appTipAlt] = tipTree // same docs content, different commit
	s.updates = append(s.updates, RefUpdate{
		LocalRef: "refs/heads/topic", LocalOID: appTipAlt, RemoteRef: "refs/heads/topic",
	})

	if _, err := s.run(t); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(s.canonical.created) != 1 {
		t.Fatalf("created %d commits, want 1 for two tips carrying the same docs tree", len(s.canonical.created))
	}
	// stdin order decides which ref describes the publication.
	if !strings.HasPrefix(s.canonical.created[0].message, "docs: product/main ") {
		t.Errorf("subject = %q, want the first ref in stdin order", s.canonical.created[0].message)
	}
}

func TestRunPublishesEachDistinctDocsTree(t *testing.T) {
	s := newScenario(t)
	s.updates = append(s.updates, RefUpdate{
		LocalRef: "refs/heads/topic", LocalOID: appTipAlt, RemoteRef: "refs/heads/topic",
	})
	// The second tip merges against the first tip's publication.
	s.canonical.mergeTree = treeOID(30)

	outcome, err := s.run(t)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(s.canonical.published) != 2 {
		t.Fatalf("published %v, want two canonical commits", s.canonical.published)
	}
	if outcome.Published != s.canonical.published[1] {
		t.Errorf("Outcome.Published = %s, want the last publication %s", outcome.Published, s.canonical.published[1])
	}
	// The second publication sits on top of the first.
	if parent := s.canonical.created[1].parent; parent != s.canonical.published[0] {
		t.Errorf("second parent = %s, want the first publication %s", parent, s.canonical.published[0])
	}
}

// --- CAS retry --------------------------------------------------------

func TestRunRetriesAfterALostRace(t *testing.T) {
	s := newScenario(t)
	s.state.base = provenance.Base{Commit: canonRoot, Tree: rootTree}
	s.app.worktree = treeOID(99)

	// A racing publisher lands just before our first push.
	s.canonical.onPush = func(c *fakeCanonical, attempt int) {
		if attempt == 1 {
			c.appendBranch(commitOID(500), treeOID(500))
		}
	}

	outcome, err := s.run(t)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if s.canonical.pushes != 2 {
		t.Fatalf("pushes = %d, want 2 (one lost, one won)", s.canonical.pushes)
	}
	if s.canonical.fetches != 2 {
		t.Fatalf("fetches = %d, want 2 (initial plus the retry refetch)", s.canonical.fetches)
	}
	// The retry must re-merge against the winner, never replay.
	if len(s.canonical.mergeCalls) != 2 {
		t.Fatalf("merge calls = %v, want the merge recomputed on retry", s.canonical.mergeCalls)
	}
	if !strings.HasSuffix(s.canonical.mergeCalls[1], "|"+commitOID(500)) {
		t.Errorf("retry merged against %q, want the racer's commit", s.canonical.mergeCalls[1])
	}
	if s.canonical.created[1].parent != commitOID(500) {
		t.Errorf("retry parent = %s, want the racer's commit", s.canonical.created[1].parent)
	}
	if outcome.Published != s.canonical.published[0] {
		t.Errorf("Outcome.Published = %s, want %s", outcome.Published, s.canonical.published[0])
	}
}

func TestRunGivesUpAfterMaxCASRetries(t *testing.T) {
	s := newScenario(t)
	s.state.base = provenance.Base{Commit: canonRoot, Tree: rootTree}

	// A publisher wins every single race.
	s.canonical.onPush = func(c *fakeCanonical, attempt int) {
		c.appendBranch(commitOID(600+attempt), treeOID(600+attempt))
	}

	_, err := s.run(t)
	if !errors.Is(err, ErrSyncRequired) {
		t.Fatalf("error = %v, want ErrSyncRequired after an exhausted retry budget", err)
	}
	var syncErr *SyncRequiredError
	if !errors.As(err, &syncErr) || syncErr.Reason != ReasonCASExhausted {
		t.Fatalf("error = %v, want reason %q", err, ReasonCASExhausted)
	}
	if s.canonical.pushes != MaxCASRetries {
		t.Fatalf("pushes = %d, want exactly MaxCASRetries (%d)", s.canonical.pushes, MaxCASRetries)
	}
	if len(s.canonical.published) != 0 {
		t.Errorf("published %v while losing every race", s.canonical.published)
	}
}

func TestRunSurfacesNonRacePushFailures(t *testing.T) {
	s := newScenario(t)
	s.canonical.onPush = func(c *fakeCanonical, attempt int) {
		// Make the CAS succeed but the branch reject the commit, so the
		// push fails for a reason that is not a lost race.
		c.created = nil
	}

	_, err := s.run(t)
	if err == nil {
		t.Fatal("Run ignored a push failure")
	}
	if errors.Is(err, ErrSyncRequired) {
		t.Fatalf("a hard push failure was reported as a lost race: %v", err)
	}
	if s.canonical.pushes != 1 {
		t.Errorf("pushes = %d, want a single attempt for a non-race failure", s.canonical.pushes)
	}
}

// --- Step 6: base advance --------------------------------------------

func TestRunAdvancesTheBaseWhenTheWorktreeMatches(t *testing.T) {
	s := newScenario(t)
	s.app.worktree = tipTree // identical to what will be published

	outcome, err := s.run(t)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !outcome.BaseAdvanced {
		t.Fatal("Outcome.BaseAdvanced = false, want true")
	}
	if len(s.state.saved) != 1 {
		t.Fatalf("saved %v, want exactly one base write", s.state.saved)
	}
	want := provenance.Base{Commit: outcome.Published, Tree: tipTree}
	if s.state.saved[0] != want {
		t.Fatalf("saved base = %+v, want %+v", s.state.saved[0], want)
	}
}

func TestRunLeavesTheBaseWhenTheWorktreeDiffers(t *testing.T) {
	s := newScenario(t)
	s.app.worktree = treeOID(98) // uncommitted docs edits

	outcome, err := s.run(t)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if outcome.BaseAdvanced {
		t.Error("Outcome.BaseAdvanced = true, want false")
	}
	if len(s.state.saved) != 0 {
		t.Fatalf("saved %v, want no base write", s.state.saved)
	}
}

func TestRunPropagatesWorktreeHashFailures(t *testing.T) {
	s := newScenario(t)
	s.app.worktreeErr = errors.New("docs unreadable")

	if _, err := s.run(t); err == nil || !strings.Contains(err.Error(), "docs unreadable") {
		t.Fatalf("error = %v, want the worktree hash failure", err)
	}
}

// TestRunAdvancedBaseIsUsedByLaterTips: once the base moves, a second
// tip in the same push is evaluated against the new base.
func TestRunAdvancedBaseIsUsedByLaterTips(t *testing.T) {
	s := newScenario(t)
	s.app.worktree = tipTree
	s.updates = append(s.updates, RefUpdate{
		LocalRef: "refs/heads/topic", LocalOID: appTipAlt, RemoteRef: "refs/heads/topic",
	})

	outcome, err := s.run(t)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// The first publication advanced the base to itself, so the second
	// tip is a fast-forward rather than a merge.
	if len(s.canonical.mergeCalls) != 0 {
		t.Fatalf("merge calls = %v, want the advanced base to make the second tip a fast forward", s.canonical.mergeCalls)
	}
	if outcome.Case != pubdom.CaseFastForward {
		t.Errorf("case = %v, want fast forward", outcome.Case)
	}
}

// --- Error vocabulary -------------------------------------------------

func TestErrorSentinelsAreDistinguishable(t *testing.T) {
	markersErr := &MarkersPresentError{Tip: appTip, Paths: []string{"docs/a.md"}}
	syncErr := &SyncRequiredError{Base: canonRoot, Head: canonHead, Reason: ReasonConflicts}

	tests := []struct {
		name    string
		err     error
		is      error
		isNot   []error
		message string
	}{
		{
			name:    "markers",
			err:     markersErr,
			is:      ErrMarkersPresent,
			isNot:   []error{ErrSyncRequired, ErrSyncInProgress, ErrHistoryRewritten},
			message: "docs/a.md",
		},
		{
			name:    "sync required",
			err:     syncErr,
			is:      ErrSyncRequired,
			isNot:   []error{ErrMarkersPresent, ErrSyncInProgress, ErrHistoryRewritten},
			message: ReasonConflicts,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !errors.Is(test.err, test.is) {
				t.Errorf("errors.Is(%v, %v) = false", test.err, test.is)
			}
			for _, other := range test.isNot {
				if errors.Is(test.err, other) {
					t.Errorf("errors.Is(%v, %v) = true, want false", test.err, other)
				}
			}
			if !strings.Contains(test.err.Error(), test.message) {
				t.Errorf("message %q does not contain %q", test.err.Error(), test.message)
			}
		})
	}

	// Wrapping must preserve recognition, which is what the hook layer
	// relies on to pick the §5.9 template.
	wrapped := errors.Join(errors.New("context"), syncErr)
	if !errors.Is(wrapped, ErrSyncRequired) {
		t.Error("a wrapped SyncRequiredError is no longer recognizable")
	}
}

func TestShortOID(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{canonHead, canonHead[:12]},
		{"abc", "abc"},
		{"", "(none)"},
		{"0123456789ab", "0123456789ab"},
	}
	for _, test := range tests {
		if got := shortOID(test.in); got != test.want {
			t.Errorf("shortOID(%q) = %q, want %q", test.in, got, test.want)
		}
	}
}

func TestIsZeroOID(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{strings.Repeat("0", 40), true},
		{strings.Repeat("0", 64), true},
		{"", true},
		{canonHead, false},
		{strings.Repeat("0", 39) + "1", false},
	}
	for _, test := range tests {
		if got := isZeroOID(test.in); got != test.want {
			t.Errorf("isZeroOID(%q) = %v, want %v", test.in, got, test.want)
		}
	}
}

// --- Bootstrap: publishing into an empty canonical repository ---------

// newBootstrapScenario is newScenario against a canonical repository
// with no commits at all — a docs repository that has just been created
// and never published into (sanho-v0.2.md §5.3).
func newBootstrapScenario(t *testing.T) *scenario {
	t.Helper()
	s := newScenario(t)
	s.canonical.branch = nil
	s.state.base, s.state.hasBase = provenance.Base{}, false
	return s
}

func TestRunBootstrapsAnEmptyCanonicalWithoutABase(t *testing.T) {
	s := newBootstrapScenario(t)

	outcome, err := s.run(t)
	if err != nil {
		t.Fatalf("Run() error = %v, want a bootstrap publication", err)
	}
	if outcome.Case != pubdom.CaseFastForward {
		t.Fatalf("Case = %v, want CaseFastForward", outcome.Case)
	}
	if outcome.Published == "" {
		t.Fatal("Published = \"\", want the new canonical root commit")
	}
	if len(s.canonical.created) != 1 {
		t.Fatalf("created %d canonical commits, want 1", len(s.canonical.created))
	}
	// A root commit: no parent, and the pushed tip's docs tree verbatim.
	created := s.canonical.created[0]
	if created.parent != "" {
		t.Fatalf("parent = %q, want a root commit with no parent", created.parent)
	}
	if created.tree != tipTree {
		t.Fatalf("tree = %s, want the tip's docs tree %s", created.tree, tipTree)
	}
	// The lease is empty: there is no remote ref to compare against yet.
	if got := s.canonical.pushLeases[0]; got != "" {
		t.Fatalf("push lease = %q, want empty for a branch-creating push", got)
	}
	if !outcome.BaseAdvanced {
		t.Fatal("BaseAdvanced = false, want the base recorded after the first publish")
	}
}

func TestRunBootstrapsAnEmptyCanonicalEvenWithARecordedBase(t *testing.T) {
	s := newBootstrapScenario(t)
	// A base recorded against history that no longer exists. Feeding it
	// into the ordinary analysis would produce a "history was rewritten"
	// rejection, which is a false diagnosis: canonical was never written.
	s.state.base = provenance.Base{Commit: canonHead, Tree: canonTree}
	s.state.hasBase = true

	outcome, err := s.run(t)
	if err != nil {
		t.Fatalf("Run() error = %v, want a bootstrap publication", err)
	}
	if outcome.Case != pubdom.CaseFastForward {
		t.Fatalf("Case = %v, want CaseFastForward", outcome.Case)
	}
	if outcome.Published == "" {
		t.Fatal("Published = \"\", want the new canonical root commit")
	}
}

func TestRunPublishesNothingForADocsFreeTipOnAnEmptyCanonical(t *testing.T) {
	s := newBootstrapScenario(t)
	// The tip carries no docs at all, so its docs tree *is* the empty
	// tree — which is exactly what an empty canonical holds.
	s.app.emptyTree = tipTree

	outcome, err := s.run(t)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if outcome.Case != pubdom.CaseUpToDate {
		t.Fatalf("Case = %v, want CaseUpToDate", outcome.Case)
	}
	if outcome.Published != "" {
		t.Fatalf("Published = %q, want nothing published", outcome.Published)
	}
	if len(s.canonical.created) != 0 {
		t.Fatalf("created %d canonical commits, want none", len(s.canonical.created))
	}
}
