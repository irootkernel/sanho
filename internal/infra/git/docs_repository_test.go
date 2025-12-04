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

func TestGitDocsRepository_GetHead(t *testing.T) {
	// Setup temp dir
	tempDir, err := os.MkdirTemp("", "kkachi-test-repo-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	repoPath := filepath.Join(tempDir, "repo")
	if err := os.Mkdir(repoPath, 0755); err != nil {
		t.Fatal(err)
	}

	// Init git repo
	cmd := exec.Command("git", "init", repoPath)
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}

	// Config user
	cmd = exec.Command("git", "-C", repoPath, "config", "user.email", "test@example.com")
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to set git user.email: %v", err)
	}
	cmd = exec.Command("git", "-C", repoPath, "config", "user.name", "Test User")
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to set git user.name: %v", err)
	}

	// Create a commit
	testFile := filepath.Join(repoPath, "test.txt")
	if err := os.WriteFile(testFile, []byte("hello"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}
	cmd = exec.Command("git", "-C", repoPath, "add", ".")
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to add files: %v", err)
	}
	cmd = exec.Command("git", "-C", repoPath, "commit", "-m", "Initial commit")
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to commit: %v", err)
	}

	// Get HEAD hash
	cmd = exec.Command("git", "-C", repoPath, "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	expectedHead := strings.TrimSpace(string(out))

	// Setup State
	statePath := filepath.Join(tempDir, "state.json")
	stateRepo, _ := state.NewFileStateRepository(statePath)
	if err := stateRepo.AddDocsRepo(config.DocsRepoConfig{ID: "repo1", Path: repoPath}); err != nil {
		t.Fatalf("failed to add docs repo: %v", err)
	}
	if err := stateRepo.AddProject("proj1", "repo1"); err != nil {
		t.Fatalf("failed to add project: %v", err)
	}

	// Setup Repository
	gitClient := git.NewClient()
	repo := git.NewGitDocsRepository(gitClient, stateRepo)

	// Test GetHead
	head, err := repo.GetHead(context.Background(), "proj1")
	if err != nil {
		t.Fatalf("GetHead failed: %v", err)
	}

	if string(head) != expectedHead {
		t.Errorf("Expected head %s, got %s", expectedHead, head)
	}
}

func TestGitDocsRepository_GetHead_UnknownProject(t *testing.T) {
	// Setup temp dir
	tempDir, err := os.MkdirTemp("", "kkachi-test-repo-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	statePath := filepath.Join(tempDir, "state.json")
	stateRepo, _ := state.NewFileStateRepository(statePath)
	gitClient := git.NewClient()
	repo := git.NewGitDocsRepository(gitClient, stateRepo)

	_, err = repo.GetHead(context.Background(), "unknown")
	if err == nil {
		t.Error("Expected error for unknown project, got nil")
	}
	if !errors.Is(err, docs.ErrUnknownProject) {
		t.Errorf("Expected unknown_project error, got %v", err)
	}
}
