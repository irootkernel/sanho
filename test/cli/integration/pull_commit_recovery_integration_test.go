package integration

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIPullCommitContinueAndAbortAfterValidRewritePreserveGitState(t *testing.T) {
	cliBinary := getCliBinary(t)
	for _, flag := range []string{"--continue", "--abort"} {
		t.Run(strings.TrimPrefix(flag, "--"), func(t *testing.T) {
			repo := t.TempDir()
			runRecoveryGit(t, repo, "init", "--initial-branch=main")
			runRecoveryGit(t, repo, "config", "user.email", "test@example.com")
			runRecoveryGit(t, repo, "config", "user.name", "Test User")
			writeRecoveryFile(t, repo, "base.txt", "sync\n")
			runRecoveryGit(t, repo, "add", "base.txt")
			runRecoveryGit(t, repo, "commit", "--no-verify", "-m", "sync")
			syncCommit := runRecoveryGit(t, repo, "rev-parse", "HEAD")
			writeRecoveryFile(t, repo, "feature.txt", "prepared\n")
			runRecoveryGit(t, repo, "add", "feature.txt")
			runRecoveryGit(t, repo, "commit", "--no-verify", "-m", "prepared")
			preparedHead := runRecoveryGit(t, repo, "rev-parse", "HEAD")
			writeRecoveryFile(t, repo, "feature.txt", "amended\n")
			runRecoveryGit(t, repo, "add", "feature.txt")
			runRecoveryGit(t, repo, "commit", "--amend", "--no-verify", "-m", "amended")
			writeRecoveryFile(t, repo, "staged.txt", "staged\n")
			runRecoveryGit(t, repo, "add", "staged.txt")
			writeRecoveryFile(t, repo, "feature.txt", "unstaged\n")

			config := WorkspaceConfig{
				SocketPath:  filepath.Join(t.TempDir(), "sanhod.sock"),
				WorkspaceID: "workspace-recovery",
				Project:     "project-recovery",
				ActorEmail:  "test@example.com",
				DocsDir:     "docs",
			}
			configData, err := json.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(repo, ".sanho.json"), configData, 0644); err != nil {
				t.Fatal(err)
			}
			stateDir := filepath.Join(repo, ".git", "sanho", "pull-commit")
			if err := os.MkdirAll(stateDir, 0700); err != nil {
				t.Fatal(err)
			}
			state := map[string]any{
				"version":       2,
				"phase":         "prepared",
				"original_head": preparedHead,
				"sync_commit":   syncCommit,
				"prepared_head": preparedHead,
				"base_hash":     "base-docs",
				"remote_hash":   "remote-docs",
			}
			stateData, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(stateDir, "state.json"), stateData, 0600); err != nil {
				t.Fatal(err)
			}

			headBefore := runRecoveryGit(t, repo, "rev-parse", "HEAD")
			indexBefore := runRecoveryGit(t, repo, "write-tree")
			statusBefore := runRecoveryGit(t, repo, "status", "--porcelain=v1")
			cmd := exec.Command(cliBinary, "pull-commit", flag)
			cmd.Dir = repo
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("pull-commit %s: %v\n%s", flag, err, output)
			}
			if got := runRecoveryGit(t, repo, "rev-parse", "HEAD"); got != headBefore {
				t.Fatalf("HEAD=%s want %s", got, headBefore)
			}
			if got := runRecoveryGit(t, repo, "write-tree"); got != indexBefore {
				t.Fatalf("index=%s want %s", got, indexBefore)
			}
			if got := runRecoveryGit(t, repo, "status", "--porcelain=v1"); got != statusBefore {
				t.Fatalf("status changed:\n%s\nwant:\n%s", got, statusBefore)
			}
			if _, err := os.Stat(filepath.Join(stateDir, "state.json")); !os.IsNotExist(err) {
				t.Fatalf("transaction remained: %v", err)
			}
		})
	}
}

func runRecoveryGit(t *testing.T, repo string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeRecoveryFile(t *testing.T, repo, name, content string) {
	t.Helper()
	path := filepath.Join(repo, name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
