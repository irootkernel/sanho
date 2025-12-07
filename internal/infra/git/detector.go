package git

import (
	"context"
	"os/exec"
	"strings"
)

// Detector provides methods for detecting Git repository properties.
type Detector struct{}

// NewDetector creates a new Git detector.
func NewDetector() *Detector {
	return &Detector{}
}

// HasGitDir checks if the given path is inside a Git repository.
func (d *Detector) HasGitDir(path string) bool {
	// Use Git itself to determine whether the path is inside a work tree.
	// This correctly handles subdirectories, worktrees, and non-standard layouts.
	cmd := exec.Command("git", "-C", path, "rev-parse", "--is-inside-work-tree")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}

// GetUserEmail retrieves the git user.email configuration for the repository.
func (d *Detector) GetUserEmail(ctx context.Context, path string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", path, "config", "user.email")
	out, err := cmd.Output()
	if err != nil {
		// Empty result or error means no config
		return "", nil
	}
	return strings.TrimSpace(string(out)), nil
}

// GetRemoteOriginURL retrieves the origin remote URL for the repository.
func (d *Detector) GetRemoteOriginURL(ctx context.Context, path string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", path, "remote", "get-url", "origin")
	out, err := cmd.Output()
	if err != nil {
		return "", nil
	}
	return strings.TrimSpace(string(out)), nil
}
