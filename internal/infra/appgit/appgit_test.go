package appgit_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/irootkernel/sanho/internal/domain/markers"
	"github.com/irootkernel/sanho/internal/infra/appgit"
	"github.com/irootkernel/sanho/internal/infra/gitx"
)

// Every test here runs against a real git binary in a temp repository;
// nothing below the git boundary is faked (AGENTS.md Git-boundary testing rule).

func TestMain(m *testing.M) {
	for key, value := range map[string]string{
		"GIT_CONFIG_GLOBAL":   os.DevNull,
		"GIT_CONFIG_SYSTEM":   os.DevNull,
		"GIT_AUTHOR_NAME":     "Test Author",
		"GIT_AUTHOR_EMAIL":    "author@example.test",
		"GIT_COMMITTER_NAME":  "Test Committer",
		"GIT_COMMITTER_EMAIL": "committer@example.test",
	} {
		if err := os.Setenv(key, value); err != nil {
			fmt.Fprintf(os.Stderr, "set %s: %v\n", key, err)
			os.Exit(1)
		}
	}
	os.Exit(m.Run())
}

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	res, err := gitx.New(dir).Run(context.Background(), args...)
	if err != nil {
		t.Fatalf("git %s in %s: %v", strings.Join(args, " "), dir, err)
	}
	return string(res.Stdout)
}

func gitLine(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := gitx.New(dir).Line(context.Background(), args...)
	if err != nil {
		t.Fatalf("git %s in %s: %v", strings.Join(args, " "), dir, err)
	}
	return out
}

func writeFile(t *testing.T, dir, path string, content []byte) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatalf("create directory for %s: %v", full, err)
	}
	if err := os.WriteFile(full, content, 0644); err != nil {
		t.Fatalf("write %s: %v", full, err)
	}
}

func commitAll(t *testing.T, dir, message string) string {
	t.Helper()
	gitRun(t, dir, "add", "-A", "--", ".")
	gitRun(t, dir, "commit", "--quiet", "--allow-empty", "-m", message)
	return gitLine(t, dir, "rev-parse", "HEAD")
}

// newRepo creates an app repository on branch main with no commits yet.
func newRepo(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "app")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("create %s: %v", dir, err)
	}
	gitRun(t, dir, "init", "--quiet", "-b", "main")
	return dir
}

func newRepoWithDocs(t *testing.T) (dir string, head string) {
	t.Helper()
	dir = newRepo(t)
	writeFile(t, dir, "README.md", []byte("readme\n"))
	writeFile(t, dir, "docs/a.md", []byte("alpha\n"))
	writeFile(t, dir, "docs/nested/b.md", []byte("beta\n"))
	head = commitAll(t, dir, "docs: seed")
	return dir, head
}

func newRepoHandle(t *testing.T, dir string) *appgit.Repo {
	t.Helper()
	return appgit.New(dir, "docs", gitx.New(dir))
}

func TestNewAppliesDefaults(t *testing.T) {
	dir, _ := newRepoWithDocs(t)

	repo := appgit.New(dir, "", nil)
	if got := repo.DocsDir(); got != appgit.DefaultDocsDir {
		t.Errorf("DocsDir() = %q, want %q", got, appgit.DefaultDocsDir)
	}
	if got := repo.WorkDir(); got != dir {
		t.Errorf("WorkDir() = %q, want %q", got, dir)
	}
	// A nil runner must still work: New builds the default policy runner.
	if _, err := repo.EmptyTree(context.Background()); err != nil {
		t.Fatalf("EmptyTree with a default runner: %v", err)
	}
}

func TestDocsTreeOf(t *testing.T) {
	dir, head := newRepoWithDocs(t)
	repo := newRepoHandle(t, dir)
	ctx := context.Background()

	got, err := repo.DocsTreeOf(ctx, head)
	if err != nil {
		t.Fatalf("DocsTreeOf: %v", err)
	}
	if want := gitLine(t, dir, "rev-parse", head+":docs"); got != want {
		t.Fatalf("DocsTreeOf = %s, want %s", got, want)
	}
}

func TestDocsTreeOfFallsBackToTheEmptyTree(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, dir, "README.md", []byte("readme\n"))
	head := commitAll(t, dir, "chore: no docs yet")

	repo := newRepoHandle(t, dir)
	ctx := context.Background()

	empty, err := repo.EmptyTree(ctx)
	if err != nil {
		t.Fatalf("EmptyTree: %v", err)
	}
	got, err := repo.DocsTreeOf(ctx, head)
	if err != nil {
		t.Fatalf("DocsTreeOf: %v", err)
	}
	if got != empty {
		t.Fatalf("DocsTreeOf = %s, want the empty tree %s", got, empty)
	}
	// The empty tree must be usable as a git object, not just a string.
	if kind := gitLine(t, dir, "cat-file", "-t", empty); kind != "tree" {
		t.Fatalf("empty tree OID %s is not resolvable: %q", empty, kind)
	}
}

func TestDocsTreeOfRejectsAnAbsentCommit(t *testing.T) {
	dir, _ := newRepoWithDocs(t)
	repo := newRepoHandle(t, dir)

	_, err := repo.DocsTreeOf(context.Background(), "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	if err == nil {
		t.Fatal("DocsTreeOf accepted a commit that does not exist")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("error = %v, want it to say the commit does not exist", err)
	}
}

func TestScanDocsBlobsForMarkers(t *testing.T) {
	const conflicted = "intro\n" +
		"<<<<<<< sanho-ours\n" +
		"mine\n" +
		"=======\n" +
		"theirs\n" +
		">>>>>>> sanho-upstream\n" +
		"outro\n"

	dir := newRepo(t)
	writeFile(t, dir, "docs/clean.md", []byte("nothing to see\n"))
	writeFile(t, dir, "docs/conflicted.md", []byte(conflicted))
	writeFile(t, dir, "docs/nested/also.md", []byte(conflicted))
	// Markers outside docs are somebody else's problem.
	writeFile(t, dir, "src/code.go", []byte(conflicted))
	// A binary blob whose bytes happen to contain marker text must not
	// be reported (audit H2's binary false positive).
	writeFile(t, dir, "docs/img.png", append([]byte{0x89, 'P', 'N', 'G', 0x00}, conflicted...))
	head := commitAll(t, dir, "docs: with markers")

	repo := newRepoHandle(t, dir)
	got, err := repo.ScanDocsBlobsAgainst(context.Background(), "", head)
	if err != nil {
		t.Fatalf("ScanDocsBlobsForMarkers: %v", err)
	}
	want := []string{"docs/conflicted.md", "docs/nested/also.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("conflicted paths = %v, want %v", got, want)
	}
}

func TestScanDocsBlobsForMarkersOnCleanDocs(t *testing.T) {
	dir, head := newRepoWithDocs(t)
	repo := newRepoHandle(t, dir)

	got, err := repo.ScanDocsBlobsAgainst(context.Background(), "", head)
	if err != nil {
		t.Fatalf("ScanDocsBlobsForMarkers: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("clean docs reported markers in %v", got)
	}
}

// TestScanDocsBlobsForMarkersSkipsSymlinks asserts a symlink's blob (its
// target path) is not scanned as text.
func TestScanDocsBlobsForMarkersSkipsSymlinks(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, dir, "docs/a.md", []byte("alpha\n"))
	if err := os.Symlink("a.md", filepath.Join(dir, "docs", "link.md")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	head := commitAll(t, dir, "docs: with symlink")

	repo := newRepoHandle(t, dir)
	got, err := repo.ScanDocsBlobsAgainst(context.Background(), "", head)
	if err != nil {
		t.Fatalf("ScanDocsBlobsForMarkers: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("symlink reported as conflicted: %v", got)
	}
}

// TestScanDocsBlobsForMarkersFailsOnOversizedBlob pins the merge contract: too-large
// files are errors naming the file, never a silent pass.
func TestScanDocsBlobsForMarkersFailsOnOversizedBlob(t *testing.T) {
	dir := newRepo(t)
	oversized := make([]byte, markers.MaxScanSize+1)
	for i := range oversized {
		oversized[i] = 'x'
	}
	writeFile(t, dir, "docs/huge.md", oversized)
	head := commitAll(t, dir, "docs: huge")

	repo := newRepoHandle(t, dir)
	_, err := repo.ScanDocsBlobsAgainst(context.Background(), "", head)
	if err == nil {
		t.Fatal("an oversized docs blob passed the gate")
	}
	if !errors.Is(err, markers.ErrTooLarge) {
		t.Fatalf("error = %v, want it to wrap markers.ErrTooLarge", err)
	}
	if !strings.Contains(err.Error(), "docs/huge.md") {
		t.Fatalf("error = %v, want it to name the offending file", err)
	}
}

func TestDocsCommitSubjects(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, dir, "docs/a.md", []byte("one\n"))
	base := commitAll(t, dir, "docs: first")
	writeFile(t, dir, "src/code.go", []byte("package main\n"))
	commitAll(t, dir, "feat: code only")
	writeFile(t, dir, "docs/a.md", []byte("two\n"))
	commitAll(t, dir, "docs: second")
	writeFile(t, dir, "docs/b.md", []byte("three\n"))
	tip := commitAll(t, dir, "docs: third")

	repo := newRepoHandle(t, dir)
	got, err := repo.DocsCommitSubjects(context.Background(), base, tip)
	if err != nil {
		t.Fatalf("DocsCommitSubjects: %v", err)
	}
	want := []string{"docs: second", "docs: third"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("subjects = %v, want %v (oldest first, docs-only)", got, want)
	}
}

func TestDocsCommitSubjectsWithoutABase(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, dir, "docs/a.md", []byte("one\n"))
	commitAll(t, dir, "docs: first")
	writeFile(t, dir, "docs/a.md", []byte("two\n"))
	tip := commitAll(t, dir, "docs: second")

	repo := newRepoHandle(t, dir)
	ctx := context.Background()

	got, err := repo.DocsCommitSubjects(ctx, "", tip)
	if err != nil {
		t.Fatalf("DocsCommitSubjects with an empty base: %v", err)
	}
	want := []string{"docs: first", "docs: second"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("subjects = %v, want %v", got, want)
	}

	// A base this repository no longer has (history rewritten upstream,
	// or the canonical base OID passed by mistake) degrades to the same
	// answer rather than failing: subjects are prose, never a gate.
	got, err = repo.DocsCommitSubjects(ctx, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef", tip)
	if err != nil {
		t.Fatalf("DocsCommitSubjects with an unknown base: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("subjects = %v, want %v", got, want)
	}
}

func TestDocsCommitSubjectsIsEmptyWhenNothingTouchedDocs(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, dir, "docs/a.md", []byte("one\n"))
	base := commitAll(t, dir, "docs: first")
	writeFile(t, dir, "src/code.go", []byte("package main\n"))
	tip := commitAll(t, dir, "feat: code only")

	repo := newRepoHandle(t, dir)
	got, err := repo.DocsCommitSubjects(context.Background(), base, tip)
	if err != nil {
		t.Fatalf("DocsCommitSubjects: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("subjects = %v, want none", got)
	}
}

func TestRepoIdentity(t *testing.T) {
	dir, _ := newRepoWithDocs(t)
	repo := newRepoHandle(t, dir)
	ctx := context.Background()

	// With no origin, the directory name identifies the repository.
	name, branch, err := repo.RepoIdentity(ctx)
	if err != nil {
		t.Fatalf("RepoIdentity: %v", err)
	}
	if name != "app" {
		t.Errorf("repoName = %q, want the directory name %q", name, "app")
	}
	if branch != "main" {
		t.Errorf("branch = %q, want main", branch)
	}

	gitRun(t, dir, "remote", "add", "origin", "git@github.com:acme/product-docs.git")
	name, _, err = repo.RepoIdentity(ctx)
	if err != nil {
		t.Fatalf("RepoIdentity with an origin: %v", err)
	}
	if name != "product-docs" {
		t.Errorf("repoName = %q, want product-docs", name)
	}

	gitRun(t, dir, "checkout", "--quiet", "-b", "feature/x")
	_, branch, err = repo.RepoIdentity(ctx)
	if err != nil {
		t.Fatalf("RepoIdentity on a feature branch: %v", err)
	}
	if branch != "feature/x" {
		t.Errorf("branch = %q, want feature/x", branch)
	}

	gitRun(t, dir, "checkout", "--quiet", "--detach", "HEAD")
	_, branch, err = repo.RepoIdentity(ctx)
	if err != nil {
		t.Fatalf("RepoIdentity when detached: %v", err)
	}
	if branch != "HEAD" {
		t.Errorf("branch = %q, want HEAD when detached", branch)
	}
}

func TestRepoIdentityURLForms(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"git@github.com:acme/docs.git", "docs"},
		{"https://github.com/acme/docs.git", "docs"},
		{"https://github.com/acme/docs", "docs"},
		{"https://github.com/acme/docs/", "docs"},
		{"/srv/git/team-docs.git", "team-docs"},
		{"file:///srv/git/team-docs", "team-docs"},
	}

	for _, test := range tests {
		t.Run(test.url, func(t *testing.T) {
			dir, _ := newRepoWithDocs(t)
			gitRun(t, dir, "remote", "add", "origin", test.url)

			name, _, err := newRepoHandle(t, dir).RepoIdentity(context.Background())
			if err != nil {
				t.Fatalf("RepoIdentity: %v", err)
			}
			if name != test.want {
				t.Fatalf("repoName = %q, want %q", name, test.want)
			}
		})
	}
}

func TestRepoIdentityOnUnbornHead(t *testing.T) {
	dir := newRepo(t)

	_, branch, err := newRepoHandle(t, dir).RepoIdentity(context.Background())
	if err != nil {
		t.Fatalf("RepoIdentity on an unborn HEAD: %v", err)
	}
	if branch != "main" {
		t.Fatalf("branch = %q, want main", branch)
	}
}

func TestWorktreeDocsTreeMatchesHeadWhenClean(t *testing.T) {
	dir, head := newRepoWithDocs(t)
	repo := newRepoHandle(t, dir)
	ctx := context.Background()

	committed, err := repo.DocsTreeOf(ctx, head)
	if err != nil {
		t.Fatalf("DocsTreeOf: %v", err)
	}
	worktree, err := repo.WorktreeDocsTree(ctx)
	if err != nil {
		t.Fatalf("WorktreeDocsTree: %v", err)
	}
	if worktree != committed {
		t.Fatalf("WorktreeDocsTree = %s, want the committed docs tree %s", worktree, committed)
	}
}

func TestWorktreeDocsTreeSeesUncommittedEdits(t *testing.T) {
	dir, head := newRepoWithDocs(t)
	repo := newRepoHandle(t, dir)
	ctx := context.Background()

	committed, err := repo.DocsTreeOf(ctx, head)
	if err != nil {
		t.Fatalf("DocsTreeOf: %v", err)
	}

	writeFile(t, dir, "docs/a.md", []byte("edited in the worktree\n"))
	writeFile(t, dir, "docs/new.md", []byte("untracked\n"))
	if err := os.Remove(filepath.Join(dir, "docs", "nested", "b.md")); err != nil {
		t.Fatalf("remove docs/nested/b.md: %v", err)
	}

	worktree, err := repo.WorktreeDocsTree(ctx)
	if err != nil {
		t.Fatalf("WorktreeDocsTree: %v", err)
	}
	if worktree == committed {
		t.Fatal("WorktreeDocsTree ignored uncommitted docs edits")
	}

	listing := gitRun(t, dir, "ls-tree", "-r", "--name-only", worktree)
	if !strings.Contains(listing, "new.md") {
		t.Errorf("worktree tree is missing the untracked file:\n%s", listing)
	}
	if strings.Contains(listing, "nested/b.md") {
		t.Errorf("worktree tree kept a deleted file:\n%s", listing)
	}
	if body := gitRun(t, dir, "cat-file", "blob", worktree+":a.md"); body != "edited in the worktree\n" {
		t.Errorf("worktree tree content = %q, want the edited body", body)
	}
}

// TestWorktreeDocsTreeLeavesTheRealIndexAlone is the worktree-inviolability
// regression: hashing the docs worktree must not disturb work the user has
// staged, which is the state a pre-push hook always runs in.
func TestWorktreeDocsTreeLeavesTheRealIndexAlone(t *testing.T) {
	dir, _ := newRepoWithDocs(t)
	repo := newRepoHandle(t, dir)

	writeFile(t, dir, "src/staged.go", []byte("package main\n"))
	gitRun(t, dir, "add", "--", "src/staged.go")
	writeFile(t, dir, "docs/a.md", []byte("unstaged docs edit\n"))

	before := gitRun(t, dir, "status", "--porcelain")
	beforeIndex := gitLine(t, dir, "rev-parse", "--verify", ":src/staged.go")

	if _, err := repo.WorktreeDocsTree(context.Background()); err != nil {
		t.Fatalf("WorktreeDocsTree: %v", err)
	}

	if after := gitRun(t, dir, "status", "--porcelain"); after != before {
		t.Fatalf("status changed:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if after := gitLine(t, dir, "rev-parse", "--verify", ":src/staged.go"); after != beforeIndex {
		t.Fatalf("staged blob changed from %s to %s", beforeIndex, after)
	}
}

// TestWorktreeDocsTreeIgnoresAHostileInheritedIndexFile is the appgit
// side of the C3 fix (docs/architecture.md "Git execution policy", gitx.Runner.env()).
//
// GIT_INDEX_FILE is scrubbed from the inherited environment like every
// other repository-identity variable, and the one caller that needs it —
// the application repository's runner, whose commit hooks read a partial
// commit's temporary index — asks for it explicitly with
// gitx.WithInheritedIndexFile. A Repo built the ordinary way therefore
// carries the ambient value on purpose, which is what makes
// WorktreeDocsTree's OWN explicit redirect load-bearing: the inherited
// value can be genuinely present and pointing somewhere real when
// WorktreeDocsTree runs, and its gitx.WithEnv setting is the only thing
// standing between that and a scratch-index computation that reads — or
// writes — the wrong file.
//
// The hostile value here is about as real as it gets: a second,
// unrelated repository's own live index. WorktreeDocsTree must still
// produce the worktree's true docs tree, and must never open that other
// repository's index at all — not to read it, and, more importantly,
// never to write into it.
func TestWorktreeDocsTreeIgnoresAHostileInheritedIndexFile(t *testing.T) {
	dir, _ := newRepoWithDocs(t)
	writeFile(t, dir, "docs/a.md", []byte("alpha changed under a hostile GIT_INDEX_FILE\n"))

	other, _ := newRepoWithDocs(t)
	otherIndexPath := filepath.Join(other, ".git", "index")
	otherIndexBefore, err := os.ReadFile(otherIndexPath)
	if err != nil {
		t.Fatalf("read the other repository's index: %v", err)
	}
	t.Setenv("GIT_INDEX_FILE", otherIndexPath)
	// Built AFTER the hostile value is in the environment, and asking for
	// it explicitly — the shape the workspace's own runner has, and the
	// only shape in which the ambient value reaches git at all.
	repo := appgit.New(dir, appgit.DefaultDocsDir, gitx.New(dir, gitx.WithInheritedIndexFile()))

	got, err := repo.WorktreeDocsTree(context.Background())
	if err != nil {
		t.Fatalf("WorktreeDocsTree: %v", err)
	}
	want := buildDocsTree(t, dir, map[string]blobSpec{
		"a.md":        regular("alpha changed under a hostile GIT_INDEX_FILE\n"),
		"nested/b.md": regular("beta\n"),
	})
	if got != want {
		listing := gitRun(t, dir, "ls-tree", "-r", got)
		t.Fatalf("WorktreeDocsTree = %s, want %s (the worktree's own docs, not anything derived "+
			"from the hostile index)\ngot tree:\n%s", got, want, listing)
	}

	// The hostile file itself was never opened for writing: a
	// scratch-index bug that lost the ordering race would corrupt an
	// entirely unrelated repository, not merely misread its own.
	otherIndexAfter, err := os.ReadFile(otherIndexPath)
	if err != nil {
		t.Fatalf("read the other repository's index after: %v", err)
	}
	if string(otherIndexBefore) != string(otherIndexAfter) {
		t.Fatal("the hostile repository's own index changed; WorktreeDocsTree wrote through the inherited GIT_INDEX_FILE")
	}
}

// TestWorktreeDocsTreeKeepsTrackedButIgnoredFiles is why the scratch
// index is seeded from HEAD: `git add` keeps tracked files regardless of
// ignore rules, and the base-advance rule must compare against what a
// commit would contain.
func TestWorktreeDocsTreeKeepsTrackedButIgnoredFiles(t *testing.T) {
	dir, _ := newRepoWithDocs(t)
	writeFile(t, dir, ".gitignore", []byte("docs/a.md\n"))
	head := commitAll(t, dir, "chore: ignore a tracked docs file")

	repo := newRepoHandle(t, dir)
	ctx := context.Background()

	committed, err := repo.DocsTreeOf(ctx, head)
	if err != nil {
		t.Fatalf("DocsTreeOf: %v", err)
	}
	worktree, err := repo.WorktreeDocsTree(ctx)
	if err != nil {
		t.Fatalf("WorktreeDocsTree: %v", err)
	}
	if worktree != committed {
		listing := gitRun(t, dir, "ls-tree", "-r", "--name-only", worktree)
		t.Fatalf("WorktreeDocsTree dropped a tracked-but-ignored file; tree holds:\n%s", listing)
	}
}

func TestWorktreeDocsTreeWithoutADocsDirectory(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, dir, "README.md", []byte("readme\n"))
	commitAll(t, dir, "chore: no docs")

	repo := newRepoHandle(t, dir)
	ctx := context.Background()

	empty, err := repo.EmptyTree(ctx)
	if err != nil {
		t.Fatalf("EmptyTree: %v", err)
	}
	got, err := repo.WorktreeDocsTree(ctx)
	if err != nil {
		t.Fatalf("WorktreeDocsTree: %v", err)
	}
	if got != empty {
		t.Fatalf("WorktreeDocsTree = %s, want the empty tree %s", got, empty)
	}
}

func TestWorktreeDocsTreeWithAnEmptyDocsDirectory(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, dir, "README.md", []byte("readme\n"))
	commitAll(t, dir, "chore: no docs")
	if err := os.MkdirAll(filepath.Join(dir, "docs"), 0755); err != nil {
		t.Fatalf("create docs dir: %v", err)
	}

	repo := newRepoHandle(t, dir)
	ctx := context.Background()

	empty, err := repo.EmptyTree(ctx)
	if err != nil {
		t.Fatalf("EmptyTree: %v", err)
	}
	got, err := repo.WorktreeDocsTree(ctx)
	if err != nil {
		t.Fatalf("WorktreeDocsTree: %v", err)
	}
	if got != empty {
		t.Fatalf("WorktreeDocsTree = %s, want the empty tree %s", got, empty)
	}
}

func TestWorktreeDocsTreeOnUnbornHead(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, dir, "docs/a.md", []byte("alpha\n"))

	repo := newRepoHandle(t, dir)
	ctx := context.Background()

	got, err := repo.WorktreeDocsTree(ctx)
	if err != nil {
		t.Fatalf("WorktreeDocsTree on an unborn HEAD: %v", err)
	}
	if body := gitRun(t, dir, "cat-file", "blob", got+":a.md"); body != "alpha\n" {
		t.Fatalf("worktree tree content = %q, want alpha", body)
	}
}

// TestWorktreeDocsTreePreservesSymlinksAndModes keeps the git-native
// content promise of the private-clone contract on the worktree side too.
func TestWorktreeDocsTreePreservesSymlinksAndModes(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, dir, "docs/target.md", []byte("target\n"))
	writeFile(t, dir, "docs/run.sh", []byte("echo\n"))
	if err := os.Chmod(filepath.Join(dir, "docs", "run.sh"), 0755); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	if err := os.Symlink("target.md", filepath.Join(dir, "docs", "link.md")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	commitAll(t, dir, "docs: symlink and executable")

	got, err := newRepoHandle(t, dir).WorktreeDocsTree(context.Background())
	if err != nil {
		t.Fatalf("WorktreeDocsTree: %v", err)
	}

	listing := gitRun(t, dir, "ls-tree", "-r", got)
	if !strings.Contains(listing, "120000 blob") {
		t.Errorf("symlink mode missing:\n%s", listing)
	}
	if !strings.Contains(listing, "100755 blob") {
		t.Errorf("executable mode missing:\n%s", listing)
	}
}
