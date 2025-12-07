package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestDetector_HasGitDir(t *testing.T) {
	detector := NewDetector()

	tests := []struct {
		name     string
		setup    func(t *testing.T) string
		expected bool
	}{
		{
			name: "inside git work tree (root)",
			setup: func(t *testing.T) string {
				t.Helper()
				repoDir := t.TempDir()
				// Initialize a real git repository so HasGitDir can use git rev-parse semantics.
				if err := exec.Command("git", "init", repoDir).Run(); err != nil {
					t.Skipf("git not available, skipping test: %v", err)
				}
				return repoDir
			},
			expected: true,
		},
		{
			name: "inside git work tree (subdirectory)",
			setup: func(t *testing.T) string {
				t.Helper()
				repoDir := t.TempDir()
				if err := exec.Command("git", "init", repoDir).Run(); err != nil {
					t.Skipf("git not available, skipping test: %v", err)
				}
				subDir := filepath.Join(repoDir, "subdir")
				if err := os.Mkdir(subDir, 0755); err != nil {
					t.Fatal(err)
				}
				return subDir
			},
			expected: true,
		},
		{
			name: "directory without git repo",
			setup: func(t *testing.T) string {
				t.Helper()
				return t.TempDir()
			},
			expected: false,
		},
		{
			name: "non-existent directory",
			setup: func(t *testing.T) string {
				t.Helper()
				return "/non/existent/path"
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := tt.setup(t)
			got := detector.HasGitDir(path)
			if got != tt.expected {
				t.Errorf("HasGitDir() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestDetector_GetUserEmail(t *testing.T) {
	detector := NewDetector()
	ctx := context.Background()

	// Test with a non-git directory (should return empty string, no error)
	dir := t.TempDir()
	email, err := detector.GetUserEmail(ctx, dir)
	if err != nil {
		t.Errorf("GetUserEmail() error = %v", err)
	}
	if email != "" {
		t.Logf("GetUserEmail() returned email: %s (may be global config)", email)
	}
}

func TestDetector_GetRemoteOriginURL(t *testing.T) {
	detector := NewDetector()
	ctx := context.Background()

	// Test with a non-git directory (should return empty string, no error)
	dir := t.TempDir()
	url, err := detector.GetRemoteOriginURL(ctx, dir)
	if err != nil {
		t.Errorf("GetRemoteOriginURL() error = %v", err)
	}
	if url != "" {
		t.Errorf("GetRemoteOriginURL() = %q, want empty string for non-git dir", url)
	}
}
