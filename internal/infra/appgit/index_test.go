package appgit_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/irootkernel/sanho/internal/domain/markers"
	"github.com/irootkernel/sanho/internal/infra/appgit"
	"github.com/irootkernel/sanho/internal/infra/gitx"
)

// The index-side pair (IndexDocsTree / ScanStagedDocsForMarkers) is what
// the pre-commit and commit-msg hooks read (sanho-v0.2.md §5.1, §5.6).
// Everything here runs against a real git index; nothing is faked.

func TestIndexDocsTreeMatchesHeadWhenNothingIsStaged(t *testing.T) {
	dir, head := newRepoWithDocs(t)
	repo := newRepoHandle(t, dir)
	ctx := context.Background()

	got, err := repo.IndexDocsTree(ctx)
	if err != nil {
		t.Fatalf("IndexDocsTree() error = %v", err)
	}
	want, err := repo.DocsTreeOf(ctx, head)
	if err != nil {
		t.Fatalf("DocsTreeOf(HEAD) error = %v", err)
	}
	if got != want {
		t.Fatalf("IndexDocsTree() = %s, want HEAD's docs tree %s", got, want)
	}
}

func TestIndexDocsTreeSeesStagedChangesAndLeavesTheIndexAlone(t *testing.T) {
	dir, head := newRepoWithDocs(t)
	repo := newRepoHandle(t, dir)
	ctx := context.Background()

	writeFile(t, dir, "docs/a.md", []byte("alpha changed\n"))
	gitRun(t, dir, "add", "--", "docs/a.md")

	before := gitRun(t, dir, "ls-files", "--stage")

	got, err := repo.IndexDocsTree(ctx)
	if err != nil {
		t.Fatalf("IndexDocsTree() error = %v", err)
	}
	headTree, err := repo.DocsTreeOf(ctx, head)
	if err != nil {
		t.Fatalf("DocsTreeOf(HEAD) error = %v", err)
	}
	if got == headTree {
		t.Fatalf("IndexDocsTree() = %s, want it to differ from HEAD's docs tree", got)
	}

	// `git write-tree` writes objects and at most refreshes the index's
	// cache-tree extension; no staged entry moves. That is what makes the
	// call safe from inside commit-msg, mid-commit.
	if after := gitRun(t, dir, "ls-files", "--stage"); after != before {
		t.Fatalf("IndexDocsTree() changed the staged entries:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestIndexDocsTreeWithoutDocsDirIsTheEmptyTree(t *testing.T) {
	dir := newRepo(t)
	repo := newRepoHandle(t, dir)
	ctx := context.Background()

	writeFile(t, dir, "README.md", []byte("readme\n"))
	commitAll(t, dir, "chore: no docs")

	got, err := repo.IndexDocsTree(ctx)
	if err != nil {
		t.Fatalf("IndexDocsTree() error = %v", err)
	}
	empty, err := repo.EmptyTree(ctx)
	if err != nil {
		t.Fatalf("EmptyTree() error = %v", err)
	}
	if got != empty {
		t.Fatalf("IndexDocsTree() = %s, want the empty tree %s", got, empty)
	}
}

func TestIndexDocsTreeReportsAnUnmergedIndexDistinguishably(t *testing.T) {
	dir, _ := newRepoWithDocs(t)
	repo := newRepoHandle(t, dir)
	ctx := context.Background()

	// A real conflicted merge, so the index really carries stage 1/2/3
	// entries rather than a hand-built approximation of them.
	makeConflictedIndex(t, dir)

	_, err := repo.IndexDocsTree(ctx)
	if !errors.Is(err, appgit.ErrUnmergedIndex) {
		t.Fatalf("IndexDocsTree() error = %v, want ErrUnmergedIndex", err)
	}
}

func TestScanStagedDocsForMarkersFindsStagedConflictMarkers(t *testing.T) {
	dir, _ := newRepoWithDocs(t)
	repo := newRepoHandle(t, dir)
	ctx := context.Background()

	writeFile(t, dir, "docs/a.md", []byte("<<<<<<< sanho-ours\nmine\n=======\ntheirs\n>>>>>>> sanho-upstream\n"))
	writeFile(t, dir, "docs/clean.md", []byte("no markers here\n"))
	gitRun(t, dir, "add", "-A", "--", "docs")

	got, err := repo.ScanStagedDocsForMarkers(ctx)
	if err != nil {
		t.Fatalf("ScanStagedDocsForMarkers() error = %v", err)
	}
	if !reflect.DeepEqual(got, []string{"docs/a.md"}) {
		t.Fatalf("ScanStagedDocsForMarkers() = %v, want [docs/a.md]", got)
	}
}

func TestScanStagedDocsForMarkersIgnoresUnstagedWorktreeMarkers(t *testing.T) {
	dir, _ := newRepoWithDocs(t)
	repo := newRepoHandle(t, dir)
	ctx := context.Background()

	// Markers in the worktree that were never staged are not part of the
	// commit being prepared, so the pre-commit gate must not block on them.
	writeFile(t, dir, "docs/a.md", []byte("<<<<<<< sanho-ours\nmine\n=======\ntheirs\n>>>>>>> sanho-upstream\n"))

	got, err := repo.ScanStagedDocsForMarkers(ctx)
	if err != nil {
		t.Fatalf("ScanStagedDocsForMarkers() error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("ScanStagedDocsForMarkers() = %v, want none", got)
	}
}

func TestScanStagedDocsForMarkersSkipsBinaryAndOversizedRules(t *testing.T) {
	dir, _ := newRepoWithDocs(t)
	repo := newRepoHandle(t, dir)
	ctx := context.Background()

	// A NUL byte in the first 8 KiB classifies as binary; markers inside
	// binary content are never reported (audit H2 false-positive class).
	writeFile(t, dir, "docs/blob.bin", append([]byte{0x00, 0x01},
		[]byte("<<<<<<< a\nx\n=======\ny\n>>>>>>> b\n")...))
	gitRun(t, dir, "add", "-A", "--", "docs")

	got, err := repo.ScanStagedDocsForMarkers(ctx)
	if err != nil {
		t.Fatalf("ScanStagedDocsForMarkers() error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("ScanStagedDocsForMarkers() = %v, want none for binary content", got)
	}
}

func TestScanStagedDocsForMarkersErrorsOnOversizedBlob(t *testing.T) {
	dir, _ := newRepoWithDocs(t)
	repo := newRepoHandle(t, dir)
	ctx := context.Background()

	// Over the cap must be an ERROR naming the file, never a silent pass.
	oversized := strings.Repeat("a", int(markers.MaxScanSize)+1)
	writeFile(t, dir, "docs/big.md", []byte(oversized))
	gitRun(t, dir, "add", "-A", "--", "docs")

	_, err := repo.ScanStagedDocsForMarkers(ctx)
	if !errors.Is(err, markers.ErrTooLarge) {
		t.Fatalf("ScanStagedDocsForMarkers() error = %v, want markers.ErrTooLarge", err)
	}
	if !strings.Contains(err.Error(), "docs/big.md") {
		t.Fatalf("ScanStagedDocsForMarkers() error = %v, want it to name docs/big.md", err)
	}
}

func TestScanStagedDocsForMarkersSkipsUnmergedStages(t *testing.T) {
	dir, _ := newRepoWithDocs(t)
	repo := newRepoHandle(t, dir)
	ctx := context.Background()

	makeConflictedIndex(t, dir)

	// Stage 1/2/3 entries are merge inputs, not committable content, and
	// git refuses to commit from this index anyway.
	got, err := repo.ScanStagedDocsForMarkers(ctx)
	if err != nil {
		t.Fatalf("ScanStagedDocsForMarkers() error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("ScanStagedDocsForMarkers() = %v, want none for unmerged stages", got)
	}
}

// makeConflictedIndex leaves the repository mid-merge with docs/a.md
// unmerged in the index.
func makeConflictedIndex(t *testing.T, dir string) {
	t.Helper()

	base := gitLine(t, dir, "rev-parse", "HEAD")
	gitRun(t, dir, "checkout", "--quiet", "-b", "side", base)
	writeFile(t, dir, "docs/a.md", []byte("side\n"))
	commitAll(t, dir, "docs: side")

	gitRun(t, dir, "checkout", "--quiet", "main")
	writeFile(t, dir, "docs/a.md", []byte("main\n"))
	commitAll(t, dir, "docs: main")

	res, err := gitx.New(dir).RunExit(context.Background(), "merge", "--no-commit", "side")
	if err != nil {
		t.Fatalf("merge side: %v", err)
	}
	if res.ExitCode == 0 {
		t.Fatal("merge succeeded, want a conflict")
	}
}
