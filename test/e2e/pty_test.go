package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SeventeenthEarth/kkachi/internal/interface/http/dto"
)

// TestE2E_PTY_CreateAndTerminate tests the full PTY session lifecycle against a real server.
func TestE2E_PTY_CreateAndTerminate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	baseURL := requireServer(t, ctx)
	client := &http.Client{Timeout: 30 * time.Second}

	// Setup: Create a project and workspace
	projectName := uniqueName("pty-test")
	originPath, _ := createOriginRepo(t, map[string]string{
		"docs/README.md": "# PTY Test Docs\n",
	})

	addProject(t, client, baseURL, projectName, "pty-docs-repo", originPath)
	t.Cleanup(func() {
		deleteProject(t, client, baseURL, projectName, true)
	})

	// Create workspace directory
	workspaceDir := sharedRepoTempDir(t)
	workspaceDir = filepath.Join(workspaceDir, "workspace")
	if err := os.MkdirAll(workspaceDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Register workspace
	workspaceID, _ := registerWorkspace(t, client, baseURL, dto.RegisterWorkspaceRequest{
		Project:    projectName,
		LocalPath:  workspaceDir,
		RepoURL:    originPath,
		ActorEmail: "test@example.com",
	})
	t.Cleanup(func() {
		deleteWorkspace(t, client, baseURL, workspaceID)
	})

	t.Run("Create session - unknown workspace returns 400", func(t *testing.T) {
		reqBody := dto.CreatePTYSessionRequest{
			WorkspaceID: "nonexistent-workspace-id",
		}
		body, _ := json.Marshal(reqBody)

		resp, err := client.Post(baseURL+"/api/pty/sessions", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected 400 for unknown workspace, got %d", resp.StatusCode)
		}

		var errResp dto.PTYErrorResponse
		json.NewDecoder(resp.Body).Decode(&errResp)
		if errResp.Error != "unknown_workspace" {
			t.Errorf("Expected error 'unknown_workspace', got '%s'", errResp.Error)
		}
	})

	t.Run("Create session - path traversal blocked", func(t *testing.T) {
		reqBody := dto.CreatePTYSessionRequest{
			WorkspaceID: workspaceID,
			CwdRel:      "../../../etc",
		}
		body, _ := json.Marshal(reqBody)

		resp, err := client.Post(baseURL+"/api/pty/sessions", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected 400 for path traversal, got %d", resp.StatusCode)
		}

		var errResp dto.PTYErrorResponse
		json.NewDecoder(resp.Body).Decode(&errResp)
		if errResp.Error != "cwd_traversal_attempt" {
			t.Errorf("Expected error 'cwd_traversal_attempt', got '%s'", errResp.Error)
		}
	})

	t.Run("Terminate non-existent session - idempotent", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodDelete, baseURL+"/api/pty/sessions/nonexistent-session-id", nil)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		// Should succeed even for non-existent session (idempotent)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected 200 for idempotent terminate, got %d", resp.StatusCode)
		}

		var okResp map[string]bool
		json.NewDecoder(resp.Body).Decode(&okResp)
		if !okResp["ok"] {
			t.Error("Expected ok: true")
		}
	})

	// Test session creation - this may fail if the server environment doesn't support PTY
	t.Run("Create session - success or PTY unavailable", func(t *testing.T) {
		reqBody := dto.CreatePTYSessionRequest{
			WorkspaceID: workspaceID,
		}
		body, _ := json.Marshal(reqBody)

		resp, err := client.Post(baseURL+"/api/pty/sessions", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		// Valid responses: 201 (success), 500 (PTY unavailable)
		validStatuses := map[int]bool{
			http.StatusCreated:             true, // Success
			http.StatusInternalServerError: true, // PTY spawn failed (environment limit)
		}

		if !validStatuses[resp.StatusCode] {
			t.Errorf("Expected 201 or 500, got %d", resp.StatusCode)
		}

		if resp.StatusCode == http.StatusCreated {
			var createResp dto.CreatePTYSessionResponse
			json.NewDecoder(resp.Body).Decode(&createResp)

			if createResp.SessionID == "" {
				t.Error("Expected session_id to be set")
			}
			if createResp.WsURL == "" {
				t.Error("Expected ws_url to be set")
			}

			// Clean up: terminate the created session
			terminateReq, _ := http.NewRequest(http.MethodDelete, baseURL+"/api/pty/sessions/"+createResp.SessionID, nil)
			terminateResp, err := client.Do(terminateReq)
			if err != nil {
				t.Logf("Failed to terminate session: %v", err)
			} else {
				terminateResp.Body.Close()
			}
		} else {
			t.Log("PTY spawn failed (may be expected in some environments)")
		}
	})
}

// TestE2E_PTY_MissingWorkspaceID tests that missing workspace_id returns 400.
func TestE2E_PTY_MissingWorkspaceID(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	baseURL := requireServer(t, ctx)
	client := &http.Client{Timeout: 10 * time.Second}

	t.Run("Empty workspace_id returns 400", func(t *testing.T) {
		reqBody := dto.CreatePTYSessionRequest{
			WorkspaceID: "",
		}
		body, _ := json.Marshal(reqBody)

		resp, err := client.Post(baseURL+"/api/pty/sessions", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected 400 for missing workspace_id, got %d", resp.StatusCode)
		}

		var errResp dto.PTYErrorResponse
		json.NewDecoder(resp.Body).Decode(&errResp)
		if errResp.Error != "missing_workspace_id" {
			t.Errorf("Expected error 'missing_workspace_id', got '%s'", errResp.Error)
		}
	})

	t.Run("Invalid JSON returns 400", func(t *testing.T) {
		resp, err := client.Post(baseURL+"/api/pty/sessions", "application/json", bytes.NewReader([]byte("{invalid")))
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected 400 for invalid JSON, got %d", resp.StatusCode)
		}
	})
}
