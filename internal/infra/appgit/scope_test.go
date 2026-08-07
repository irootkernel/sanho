package appgit_test

// The F-H4 gate-scoping and F-M8 scanner-ordering regressions, against
// real git.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/irootkernel/sanho/internal/domain/markers"
	"github.com/irootkernel/sanho/internal/infra/gitx"
)

const markerText = "<<<<<<< sanho-ours\nmine\n=======\ntheirs\n>>>>>>> sanho-upstream\n"

// TestStagedScanIgnoresUnchangedFiles is F-H4a.
//
// The pre-commit gate is about what THIS commit introduces. A marker
// that already sits in HEAD — a `--no-verify` commit, a checkout, a
// v0.1-era file — arrived by some other route, and blocking every
// unrelated commit until the user fixes a file they are not touching is
// a gate aimed at the wrong action. The old scan walked the whole index
// and did exactly that.
func TestStagedScanIgnoresUnchangedFiles(t *testing.T) {
	dir, _ := newRepoWithDocs(t)
	repo := newRepoHandle(t, dir)

	// A marker lands in history without passing the gate.
	writeFile(t, dir, "docs/legacy.md", []byte(markerText))
	commitAll(t, dir, "docs: markers slipped in")

	// Now stage something else entirely.
	writeFile(t, dir, "docs/new.md", []byte("clean content\n"))
	gitRun(t, dir, "add", "--", "docs/new.md")

	conflicted, err := repo.ScanStagedDocsForMarkers(context.Background())
	if err != nil {
		t.Fatalf("ScanStagedDocsForMarkers: %v", err)
	}
	if len(conflicted) != 0 {
		t.Fatalf("conflicted = %v, want none: the marker is in an unchanged file", conflicted)
	}
}

// TestStagedScanStillCatchesStagedMarkers keeps the scoping from
// hollowing the gate out.
func TestStagedScanStillCatchesStagedMarkers(t *testing.T) {
	dir, _ := newRepoWithDocs(t)
	repo := newRepoHandle(t, dir)

	writeFile(t, dir, "docs/a.md", []byte(markerText))
	gitRun(t, dir, "add", "--", "docs/a.md")

	conflicted, err := repo.ScanStagedDocsForMarkers(context.Background())
	if err != nil {
		t.Fatalf("ScanStagedDocsForMarkers: %v", err)
	}
	if want := []string{"docs/a.md"}; len(conflicted) != 1 || conflicted[0] != want[0] {
		t.Fatalf("conflicted = %v, want %v", conflicted, want)
	}
}

// TestStagedScanOnAnUnbornHead treats every staged docs path as added.
func TestStagedScanOnAnUnbornHead(t *testing.T) {
	dir := newRepo(t)
	repo := newRepoHandle(t, dir)

	writeFile(t, dir, "docs/a.md", []byte(markerText))
	gitRun(t, dir, "add", "-A", "--", ".")

	conflicted, err := repo.ScanStagedDocsForMarkers(context.Background())
	if err != nil {
		t.Fatalf("ScanStagedDocsForMarkers: %v", err)
	}
	if len(conflicted) != 1 {
		t.Fatalf("conflicted = %v, want the staged marker on a first commit", conflicted)
	}
}

// TestPushScanScopesToTheDiffAgainstTheRemoteTip is F-H4b: what the push
// introduces is what the push gate reads.
func TestPushScanScopesToTheDiffAgainstTheRemoteTip(t *testing.T) {
	dir, _ := newRepoWithDocs(t)
	repo := newRepoHandle(t, dir)

	writeFile(t, dir, "docs/legacy.md", []byte(markerText))
	published := commitAll(t, dir, "docs: markers already upstream")

	writeFile(t, dir, "docs/new.md", []byte("clean\n"))
	tip := commitAll(t, dir, "docs: add a clean file")

	// Scoped to the diff: nothing new carries markers.
	conflicted, err := repo.ScanDocsBlobsSince(context.Background(), published, tip)
	if err != nil {
		t.Fatalf("ScanDocsBlobsSince: %v", err)
	}
	if len(conflicted) != 0 {
		t.Fatalf("conflicted = %v, want none for the diff-scoped scan", conflicted)
	}

	// With no previous remote tip — a brand-new branch — the whole tree
	// is read, and the marker is caught. That is the fail-closed half of
	// the same rule.
	conflicted, err = repo.ScanDocsBlobsSince(context.Background(), "", tip)
	if err != nil {
		t.Fatalf("ScanDocsBlobsSince(full tree): %v", err)
	}
	if len(conflicted) != 1 || conflicted[0] != "docs/legacy.md" {
		t.Fatalf("conflicted = %v, want the full-tree scan to catch docs/legacy.md", conflicted)
	}
}

// TestPushScanFallsBackToTheFullTreeForAnUnknownSince covers a rewritten
// or pruned remote tip: an OID this repository no longer has cannot be
// diffed against, so the scan must not silently pass.
func TestPushScanFallsBackToTheFullTreeForAnUnknownSince(t *testing.T) {
	dir, _ := newRepoWithDocs(t)
	repo := newRepoHandle(t, dir)

	writeFile(t, dir, "docs/legacy.md", []byte(markerText))
	tip := commitAll(t, dir, "docs: markers")

	conflicted, err := repo.ScanDocsBlobsSince(context.Background(), strings.Repeat("a", 40), tip)
	if err != nil {
		t.Fatalf("ScanDocsBlobsSince: %v", err)
	}
	if len(conflicted) != 1 {
		t.Fatalf("conflicted = %v, want the unknown 'since' to force a full-tree scan", conflicted)
	}
}

// --- F-M8: sniff first, size second ------------------------------------

// TestOversizedBinaryDocsAreSkipped: a large binary file under docs/ is
// not text, cannot carry a conflict marker, and must not block the
// commit path merely for being big (C1 alignment).
func TestOversizedBinaryDocsAreSkipped(t *testing.T) {
	dir, _ := newRepoWithDocs(t)
	repo := newRepoHandle(t, dir)

	blob := make([]byte, markers.MaxScanSize+4096)
	blob[10] = 0 // a NUL in the first 8 KiB: binary by §5.4's rule
	for i := range blob {
		if i != 10 {
			blob[i] = 'x'
		}
	}
	writeFile(t, dir, "docs/asset.bin", blob)
	tip := commitAll(t, dir, "docs: add a large binary asset")

	conflicted, err := repo.ScanDocsBlobsSince(context.Background(), "", tip)
	if err != nil {
		t.Fatalf("a large BINARY doc must not fail the scan: %v", err)
	}
	if len(conflicted) != 0 {
		t.Fatalf("conflicted = %v, want none", conflicted)
	}
}

// TestOversizedTextDocsStillFailClosed keeps audit H2's contract for the
// case the size cap was written for.
func TestOversizedTextDocsStillFailClosed(t *testing.T) {
	dir, _ := newRepoWithDocs(t)
	repo := newRepoHandle(t, dir)

	writeFile(t, dir, "docs/huge.md", []byte(strings.Repeat("a", markers.MaxScanSize+1)))
	tip := commitAll(t, dir, "docs: add a huge text file")

	_, err := repo.ScanDocsBlobsSince(context.Background(), "", tip)
	if !errors.Is(err, markers.ErrTooLarge) {
		t.Fatalf("error = %v, want markers.ErrTooLarge", err)
	}
	if !strings.Contains(err.Error(), "docs/huge.md") {
		t.Errorf("error = %v, want it to name the file", err)
	}
}

// --- the perf smoke test (F-H4) ----------------------------------------

// TestGateCostIsProportionalToTheChange is the smoke test the fix wave
// owes: 500 docs files used to mean 1,000 git child processes per gate
// (39 seconds at 4,000 files, which is where this started). Both gates
// now spawn a bounded handful.
//
// The bound is deliberately loose — it is a smoke test on shared CI
// hardware, not a benchmark — but two seconds is far below what
// per-file spawning costs at this size.
func TestGateCostIsProportionalToTheChange(t *testing.T) {
	const files = 500
	const budget = 2 * time.Second

	dir, _ := newRepoWithDocs(t)
	repo := newRepoHandle(t, dir)

	for i := 0; i < files; i++ {
		writeFile(t, dir, fmt.Sprintf("docs/bulk/%03d.md", i), []byte("bulk content\n"))
	}
	tip := commitAll(t, dir, "docs: bulk import")

	started := time.Now()
	if _, err := repo.ScanDocsBlobsSince(context.Background(), "", tip); err != nil {
		t.Fatalf("full-tree scan of %d files: %v", files, err)
	}
	fullTree := time.Since(started)
	if fullTree > budget {
		t.Errorf("full-tree scan of %d files took %s, want under %s", files, fullTree, budget)
	}

	writeFile(t, dir, "docs/bulk/000.md", []byte("edited\n"))
	gitRun(t, dir, "add", "--", "docs/bulk/000.md")

	started = time.Now()
	if _, err := repo.ScanStagedDocsForMarkers(context.Background()); err != nil {
		t.Fatalf("staged scan: %v", err)
	}
	if staged := time.Since(started); staged > budget {
		t.Errorf("staged scan with %d unchanged files took %s, want under %s", files, staged, budget)
	}
	t.Logf("500-file gate: full tree %s, staged diff %s", fullTree, time.Since(started))
}

// --- F-M7: validate every path before deleting anything ----------------

// TestCheckoutDocsTreeRefusesEscapingPaths.
//
// A docs tree comes from canonical, which is somebody else's repository,
// and `git mktree` will happily build a tree whose entries are named
// `..` or `.git` even though `git add` never would. CheckoutDocsTree
// deletes before it writes, so a target rejected half-way through would
// leave the docs directory emptied of everything the deletion pass had
// already removed. Validation therefore covers the whole path list
// BEFORE the first deletion.
func TestCheckoutDocsTreeRefusesEscapingPaths(t *testing.T) {
	for _, name := range []string{"..", ".git"} {
		t.Run(name, func(t *testing.T) {
			dir, _ := newRepoWithDocs(t)
			repo := newRepoHandle(t, dir)

			blob := gitLine(t, dir, "hash-object", "-w", "-t", "blob", os.DevNull)
			hostile := mktree(t, dir, "100644 blob "+blob+"\t"+name+"\n")

			before := readDocsFile(t, dir, "docs/a.md")
			err := repo.CheckoutDocsTree(context.Background(), hostile)
			if err == nil {
				t.Fatalf("CheckoutDocsTree accepted a tree entry named %q", name)
			}
			if !strings.Contains(err.Error(), name) {
				t.Errorf("error = %v, want it to name the offending path", err)
			}
			// Nothing moved: the refusal precedes the deletion pass.
			if got := readDocsFile(t, dir, "docs/a.md"); got != before {
				t.Fatalf("docs/a.md = %q after a refused checkout, want %q", got, before)
			}
			if !fileExists(t, filepath.Join(dir, "docs", "nested", "b.md")) {
				t.Error("the refused checkout deleted docs/nested/b.md")
			}
		})
	}
}

// mktree builds a tree from a raw `git mktree` record, which accepts
// entry names git's porcelain would reject.
func mktree(t *testing.T, dir, record string) string {
	t.Helper()

	cmd := gitx.New(dir)
	res, err := cmd.RunWithStdin(context.Background(), strings.NewReader(record), "mktree")
	if err != nil {
		t.Fatalf("git mktree: %v", err)
	}
	return strings.TrimSpace(string(res.Stdout))
}

func readDocsFile(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(data)
}

func fileExists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Lstat(path)
	return err == nil
}
