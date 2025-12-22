package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/SeventeenthEarth/kkachi/internal/domain/workspace"
	"github.com/SeventeenthEarth/kkachi/internal/interface/http/dto"
	"github.com/SeventeenthEarth/kkachi/internal/pty"
)

// mockWorkspaceLookup implements WorkspaceLookup for testing
type mockWorkspaceLookup struct {
	workspaces map[string]*workspace.Workspace
}

func (m *mockWorkspaceLookup) Get(_ context.Context, id workspace.WorkspaceID) (*workspace.Workspace, error) {
	ws := m.workspaces[string(id)]
	return ws, nil
}

func newTestPTYHandler(workspaces map[string]*workspace.Workspace) *PTYHandler {
	cfg := pty.Config{
		AllowedShells: []string{"/bin/sh", "/bin/bash"},
		DefaultShell:  "/bin/sh",
		DefaultCols:   80,
		DefaultRows:   24,
	}
	return NewPTYHandler(
		pty.NewSessionManager(),
		&mockWorkspaceLookup{workspaces: workspaces},
		cfg,
	)
}

func TestPTYHandler_Create_MissingWorkspaceID(t *testing.T) {
	handler := newTestPTYHandler(nil)

	body := `{}`
	req := httptest.NewRequest(http.MethodPost, "/api/pty/sessions", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", w.Code)
	}

	var resp dto.PTYErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error != "missing_workspace_id" {
		t.Errorf("Expected error 'missing_workspace_id', got '%s'", resp.Error)
	}
}

func TestPTYHandler_Create_UnknownWorkspace(t *testing.T) {
	handler := newTestPTYHandler(map[string]*workspace.Workspace{})

	body := `{"workspace_id": "nonexistent"}`
	req := httptest.NewRequest(http.MethodPost, "/api/pty/sessions", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", w.Code)
	}

	var resp dto.PTYErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error != "unknown_workspace" {
		t.Errorf("Expected error 'unknown_workspace', got '%s'", resp.Error)
	}
}

func TestPTYHandler_Create_PathTraversal(t *testing.T) {
	workspaceDir := t.TempDir()

	workspaces := map[string]*workspace.Workspace{
		"test-ws": {
			ID:        "test-ws",
			LocalPath: workspaceDir,
		},
	}
	handler := newTestPTYHandler(workspaces)

	body := `{"workspace_id": "test-ws", "cwd_rel": "../../../etc"}`
	req := httptest.NewRequest(http.MethodPost, "/api/pty/sessions", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", w.Code)
	}

	var resp dto.PTYErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error != "cwd_traversal_attempt" {
		t.Errorf("Expected error 'cwd_traversal_attempt', got '%s'", resp.Error)
	}
}

func TestPTYHandler_Create_AbsolutePath(t *testing.T) {
	workspaceDir := t.TempDir()

	workspaces := map[string]*workspace.Workspace{
		"test-ws": {
			ID:        "test-ws",
			LocalPath: workspaceDir,
		},
	}
	handler := newTestPTYHandler(workspaces)

	body := `{"workspace_id": "test-ws", "cwd_rel": "/etc/passwd"}`
	req := httptest.NewRequest(http.MethodPost, "/api/pty/sessions", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", w.Code)
	}

	var resp dto.PTYErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error != "absolute_path_not_allowed" {
		t.Errorf("Expected error 'absolute_path_not_allowed', got '%s'", resp.Error)
	}
}

func TestPTYHandler_Create_ShellNotAllowed(t *testing.T) {
	workspaceDir := t.TempDir()

	workspaces := map[string]*workspace.Workspace{
		"test-ws": {
			ID:        "test-ws",
			LocalPath: workspaceDir,
		},
	}
	handler := newTestPTYHandler(workspaces)

	body := `{"workspace_id": "test-ws", "shell": "/bin/fish"}`
	req := httptest.NewRequest(http.MethodPost, "/api/pty/sessions", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", w.Code)
	}

	var resp dto.PTYErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error != "shell_not_allowed" {
		t.Errorf("Expected error 'shell_not_allowed', got '%s'", resp.Error)
	}
}

func TestPTYHandler_Create_InvalidJSON(t *testing.T) {
	handler := newTestPTYHandler(nil)

	body := `{invalid json`
	req := httptest.NewRequest(http.MethodPost, "/api/pty/sessions", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", w.Code)
	}

	var resp dto.PTYErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error != "invalid_request_body" {
		t.Errorf("Expected error 'invalid_request_body', got '%s'", resp.Error)
	}
}

func TestPTYHandler_Create_InvalidTerminalSize(t *testing.T) {
	workspaceDir := t.TempDir()
	handler := newTestPTYHandler(map[string]*workspace.Workspace{
		"test-ws": {
			ID:        "test-ws",
			LocalPath: workspaceDir,
		},
	})

	tests := []struct {
		name string
		body string
	}{
		{
			name: "Negative cols",
			body: `{"workspace_id": "test-ws", "cols": -1, "rows": 24}`,
		},
		{
			name: "Rows too large",
			body: `{"workspace_id": "test-ws", "cols": 80, "rows": 1000}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/pty/sessions", bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			handler.Create(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("Expected 400, got %d", w.Code)
			}

			var resp dto.PTYErrorResponse
			json.NewDecoder(w.Body).Decode(&resp)
			if resp.Error != "invalid_terminal_size" {
				t.Errorf("Expected error 'invalid_terminal_size', got '%s'", resp.Error)
			}
		})
	}
}

func TestPTYHandler_Terminate_MissingSessionID(t *testing.T) {
	handler := newTestPTYHandler(nil)

	// Without path value set (simulating missing ID)
	req := httptest.NewRequest(http.MethodDelete, "/api/pty/sessions/", nil)
	w := httptest.NewRecorder()

	handler.Terminate(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", w.Code)
	}
}

func TestPTYHandler_Terminate_Idempotent(t *testing.T) {
	handler := newTestPTYHandler(nil)

	// Create a mock request with path value
	req := httptest.NewRequest(http.MethodDelete, "/api/pty/sessions/nonexistent", nil)
	req.SetPathValue("id", "nonexistent")
	w := httptest.NewRecorder()

	handler.Terminate(w, req)

	// Should succeed even for non-existent session (idempotent)
	if w.Code != http.StatusOK {
		t.Errorf("Expected 200 for idempotent terminate, got %d", w.Code)
	}

	var resp map[string]bool
	json.NewDecoder(w.Body).Decode(&resp)
	if !resp["ok"] {
		t.Error("Expected ok: true")
	}
}
