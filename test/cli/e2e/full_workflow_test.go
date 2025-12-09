package e2e

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestE2ECLI_ProjectWorkspaceLifecycle covers project add, workspace register/unregister, and project delete via CLI against real server.
func TestE2ECLI_ProjectWorkspaceLifecycle(t *testing.T) {
	cliBinary := getCliBinary(t)
	serverURL := getServerURL(t)
	ensureServerAvailable(t, serverURL)

	originPath, _ := createOriginRepo(t, map[string]string{
		"docs/index.md": "# lifecycle\n",
	})

	workspaceDir := t.TempDir()
	initGitRepo(t, workspaceDir)
	setGitUser(t, workspaceDir, "cli-e2e@example.com")
	runCmd(t, workspaceDir, "git", "remote", "add", "origin", originPath)

	project := "cli-e2e-project-" + strings.ReplaceAll(filepath.Base(workspaceDir), string(filepath.Separator), "_")

	// Project add
	repoID := registerProjectViaCLI(t, cliBinary, serverURL, project, originPath, workspaceDir)

	// Workspace register
	wsID, currentHead := registerWorkspaceViaCLI(t, cliBinary, serverURL, project, workspaceDir)
	if currentHead == "" {
		t.Fatalf("current head empty")
	}

	// Validate server state contains workspace
	state := fetchStateViaHTTP(t, serverURL)
	found := false
	if wss, ok := state["workspaces"].([]interface{}); ok {
		for _, w := range wss {
			if m, ok := w.(map[string]interface{}); ok && m["workspace_id"] == wsID {
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatalf("workspace %s not found in server state", wsID)
	}

	// Workspace unregister
	deleteWorkspaceViaCLI(t, cliBinary, serverURL, wsID)

	// Project delete with force (cleanup)
	deleteProjectViaCLI(t, cliBinary, serverURL, project, true)

	// Verify project removal: /docs/head should now be unknown_project
	req, _ := http.NewRequest(http.MethodGet, strings.TrimRight(serverURL, "/")+"/docs/head?project="+project, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("head after delete failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 after project delete, got %d", resp.StatusCode)
	}

	// Prevent unused warnings
	_ = repoID
}

// TestE2ECLI_FixWorkflow exercises fix happy-path: pending fix exists, push updated docs, hashes update.
func TestE2ECLI_FixWorkflow(t *testing.T) {
	cliBinary := getCliBinary(t)
	serverURL := getServerURL(t)
	ensureServerAvailable(t, serverURL)

	originPath, initialHead := createOriginRepo(t, map[string]string{
		"docs/index.md": "# initial\n",
	})

	workspaceDir := t.TempDir()
	initGitRepo(t, workspaceDir)
	setGitUser(t, workspaceDir, "cli-fix@example.com")
	runCmd(t, workspaceDir, "git", "remote", "add", "origin", originPath)

	project := "cli-fix-project-" + strings.ReplaceAll(filepath.Base(workspaceDir), string(filepath.Separator), "_")

	registerProjectViaCLI(t, cliBinary, serverURL, project, originPath, workspaceDir)
	wsID, currentHead := registerWorkspaceViaCLI(t, cliBinary, serverURL, project, workspaceDir)
	if currentHead == "" {
		currentHead = initialHead
	}

	// Prepare workspace config and state files
	writeConfig(t, workspaceDir, serverURL, project, wsID, "cli-fix@example.com")
	writeDocsHash(t, workspaceDir, currentHead)
	writePendingFix(t, workspaceDir, currentHead, currentHead)

	// Update docs content
	docsDir := filepath.Join(workspaceDir, "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(docsDir, "index.md"), []byte("# updated by cli fix\n"), 0644); err != nil {
		t.Fatalf("write docs: %v", err)
	}

	// Run fix
	cmd := exec.Command(cliBinary, "fix")
	cmd.Dir = workspaceDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("fix failed: %v\nOutput:\n%s", err, string(out))
	}

	// Pending fix file should be gone
	if _, err := os.Stat(filepath.Join(workspaceDir, ".kkachi_pending_fix")); err == nil {
		t.Fatalf("pending fix file still exists")
	}

	// Docs hash should be updated
	newHashBytes, err := os.ReadFile(filepath.Join(workspaceDir, ".kkachi_docs_hash"))
	if err != nil {
		t.Fatalf("read docs hash: %v", err)
	}
	newHash := strings.TrimSpace(string(newHashBytes))
	if newHash == currentHead {
		t.Fatalf("docs hash not updated; still %s", newHash)
	}

	// Server head should match updated hash
	serverHead := fetchHeadViaHTTP(t, serverURL, project)
	if serverHead != newHash {
		t.Fatalf("server head %s != local hash %s", serverHead, newHash)
	}
}
