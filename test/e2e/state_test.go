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

	"github.com/SeventeenthEarth/kkachi/internal/interface/http/dto"
	testutil "github.com/SeventeenthEarth/kkachi/test/util"
)

// TestE2E_State tests the /state endpoint with real server and Git operations.
// Per F5-Data requirement: "서버에서 /workspaces/register를 여러 번 호출한 뒤 /state 응답이 이를 반영하는지 확인"
func TestE2E_State(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("failed to resolve repo root: %v", err)
	}

	tmp, err := os.MkdirTemp("", "kkachi-e2e-state-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmp)

	// Prepare origin repo with one commit.
	originPath := filepath.Join(tmp, "origin")
	if err := os.Mkdir(originPath, 0755); err != nil {
		t.Fatalf("failed to create origin dir: %v", err)
	}
	runStateTestCmd(t, "", nil, "git", "init", originPath)
	runStateTestCmd(t, "", nil, "git", "-C", originPath, "config", "user.email", "test@example.com")
	runStateTestCmd(t, "", nil, "git", "-C", originPath, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(originPath, "README.md"), []byte("# Test Repo\n"), 0644); err != nil {
		t.Fatalf("failed to write README: %v", err)
	}
	runStateTestCmd(t, "", nil, "git", "-C", originPath, "add", ".")
	runStateTestCmd(t, "", nil, "git", "-C", originPath, "commit", "-m", "Initial commit")
	headOut := runStateTestCmd(t, "", nil, "git", "-C", originPath, "rev-parse", "HEAD")
	expectedHead := strings.TrimSpace(string(headOut))

	projectName := fmt.Sprintf("state-test-project-%d", time.Now().UnixNano())
	repoID := fmt.Sprintf("state-test-repo-%d", time.Now().UnixNano())

	baseURL, stop := maybeStartStateServer(ctx, t, repoRoot, tmp)
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

	// Test 1: Initial /state - no workspaces
	t.Run("Initial state - project with no workspaces", func(t *testing.T) {
		resp, err := client.Get(baseURL + "/state")
		if err != nil {
			t.Fatalf("get state request failed: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("get state status = %d", resp.StatusCode)
		}

		var stateResp map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&stateResp)
		resp.Body.Close()

		// Verify docs_heads contains our project
		docsHeads, ok := stateResp["docs_heads"].(map[string]interface{})
		if !ok {
			t.Fatalf("docs_heads not in response")
		}
		if docsHeads[projectName] != expectedHead {
			t.Errorf("expected docs_heads[%s] = %s, got %v", projectName, expectedHead, docsHeads[projectName])
		}

		// Verify workspaces is empty (for our project)
		workspaces, ok := stateResp["workspaces"].([]interface{})
		if !ok {
			t.Fatalf("workspaces not in response")
		}
		projectWSCount := 0
		for _, ws := range workspaces {
			wsMap := ws.(map[string]interface{})
			if wsMap["project"] == projectName {
				projectWSCount++
			}
		}
		if projectWSCount != 0 {
			t.Errorf("expected 0 workspaces for project %s, got %d", projectName, projectWSCount)
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
		t.Fatalf("register workspace 1 failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("register workspace 1 status = %d", resp.StatusCode)
	}
	var wsResp1 map[string]string
	json.NewDecoder(resp.Body).Decode(&wsResp1)
	resp.Body.Close()
	wsID1 := wsResp1["workspace_id"]

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
		t.Fatalf("register workspace 2 failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("register workspace 2 status = %d", resp.StatusCode)
	}
	var wsResp2 map[string]string
	json.NewDecoder(resp.Body).Decode(&wsResp2)
	resp.Body.Close()
	wsID2 := wsResp2["workspace_id"]

	// Test 2: /state after registering workspaces
	t.Run("State with multiple workspaces", func(t *testing.T) {
		resp, err := client.Get(baseURL + "/state")
		if err != nil {
			t.Fatalf("get state request failed: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("get state status = %d", resp.StatusCode)
		}

		var stateResp map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&stateResp)
		resp.Body.Close()

		// Verify docs_heads
		docsHeads, _ := stateResp["docs_heads"].(map[string]interface{})
		if docsHeads[projectName] != expectedHead {
			t.Errorf("expected docs_heads[%s] = %s, got %v", projectName, expectedHead, docsHeads[projectName])
		}

		// Verify workspaces contains our registered workspaces
		workspaces, _ := stateResp["workspaces"].([]interface{})
		foundWS1 := false
		foundWS2 := false
		for _, ws := range workspaces {
			wsMap := ws.(map[string]interface{})
			if wsMap["workspace_id"] == wsID1 {
				foundWS1 = true
				if wsMap["project"] != projectName {
					t.Errorf("ws1 project = %v, want %s", wsMap["project"], projectName)
				}
				if wsMap["local_path"] != "/tmp/ws1" {
					t.Errorf("ws1 local_path = %v, want /tmp/ws1", wsMap["local_path"])
				}
				if wsMap["last_actor_email"] != "dev1@example.com" {
					t.Errorf("ws1 last_actor_email = %v, want dev1@example.com", wsMap["last_actor_email"])
				}
			}
			if wsMap["workspace_id"] == wsID2 {
				foundWS2 = true
				if wsMap["project"] != projectName {
					t.Errorf("ws2 project = %v, want %s", wsMap["project"], projectName)
				}
				if wsMap["local_path"] != "/tmp/ws2" {
					t.Errorf("ws2 local_path = %v, want /tmp/ws2", wsMap["local_path"])
				}
				if wsMap["last_actor_email"] != "dev2@example.com" {
					t.Errorf("ws2 last_actor_email = %v, want dev2@example.com", wsMap["last_actor_email"])
				}
			}
		}
		if !foundWS1 {
			t.Errorf("workspace %s not found in /state response", wsID1)
		}
		if !foundWS2 {
			t.Errorf("workspace %s not found in /state response", wsID2)
		}
	})

	// Test 3: Verify workspace data contains required fields
	t.Run("Workspace data has all required fields", func(t *testing.T) {
		resp, err := client.Get(baseURL + "/state")
		if err != nil {
			t.Fatalf("get state request failed: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("get state status = %d", resp.StatusCode)
		}

		var stateResp map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&stateResp)
		resp.Body.Close()

		workspaces, _ := stateResp["workspaces"].([]interface{})
		requiredFields := []string{"workspace_id", "project", "docs_repo_id", "local_path", "repo_url", "docs_hash", "last_reported_at", "last_actor_email"}

		for _, ws := range workspaces {
			wsMap := ws.(map[string]interface{})
			for _, field := range requiredFields {
				if _, ok := wsMap[field]; !ok {
					t.Errorf("missing field %s in workspace response", field)
				}
			}
		}
	})
}

func maybeStartStateServer(ctx context.Context, t *testing.T, repoRoot, tmp string) (string, func()) {
	if base := strings.TrimSpace(os.Getenv("KKACHI_E2E_BASE_URL")); base != "" {
		return strings.TrimRight(base, "/"), nil
	}

	// Build binary into temp dir.
	binPath := filepath.Join(tmp, "kkachi-server")
	runStateTestCmd(t, repoRoot, map[string]string{}, "go", "build", "-o", binPath, "./cmd/server")

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

func runStateTestCmd(t *testing.T, dir string, extraEnv map[string]string, name string, args ...string) []byte {
	t.Helper()
	out, err := testutil.RunCmd(dir, extraEnv, name, args...)
	if err != nil {
		t.Fatalf("command %s %v failed: %v\noutput:\n%s", name, args, err, string(out))
	}
	return out
}
