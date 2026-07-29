package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"

	"github.com/irootkernel/sanho/internal/infra/state"
	"github.com/irootkernel/sanho/internal/interface/http/handler"
	"github.com/irootkernel/sanho/internal/usecase/workspace"
)

func TestWorkspaceHandler_Delete(t *testing.T) {
	tempDir := t.TempDir()

	statePath := filepath.Join(tempDir, "state.json")
	stateRepo, err := state.NewFileStateRepository(statePath)
	if err != nil {
		t.Fatalf("failed to create state repo: %v", err)
	}

	// Seed two workspaces
	ws1 := state.WorkspaceState{ID: "proj:/tmp/ws1", Project: "proj", DocsRepoID: "repo1"}
	ws2 := state.WorkspaceState{ID: "proj:/tmp/ws2", Project: "proj", DocsRepoID: "repo1"}
	if err := stateRepo.AddWorkspace(ws1); err != nil {
		t.Fatalf("failed to add ws1: %v", err)
	}
	if err := stateRepo.AddWorkspace(ws2); err != nil {
		t.Fatalf("failed to add ws2: %v", err)
	}

	deleteUC := workspace.NewDeleteWorkspaceUseCase(stateRepo)
	wsHandler := handler.NewWorkspaceHandler(deleteUC, nil, nil)

	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /workspaces/{workspace_id}", wsHandler.Delete)
	server := httptest.NewServer(mux)
	defer server.Close()

	client := server.Client()
	encodedID := url.PathEscape(ws1.ID)

	// First delete should succeed and remove only the targeted workspace.
	req, _ := http.NewRequest(http.MethodDelete, server.URL+"/workspaces/"+encodedID, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("delete request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var okResp map[string]bool
	if err := json.NewDecoder(resp.Body).Decode(&okResp); err != nil {
		t.Fatalf("failed to decode success response: %v", err)
	}
	if !okResp["ok"] {
		t.Fatalf("expected ok=true, got %v", okResp)
	}

	// Reload state file to verify persistence.
	reloadedRepo, err := state.NewFileStateRepository(statePath)
	if err != nil {
		t.Fatalf("failed to reload state repo: %v", err)
	}
	if _, ok := reloadedRepo.GetWorkspace(ws1.ID); ok {
		t.Fatalf("workspace %s should be deleted", ws1.ID)
	}
	if _, ok := reloadedRepo.GetWorkspace(ws2.ID); !ok {
		t.Fatalf("workspace %s should remain", ws2.ID)
	}

	// Second delete should return 404 unknown_workspace.
	req, _ = http.NewRequest(http.MethodDelete, server.URL+"/workspaces/"+encodedID, nil)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("second delete request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
	var errResp map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp["error"] != "unknown_workspace" {
		t.Fatalf("expected error unknown_workspace, got %v", errResp["error"])
	}
}
