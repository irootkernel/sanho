package e2e_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SeventeenthEarth/kkachi/internal/interface/http/dto"
	testutil "github.com/SeventeenthEarth/kkachi/test/util"
)

// TestE2E_DocsPush tests the full /docs/push flow with a running server
func TestE2E_DocsPush(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("failed to resolve repo root: %v", err)
	}

	tmp, err := os.MkdirTemp("", "kkachi-e2e-push-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmp)

	// Create origin repo
	originPath := filepath.Join(tmp, "origin")
	if err := os.Mkdir(originPath, 0755); err != nil {
		t.Fatalf("failed to create origin dir: %v", err)
	}
	runCmdE2E(t, "", nil, "git", "init", "--bare", originPath)

	// Clone and setup local repo
	localPath := filepath.Join(tmp, "local")
	runCmdE2E(t, "", nil, "git", "clone", originPath, localPath)
	runCmdE2E(t, localPath, nil, "git", "config", "user.email", "test@example.com")
	runCmdE2E(t, localPath, nil, "git", "config", "user.name", "Test User")

	// Create initial docs
	docsDir := filepath.Join(localPath, "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatalf("failed to create docs dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(docsDir, "index.md"), []byte("# Initial\n"), 0644); err != nil {
		t.Fatalf("failed to write initial file: %v", err)
	}
	runCmdE2E(t, localPath, nil, "git", "add", ".")
	runCmdE2E(t, localPath, nil, "git", "commit", "-m", "Initial commit")
	runCmdE2E(t, localPath, nil, "git", "push", "origin", "HEAD")

	initialHead := strings.TrimSpace(string(runCmdE2E(t, localPath, nil, "git", "rev-parse", "HEAD")))

	projectName := fmt.Sprintf("test-push-%d", time.Now().UnixNano())
	repoID := fmt.Sprintf("test-repo-%d", time.Now().UnixNano())

	baseURL, stop := maybeStartServerForPush(ctx, t, repoRoot, tmp)
	if stop != nil {
		defer stop()
	}

	if err := testutil.WaitForHealth(ctx, baseURL+"/healthz"); err != nil {
		t.Fatalf("server did not become healthy: %v", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}

	// Add project
	body, _ := json.Marshal(map[string]string{
		"project":       projectName,
		"docs_repo_id":  repoID,
		"docs_repo_url": originPath,
		"actor_email":   "admin@example.com",
	})
	resp, err := client.Post(baseURL+"/projects", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("add project failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("add project status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	t.Cleanup(func() {
		req, _ := http.NewRequest(http.MethodDelete, baseURL+"/projects/"+projectName+"?force=true", nil)
		if r, err := client.Do(req); err == nil {
			r.Body.Close()
		}
	})

	// Register first workspace
	wsReq1 := dto.RegisterWorkspaceRequest{
		Project:    projectName,
		LocalPath:  "/tmp/ws1",
		RepoURL:    originPath,
		ActorEmail: "dev1@example.com",
	}
	wsBody1, _ := json.Marshal(wsReq1)
	resp, err = client.Post(baseURL+"/workspaces/register", "application/json", bytes.NewReader(wsBody1))
	if err != nil {
		t.Fatalf("register ws1 failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("register ws1 status = %d", resp.StatusCode)
	}
	var wsResp1 map[string]string
	json.NewDecoder(resp.Body).Decode(&wsResp1)
	resp.Body.Close()
	ws1ID := wsResp1["workspace_id"]

	// Register second workspace
	wsReq2 := dto.RegisterWorkspaceRequest{
		Project:    projectName,
		LocalPath:  "/tmp/ws2",
		RepoURL:    originPath,
		ActorEmail: "dev2@example.com",
	}
	wsBody2, _ := json.Marshal(wsReq2)
	resp, err = client.Post(baseURL+"/workspaces/register", "application/json", bytes.NewReader(wsBody2))
	if err != nil {
		t.Fatalf("register ws2 failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("register ws2 status = %d", resp.StatusCode)
	}
	var wsResp2 map[string]string
	json.NewDecoder(resp.Body).Decode(&wsResp2)
	resp.Body.Close()
	ws2ID := wsResp2["workspace_id"]

	// WS1 pushes changes (should succeed with "updated")
	t.Run("WS1 Push - Updated", func(t *testing.T) {
		snapshot := createSnapshotE2E(t, map[string]string{
			"index.md": "# Updated by WS1\n",
		})
		pushReq := dto.DocsPushRequest{
			WorkspaceID:  ws1ID,
			BaseDocsHash: initialHead,
			DocsSnapshot: base64.StdEncoding.EncodeToString(snapshot),
			ActorEmail:   "dev1@example.com",
		}
		pushBody, _ := json.Marshal(pushReq)
		resp, err := client.Post(baseURL+"/docs/push", "application/json", bytes.NewReader(pushBody))
		if err != nil {
			t.Fatalf("push failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		var pushResp dto.DocsPushResponse
		json.NewDecoder(resp.Body).Decode(&pushResp)

		if !pushResp.Ok {
			t.Errorf("expected Ok=true, got false. Error: %s", pushResp.Error)
		}
		if pushResp.Status != "updated" {
			t.Errorf("expected status 'updated', got '%s'", pushResp.Status)
		}
		if pushResp.NewDocsHash == "" {
			t.Error("expected NewDocsHash to be set")
		}
	})

	// WS2 pushes with old base (should get "outdated")
	t.Run("WS2 Push - Outdated", func(t *testing.T) {
		snapshot := createSnapshotE2E(t, map[string]string{
			"index.md": "# Updated by WS2\n",
		})
		pushReq := dto.DocsPushRequest{
			WorkspaceID:  ws2ID,
			BaseDocsHash: initialHead, // Still using old base!
			DocsSnapshot: base64.StdEncoding.EncodeToString(snapshot),
			ActorEmail:   "dev2@example.com",
		}
		pushBody, _ := json.Marshal(pushReq)
		resp, err := client.Post(baseURL+"/docs/push", "application/json", bytes.NewReader(pushBody))
		if err != nil {
			t.Fatalf("push failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		var pushResp dto.DocsPushResponse
		json.NewDecoder(resp.Body).Decode(&pushResp)

		if !pushResp.Ok {
			t.Errorf("expected Ok=true, got false. Error: %s", pushResp.Error)
		}
		if pushResp.Status != "outdated" {
			t.Errorf("expected status 'outdated', got '%s'", pushResp.Status)
		}
		if pushResp.CurrentDocsHash == "" {
			t.Error("expected CurrentDocsHash to be set")
		}
	})
}

func maybeStartServerForPush(ctx context.Context, t *testing.T, repoRoot, tmp string) (string, func()) {
	if base := strings.TrimSpace(os.Getenv("KKACHI_E2E_BASE_URL")); base != "" {
		return strings.TrimRight(base, "/"), nil
	}

	binPath := filepath.Join(tmp, "kkachi-server")
	runCmdE2E(t, repoRoot, map[string]string{}, "go", "build", "-o", binPath, "./cmd/server")

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to pick port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()

	statePath := filepath.Join(tmp, "state.json")
	workDir := filepath.Join(tmp, "server_workdir")
	if err := os.MkdirAll(workDir, 0755); err != nil {
		t.Fatalf("failed to create workdir: %v", err)
	}

	serverCmd := exec.CommandContext(ctx, binPath)
	serverCmd.Dir = workDir
	var stdout, stderr bytes.Buffer
	serverCmd.Stdout = &stdout
	serverCmd.Stderr = &stderr
	serverCmd.Env = append(os.Environ(),
		fmt.Sprintf("PORT=%d", port),
		fmt.Sprintf("STATE_FILE_PATH=%s", statePath),
	)
	if err := serverCmd.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("server stdout:\n%s", stdout.String())
			t.Logf("server stderr:\n%s", stderr.String())
		}
	})

	stop := func() {
		_ = serverCmd.Process.Kill()
		_ = serverCmd.Wait()
	}

	return fmt.Sprintf("http://127.0.0.1:%d", port), stop
}

func runCmdE2E(t *testing.T, dir string, extraEnv map[string]string, name string, args ...string) []byte {
	t.Helper()
	out, err := testutil.RunCmd(dir, extraEnv, name, args...)
	if err != nil {
		t.Fatalf("command %s %v failed: %v\noutput:\n%s", name, args, err, string(out))
	}
	return out
}

func createSnapshotE2E(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	for name, content := range files {
		hdr := &tar.Header{
			Name: name,
			Mode: 0644,
			Size: int64(len(content)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("Failed to write tar header: %v", err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("Failed to write tar content: %v", err)
		}
	}

	tw.Close()
	gw.Close()

	return buf.Bytes()
}
