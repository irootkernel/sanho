package git_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/SeventeenthEarth/kkachi/internal/config"
	"github.com/SeventeenthEarth/kkachi/internal/infra/git"
)

func TestDocsRepoManager_Sync(t *testing.T) {
	// Setup origin repo
	tempDir, err := os.MkdirTemp("", "kkachi-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	originPath := filepath.Join(tempDir, "origin")
	if err := os.Mkdir(originPath, 0755); err != nil {
		t.Fatal(err)
	}

	// Initialize bare repo
	cmd := exec.Command("git", "init", "--bare", originPath)
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}

	// Setup target path
	targetPath := filepath.Join(tempDir, "target")

	// Config
	cfg := config.DocsRepoConfig{
		ID:      "test-repo",
		Path:    targetPath,
		RepoURL: originPath,
	}

	client := git.NewClient()
	manager := git.NewDocsRepoManager(client)

	// Test 1: Clone
	if err := manager.Sync(context.Background(), []config.DocsRepoConfig{cfg}); err != nil {
		t.Fatalf("First Sync (Clone) failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(targetPath, ".git")); os.IsNotExist(err) {
		t.Errorf("Target .git directory not found after clone")
	}

	// Test 2: Fetch
	// We can't easily verify fetch without new commits, but we can verify it doesn't error
	if err := manager.Sync(context.Background(), []config.DocsRepoConfig{cfg}); err != nil {
		t.Fatalf("Second Sync (Fetch) failed: %v", err)
	}
}
