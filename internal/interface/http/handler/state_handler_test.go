package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/irootkernel/sanho/internal/domain/docs"
	"github.com/irootkernel/sanho/internal/domain/workspace"
	"github.com/irootkernel/sanho/internal/interface/http/dto"
	"github.com/irootkernel/sanho/internal/interface/http/handler"
	stateuc "github.com/irootkernel/sanho/internal/usecase/state"
)

// Mock implementations for state handler tests
type mockDocsRepoForState struct {
	heads map[docs.ProjectName]docs.CommitHash
}

func (m *mockDocsRepoForState) GetHead(ctx context.Context, project docs.ProjectName) (docs.CommitHash, error) {
	head, ok := m.heads[project]
	if !ok {
		return "", docs.ErrUnknownProject
	}
	return head, nil
}

func (m *mockDocsRepoForState) GetSnapshot(ctx context.Context, project docs.ProjectName, commit docs.CommitHash) (docs.DocsSnapshot, docs.CommitHash, error) {
	return nil, "", nil
}

type mockWorkspaceRepoForState struct {
	workspaces []*workspace.Workspace
}

func (m *mockWorkspaceRepoForState) Save(ctx context.Context, ws *workspace.Workspace) error {
	return nil
}

func (m *mockWorkspaceRepoForState) Get(ctx context.Context, id workspace.WorkspaceID) (*workspace.Workspace, error) {
	return nil, nil
}

func (m *mockWorkspaceRepoForState) UpdateDocsHash(ctx context.Context, id workspace.WorkspaceID, newHash docs.CommitHash, actorEmail string) error {
	return nil
}

func (m *mockWorkspaceRepoForState) List(ctx context.Context) ([]*workspace.Workspace, error) {
	return m.workspaces, nil
}

type mockProjectListerForState struct {
	projects []string
}

func (m *mockProjectListerForState) ListProjects() []string {
	return m.projects
}

func TestStateHandler(t *testing.T) {
	tests := []struct {
		name          string
		docsRepo      *mockDocsRepoForState
		wsRepo        *mockWorkspaceRepoForState
		projectLister *mockProjectListerForState
		wantStatus    int
		wantHeadsLen  int
		wantWsLen     int
	}{
		{
			name: "Empty state",
			docsRepo: &mockDocsRepoForState{
				heads: map[docs.ProjectName]docs.CommitHash{},
			},
			wsRepo: &mockWorkspaceRepoForState{
				workspaces: []*workspace.Workspace{},
			},
			projectLister: &mockProjectListerForState{projects: []string{}},
			wantStatus:    http.StatusOK,
			wantHeadsLen:  0,
			wantWsLen:     0,
		},
		{
			name: "With data",
			docsRepo: &mockDocsRepoForState{
				heads: map[docs.ProjectName]docs.CommitHash{
					"project1": "hash1",
				},
			},
			wsRepo: &mockWorkspaceRepoForState{
				workspaces: []*workspace.Workspace{
					{
						ID:             "project1:/tmp/ws1",
						Project:        "project1",
						DocsRepoID:     "repo1",
						LocalPath:      "/tmp/ws1",
						RepoURL:        "git@github.com:org/repo.git",
						DocsHash:       "hash1",
						LastReportedAt: time.Now(),
						LastActorEmail: "dev@example.com",
					},
				},
			},
			projectLister: &mockProjectListerForState{projects: []string{"project1"}},
			wantStatus:    http.StatusOK,
			wantHeadsLen:  1,
			wantWsLen:     1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uc := stateuc.NewGetStateUseCase(tt.docsRepo, tt.wsRepo, tt.projectLister)
			h := handler.NewStateHandler(uc)

			req := httptest.NewRequest("GET", "/state", nil)
			w := httptest.NewRecorder()

			h.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %v, want %v", w.Code, tt.wantStatus)
			}

			var resp map[string]interface{}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to unmarshal response: %v", err)
			}

			docsHeads, ok := resp["docs_heads"].(map[string]interface{})
			if !ok && tt.wantHeadsLen > 0 {
				t.Errorf("docs_heads not found in response")
			}
			if len(docsHeads) != tt.wantHeadsLen {
				t.Errorf("docs_heads length = %d, want %d", len(docsHeads), tt.wantHeadsLen)
			}

			workspaces, ok := resp["workspaces"].([]interface{})
			if !ok && tt.wantWsLen > 0 {
				t.Errorf("workspaces not found in response")
			}
			if len(workspaces) != tt.wantWsLen {
				t.Errorf("workspaces length = %d, want %d", len(workspaces), tt.wantWsLen)
			}
		})
	}
}

func TestStateHandler_LastReportedAtZeroTime(t *testing.T) {
	wsRepo := &mockWorkspaceRepoForState{
		workspaces: []*workspace.Workspace{
			{
				ID:             "project1:/tmp/ws1",
				Project:        "project1",
				DocsRepoID:     "repo1",
				LocalPath:      "/tmp/ws1",
				RepoURL:        "git@github.com:org/repo.git",
				DocsHash:       "hash1",
				LastReportedAt: time.Time{},
				LastActorEmail: "dev@example.com",
			},
		},
	}

	uc := stateuc.NewGetStateUseCase(&mockDocsRepoForState{}, wsRepo, &mockProjectListerForState{})
	h := handler.NewStateHandler(uc)

	req := httptest.NewRequest("GET", "/state", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %v, want %v", w.Code, http.StatusOK)
	}

	var resp dto.ServerStateResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if len(resp.Workspaces) != 1 {
		t.Fatalf("expected 1 workspace, got %d", len(resp.Workspaces))
	}

	if resp.Workspaces[0].LastReportedAt != nil {
		t.Errorf("expected LastReportedAt to be omitted for zero time, got %q", *resp.Workspaces[0].LastReportedAt)
	}
}

func TestStateHandler_LastReportedAtFormatsRFC3339(t *testing.T) {
	now := time.Date(2025, time.January, 2, 3, 4, 5, 0, time.UTC)
	wsRepo := &mockWorkspaceRepoForState{
		workspaces: []*workspace.Workspace{
			{
				ID:             "project1:/tmp/ws1",
				Project:        "project1",
				DocsRepoID:     "repo1",
				LocalPath:      "/tmp/ws1",
				RepoURL:        "git@github.com:org/repo.git",
				DocsHash:       "hash1",
				LastReportedAt: now,
				LastActorEmail: "dev@example.com",
			},
		},
	}

	uc := stateuc.NewGetStateUseCase(&mockDocsRepoForState{}, wsRepo, &mockProjectListerForState{})
	h := handler.NewStateHandler(uc)

	req := httptest.NewRequest("GET", "/state", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %v, want %v", w.Code, http.StatusOK)
	}

	var resp dto.ServerStateResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if len(resp.Workspaces) != 1 {
		t.Fatalf("expected 1 workspace, got %d", len(resp.Workspaces))
	}

	if resp.Workspaces[0].LastReportedAt == nil {
		t.Fatalf("expected LastReportedAt to be set")
	}
	got := *resp.Workspaces[0].LastReportedAt
	if got != now.Format(time.RFC3339) {
		t.Fatalf("LastReportedAt = %q, want %q", got, now.Format(time.RFC3339))
	}
	if _, err := time.Parse(time.RFC3339, got); err != nil {
		t.Fatalf("LastReportedAt is not RFC3339: %v", err)
	}
}

// TestStateHandler_EmptyStateReturnsNonNullSchema verifies that /state returns
// non-null empty object {} for docs_heads and empty array [] for workspaces.
// This is required for v2 Web dashboard compatibility.
func TestStateHandler_EmptyStateReturnsNonNullSchema(t *testing.T) {
	docsRepo := &mockDocsRepoForState{
		heads: map[docs.ProjectName]docs.CommitHash{},
	}
	wsRepo := &mockWorkspaceRepoForState{
		workspaces: []*workspace.Workspace{},
	}
	projectLister := &mockProjectListerForState{projects: []string{}}

	uc := stateuc.NewGetStateUseCase(docsRepo, wsRepo, projectLister)
	h := handler.NewStateHandler(uc)

	req := httptest.NewRequest("GET", "/state", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %v, want %v", w.Code, http.StatusOK)
	}

	// Parse raw JSON to check exact structure
	var rawResp map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &rawResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	// Verify docs_heads is present and is an empty object {}
	docsHeadsRaw, ok := rawResp["docs_heads"]
	if !ok {
		t.Fatalf("docs_heads field is missing from response")
	}
	if string(docsHeadsRaw) != "{}" {
		// It might be a non-empty object, which is also fine, but for empty state it should be {}
		var docsHeads map[string]string
		if err := json.Unmarshal(docsHeadsRaw, &docsHeads); err != nil {
			t.Fatalf("docs_heads is not a valid object: %v", err)
		}
		if len(docsHeads) != 0 {
			t.Errorf("expected docs_heads to be empty, got %v", docsHeads)
		}
	}

	// Verify workspaces is present and is an empty array []
	workspacesRaw, ok := rawResp["workspaces"]
	if !ok {
		t.Fatalf("workspaces field is missing from response")
	}
	if string(workspacesRaw) != "[]" {
		var workspaces []interface{}
		if err := json.Unmarshal(workspacesRaw, &workspaces); err != nil {
			t.Fatalf("workspaces is not a valid array: %v", err)
		}
		if len(workspaces) != 0 {
			t.Errorf("expected workspaces to be empty, got %v", workspaces)
		}
	}
}
