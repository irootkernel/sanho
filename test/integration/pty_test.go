package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/SeventeenthEarth/kkachi/internal/infra/state"
	kkachihttp "github.com/SeventeenthEarth/kkachi/internal/interface/http"
	"github.com/SeventeenthEarth/kkachi/internal/interface/http/dto"
	"github.com/SeventeenthEarth/kkachi/internal/interface/http/handler"
	"github.com/SeventeenthEarth/kkachi/internal/pty"
)

// TestPTY_Integration tests the full PTY HTTP API flow
func TestPTY_Integration(t *testing.T) {
	// Setup temp directories
	tempDir, err := os.MkdirTemp("", "kkachi-pty-integration-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	// Create workspace directory
	workspaceDir := filepath.Join(tempDir, "workspace")
	if err := os.MkdirAll(workspaceDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Setup state
	statePath := filepath.Join(tempDir, "state.json")
	stateRepo, err := state.NewFileStateRepository(statePath)
	if err != nil {
		t.Fatal(err)
	}

	// Add workspace to state
	wsState := state.WorkspaceState{
		ID:        "test-ws",
		Project:   "test-project",
		LocalPath: workspaceDir,
	}
	if err := stateRepo.AddWorkspace(wsState); err != nil {
		t.Fatal(err)
	}

	workspaceRepo := state.NewFileWorkspaceRepository(stateRepo)

	// Create PTY components
	ptyConfig := pty.Config{
		AllowedShells: []string{"/bin/sh", "/bin/bash"},
		DefaultShell:  "/bin/sh",
		DefaultCols:   80,
		DefaultRows:   24,
	}
	sessionManager := pty.NewSessionManager()
	defer sessionManager.Close()

	ptyHandler := handler.NewPTYHandler(sessionManager, workspaceRepo, ptyConfig)

	// Create minimal server with only PTY endpoint
	srv := kkachihttp.NewHTTPServer(
		kkachihttp.ServerConfig{Addr: ":0"},
		nil, nil, nil, nil, nil, nil, ptyHandler,
	)
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	client := ts.Client()

	// Test 1: Create session - success (may fail in CI without PTY support)
	t.Run("Create session success", func(t *testing.T) {
		reqBody := dto.CreatePTYSessionRequest{
			WorkspaceID: "test-ws",
		}
		body, _ := json.Marshal(reqBody)

		resp, err := client.Post(ts.URL+"/api/pty/sessions", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		// PTY spawn might fail in CI, but validation should pass
		if resp.StatusCode == http.StatusCreated {
			var createResp dto.CreatePTYSessionResponse
			if err := json.NewDecoder(resp.Body).Decode(&createResp); err != nil {
				t.Fatalf("Failed to decode response: %v", err)
			}

			if createResp.SessionID == "" {
				t.Error("Expected session_id to be set")
			}
			if createResp.WsURL == "" {
				t.Error("Expected ws_url to be set")
			}
			if createResp.ResolvedCWD != workspaceDir {
				t.Errorf("Expected resolved_cwd '%s', got '%s'", workspaceDir, createResp.ResolvedCWD)
			}
		} else if resp.StatusCode == http.StatusInternalServerError {
			// PTY spawn failed - acceptable in some CI environments
			t.Log("PTY spawn failed (may be expected in CI environments)")
		} else {
			t.Errorf("Expected 201 or 500, got %d", resp.StatusCode)
		}
	})

	// Test 2: Create session - unknown workspace
	t.Run("Create session unknown workspace", func(t *testing.T) {
		reqBody := dto.CreatePTYSessionRequest{
			WorkspaceID: "nonexistent",
		}
		body, _ := json.Marshal(reqBody)

		resp, err := client.Post(ts.URL+"/api/pty/sessions", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected 400, got %d", resp.StatusCode)
		}

		var errResp dto.PTYErrorResponse
		json.NewDecoder(resp.Body).Decode(&errResp)
		if errResp.Error != "unknown_workspace" {
			t.Errorf("Expected error 'unknown_workspace', got '%s'", errResp.Error)
		}
	})

	// Test 3: Create session - path traversal
	t.Run("Create session path traversal blocked", func(t *testing.T) {
		reqBody := dto.CreatePTYSessionRequest{
			WorkspaceID: "test-ws",
			CwdRel:      "../../../etc",
		}
		body, _ := json.Marshal(reqBody)

		resp, err := client.Post(ts.URL+"/api/pty/sessions", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected 400, got %d", resp.StatusCode)
		}

		var errResp dto.PTYErrorResponse
		json.NewDecoder(resp.Body).Decode(&errResp)
		if errResp.Error != "cwd_traversal_attempt" {
			t.Errorf("Expected error 'cwd_traversal_attempt', got '%s'", errResp.Error)
		}
	})

	// Test 4: Terminate non-existent session (idempotent)
	t.Run("Terminate non-existent session idempotent", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/pty/sessions/nonexistent-session-id", nil)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		// Should succeed even for non-existent session (idempotent)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected 200 for idempotent terminate, got %d", resp.StatusCode)
		}
	})
}
