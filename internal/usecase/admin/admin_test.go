package admin

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/irootkernel/sanho/internal/domain/provenance"
	pubdom "github.com/irootkernel/sanho/internal/domain/publish"
)

// StatusQuery's subject is the *unknown* answer: which facts it refuses
// to invent when the clone cannot establish them. Real git is covered by
// the black-box CLI suite; what is driven here are the states a real
// repository reaches rarely — an empty canonical, an orphaned base, a
// registry naming commits this clone has never seen.

var (
	baseCommit    = oid(1)
	baseTree      = oid(2)
	headCommit    = oid(3)
	headTree      = oid(4)
	siblingCommit = oid(5)
	strangeCommit = oid(9)
)

func oid(n int) string { return fmt.Sprintf("%040x", 0x1000+n) }

// fakeCanonical models the clone: it knows a set of commits and a
// distance table, and reports ErrEmptyBranch exactly as infra does.
type fakeCanonical struct {
	head, headTree string
	empty          bool
	known          map[string]bool
	// distances maps "a...b" to (behind, ahead).
	distances map[string][2]int
	age       time.Duration
	fetched   bool
	fetchErr  error
	fetches   int
}

func (c *fakeCanonical) Fetch(ctx context.Context) error {
	c.fetches++
	return c.fetchErr
}

func (c *fakeCanonical) Head(ctx context.Context) (string, string, error) {
	if c.empty {
		return "", "", fmt.Errorf("%w: main", pubdom.ErrEmptyBranch)
	}
	return c.head, c.headTree, nil
}

func (c *fakeCanonical) Age() (time.Duration, bool) { return c.age, c.fetched }

func (c *fakeCanonical) ResolveCommit(ctx context.Context, id string) (bool, error) {
	return c.known[id], nil
}

func (c *fakeCanonical) Distance(ctx context.Context, a, b string) (int, int, error) {
	pair, ok := c.distances[a+"..."+b]
	if !ok {
		return 0, 0, errors.New("no distance recorded")
	}
	return pair[0], pair[1], nil
}

type fakeState struct {
	base       provenance.Base
	hasBase    bool
	loadErr    error
	inProgress bool
}

func (s fakeState) LoadBase() (provenance.Base, bool, error) {
	return s.base, s.hasBase, s.loadErr
}

func (s fakeState) SyncInProgress() (bool, error) { return s.inProgress, nil }

type fakeRegistry struct {
	entries []SiblingEntry
	err     error
}

func (r fakeRegistry) Siblings(ctx context.Context) ([]SiblingEntry, error) {
	return r.entries, r.err
}

type fakePreview struct {
	known, clean bool
	conflicts    []string
	calls        int
}

type fakeLocal struct {
	tree        string
	worktree    string
	err         error
	cleanErr    error
	worktreeErr error
	dirty       bool
}

func (l fakeLocal) HeadDocsTree(ctx context.Context) (string, error) { return l.tree, l.err }
func (l fakeLocal) DocsClean(ctx context.Context) (bool, error)      { return !l.dirty, l.cleanErr }
func (l fakeLocal) WorktreeDocsTree(ctx context.Context) (string, error) {
	if l.worktree == "" {
		return l.tree, l.worktreeErr
	}
	return l.worktree, l.worktreeErr
}

func (p *fakePreview) Preview(ctx context.Context, base provenance.Base, head, headTree string) (bool, bool, []string) {
	p.calls++
	return p.known, p.clean, p.conflicts
}

// newQuery builds the ordinary case: a base two commits behind head,
// with a clean sync predicted.
func newQuery() (*StatusQuery, *fakeCanonical, *fakePreview) {
	canonical := &fakeCanonical{
		head:     headCommit,
		headTree: headTree,
		known:    map[string]bool{baseCommit: true, headCommit: true, siblingCommit: true},
		distances: map[string][2]int{
			baseCommit + "..." + headCommit:    {2, 0},
			siblingCommit + "..." + baseCommit: {0, 1},
			siblingCommit + "..." + headCommit: {3, 0},
		},
		age:     time.Hour,
		fetched: true,
	}
	preview := &fakePreview{known: true, clean: true}
	return &StatusQuery{
		Canonical:   canonical,
		State:       fakeState{base: provenance.Base{Commit: baseCommit, Tree: baseTree}, hasBase: true},
		Registry:    fakeRegistry{},
		Preview:     preview,
		Local:       fakeLocal{tree: baseTree},
		Project:     "product",
		WorkspaceID: "product:/home/u/product",
	}, canonical, preview
}

func TestRunReportsPendingLocalPublication(t *testing.T) {
	query, _, _ := newQuery()
	query.Local = fakeLocal{tree: oid(20)}

	report, err := query.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !report.PublicationKnown || !report.PublicationPending {
		t.Fatalf("publication = (known=%t, pending=%t), want both true",
			report.PublicationKnown, report.PublicationPending)
	}
}

func TestRunReportsLocalReadiness(t *testing.T) {
	tests := []struct {
		name      string
		state     fakeState
		local     fakeLocal
		wantClean bool
		wantSync  Readiness
		wantPull  Readiness
	}{
		{
			name:      "ready",
			state:     fakeState{base: provenance.Base{Commit: baseCommit, Tree: baseTree}, hasBase: true},
			local:     fakeLocal{tree: baseTree},
			wantClean: true,
			wantSync:  Readiness{Ready: true},
			wantPull:  Readiness{Ready: true},
		},
		{
			name:     "dirty docs",
			state:    fakeState{base: provenance.Base{Commit: baseCommit, Tree: baseTree}, hasBase: true},
			local:    fakeLocal{tree: baseTree, dirty: true},
			wantSync: blocked(ReadinessDocsDirty),
			wantPull: blocked(ReadinessDocsDirty),
		},
		{
			name:     "active sync outranks dirty docs",
			state:    fakeState{base: provenance.Base{Commit: baseCommit, Tree: baseTree}, hasBase: true, inProgress: true},
			local:    fakeLocal{tree: baseTree, dirty: true},
			wantSync: blocked(ReadinessSyncInProgress),
			wantPull: blocked(ReadinessSyncInProgress),
		},
		{
			name:      "no base blocks pull only",
			state:     fakeState{},
			local:     fakeLocal{tree: baseTree},
			wantClean: true,
			wantSync:  Readiness{Ready: true},
			wantPull:  blocked(ReadinessNoBase),
		},
		{
			name:      "committed local docs block pull only",
			state:     fakeState{base: provenance.Base{Commit: baseCommit, Tree: baseTree}, hasBase: true},
			local:     fakeLocal{tree: oid(20)},
			wantClean: true,
			wantSync:  Readiness{Ready: true},
			wantPull:  blocked(ReadinessLocalDocsChanged),
		},
		{
			name:     "unreadable working copy",
			state:    fakeState{base: provenance.Base{Commit: baseCommit, Tree: baseTree}, hasBase: true},
			local:    fakeLocal{tree: baseTree, cleanErr: errors.New("status failed")},
			wantSync: blocked(ReadinessWorkingCopyUnknown),
			wantPull: blocked(ReadinessWorkingCopyUnknown),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query, _, _ := newQuery()
			query.State, query.Local = tt.state, tt.local
			report, err := query.Run(context.Background())
			if err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			if report.DocsClean != tt.wantClean {
				t.Errorf("DocsClean = %t, want %t", report.DocsClean, tt.wantClean)
			}
			if !reflect.DeepEqual(report.SyncReadiness, tt.wantSync) {
				t.Errorf("sync readiness = %+v, want %+v", report.SyncReadiness, tt.wantSync)
			}
			if !reflect.DeepEqual(report.PullReadiness, tt.wantPull) {
				t.Errorf("pull readiness = %+v, want %+v", report.PullReadiness, tt.wantPull)
			}
		})
	}
}

func TestRunLeavesPublicationUnknownWithoutAStableBaseComparison(t *testing.T) {
	tests := []struct {
		name    string
		state   fakeState
		local   fakeLocal
		known   bool
		pending bool
	}{
		{name: "no base", state: fakeState{}, local: fakeLocal{tree: oid(20)}},
		{name: "sync in progress", state: fakeState{base: provenance.Base{Commit: baseCommit, Tree: baseTree}, hasBase: true, inProgress: true}, local: fakeLocal{tree: oid(20)}},
		{name: "head unreadable", state: fakeState{base: provenance.Base{Commit: baseCommit, Tree: baseTree}, hasBase: true}, local: fakeLocal{err: errors.New("head unreadable")}},
		{name: "equal to base", state: fakeState{base: provenance.Base{Commit: baseCommit, Tree: baseTree}, hasBase: true}, local: fakeLocal{tree: baseTree}, known: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query, _, _ := newQuery()
			query.State = tt.state
			query.Local = tt.local
			report, err := query.Run(context.Background())
			if err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			if report.PublicationKnown != tt.known || report.PublicationPending != tt.pending {
				t.Fatalf("publication = (known=%t, pending=%t), want (%t, %t)",
					report.PublicationKnown, report.PublicationPending, tt.known, tt.pending)
			}
		})
	}
}

func TestRunReportsTheOrdinaryCase(t *testing.T) {
	query, canonical, preview := newQuery()

	report, err := query.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !report.RelationKnown || report.Behind != 2 || report.Ahead != 0 {
		t.Fatalf("relation = (known=%v, behind=%d, ahead=%d), want (true, 2, 0)",
			report.RelationKnown, report.Behind, report.Ahead)
	}
	if !report.SyncPreviewKnown || !report.SyncClean {
		t.Fatalf("preview = (known=%v, clean=%v), want both true", report.SyncPreviewKnown, report.SyncClean)
	}
	if report.Head != headCommit || report.HeadTree != headTree {
		t.Fatalf("head = (%s, %s), want (%s, %s)", report.Head, report.HeadTree, headCommit, headTree)
	}
	if !report.FetchedEver || report.DataAge != time.Hour {
		t.Fatalf("data age = (%v, fetched=%v), want (1h, true)", report.DataAge, report.FetchedEver)
	}
	// Without --refresh nothing goes to the network (the private-clone contract read paths).
	if canonical.fetches != 0 {
		t.Fatalf("Fetch called %d times without --refresh, want 0", canonical.fetches)
	}
	if preview.calls != 1 {
		t.Fatalf("Preview called %d times, want 1", preview.calls)
	}
}

func TestRunRefreshFetchesFirstAndPropagatesFailure(t *testing.T) {
	query, canonical, _ := newQuery()
	query.Refresh = true

	if _, err := query.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if canonical.fetches != 1 {
		t.Fatalf("Fetch called %d times with --refresh, want 1", canonical.fetches)
	}

	// A fetch the user explicitly asked for is the one network failure
	// status does not paper over.
	canonical.fetchErr = pubdom.ErrUnreachable
	if _, err := query.Run(context.Background()); !errors.Is(err, pubdom.ErrUnreachable) {
		t.Fatalf("Run() error = %v, want it to wrap ErrUnreachable", err)
	}
}

func TestRunReportsAnEmptyCanonicalWithoutARelation(t *testing.T) {
	query, canonical, preview := newQuery()
	canonical.empty = true

	report, err := query.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !report.CanonicalEmpty {
		t.Fatal("CanonicalEmpty = false, want true")
	}
	if report.Head != "" {
		t.Fatalf("Head = %q, want empty", report.Head)
	}
	if report.RelationKnown {
		t.Fatal("RelationKnown = true against an empty canonical, want false")
	}
	if preview.calls != 0 {
		t.Fatalf("Preview called %d times against an empty canonical, want 0", preview.calls)
	}
}

// A base the clone cannot resolve — history rewritten, or a fetch that
// never happened — is reported as unknown rather than as behind 0.
func TestRunReportsAnUnresolvableBaseAsUnknown(t *testing.T) {
	query, canonical, preview := newQuery()
	canonical.known = map[string]bool{headCommit: true}

	report, err := query.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if report.RelationKnown {
		t.Fatal("RelationKnown = true for an unresolvable base, want false")
	}
	if report.SyncPreviewKnown {
		t.Fatal("SyncPreviewKnown = true for an unresolvable base, want false")
	}
	if preview.calls != 0 {
		t.Fatalf("Preview called %d times for an unresolvable base, want 0", preview.calls)
	}
}

func TestRunFailsClosedOnAnUnreadableBaseFile(t *testing.T) {
	query, _, _ := newQuery()
	query.State = fakeState{loadErr: errors.New("base file is corrupt")}

	// Never guess a base (the state contract): an unreadable one stops the report.
	if _, err := query.Run(context.Background()); err == nil {
		t.Fatal("Run() error = nil for an unreadable base file, want a failure")
	}
}

func TestRunComputesSiblingRelations(t *testing.T) {
	query, _, _ := newQuery()
	updated := time.Date(2026, 8, 7, 9, 14, 0, 0, time.UTC)
	query.Registry = fakeRegistry{entries: []SiblingEntry{
		{
			WorkspaceID:   "product:/home/u/other",
			Base:          provenance.Base{Commit: siblingCommit},
			ActorEmail:    "other@example.test",
			LastUpdatedAt: updated,
		},
		{
			// A commit this clone has never seen: unknown, not "same".
			WorkspaceID: "product:/home/u/remote-machine",
			Base:        provenance.Base{Commit: strangeCommit},
		},
	}}

	report, err := query.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(report.Siblings) != 2 {
		t.Fatalf("got %d siblings, want 2", len(report.Siblings))
	}

	first := report.Siblings[0]
	if first.VsMine != "ahead 1" || first.VsHead != "behind 3" {
		t.Fatalf("first sibling = (vs mine %q, vs head %q), want (ahead 1, behind 3)", first.VsMine, first.VsHead)
	}
	if first.LastUpdatedAt != updated {
		t.Fatalf("LastUpdatedAt = %v, want %v", first.LastUpdatedAt, updated)
	}

	second := report.Siblings[1]
	if second.VsMine != RelationUnknown || second.VsHead != RelationUnknown {
		t.Fatalf("unresolvable sibling = (%q, %q), want both %q", second.VsMine, second.VsHead, RelationUnknown)
	}
}

// A sibling reporting exactly our base is "same" without consulting the
// clone at all — equal OIDs are equal wherever you stand.
func TestRunReportsAnIdenticalSiblingAsSameWithoutTheClone(t *testing.T) {
	query, canonical, _ := newQuery()
	canonical.known = map[string]bool{baseCommit: true, headCommit: true}
	query.Registry = fakeRegistry{entries: []SiblingEntry{
		{WorkspaceID: "product:/home/u/twin", Base: provenance.Base{Commit: baseCommit}},
	}}

	report, err := query.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := report.Siblings[0].VsMine; got != RelationSame {
		t.Fatalf("VsMine = %q, want %q", got, RelationSame)
	}
}

// A registry that cannot be read costs the sibling table, not the
// report: the registry is observational (D4).
func TestRunSurvivesAnUnreadableRegistry(t *testing.T) {
	query, _, _ := newQuery()
	query.Registry = fakeRegistry{err: errors.New("registry state is unreadable")}

	report, err := query.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v, want the report to survive", err)
	}
	if len(report.Siblings) != 0 {
		t.Fatalf("got %d siblings, want none", len(report.Siblings))
	}
	if !report.RelationKnown {
		t.Fatal("the canonical relation was lost with the registry")
	}
}

func TestDescribeDistance(t *testing.T) {
	tests := []struct {
		behind, ahead int
		want          string
	}{
		{0, 0, RelationSame},
		{3, 0, "behind 3"},
		{0, 2, "ahead 2"},
		{1, 1, RelationDiverged},
	}
	for _, test := range tests {
		if got := describeDistance(test.behind, test.ahead); got != test.want {
			t.Errorf("describeDistance(%d, %d) = %q, want %q", test.behind, test.ahead, got, test.want)
		}
	}
}

func TestRunReportsSyncInProgress(t *testing.T) {
	query, _, _ := newQuery()
	query.State = fakeState{
		base:       provenance.Base{Commit: baseCommit, Tree: baseTree},
		hasBase:    true,
		inProgress: true,
	}

	report, err := query.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !report.SyncInProgress {
		t.Fatal("SyncInProgress = false, want true")
	}
}

func TestRunReportsConflictPreview(t *testing.T) {
	query, _, preview := newQuery()
	preview.clean = false
	preview.conflicts = []string{"docs/api.md", "docs/schema.md"}

	report, err := query.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if report.SyncClean {
		t.Fatal("SyncClean = true, want false")
	}
	if !reflect.DeepEqual(report.SyncConflicts, []string{"docs/api.md", "docs/schema.md"}) {
		t.Fatalf("SyncConflicts = %v, want the two predicted paths", report.SyncConflicts)
	}
}
