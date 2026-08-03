package git

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
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

func TestDetector_DetectOperation(t *testing.T) {
	tests := []struct {
		name         string
		setup        func(t *testing.T, repo string)
		wantType     OperationType
		wantCommands []string
	}{
		{
			name:     "clear",
			wantType: OperationNone,
		},
		{
			name: "rebase merge",
			setup: func(t *testing.T, repo string) {
				makeDetectorGitPath(t, repo, "rebase-merge", true, "")
			},
			wantType:     OperationRebase,
			wantCommands: []string{"git status", "git rebase --continue", "git rebase --abort", "git rebase --quit"},
		},
		{
			name: "rebase apply",
			setup: func(t *testing.T, repo string) {
				makeDetectorGitPath(t, repo, "rebase-apply", true, "")
			},
			wantType: OperationRebase,
		},
		{
			name: "am",
			setup: func(t *testing.T, repo string) {
				makeDetectorGitPath(t, repo, "rebase-apply", true, "")
				makeDetectorGitPath(t, repo, "rebase-apply/applying", false, "")
			},
			wantType: OperationAM,
		},
		{
			name: "merge",
			setup: func(t *testing.T, repo string) {
				makeDetectorGitPath(t, repo, "MERGE_HEAD", false, "deadbeef\n")
			},
			wantType: OperationMerge,
		},
		{
			name: "cherry-pick",
			setup: func(t *testing.T, repo string) {
				makeDetectorGitPath(t, repo, "CHERRY_PICK_HEAD", false, "deadbeef\n")
			},
			wantType: OperationCherryPick,
		},
		{
			name: "revert",
			setup: func(t *testing.T, repo string) {
				makeDetectorGitPath(t, repo, "REVERT_HEAD", false, "deadbeef\n")
			},
			wantType: OperationRevert,
		},
		{
			name: "bisect",
			setup: func(t *testing.T, repo string) {
				makeDetectorGitPath(t, repo, "BISECT_LOG", false, "git bisect start\n")
			},
			wantType: OperationBisect,
		},
		{
			name: "cherry-pick sequencer",
			setup: func(t *testing.T, repo string) {
				makeDetectorGitPath(t, repo, "sequencer", true, "")
				makeDetectorGitPath(t, repo, "sequencer/todo", false, "pick deadbeef subject\n")
			},
			wantType: OperationCherryPick,
		},
		{
			name: "unknown sequencer",
			setup: func(t *testing.T, repo string) {
				makeDetectorGitPath(t, repo, "sequencer", true, "")
				makeDetectorGitPath(t, repo, "sequencer/todo", false, "exec false\n")
			},
			wantType: OperationSequencer,
		},
		{
			name: "multiple",
			setup: func(t *testing.T, repo string) {
				makeDetectorGitPath(t, repo, "MERGE_HEAD", false, "deadbeef\n")
				makeDetectorGitPath(t, repo, "REVERT_HEAD", false, "deadbeef\n")
			},
			wantType: OperationMultiple,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := initDetectorRepo(t)
			if test.setup != nil {
				test.setup(t, repo)
			}
			operation, err := NewDetector().DetectOperation(context.Background(), repo)
			if err != nil {
				t.Fatal(err)
			}
			if operation.Type != test.wantType {
				t.Fatalf("operation type = %q, want %q", operation.Type, test.wantType)
			}
			if test.wantType == OperationNone {
				if operation.Active || operation.Classification != OperationClear || operation.NextCommands == nil {
					t.Fatalf("clear operation = %+v", operation)
				}
				return
			}
			if !operation.Active || operation.Classification != OperationBlocked || operation.Reason == "" {
				t.Fatalf("blocked operation = %+v", operation)
			}
			if test.wantCommands != nil && !slices.Equal(operation.NextCommands, test.wantCommands) {
				t.Fatalf("commands = %v, want %v", operation.NextCommands, test.wantCommands)
			}
		})
	}
}

func TestDetector_RequireNoOperationReturnsTypedError(t *testing.T) {
	repo := initDetectorRepo(t)
	makeDetectorGitPath(t, repo, "MERGE_HEAD", false, "deadbeef\n")

	err := NewDetector().RequireNoOperation(context.Background(), repo)
	var blocked *GitOperationBlockedError
	if !errors.As(err, &blocked) {
		t.Fatalf("RequireNoOperation() error = %v, want GitOperationBlockedError", err)
	}
	if blocked.Operation.Type != OperationMerge {
		t.Fatalf("blocked operation = %+v", blocked.Operation)
	}
}

func TestDetector_DetectOperationUsesLinkedWorktreeGitDir(t *testing.T) {
	mainRepo := initDetectorRepo(t)
	runDetectorGit(t, mainRepo, "config", "user.email", "test@example.com")
	runDetectorGit(t, mainRepo, "config", "user.name", "Test User")
	runDetectorGit(t, mainRepo, "commit", "--allow-empty", "-m", "base")
	linked := filepath.Join(t.TempDir(), "linked")
	runDetectorGit(t, mainRepo, "worktree", "add", "-b", "linked", linked)
	makeDetectorGitPath(t, linked, "rebase-merge", true, "")

	linkedOperation, err := NewDetector().DetectOperation(context.Background(), linked)
	if err != nil {
		t.Fatal(err)
	}
	if linkedOperation.Type != OperationRebase {
		t.Fatalf("linked operation = %+v", linkedOperation)
	}
	mainOperation, err := NewDetector().DetectOperation(context.Background(), mainRepo)
	if err != nil {
		t.Fatal(err)
	}
	if mainOperation.Active {
		t.Fatalf("main worktree operation = %+v, want clear", mainOperation)
	}
	gitFile, err := os.Stat(filepath.Join(linked, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	if !gitFile.Mode().IsRegular() {
		t.Fatalf("linked .git mode = %v, want regular file", gitFile.Mode())
	}
}

func initDetectorRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runDetectorGit(t, repo, "init", "--initial-branch=main")
	return repo
}

func makeDetectorGitPath(t *testing.T, repo, name string, directory bool, content string) {
	t.Helper()
	path := runDetectorGit(t, repo, "rev-parse", "--git-path", name)
	if !filepath.IsAbs(path) {
		path = filepath.Join(repo, path)
	}
	if directory {
		if err := os.MkdirAll(path, 0755); err != nil {
			t.Fatal(err)
		}
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func runDetectorGit(t *testing.T, repo string, args ...string) string {
	t.Helper()
	commandArgs := append([]string{"-C", repo}, args...)
	out, err := exec.Command("git", commandArgs...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}
