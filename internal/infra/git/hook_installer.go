package git

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// HookInstaller provides methods for installing Git hooks.
type HookInstaller struct{}

// NewHookInstaller creates a new HookInstaller.
func NewHookInstaller() *HookInstaller {
	return &HookInstaller{}
}

// InstallHook installs a hook by adding a line to the hook script.
// If the hook file doesn't exist, it creates a new one.
// If the hook file exists, it appends the line if not already present.
// Handles both regular repos and worktrees/submodules where .git is a file.
func (h *HookInstaller) InstallHook(ctx context.Context, repoPath, hookName, line string) error {
	// Resolve actual hooks directory - handles worktrees/submodules
	hooksDir, err := h.resolveHooksDir(ctx, repoPath)
	if err != nil {
		return fmt.Errorf("failed to resolve hooks directory: %w", err)
	}

	// Ensure hooks directory exists
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		return fmt.Errorf("failed to create hooks directory: %w", err)
	}

	hookPath := filepath.Join(hooksDir, hookName)

	// Read existing content
	existingContent := ""
	if data, err := os.ReadFile(hookPath); err == nil {
		existingContent = string(data)
		// Check if line already exists
		if strings.Contains(existingContent, line) {
			// Ensure file is executable even if line already exists
			if err := os.Chmod(hookPath, 0755); err != nil {
				return fmt.Errorf("failed to chmod hook file: %w", err)
			}
			return nil // Already installed
		}
	}

	// Prepare new content
	var content string
	if existingContent == "" {
		content = "#!/bin/sh\n" + line + "\n"
	} else {
		// Append to existing content
		if !strings.HasSuffix(existingContent, "\n") {
			existingContent += "\n"
		}
		content = existingContent + line + "\n"
	}

	// Write hook file
	if err := os.WriteFile(hookPath, []byte(content), 0755); err != nil {
		return fmt.Errorf("failed to write hook file: %w", err)
	}

	// Explicitly chmod to ensure executability (WriteFile doesn't change mode of existing files)
	if err := os.Chmod(hookPath, 0755); err != nil {
		return fmt.Errorf("failed to chmod hook file: %w", err)
	}

	return nil
}

// resolveHooksDir resolves the actual git hooks directory.
// For regular repos: <repo>/.git/hooks
// For worktrees/submodules: parses .git file to find actual gitdir
func (h *HookInstaller) resolveHooksDir(ctx context.Context, repoPath string) (string, error) {
	gitPath := filepath.Join(repoPath, ".git")

	info, err := os.Stat(gitPath)
	if err != nil {
		// If .git is not found at repoPath (e.g., when running from a subdirectory),
		// fall back to Git's own resolution so subdirectories and worktrees still work.
		return h.resolveHooksDirViaGit(ctx, repoPath)
	}

	if info.IsDir() {
		// Regular repository
		return filepath.Join(gitPath, "hooks"), nil
	}

	// .git is a file (worktree or submodule) - parse it to find actual gitdir
	file, err := os.Open(gitPath)
	if err != nil {
		return "", fmt.Errorf("failed to open .git file: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "gitdir:") {
			gitdir := strings.TrimSpace(strings.TrimPrefix(line, "gitdir:"))
			// Handle relative paths
			if !filepath.IsAbs(gitdir) {
				gitdir = filepath.Join(repoPath, gitdir)
			}
			return filepath.Join(gitdir, "hooks"), nil
		}
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("failed to read .git file: %w", err)
	}

	// Fallback to git command
	return h.resolveHooksDirViaGit(ctx, repoPath)
}

// resolveHooksDirViaGit uses git rev-parse to find hooks directory
func (h *HookInstaller) resolveHooksDirViaGit(ctx context.Context, repoPath string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "rev-parse", "--git-path", "hooks")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse --git-path hooks failed: %w", err)
	}

	hooksPath := strings.TrimSpace(string(out))
	// Handle relative paths
	if !filepath.IsAbs(hooksPath) {
		hooksPath = filepath.Join(repoPath, hooksPath)
	}
	return hooksPath, nil
}

// InstallAllHooks installs all kkachi Git hooks.
func (h *HookInstaller) InstallAllHooks(ctx context.Context, repoPath string) error {
	hooks := map[string]string{
		"pre-commit":    "kkachi hook pre-commit",
		"post-checkout": "kkachi hook post-checkout",
		"post-merge":    "kkachi hook post-merge",
		"post-rewrite":  "kkachi hook post-rewrite \"$@\"",
		"pre-push":      "kkachi hook pre-push",
		"commit-msg":    "kkachi hook commit-msg \"$1\"",
	}

	for hookName, line := range hooks {
		if err := h.InstallHook(ctx, repoPath, hookName, line); err != nil {
			return fmt.Errorf("failed to install %s hook: %w", hookName, err)
		}
	}

	return nil
}
