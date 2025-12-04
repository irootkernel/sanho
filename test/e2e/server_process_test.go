package e2e_test

import (
	"bytes"
	"context"
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

	testutil "github.com/SeventeenthEarth/kkachi/test/util"
)

// E2E: Prefer hitting a running server via KKACHI_E2E_BASE_URL; otherwise build & launch locally.
func TestE2E_ServerProcess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("failed to resolve repo root: %v", err)
	}

	tmp, err := os.MkdirTemp("", "kkachi-e2e-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmp)

	// Prepare origin repo with one commit.
	originPath := filepath.Join(tmp, "origin")
	if err := os.Mkdir(originPath, 0755); err != nil {
		t.Fatalf("failed to create origin dir: %v", err)
	}
	runCmd(t, "", nil, "git", "init", originPath)
	runCmd(t, "", nil, "git", "-C", originPath, "config", "user.email", "test@example.com")
	runCmd(t, "", nil, "git", "-C", originPath, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(originPath, "README.md"), []byte("# Test Repo\n"), 0644); err != nil {
		t.Fatalf("failed to write README: %v", err)
	}
	runCmd(t, "", nil, "git", "-C", originPath, "add", ".")
	runCmd(t, "", nil, "git", "-C", originPath, "commit", "-m", "Initial commit")
	headOut := runCmd(t, "", nil, "git", "-C", originPath, "rev-parse", "HEAD")
	expectedHead := strings.TrimSpace(string(headOut))

	projectName := fmt.Sprintf("test-project-%d", time.Now().UnixNano())
	repoID := fmt.Sprintf("test-repo-%d", time.Now().UnixNano())

	baseURL, stop := maybeStartServer(ctx, t, repoRoot, tmp)
	if stop != nil {
		defer stop()
	}

	if err := testutil.WaitForHealth(ctx, baseURL+"/healthz"); err != nil {
		t.Fatalf("server did not become healthy: %v", err)
	}

	client := &http.Client{Timeout: 5 * time.Second}

	// Add project.
	body, _ := json.Marshal(map[string]string{
		"project":       projectName,
		"docs_repo_id":  repoID,
		"docs_repo_url": originPath,
		"actor_email":   "admin@example.com",
	})
	resp, err := client.Post(baseURL+"/projects", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("add project request failed: %v", err)
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

	// Get head.
	resp, err = client.Get(baseURL + "/docs/head?project=" + projectName)
	if err != nil {
		t.Fatalf("get head request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get head status = %d", resp.StatusCode)
	}
	var headResp map[string]string
	json.NewDecoder(resp.Body).Decode(&headResp)
	resp.Body.Close()
	if headResp["head"] != expectedHead {
		t.Fatalf("expected head %s, got %s", expectedHead, headResp["head"])
	}

	// Delete project.
	req, _ := http.NewRequest(http.MethodDelete, baseURL+"/projects/"+projectName+"?force=true", nil)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("delete project request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete project status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Confirm project gone.
	resp, err = client.Get(baseURL + "/docs/head?project=" + projectName)
	if err != nil {
		t.Fatalf("get head after delete failed: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 unknown_project, got %d", resp.StatusCode)
	}
	var errResp map[string]string
	json.NewDecoder(resp.Body).Decode(&errResp)
	resp.Body.Close()
	if errResp["error"] != "unknown_project" {
		t.Fatalf("expected error unknown_project, got %s", errResp["error"])
	}
}

func maybeStartServer(ctx context.Context, t *testing.T, repoRoot, tmp string) (string, func()) {
	if base := strings.TrimSpace(os.Getenv("KKACHI_E2E_BASE_URL")); base != "" {
		return strings.TrimRight(base, "/"), nil
	}

	// Build binary into temp dir.
	binPath := filepath.Join(tmp, "kkachi-server")
	runCmd(t, repoRoot, map[string]string{}, "go", "build", "-o", binPath, "./cmd/server")

	// Pick a free port.
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

func runCmd(t *testing.T, dir string, extraEnv map[string]string, name string, args ...string) []byte {
	t.Helper()
	out, err := testutil.RunCmd(dir, extraEnv, name, args...)
	if err != nil {
		t.Fatalf("command %s %v failed: %v\noutput:\n%s", name, args, err, string(out))
	}
	return out
}
