package appgit_test

// Real-git coverage for the write side of the app-repository adapter
// (docs/architecture.md "Synchronization"). Two properties dominate: docs paths must end up
// exactly at the requested tree — additions, modifications *and*
// deletions — and nothing outside the docs directory may move, not even
// content the user has already staged.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/irootkernel/sanho/internal/domain/markers"
	"github.com/irootkernel/sanho/internal/infra/gitx"
)

// blobSpec is one file of a composed docs tree.
type blobSpec struct {
	content string
	// mode is the git file mode; "" means a regular file.
	mode string
}

func regular(content string) blobSpec    { return blobSpec{content: content} }
func executable(content string) blobSpec { return blobSpec{content: content, mode: "100755"} }

// symlink composes a symlink entry: a git symlink's blob content is its
// target path.
func symlink(target string) blobSpec { return blobSpec{content: target, mode: "120000"} }

// buildDocsTree writes files into dir's object database and returns the
// tree they form. Paths are relative to the docs root, which is what
// CheckoutDocsTree consumes.
//
// It uses a scratch index (GIT_INDEX_FILE) so composing a target tree
// never disturbs the repository's real index — the very thing these
// tests are watching.
func buildDocsTree(t *testing.T, dir string, files map[string]blobSpec) string {
	t.Helper()

	scratch := t.TempDir()
	run := gitx.New(dir, gitx.WithEnv("GIT_INDEX_FILE="+filepath.Join(scratch, "index")))
	ctx := context.Background()

	if _, err := run.Run(ctx, "read-tree", "--empty"); err != nil {
		t.Fatalf("empty the scratch index: %v", err)
	}
	for path, spec := range files {
		source := filepath.Join(scratch, "blob")
		if err := os.WriteFile(source, []byte(spec.content), 0644); err != nil {
			t.Fatalf("write blob source: %v", err)
		}
		oid, err := run.Line(ctx, "hash-object", "-w", "--", source)
		if err != nil {
			t.Fatalf("hash %s: %v", path, err)
		}
		mode := spec.mode
		if mode == "" {
			mode = "100644"
		}
		if _, err := run.Run(ctx, "update-index", "--add", "--cacheinfo", mode+","+oid+","+path); err != nil {
			t.Fatalf("stage %s: %v", path, err)
		}
	}

	tree, err := run.Line(ctx, "write-tree")
	if err != nil {
		t.Fatalf("write the composed tree: %v", err)
	}
	return tree
}

// statusOutsideDocs renders `git status --porcelain` with every docs
// line dropped. Equality of this string across an operation is the
// assertion that the user's own work in progress was left alone.
func statusOutsideDocs(t *testing.T, dir string) string {
	t.Helper()

	var kept []string
	for _, line := range strings.Split(gitRun(t, dir, "status", "--porcelain"), "\n") {
		if len(line) < 4 {
			continue
		}
		if path := line[3:]; path == "docs" || path == "docs/" || strings.HasPrefix(path, "docs/") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// requireAbsent fails when path still exists in the worktree.
func requireAbsent(t *testing.T, dir, path string) {
	t.Helper()
	if _, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(path))); !os.IsNotExist(err) {
		t.Fatalf("%s still exists (lstat error %v)", path, err)
	}
}

// indexedDocs lists the docs paths the index holds, one per line.
func indexedDocs(t *testing.T, dir string) string {
	t.Helper()
	return strings.TrimSpace(gitRun(t, dir, "ls-files", "--", "docs"))
}

// seedWorkspace builds an app repository with docs and a code file, and
// returns it with one commit in place.
func seedWorkspace(t *testing.T) string {
	t.Helper()
	dir := newRepo(t)
	writeFile(t, dir, "README.md", []byte("readme\n"))
	writeFile(t, dir, "src/app.go", []byte("package main\n"))
	writeFile(t, dir, "docs/a.md", []byte("alpha\n"))
	writeFile(t, dir, "docs/gone.md", []byte("gone\n"))
	writeFile(t, dir, "docs/nested/deep.md", []byte("deep\n"))
	commitAll(t, dir, "docs: seed workspace")
	return dir
}

func TestDocsCleanReportsTheDocsPathspecOnly(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name  string
		dirty func(t *testing.T, dir string)
		want  bool
	}{
		{
			name:  "clean checkout",
			dirty: func(t *testing.T, dir string) {},
			want:  true,
		},
		{
			name: "non-docs work in progress does not count",
			dirty: func(t *testing.T, dir string) {
				writeFile(t, dir, "README.md", []byte("edited\n"))
				writeFile(t, dir, "src/new.go", []byte("package main\n"))
				gitRun(t, dir, "add", "--", "README.md")
			},
			want: true,
		},
		{
			name: "unstaged docs edit",
			dirty: func(t *testing.T, dir string) {
				writeFile(t, dir, "docs/a.md", []byte("alpha edited\n"))
			},
			want: false,
		},
		{
			name: "staged docs edit",
			dirty: func(t *testing.T, dir string) {
				writeFile(t, dir, "docs/a.md", []byte("alpha edited\n"))
				gitRun(t, dir, "add", "--", "docs/a.md")
			},
			want: false,
		},
		{
			name: "deleted docs file",
			dirty: func(t *testing.T, dir string) {
				if err := os.Remove(filepath.Join(dir, "docs", "a.md")); err != nil {
					t.Fatalf("remove docs/a.md: %v", err)
				}
			},
			want: false,
		},
		{
			name: "untracked docs file",
			dirty: func(t *testing.T, dir string) {
				writeFile(t, dir, "docs/new.md", []byte("new\n"))
			},
			want: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := seedWorkspace(t)
			test.dirty(t, dir)

			got, err := newRepoHandle(t, dir).DocsClean(ctx)
			if err != nil {
				t.Fatalf("DocsClean: %v", err)
			}
			if got != test.want {
				t.Fatalf("DocsClean = %v, want %v; status:\n%s", got, test.want,
					gitRun(t, dir, "status", "--porcelain"))
			}
		})
	}
}

func TestHeadDocsTree(t *testing.T) {
	ctx := context.Background()

	t.Run("matches the committed docs tree", func(t *testing.T) {
		dir, head := newRepoWithDocs(t)
		repo := newRepoHandle(t, dir)

		want, err := repo.DocsTreeOf(ctx, head)
		if err != nil {
			t.Fatalf("DocsTreeOf: %v", err)
		}
		got, err := repo.HeadDocsTree(ctx)
		if err != nil {
			t.Fatalf("HeadDocsTree: %v", err)
		}
		if got != want {
			t.Fatalf("HeadDocsTree = %s, want %s", got, want)
		}
	})

	t.Run("unborn HEAD is the empty tree", func(t *testing.T) {
		dir := newRepo(t)
		repo := newRepoHandle(t, dir)

		empty, err := repo.EmptyTree(ctx)
		if err != nil {
			t.Fatalf("EmptyTree: %v", err)
		}
		got, err := repo.HeadDocsTree(ctx)
		if err != nil {
			t.Fatalf("HeadDocsTree: %v", err)
		}
		if got != empty {
			t.Fatalf("HeadDocsTree = %s, want the empty tree %s", got, empty)
		}
	})

	t.Run("a HEAD without docs is the empty tree", func(t *testing.T) {
		dir := newRepo(t)
		writeFile(t, dir, "README.md", []byte("readme\n"))
		commitAll(t, dir, "chore: no docs")
		repo := newRepoHandle(t, dir)

		empty, err := repo.EmptyTree(ctx)
		if err != nil {
			t.Fatalf("EmptyTree: %v", err)
		}
		got, err := repo.HeadDocsTree(ctx)
		if err != nil {
			t.Fatalf("HeadDocsTree: %v", err)
		}
		if got != empty {
			t.Fatalf("HeadDocsTree = %s, want the empty tree %s", got, empty)
		}
	})
}

func TestCommitTreeResolvesTheRootTree(t *testing.T) {
	dir, head := newRepoWithDocs(t)

	got, err := newRepoHandle(t, dir).CommitTree(context.Background(), head)
	if err != nil {
		t.Fatalf("CommitTree: %v", err)
	}
	if want := gitLine(t, dir, "rev-parse", head+"^{tree}"); got != want {
		t.Fatalf("CommitTree = %s, want %s", got, want)
	}
}

// TestCheckoutDocsTreeAppliesEveryKindOfChange is the deletion case:
// files present locally but absent from the target tree must leave the
// worktree *and* the index, which is exactly what a plain
// `git checkout <tree> -- docs/` does not do.
func TestCheckoutDocsTreeAppliesEveryKindOfChange(t *testing.T) {
	dir := seedWorkspace(t)
	ctx := context.Background()
	repo := newRepoHandle(t, dir)

	before := statusOutsideDocs(t, dir)
	target := buildDocsTree(t, dir, map[string]blobSpec{
		"a.md":            regular("alpha upstream\n"), // modified
		"added.md":        regular("added\n"),          // added
		"fresh/leaf.md":   regular("leaf\n"),           // added in a new directory
		"nested/other.md": regular("other\n"),          // sibling of a deleted file
	})

	if err := repo.CheckoutDocsTree(ctx, target); err != nil {
		t.Fatalf("CheckoutDocsTree: %v", err)
	}

	// Deletions reached the worktree, and the directory a deletion
	// emptied is gone with them.
	requireAbsent(t, dir, "docs/gone.md")
	requireAbsent(t, dir, "docs/nested/deep.md")

	// Deletions reached the index too.
	wantIndex := "docs/a.md\ndocs/added.md\ndocs/fresh/leaf.md\ndocs/nested/other.md"
	if got := indexedDocs(t, dir); got != wantIndex {
		t.Fatalf("index docs paths =\n%s\nwant\n%s", got, wantIndex)
	}

	// The worktree hashes to exactly the requested tree.
	worktree, err := repo.WorktreeDocsTree(ctx)
	if err != nil {
		t.Fatalf("WorktreeDocsTree: %v", err)
	}
	if worktree != target {
		t.Fatalf("worktree docs tree = %s, want %s\n%s", worktree, target,
			gitRun(t, dir, "ls-tree", "-r", "--name-only", worktree))
	}

	// The docs difference is fully staged: no unstaged residue, which is
	// what lets CommitDocs record the result in one step.
	for _, line := range strings.Split(gitRun(t, dir, "status", "--porcelain", "--", "docs"), "\n") {
		if line == "" {
			continue
		}
		if line[1] != ' ' {
			t.Fatalf("docs status has unstaged residue: %q", line)
		}
	}

	if after := statusOutsideDocs(t, dir); after != before {
		t.Fatalf("paths outside docs changed:\nbefore: %q\nafter:  %q", before, after)
	}
}

// TestCheckoutDocsTreeKeepsStagedNonDocsWork pins the property that
// makes sync safe to run in the middle of someone's work (D3): staged
// non-docs content survives byte for byte.
func TestCheckoutDocsTreeKeepsStagedNonDocsWork(t *testing.T) {
	dir := seedWorkspace(t)
	ctx := context.Background()

	// A staged edit, a staged addition, an unstaged edit and an
	// untracked file — every non-docs state at once.
	writeFile(t, dir, "README.md", []byte("readme staged\n"))
	writeFile(t, dir, "src/added.go", []byte("package added\n"))
	gitRun(t, dir, "add", "--", "README.md", "src/added.go")
	writeFile(t, dir, "src/app.go", []byte("package main // edited\n"))
	writeFile(t, dir, "src/untracked.go", []byte("package untracked\n"))

	before := statusOutsideDocs(t, dir)
	stagedREADME := gitLine(t, dir, "rev-parse", ":README.md")
	stagedAdded := gitLine(t, dir, "rev-parse", ":src/added.go")
	worktreeApp, err := os.ReadFile(filepath.Join(dir, "src", "app.go"))
	if err != nil {
		t.Fatalf("read src/app.go: %v", err)
	}

	target := buildDocsTree(t, dir, map[string]blobSpec{"only.md": regular("only\n")})
	if err := newRepoHandle(t, dir).CheckoutDocsTree(ctx, target); err != nil {
		t.Fatalf("CheckoutDocsTree: %v", err)
	}

	if after := statusOutsideDocs(t, dir); after != before {
		t.Fatalf("status outside docs changed:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if got := gitLine(t, dir, "rev-parse", ":README.md"); got != stagedREADME {
		t.Fatalf("staged README.md became %s, want %s", got, stagedREADME)
	}
	if got := gitLine(t, dir, "rev-parse", ":src/added.go"); got != stagedAdded {
		t.Fatalf("staged src/added.go became %s, want %s", got, stagedAdded)
	}
	got, err := os.ReadFile(filepath.Join(dir, "src", "app.go"))
	if err != nil {
		t.Fatalf("read src/app.go: %v", err)
	}
	if string(got) != string(worktreeApp) {
		t.Fatalf("unstaged src/app.go = %q, want %q", got, worktreeApp)
	}
}

func TestCheckoutDocsTreePreservesModesAndSymlinks(t *testing.T) {
	dir := seedWorkspace(t)
	ctx := context.Background()
	repo := newRepoHandle(t, dir)

	target := buildDocsTree(t, dir, map[string]blobSpec{
		"a.md":   regular("alpha\n"),
		"run.sh": executable("#!/bin/sh\n"),
		"link":   symlink("a.md"),
	})
	if err := repo.CheckoutDocsTree(ctx, target); err != nil {
		t.Fatalf("CheckoutDocsTree: %v", err)
	}

	info, err := os.Lstat(filepath.Join(dir, "docs", "link"))
	if err != nil {
		t.Fatalf("lstat docs/link: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("docs/link is not a symlink (mode %v)", info.Mode())
	}
	script, err := os.Stat(filepath.Join(dir, "docs", "run.sh"))
	if err != nil {
		t.Fatalf("stat docs/run.sh: %v", err)
	}
	if script.Mode()&0100 == 0 {
		t.Fatalf("docs/run.sh lost its executable bit (mode %v)", script.Mode())
	}

	worktree, err := repo.WorktreeDocsTree(ctx)
	if err != nil {
		t.Fatalf("WorktreeDocsTree: %v", err)
	}
	if worktree != target {
		t.Fatalf("worktree docs tree = %s, want %s", worktree, target)
	}
}

func TestCheckoutDocsTreeToTheEmptyTreeRemovesEveryDocsFile(t *testing.T) {
	dir := seedWorkspace(t)
	ctx := context.Background()
	repo := newRepoHandle(t, dir)

	before := statusOutsideDocs(t, dir)
	empty, err := repo.EmptyTree(ctx)
	if err != nil {
		t.Fatalf("EmptyTree: %v", err)
	}
	if err := repo.CheckoutDocsTree(ctx, empty); err != nil {
		t.Fatalf("CheckoutDocsTree: %v", err)
	}

	requireAbsent(t, dir, "docs")
	if got := indexedDocs(t, dir); got != "" {
		t.Fatalf("index still holds docs paths:\n%s", got)
	}
	if after := statusOutsideDocs(t, dir); after != before {
		t.Fatalf("paths outside docs changed:\nbefore: %q\nafter:  %q", before, after)
	}
}

// TestRestoreDocsFromHeadDropsFilesHeadNeverHad is the abort case: a
// conflicted sync can materialize files that HEAD does not carry, and
// the restore has to take them back out of both the worktree and the
// index.
func TestRestoreDocsFromHeadDropsFilesHeadNeverHad(t *testing.T) {
	dir := seedWorkspace(t)
	ctx := context.Background()
	repo := newRepoHandle(t, dir)

	headDocs, err := repo.HeadDocsTree(ctx)
	if err != nil {
		t.Fatalf("HeadDocsTree: %v", err)
	}
	writeFile(t, dir, "src/wip.go", []byte("package wip\n"))
	gitRun(t, dir, "add", "--", "src/wip.go")
	before := statusOutsideDocs(t, dir)

	// Stand in for a conflicted sync: a new file, a rewritten file and a
	// deleted one, all staged.
	merged := buildDocsTree(t, dir, map[string]blobSpec{
		"a.md":     regular("<<<<<<< sanho-ours\nalpha\n=======\nalpha upstream\n>>>>>>> sanho-upstream\n"),
		"brand.md": regular("materialized by the merge\n"),
	})
	if err := repo.CheckoutDocsTree(ctx, merged); err != nil {
		t.Fatalf("CheckoutDocsTree: %v", err)
	}

	if err := repo.RestoreDocsFromHead(ctx); err != nil {
		t.Fatalf("RestoreDocsFromHead: %v", err)
	}

	requireAbsent(t, dir, "docs/brand.md")
	restored, err := repo.WorktreeDocsTree(ctx)
	if err != nil {
		t.Fatalf("WorktreeDocsTree: %v", err)
	}
	if restored != headDocs {
		t.Fatalf("restored docs tree = %s, want HEAD's %s", restored, headDocs)
	}
	if status := gitRun(t, dir, "status", "--porcelain", "--", "docs"); strings.TrimSpace(status) != "" {
		t.Fatalf("docs are not clean after the restore:\n%s", status)
	}
	if after := statusOutsideDocs(t, dir); after != before {
		t.Fatalf("paths outside docs changed:\nbefore: %q\nafter:  %q", before, after)
	}
}

// TestCommitDocsRecordsOnlyDocs pins the synchronization contract commit: the docs
// pathspec goes in, staged non-docs work stays staged.
func TestCommitDocsRecordsOnlyDocs(t *testing.T) {
	dir := seedWorkspace(t)
	ctx := context.Background()
	repo := newRepoHandle(t, dir)

	writeFile(t, dir, "README.md", []byte("readme staged\n"))
	gitRun(t, dir, "add", "--", "README.md")
	stagedREADME := gitLine(t, dir, "rev-parse", ":README.md")

	target := buildDocsTree(t, dir, map[string]blobSpec{"a.md": regular("alpha upstream\n")})
	if err := repo.CheckoutDocsTree(ctx, target); err != nil {
		t.Fatalf("CheckoutDocsTree: %v", err)
	}

	const message = "[SANHO] Sync docs to 0123456789ab"
	commit, err := repo.CommitDocs(ctx, message)
	if err != nil {
		t.Fatalf("CommitDocs: %v", err)
	}

	if head := gitLine(t, dir, "rev-parse", "HEAD"); head != commit {
		t.Fatalf("CommitDocs returned %s, HEAD is %s", commit, head)
	}
	if subject := gitLine(t, dir, "log", "-1", "--format=%s", commit); subject != message {
		t.Fatalf("subject = %q, want %q", subject, message)
	}

	changed := strings.Fields(gitRun(t, dir, "diff-tree", "--no-commit-id", "--name-only", "-r", commit))
	for _, path := range changed {
		if !strings.HasPrefix(path, "docs/") {
			t.Fatalf("the sync commit touched %s; it changed %v", path, changed)
		}
	}
	if len(changed) == 0 {
		t.Fatal("the sync commit changed nothing")
	}

	// The user's staged non-docs work is still staged and still theirs.
	if got := gitLine(t, dir, "rev-parse", ":README.md"); got != stagedREADME {
		t.Fatalf("staged README.md became %s, want %s", got, stagedREADME)
	}
	if status := strings.TrimSpace(gitRun(t, dir, "status", "--porcelain")); status != "M  README.md" {
		t.Fatalf("status after the sync commit = %q, want the staged README alone", status)
	}
	if author := gitLine(t, dir, "log", "-1", "--format=%an <%ae>", commit); author != "Test Author <author@example.test>" {
		t.Fatalf("author = %q, want the repository's configured user", author)
	}
}

func TestScanWorktreeDocsForMarkers(t *testing.T) {
	ctx := context.Background()

	t.Run("names the files carrying markers", func(t *testing.T) {
		dir := newRepo(t)
		writeFile(t, dir, "docs/clean.md", []byte("nothing here\n"))
		writeFile(t, dir, "docs/nested/conflicted.md",
			[]byte("intro\n<<<<<<< sanho-ours\nmine\n=======\ntheirs\n>>>>>>> sanho-upstream\n"))
		// Untracked, never committed: resolving can create files, and the
		// worktree scan has to see them.
		writeFile(t, dir, "docs/untracked.md",
			[]byte("<<<<<<< sanho-ours\na\n=======\nb\n>>>>>>> sanho-upstream\n"))
		writeFile(t, dir, "src/conflicted.go",
			[]byte("<<<<<<< sanho-ours\na\n=======\nb\n>>>>>>> sanho-upstream\n"))

		got, err := newRepoHandle(t, dir).ScanWorktreeDocsForMarkers(ctx)
		if err != nil {
			t.Fatalf("ScanWorktreeDocsForMarkers: %v", err)
		}
		want := []string{"docs/nested/conflicted.md", "docs/untracked.md"}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("marker paths = %v, want %v", got, want)
		}
	})

	t.Run("clean docs report nothing", func(t *testing.T) {
		dir, _ := newRepoWithDocs(t)
		got, err := newRepoHandle(t, dir).ScanWorktreeDocsForMarkers(ctx)
		if err != nil {
			t.Fatalf("ScanWorktreeDocsForMarkers: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("clean docs reported as conflicted: %v", got)
		}
	})

	t.Run("binaries and symlinks are skipped", func(t *testing.T) {
		dir := newRepo(t)
		writeFile(t, dir, "docs/a.md", []byte("alpha\n"))
		writeFile(t, dir, "docs/image.png",
			append([]byte{0x89, 'P', 'N', 'G', 0x00},
				"<<<<<<< sanho-ours\na\n=======\nb\n>>>>>>> sanho-upstream\n"...))
		if err := os.Symlink("a.md", filepath.Join(dir, "docs", "link.md")); err != nil {
			t.Fatalf("symlink: %v", err)
		}

		got, err := newRepoHandle(t, dir).ScanWorktreeDocsForMarkers(ctx)
		if err != nil {
			t.Fatalf("ScanWorktreeDocsForMarkers: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("binary or symlink reported as conflicted: %v", got)
		}
	})

	t.Run("an oversized file is an error naming it", func(t *testing.T) {
		dir := newRepo(t)
		oversized := make([]byte, markers.MaxScanSize+1)
		for i := range oversized {
			oversized[i] = 'x'
		}
		writeFile(t, dir, "docs/huge.md", oversized)

		_, err := newRepoHandle(t, dir).ScanWorktreeDocsForMarkers(ctx)
		if err == nil {
			t.Fatal("an oversized docs file passed the gate")
		}
		if !errors.Is(err, markers.ErrTooLarge) {
			t.Fatalf("error = %v, want it to wrap markers.ErrTooLarge", err)
		}
		if !strings.Contains(err.Error(), "docs/huge.md") {
			t.Fatalf("error = %v, want it to name the offending file", err)
		}
	})

	t.Run("an absent docs directory scans clean", func(t *testing.T) {
		dir := newRepo(t)
		writeFile(t, dir, "README.md", []byte("readme\n"))
		commitAll(t, dir, "chore: no docs")

		got, err := newRepoHandle(t, dir).ScanWorktreeDocsForMarkers(ctx)
		if err != nil {
			t.Fatalf("ScanWorktreeDocsForMarkers: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("paths = %v, want none", got)
		}
	})
}
