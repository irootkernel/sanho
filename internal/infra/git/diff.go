package git

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// HasDocsChangeStaged checks if there are staged changes in the specified docs directory.
// This is used by commit-msg hook.
func (c *Client) HasDocsChangeStaged(ctx context.Context, repoPath, docsDir string) (bool, error) {
	// git diff --cached --quiet <docsDir>
	// Exit 0 = no changes, Exit 1 = changes exist
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "diff", "--cached", "--quiet", docsDir)
	err := cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			// Non-zero exit means there are changes
			if exitErr.ExitCode() == 1 {
				return true, nil
			}
		}
		return false, fmt.Errorf("git diff --cached failed: %w", err)
	}
	return false, nil
}

// HasDocsChangeForCommit checks if the current staged changes include docs directory modifications.
// This is used by pre-commit hook.
func (c *Client) HasDocsChangeForCommit(ctx context.Context, repoPath, docsDir string) (bool, error) {
	// Use diff-index to compare HEAD with staged changes for docs dir
	// git diff-index --cached --quiet HEAD -- <docsDir>
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "diff-index", "--cached", "--quiet", "HEAD", "--", docsDir)
	err := cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			// Non-zero exit means there are changes
			if exitErr.ExitCode() == 1 {
				return true, nil
			}
		}
		// Check if this is a new repo with no commits
		if c.isInitialCommit(ctx, repoPath) {
			// For initial commit, check if docs dir exists in index
			return c.hasDocsStagedForInitialCommit(ctx, repoPath, docsDir)
		}
		return false, fmt.Errorf("git diff-index failed: %w", err)
	}
	return false, nil
}

// isInitialCommit checks if this is the initial commit (no HEAD yet).
func (c *Client) isInitialCommit(ctx context.Context, repoPath string) bool {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "rev-parse", "--verify", "HEAD")
	return cmd.Run() != nil
}

// hasDocsStagedForInitialCommit checks if docs are staged for the initial commit.
func (c *Client) hasDocsStagedForInitialCommit(ctx context.Context, repoPath, docsDir string) (bool, error) {
	// git diff --cached --name-only
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "diff", "--cached", "--name-only")
	output, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("git diff --cached --name-only failed: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	prefix := docsDir + "/"
	if docsDir == "" {
		prefix = "docs/"
	}

	for _, line := range lines {
		if strings.HasPrefix(line, prefix) {
			return true, nil
		}
	}
	return false, nil
}

// HasLocalDocsChanges checks if there are any uncommitted changes (staged, unstaged, or untracked) in the docs directory.
// This is used by kkachi pull to detect local modifications and prevent data loss.
func (c *Client) HasLocalDocsChanges(ctx context.Context, repoPath, docsDir string) (bool, error) {
	// Check for unstaged changes: git diff --quiet <docsDir>
	cmdUnstaged := exec.CommandContext(ctx, "git", "-C", repoPath, "diff", "--quiet", "--", docsDir)
	if err := cmdUnstaged.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return true, nil // has unstaged changes
		}
		return false, fmt.Errorf("git diff failed: %w", err)
	}

	// Check for staged changes: git diff --cached --quiet <docsDir>
	cmdStaged := exec.CommandContext(ctx, "git", "-C", repoPath, "diff", "--cached", "--quiet", "--", docsDir)
	if err := cmdStaged.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return true, nil // has staged changes
		}
		return false, fmt.Errorf("git diff --cached failed: %w", err)
	}

	// Check for untracked files: git status --porcelain -- <docsDir>
	// This catches new files that haven't been added to git yet.
	cmdUntracked := exec.CommandContext(ctx, "git", "-C", repoPath, "status", "--porcelain", "--", docsDir)
	output, err := cmdUntracked.Output()
	if err != nil {
		return false, fmt.Errorf("git status --porcelain failed: %w", err)
	}
	if len(bytes.TrimSpace(output)) > 0 {
		return true, nil
	}

	return false, nil
}
