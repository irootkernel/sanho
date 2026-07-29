package httpclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/irootkernel/sanho/internal/domain/docs"
	"github.com/irootkernel/sanho/internal/domain/workspace"
)

func TestHTTPClient_DocsHead_Success(t *testing.T) {
	server := newUnixTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/docs/head" {
			t.Errorf("expected path /docs/head, got %s", r.URL.Path)
		}
		if r.URL.Query().Get("project") != "test-project" {
			t.Errorf("expected project query param 'test-project', got %s", r.URL.Query().Get("project"))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"head": "abc123"})
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL)
	head, err := client.DocsHead(context.Background(), "test-project")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if head != "abc123" {
		t.Errorf("expected head 'abc123', got %q", head)
	}
}

func TestHTTPClient_DocsHead_UnknownProject(t *testing.T) {
	server := newUnixTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "unknown_project"})
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL)
	_, err := client.DocsHead(context.Background(), "unknown")

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrUnknownProject) {
		t.Errorf("expected ErrUnknownProject, got %v", err)
	}
}

func TestHTTPClient_RegisterWorkspace_Success(t *testing.T) {
	server := newUnixTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/workspaces/register" {
			t.Errorf("expected path /workspaces/register, got %s", r.URL.Path)
		}

		var req RegisterWorkspaceRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Project != "sudal" {
			t.Errorf("expected project 'sudal', got %s", req.Project)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(RegisterWorkspaceResponse{
			WorkspaceID:     "ws-123",
			CurrentDocsHead: "def456",
		})
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL)
	resp, err := client.RegisterWorkspace(context.Background(), RegisterWorkspaceRequest{
		Project:    "sudal",
		LocalPath:  "/path/to/workspace",
		RepoURL:    "git@github.com:org/repo.git",
		ActorEmail: "user@example.com",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.WorkspaceID != "ws-123" {
		t.Errorf("expected workspace_id 'ws-123', got %q", resp.WorkspaceID)
	}
	if resp.CurrentDocsHead != "def456" {
		t.Errorf("expected current_docs_head 'def456', got %q", resp.CurrentDocsHead)
	}
}

func TestHTTPClient_DocsPush_RetryOnBusy(t *testing.T) {
	attempts := 0
	server := newUnixTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]string{"error": "docs_repo_busy"})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(DocsPushResponse{
			Status:      docs.DocsPushStatusUpdated,
			NewDocsHash: "new123",
		})
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, WithRetryDelay(10*time.Millisecond))
	resp, err := client.DocsPush(context.Background(), DocsPushRequest{
		WorkspaceID:  "ws-123",
		BaseDocsHash: "base123",
		DocsSnapshot: "c25hcHNob3Q=", // base64-encoded "snapshot"
		ActorEmail:   "user@example.com",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
	if resp.Status != docs.DocsPushStatusUpdated {
		t.Errorf("expected status 'updated', got %q", resp.Status)
	}
}

func TestHTTPClient_DocsPush_MaxRetriesExceeded(t *testing.T) {
	server := newUnixTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{"error": "docs_repo_busy"})
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, WithMaxRetries(2), WithRetryDelay(10*time.Millisecond))
	_, err := client.DocsPush(context.Background(), DocsPushRequest{
		WorkspaceID:  "ws-123",
		BaseDocsHash: "base123",
	})

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrDocsRepoBusy) {
		t.Errorf("expected ErrDocsRepoBusy, got %v", err)
	}
}

func TestHTTPClient_DeleteProject_Success(t *testing.T) {
	server := newUnixTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		if r.URL.Path != "/projects/test-project" {
			t.Errorf("expected path /projects/test-project, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL)
	err := client.DeleteProject(context.Background(), "test-project", false)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHTTPClient_DeleteProject_HasWorkspaces(t *testing.T) {
	server := newUnixTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{"error": "project_has_workspaces"})
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL)
	err := client.DeleteProject(context.Background(), "test-project", false)

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrProjectHasWorkspaces) {
		t.Errorf("expected ErrProjectHasWorkspaces, got %v", err)
	}
}

func TestHTTPClient_DeleteWorkspace_UnknownWorkspace(t *testing.T) {
	server := newUnixTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "unknown_workspace"})
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL)
	err := client.DeleteWorkspace(context.Background(), "unknown-ws")

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrUnknownWorkspace) {
		t.Errorf("expected ErrUnknownWorkspace, got %v", err)
	}
}

func TestHTTPClient_DeleteWorkspace_WorkspaceNotFound(t *testing.T) {
	server := newUnixTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "workspace_not_found"})
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL)
	err := client.DeleteWorkspace(context.Background(), "unknown-ws")

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrUnknownWorkspace) {
		t.Errorf("expected ErrUnknownWorkspace, got %v", err)
	}
}

func TestHTTPClient_GetState_Success(t *testing.T) {
	server := newUnixTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/state" {
			t.Errorf("expected path /state, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(StateResponse{
			DocsHeads: map[string]string{
				"sudal": "abc123",
			},
			Workspaces: []WorkspaceSummary{
				{
					WorkspaceID: "ws-1",
					Project:     "sudal",
					LocalPath:   "/path",
					DocsHash:    "abc123",
				},
			},
		})
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL)
	resp, err := client.GetState(context.Background(), nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.DocsHeads) != 1 {
		t.Errorf("expected 1 project in DocsHeads, got %d", len(resp.DocsHeads))
	}
	if len(resp.Workspaces) != 1 {
		t.Errorf("expected 1 workspace, got %d", len(resp.Workspaces))
	}
}

func TestHTTPClientGetProjectStatus(t *testing.T) {
	server := newUnixTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/projects/test-project/status" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.URL.Query().Get("workspace_id") != "ws-1" || r.URL.Query().Get("docs_hash") != "base" {
			t.Errorf("query = %v", r.URL.Query())
		}
		_ = json.NewEncoder(w).Encode(ProjectStatusResponse{
			Project:              "test-project",
			ReferenceWorkspaceID: "ws-1",
			ReferenceDocsHash:    "base",
			DocsHead:             "head",
			ReferenceToHead: CommitRelation{
				Status: docs.CommitRelationBehind,
				Behind: 1,
			},
			Workspaces: []ProjectStatusWorkspace{{
				WorkspaceID: "ws-2",
				DocsHash:    "head",
				RelativeToReference: CommitRelation{
					Status: docs.CommitRelationAhead,
					Ahead:  1,
				},
			}},
		})
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL)
	response, err := client.GetProjectStatus(context.Background(), "test-project", "ws-1", "base")
	if err != nil {
		t.Fatalf("GetProjectStatus() error = %v", err)
	}
	if response.DocsHead != "head" || response.ReferenceToHead.Behind != 1 {
		t.Fatalf("response = %#v", response)
	}
	if len(response.Workspaces) != 1 || response.Workspaces[0].RelativeToReference.Status != docs.CommitRelationAhead {
		t.Fatalf("workspaces = %#v", response.Workspaces)
	}
}

func TestHTTPClientGetProjectStatusProjectMismatch(t *testing.T) {
	server := newUnixTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "workspace_project_mismatch"})
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL)
	_, err := client.GetProjectStatus(context.Background(), "test-project", "ws-1", "base")
	if !errors.Is(err, ErrWorkspaceProjectMismatch) {
		t.Fatalf("GetProjectStatus() error = %v, want workspace project mismatch", err)
	}
}

func TestHTTPClientGetProjectStatusEndpointNotFound(t *testing.T) {
	server := newUnixTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "not_found"})
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL)
	_, err := client.GetProjectStatus(context.Background(), "test-project", "ws-1", "base")
	if !errors.Is(err, ErrEndpointNotFound) {
		t.Fatalf("GetProjectStatus() error = %v, want endpoint not found", err)
	}
}

func TestHTTPClientReportWorkspaceDocsHash(t *testing.T) {
	server := newUnixTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Fatalf("method=%s want PUT", r.Method)
		}
		if r.URL.EscapedPath() != "/workspaces/workspace%2Fone/docs-hash" {
			t.Fatalf("path=%q", r.URL.EscapedPath())
		}
		var req ReportWorkspaceDocsHashRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req.DocsHash != "docs-2" || req.ActorEmail != "actor@example.com" {
			t.Fatalf("request=%+v", req)
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
	defer server.Close()

	err := NewHTTPClient(server.URL).ReportWorkspaceDocsHash(
		context.Background(),
		workspace.WorkspaceID("workspace/one"),
		ReportWorkspaceDocsHashRequest{
			DocsHash:   "docs-2",
			ActorEmail: "actor@example.com",
		},
	)
	if err != nil {
		t.Fatalf("ReportWorkspaceDocsHash() error=%v", err)
	}
}

func TestHTTPClientReportWorkspaceDocsHashRejectsDivergedCommit(t *testing.T) {
	server := newUnixTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "docs_hash_not_in_current_history"})
	}))
	defer server.Close()

	err := NewHTTPClient(server.URL).ReportWorkspaceDocsHash(
		context.Background(),
		"workspace-1",
		ReportWorkspaceDocsHashRequest{DocsHash: "diverged", ActorEmail: "actor@example.com"},
	)
	if !errors.Is(err, ErrDocsHashNotInCurrentHistory) {
		t.Fatalf("error=%v want %v", err, ErrDocsHashNotInCurrentHistory)
	}
}
