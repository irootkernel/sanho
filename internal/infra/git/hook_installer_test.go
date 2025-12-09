package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runGitOrSkip runs a git command and skips the test if git is not available.
func runGitOrSkip(t *testing.T, args ...string) {
	t.Helper()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not available, skipping test: %v", err)
	}

	cmd := exec.Command("git", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
}

func TestHookInstaller_InstallHook_FromRootAndSubdir(t *testing.T) {
	// Initialize a real git repository
	repoDir := t.TempDir()
	runGitOrSkip(t, "init", repoDir)

	// Create a subdirectory inside the repo
	subDir := filepath.Join(repoDir, "subdir")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}

	ctx := context.Background()
	installer := NewHookInstaller()

	const hookName = "pre-commit"
	const hookLine = "kkachi hook pre-commit"

	// 1) Install hook from repo root
	if err := installer.InstallHook(ctx, repoDir, hookName, hookLine); err != nil {
		t.Fatalf("InstallHook from repo root failed: %v", err)
	}

	hookPath := filepath.Join(repoDir, ".git", "hooks", hookName)
	data, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("failed to read hook file from root install: %v", err)
	}
	if !strings.Contains(string(data), hookLine) {
		t.Fatalf("hook file does not contain expected line %q, got:\n%s", hookLine, string(data))
	}

	// 2) Install the same hook from a subdirectory.
	// This exercises the fallback path where <subdir>/.git does not exist
	// and resolveHooksDirViaGit is used.
	if err := installer.InstallHook(ctx, subDir, hookName, hookLine); err != nil {
		t.Fatalf("InstallHook from subdir failed: %v", err)
	}

	// Hook should still be in the same .git/hooks directory at repo root,
	// and remain executable / containing the line (idempotent behavior).
	data, err = os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("failed to read hook file after subdir install: %v", err)
	}
	if !strings.Contains(string(data), hookLine) {
		t.Fatalf("hook file lost expected line %q after subdir install, got:\n%s", hookLine, string(data))
	}
}
