package git_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SeventeenthEarth/kkachi/internal/config"
	"github.com/SeventeenthEarth/kkachi/internal/domain/docs"
	"github.com/SeventeenthEarth/kkachi/internal/infra/git"
	"github.com/SeventeenthEarth/kkachi/internal/infra/state"
)

func TestGitDocsRepositoryCompareProjectCommits(t *testing.T) {
	tempDir := t.TempDir()
	remotePath := filepath.Join(tempDir, "remote.git")
	seedPath := filepath.Join(tempDir, "seed")
	clonePath := filepath.Join(tempDir, "clone")

	runGit(t, "", "init", "--bare", remotePath)
	runGit(t, "", "init", seedPath)
	runGit(t, seedPath, "config", "user.email", "test@example.com")
	runGit(t, seedPath, "config", "user.name", "Test User")
	writeAndCommit(t, seedPath, "base.txt", "base", "base")
	runGit(t, seedPath, "branch", "-M", "main")
	runGit(t, seedPath, "remote", "add", "origin", remotePath)
	base := revParse(t, seedPath, "HEAD")

	runGit(t, seedPath, "switch", "-c", "side", base)
	writeAndCommit(t, seedPath, "side.txt", "side", "side")
	side := revParse(t, seedPath, "HEAD")
	runGit(t, seedPath, "push", "origin", "side")

	runGit(t, seedPath, "switch", "main")
	writeAndCommit(t, seedPath, "main-1.txt", "main-1", "main 1")
	mainOne := revParse(t, seedPath, "HEAD")
	writeAndCommit(t, seedPath, "main-2.txt", "main-2", "main 2")
	head := revParse(t, seedPath, "HEAD")
	runGit(t, seedPath, "push", "-u", "origin", "main")
	runGit(t, remotePath, "symbolic-ref", "HEAD", "refs/heads/main")
	runGit(t, "", "clone", remotePath, clonePath)

	stateRepo, err := state.NewFileStateRepository(filepath.Join(tempDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := stateRepo.AddDocsRepo(config.DocsRepoConfig{ID: "docs", Path: clonePath}); err != nil {
		t.Fatal(err)
	}
	if err := stateRepo.AddProject("project", "docs"); err != nil {
		t.Fatal(err)
	}

	repo := git.NewGitDocsRepository(git.NewClient(), stateRepo, git.NewRepoCoordinator())
	result, err := repo.CompareProjectCommits(
		context.Background(),
		"project",
		docs.CommitHash(mainOne),
		[]docs.CommitHash{
			docs.CommitHash(head),
			docs.CommitHash(base),
			docs.CommitHash(side),
			"0000000000000000000000000000000000000000",
			docs.CommitHash(head),
		},
	)
	if err != nil {
		t.Fatalf("CompareProjectCommits() error = %v", err)
	}

	assertRelation(t, result.ReferenceToHead, docs.CommitRelationBehind, 0, 1)
	assertComparison(t, result.WorkspaceComparisons[docs.CommitHash(head)],
		docs.CommitRelationAhead, 1, 0,
		docs.CommitRelationSame, 0, 0,
	)
	assertComparison(t, result.WorkspaceComparisons[docs.CommitHash(base)],
		docs.CommitRelationBehind, 0, 1,
		docs.CommitRelationBehind, 0, 2,
	)
	assertComparison(t, result.WorkspaceComparisons[docs.CommitHash(side)],
		docs.CommitRelationDiverged, 1, 1,
		docs.CommitRelationDiverged, 1, 2,
	)
	assertComparison(t, result.WorkspaceComparisons["0000000000000000000000000000000000000000"],
		docs.CommitRelationUnknown, 0, 0,
		docs.CommitRelationUnknown, 0, 0,
	)
}

func TestGitDocsRepositoryCompareProjectCommitsUnknownReference(t *testing.T) {
	tempDir := t.TempDir()
	remotePath := filepath.Join(tempDir, "remote.git")
	seedPath := filepath.Join(tempDir, "seed")
	clonePath := filepath.Join(tempDir, "clone")
	runGit(t, "", "init", "--bare", remotePath)
	runGit(t, "", "init", seedPath)
	runGit(t, seedPath, "config", "user.email", "test@example.com")
	runGit(t, seedPath, "config", "user.name", "Test User")
	writeAndCommit(t, seedPath, "base.txt", "base", "base")
	runGit(t, seedPath, "branch", "-M", "main")
	runGit(t, seedPath, "remote", "add", "origin", remotePath)
	runGit(t, seedPath, "push", "-u", "origin", "main")
	runGit(t, remotePath, "symbolic-ref", "HEAD", "refs/heads/main")
	runGit(t, "", "clone", remotePath, clonePath)

	stateRepo, err := state.NewFileStateRepository(filepath.Join(tempDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := stateRepo.AddDocsRepo(config.DocsRepoConfig{ID: "docs", Path: clonePath}); err != nil {
		t.Fatal(err)
	}
	if err := stateRepo.AddProject("project", "docs"); err != nil {
		t.Fatal(err)
	}

	repo := git.NewGitDocsRepository(git.NewClient(), stateRepo, git.NewRepoCoordinator())
	_, err = repo.CompareProjectCommits(
		context.Background(),
		"project",
		"0000000000000000000000000000000000000000",
		nil,
	)
	if !errors.Is(err, docs.ErrUnknownDocsCommit) {
		t.Fatalf("CompareProjectCommits() error = %v, want unknown docs commit", err)
	}
}

func runGit(t *testing.T, repoPath string, args ...string) {
	t.Helper()
	if repoPath != "" {
		args = append([]string{"-C", repoPath}, args...)
	}
	cmd := exec.Command("git", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
}

func writeAndCommit(t *testing.T, repoPath, name, content, message string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repoPath, name), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repoPath, "add", name)
	runGit(t, repoPath, "commit", "-m", message)
}

func revParse(t *testing.T, repoPath, revision string) string {
	t.Helper()
	output, err := exec.Command("git", "-C", repoPath, "rev-parse", revision).Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(output))
}

func assertComparison(
	t *testing.T,
	comparison docs.CommitComparison,
	referenceStatus docs.CommitRelationStatus,
	referenceAhead int,
	referenceBehind int,
	headStatus docs.CommitRelationStatus,
	headAhead int,
	headBehind int,
) {
	t.Helper()
	assertRelation(t, comparison.RelativeToReference, referenceStatus, referenceAhead, referenceBehind)
	assertRelation(t, comparison.RelativeToHead, headStatus, headAhead, headBehind)
}

func assertRelation(
	t *testing.T,
	relation docs.CommitRelation,
	status docs.CommitRelationStatus,
	ahead int,
	behind int,
) {
	t.Helper()
	if relation.Status != status || relation.Ahead != ahead || relation.Behind != behind {
		t.Fatalf("relation = %#v, want status=%s ahead=%d behind=%d", relation, status, ahead, behind)
	}
}
