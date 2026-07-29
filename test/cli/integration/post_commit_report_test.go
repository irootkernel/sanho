package integration

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestCLIPostCommitReportsWorkspaceDocsHash(t *testing.T) {
	cliBinary := getCliBinary(t)
	var calls atomic.Int32
	daemon := newUnixTestDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPut || r.URL.Path != "/workspaces/workspace-1/docs-hash" {
			t.Fatalf("request=%s %s", r.Method, r.URL.Path)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["docs_hash"] != "docs-2" || body["actor_email"] != "actor@example.com" {
			t.Fatalf("body=%v", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
	defer daemon.Close()

	repo := t.TempDir()
	runGitCommand(t, repo, "init", "--initial-branch=main")
	writePostCommitReportConfig(t, repo, daemon.SocketPath)

	cmd := exec.Command(cliBinary, "hook", "post-commit")
	cmd.Dir = repo
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("post-commit hook failed: %v\n%s", err, output)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls=%d want 1", calls.Load())
	}
	pathCmd := exec.Command("git", "-C", repo, "rev-parse", "--git-path", "sanho/workspace-report.json")
	pathOutput, err := pathCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("resolve report path: %v\n%s", err, pathOutput)
	}
	reportPath := strings.TrimSpace(string(pathOutput))
	if !filepath.IsAbs(reportPath) {
		reportPath = filepath.Join(repo, reportPath)
	}
	if _, err := os.Stat(reportPath); !os.IsNotExist(err) {
		t.Fatalf("completed report was not removed: %v", err)
	}
}

func writePostCommitReportConfig(t *testing.T, repo, socketPath string) {
	t.Helper()
	config := map[string]string{
		"socket_path":    socketPath,
		"workspace_id":   "workspace-1",
		"project":        "project-1",
		"actor_email":    "actor@example.com",
		"docs_dir":       "docs",
		"docs_hash_file": ".sanho_docs_hash",
	}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".sanho.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".sanho_docs_hash"), []byte("docs-2\n"), 0644); err != nil {
		t.Fatal(err)
	}
}
