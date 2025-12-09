package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestHasLocalDocsChanges(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		mutate   func(t *testing.T, repo string)
		expected bool
	}{
		{
			name:     "clean",
			mutate:   func(t *testing.T, repo string) {},
			expected: false,
		},
		{
			name: "unstaged_changes",
			mutate: func(t *testing.T, repo string) {
				path := filepath.Join(repo, "docs", "index.md")
				if err := os.WriteFile(path, []byte("unstaged"), 0644); err != nil {
					t.Fatalf("write file: %v", err)
				}
			},
			expected: true,
		},
		{
			name: "staged_changes",
			mutate: func(t *testing.T, repo string) {
				path := filepath.Join(repo, "docs", "index.md")
				if err := os.WriteFile(path, []byte("staged"), 0644); err != nil {
					t.Fatalf("write file: %v", err)
				}
				runGit(t, repo, "add", "docs/index.md")
			},
			expected: true,
		},
		{
			name: "untracked_files",
			mutate: func(t *testing.T, repo string) {
				if err := os.WriteFile(filepath.Join(repo, "docs", "new.md"), []byte("untracked"), 0644); err != nil {
					t.Fatalf("write file: %v", err)
				}
			},
			expected: true,
		},
	}

	client := NewClient()

	for _, tt := range cases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			repo := initDocsRepo(t)
			tt.mutate(t, repo)

			hasChanges, err := client.HasLocalDocsChanges(context.Background(), repo, filepath.Join(repo, "docs"))
			if err != nil {
				t.Fatalf("HasLocalDocsChanges returned error: %v", err)
			}
			if hasChanges != tt.expected {
				t.Fatalf("expected %v, got %v", tt.expected, hasChanges)
			}
		})
	}
}

func initDocsRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test User")

	docsDir := filepath.Join(repo, "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(docsDir, "index.md"), []byte("initial"), 0644); err != nil {
		t.Fatalf("write docs: %v", err)
	}
	runGit(t, repo, "add", "docs")
	runGit(t, repo, "commit", "-m", "initial commit")

	return repo
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := execCommand("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(output))
	}
}

// execCommand is a thin wrapper to allow testing without import cycles in this package.
var execCommand = func(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}
