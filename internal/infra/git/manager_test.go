package git_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/irootkernel/sanho/internal/config"
	"github.com/irootkernel/sanho/internal/infra/git"
)

func TestDocsRepoManager_Sync(t *testing.T) {
	// Setup origin repo
	tempDir, err := os.MkdirTemp("", "sanho-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	originPath := filepath.Join(tempDir, "origin")
	if err := os.Mkdir(originPath, 0755); err != nil {
		t.Fatal(err)
	}

	// Initialize origin repo with a commit
	runCmd(t, "git", "init", originPath)
	runCmd(t, "git", "-C", originPath, "config", "user.email", "test@example.com")
	runCmd(t, "git", "-C", originPath, "config", "user.name", "Test User")
	runCmd(t, "git", "-C", originPath, "commit", "--allow-empty", "-m", "Initial commit")

	// Setup target path
	targetPath := filepath.Join(tempDir, "target")

	// Config
	cfg := config.DocsRepoConfig{
		ID:      "test-repo",
		Path:    targetPath,
		RepoURL: originPath,
	}

	client := git.NewClient()
	manager := git.NewDocsRepoManager(client, git.NewRepoCoordinator())

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

func runCmd(t *testing.T, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("command %v failed: %v\n%s", args, err, out)
	}
}
