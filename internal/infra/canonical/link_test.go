package canonical

import (
	"context"
	"path/filepath"
	"testing"
)

// publishCanonicalPort mirrors usecase/publish.CanonicalPort, and
// docsyncCanonicalPort the canonical half of usecase/docsync.CanonicalPort.
//
// They are declared here rather than imported because the architecture
// gate forbids infra from importing usecase. Duplicating the two small
// interfaces buys the property that matters — a signature drift between
// Link and either port fails *this* package's build, instead of
// surfacing only when interface/cli wires them together.
type publishCanonicalPort interface {
	Fetch(ctx context.Context) error
	Head(ctx context.Context) (commit, tree string, err error)
	ResolveCommit(ctx context.Context, oid string) (bool, error)
	IsAncestor(ctx context.Context, a, b string) (bool, error)
	FindCommitByDocsTree(ctx context.Context, tree string) (string, bool, error)
	AbsorbedByTip(ctx context.Context, tipTree, head string) (bool, error)
	GcAuto(ctx context.Context) error
	FetchFromApp(ctx context.Context, tipOID string) error
	MergeDocs(ctx context.Context, baseCommit, oursTree, theirsCommit string) (tree string, conflicts []string, clean bool, err error)
	CommitDocsTree(ctx context.Context, tree, parent, authorName, authorEmail, message string) (string, error)
	PushHead(ctx context.Context, newHead, expectedOld string) error
}

func TestLinkAbsorbedByTipUsesContentRatherThanHistory(t *testing.T) {
	f := newFlow(t,
		map[string]entry{"a.md": text("alpha\n"), "b.md": text("beta\n")},
		map[string]entry{"a.md": text("alpha\n"), "b.md": text("beta\n")},
	)
	ctx := context.Background()
	head, _, err := f.link.Head(ctx)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}

	tests := []struct {
		name  string
		files map[string]entry
		want  bool
	}{
		{
			name: "canonical content plus a local file",
			files: map[string]entry{
				"a.md": text("alpha\n"), "b.md": text("beta\n"), "local.md": text("local\n"),
			},
			want: true,
		},
		{
			name:  "canonical file omitted",
			files: map[string]entry{"a.md": text("alpha\n")},
		},
		{
			name: "canonical file replaced",
			files: map[string]entry{
				"a.md": text("local alpha\n"), "b.md": text("beta\n"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tip := f.commitDocs(t, "docs: "+tt.name, tt.files)
			tipTree, err := f.app.DocsTreeOf(ctx, tip)
			if err != nil {
				t.Fatalf("DocsTreeOf: %v", err)
			}
			if err := f.link.FetchFromApp(ctx, tip); err != nil {
				t.Fatalf("FetchFromApp: %v", err)
			}
			got, err := f.link.AbsorbedByTip(ctx, tipTree, head)
			if err != nil {
				t.Fatalf("AbsorbedByTip: %v", err)
			}
			if got != tt.want {
				t.Fatalf("AbsorbedByTip = %t, want %t", got, tt.want)
			}
		})
	}
}

type docsyncCanonicalPort interface {
	Fetch(ctx context.Context) error
	Head(ctx context.Context) (commit, tree string, err error)
	FetchIntoApp(ctx context.Context) (headCommit string, err error)
	ResolveCommit(ctx context.Context, oid string) (bool, error)
	FindCommitByDocsTree(ctx context.Context, tree string) (commit string, found bool, err error)
}

var (
	_ publishCanonicalPort = (*Link)(nil)
	_ docsyncCanonicalPort = (*Link)(nil)
)

func TestLinkExposesItsStore(t *testing.T) {
	origin := newOrigin(t, "main", map[string]entry{"a.md": text("a\n")})
	store := ensureStore(t, origin)

	link := NewLink(store, filepath.Join(t.TempDir(), ".git"))
	if link.Store() != store {
		t.Fatal("Store() did not return the bound store")
	}
}

// TestLinkMergeDocsResolvesCommitsToDocsTrees asserts the adapter turns
// the commit-shaped port arguments into the tree-shaped MergeTree inputs.
func TestLinkMergeDocsResolvesCommitsToDocsTrees(t *testing.T) {
	f := newFlow(t,
		map[string]entry{"a.md": text("alpha\n"), "b.md": text("beta\n")},
		map[string]entry{"a.md": text("alpha\n"), "b.md": text("beta\n")},
	)
	ctx := context.Background()

	base, _, err := f.link.Head(ctx)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	commitToOrigin(t, f.origin, "main", map[string]entry{
		"a.md": text("alpha\n"),
		"b.md": text("beta upstream\n"),
	})
	if err := f.link.Fetch(ctx); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	head, _, err := f.link.Head(ctx)
	if err != nil {
		t.Fatalf("Head after fetch: %v", err)
	}

	tip := f.commitDocs(t, "docs: local", map[string]entry{
		"a.md": text("alpha local\n"),
		"b.md": text("beta\n"),
	})
	tipTree, err := f.app.DocsTreeOf(ctx, tip)
	if err != nil {
		t.Fatalf("DocsTreeOf: %v", err)
	}
	if err := f.link.FetchFromApp(ctx, tip); err != nil {
		t.Fatalf("FetchFromApp: %v", err)
	}

	tree, conflicts, clean, err := f.link.MergeDocs(ctx, base, tipTree, head)
	if err != nil {
		t.Fatalf("MergeDocs: %v", err)
	}
	if !clean {
		t.Fatalf("merge was not clean: %v", conflicts)
	}
	if got := gitRun(t, f.store.dir, "cat-file", "blob", tree+":a.md"); got != "alpha local\n" {
		t.Errorf("merged a.md = %q, want the local edit", got)
	}
	if got := gitRun(t, f.store.dir, "cat-file", "blob", tree+":b.md"); got != "beta upstream\n" {
		t.Errorf("merged b.md = %q, want the upstream edit", got)
	}
}

func TestLinkFetchIntoApp(t *testing.T) {
	f := newFlow(t,
		map[string]entry{"a.md": text("alpha\n")},
		map[string]entry{"a.md": text("alpha\n")},
	)
	ctx := context.Background()

	head, _, err := f.link.Head(ctx)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	imported, err := f.link.FetchIntoApp(ctx)
	if err != nil {
		t.Fatalf("FetchIntoApp: %v", err)
	}
	if imported != head {
		t.Fatalf("FetchIntoApp returned %s, want the canonical head %s", imported, head)
	}
	if kind := gitLine(t, f.appDir, "cat-file", "-t", head); kind != "commit" {
		t.Fatalf("canonical head is not addressable app-side: %q", kind)
	}
}
