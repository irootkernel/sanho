package git

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// MergeResult represents the result of a 3-way merge operation.
type MergeResult struct {
	// Content is the merged file content.
	Content []byte
	// HasConflicts indicates whether the merge resulted in conflicts.
	HasConflicts bool
}

// MergeFile performs a 3-way merge using git merge-file.
// It merges localContent with remoteContent using baseContent as the common ancestor.
// The merge is performed in a temporary directory.
// Returns the merged content and whether conflicts occurred.
func (c *Client) MergeFile(ctx context.Context, baseContent, localContent, remoteContent []byte) (MergeResult, error) {
	// Create temp directory for merge files
	tempDir, err := os.MkdirTemp("", "kkachi-merge-*")
	if err != nil {
		return MergeResult{}, fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	// Write contents to temp files
	basePath := filepath.Join(tempDir, "base")
	localPath := filepath.Join(tempDir, "local")
	remotePath := filepath.Join(tempDir, "remote")

	if err := os.WriteFile(basePath, baseContent, 0644); err != nil {
		return MergeResult{}, fmt.Errorf("failed to write base file: %w", err)
	}
	if err := os.WriteFile(localPath, localContent, 0644); err != nil {
		return MergeResult{}, fmt.Errorf("failed to write local file: %w", err)
	}
	if err := os.WriteFile(remotePath, remoteContent, 0644); err != nil {
		return MergeResult{}, fmt.Errorf("failed to write remote file: %w", err)
	}

	// Run git merge-file
	// git merge-file modifies the first file (local) in place
	// Exit code: 0 = clean merge, 1 = conflicts, >1 = error
	cmd := exec.CommandContext(ctx, "git", "merge-file", "-p", localPath, basePath, remotePath)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	hasConflicts := false

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() == 1 || exitErr.ExitCode() < 0 {
				// Exit code 1 or negative (conflicts count) means conflicts occurred
				hasConflicts = true
			} else if exitErr.ExitCode() > 1 {
				// Exit code > 1 means actual error
				return MergeResult{}, fmt.Errorf("git merge-file failed: %w, stderr: %s", err, stderr.String())
			}
		} else {
			return MergeResult{}, fmt.Errorf("git merge-file failed: %w", err)
		}
	}

	return MergeResult{
		Content:      stdout.Bytes(),
		HasConflicts: hasConflicts,
	}, nil
}

// MergeFileFromPaths performs a 3-way merge using file paths.
// This is useful when working with existing files on disk.
func (c *Client) MergeFileFromPaths(ctx context.Context, basePath, localPath, remotePath string) (MergeResult, error) {
	// Read file contents
	baseContent, err := os.ReadFile(basePath)
	if err != nil {
		return MergeResult{}, fmt.Errorf("failed to read base file: %w", err)
	}
	localContent, err := os.ReadFile(localPath)
	if err != nil {
		return MergeResult{}, fmt.Errorf("failed to read local file: %w", err)
	}
	remoteContent, err := os.ReadFile(remotePath)
	if err != nil {
		return MergeResult{}, fmt.Errorf("failed to read remote file: %w", err)
	}

	return c.MergeFile(ctx, baseContent, localContent, remoteContent)
}
