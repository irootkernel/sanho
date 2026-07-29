package e2e

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestE2ECLI_TwoWorkspacesSequentialPush verifies outdated/current_hash behavior with two workspaces.
func TestE2ECLI_TwoWorkspacesSequentialPush(t *testing.T) {
	cliBinary := getCliBinary(t)
	socketPath := getSocketPath(t)
	ensureServerAvailable(t, socketPath)

	originPath, initialHead := createOriginRepo(t, map[string]string{
		"docs/index.md": "# initial\n",
	})

	project := "cli-two-ws-" + strings.ReplaceAll(filepath.Base(originPath), string(filepath.Separator), "_")

	// Workspace directories
	wsDir1 := t.TempDir()
	wsDir2 := t.TempDir()
	initGitRepo(t, wsDir1)
	initGitRepo(t, wsDir2)
	setGitUser(t, wsDir1, "ws1@example.com")
	setGitUser(t, wsDir2, "ws2@example.com")
	runCmd(t, wsDir1, "git", "remote", "add", "origin", originPath)
	runCmd(t, wsDir2, "git", "remote", "add", "origin", originPath)

	// Register project & workspaces via CLI
	registerProjectViaCLI(t, cliBinary, socketPath, project, originPath, wsDir1)
	ws1ID, head1 := registerWorkspaceViaCLI(t, cliBinary, socketPath, project, wsDir1)
	ws2ID, head2 := registerWorkspaceViaCLI(t, cliBinary, socketPath, project, wsDir2)
	if head1 == "" {
		head1 = initialHead
	}
	if head2 == "" {
		head2 = initialHead
	}

	// Prepare local state for ws1 to push via HTTP
	writeConfig(t, wsDir1, socketPath, project, ws1ID, "ws1@example.com")
	writeDocsHash(t, wsDir1, head1)
	if err := os.MkdirAll(filepath.Join(wsDir1, "docs", "docs"), 0755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wsDir1, "docs", "docs", "index.md"), []byte("# updated by ws1\n"), 0644); err != nil {
		t.Fatalf("write docs: %v", err)
	}

	// Push from ws1 using HTTP helper (updated)
	newHead := pushDocsViaHTTP(t, socketPath, ws1ID, head1, map[string]string{
		"docs/index.md": "# updated by ws1\n",
	}, "ws1@example.com")
	if newHead == head1 {
		t.Fatalf("push did not update head")
	}

	// Workspace 2 attempts push with old base -> should get outdated/current_docs_hash=newHead
	snapshot := createSnapshotTar(t, map[string]string{
		"docs/index.md": "# ws2 change\n",
	})
	reqBody, _ := json.Marshal(map[string]string{
		"workspace_id":   ws2ID,
		"base_docs_hash": head2,
		"docs_snapshot":  base64.StdEncoding.EncodeToString(snapshot),
		"actor_email":    "ws2@example.com",
	})
	resp, err := unixHTTPClient(socketPath, 10*time.Second).Post(
		"http://sanho/docs/push",
		"application/json",
		bytes.NewReader(reqBody),
	)
	if err != nil {
		t.Fatalf("ws2 push failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ws2 push status=%d", resp.StatusCode)
	}
	var pushResp map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&pushResp); err != nil {
		t.Fatalf("decode push resp: %v", err)
	}
	if pushResp["status"] != "outdated" {
		t.Fatalf("expected outdated, got %v", pushResp["status"])
	}
	if pushResp["current_docs_hash"] != newHead {
		t.Fatalf("expected current_docs_hash %s, got %v", newHead, pushResp["current_docs_hash"])
	}

	// Cleanup
	deleteProjectViaCLI(t, cliBinary, socketPath, project, true)
}
