package handler_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	domain "github.com/SeventeenthEarth/kkachi/internal/domain/docs"
	"github.com/SeventeenthEarth/kkachi/internal/interface/http/dto"
	"github.com/SeventeenthEarth/kkachi/internal/interface/http/handler"
	uc "github.com/SeventeenthEarth/kkachi/internal/usecase/docs"
)

type mockPushDocsUseCase struct {
	result domain.DocsPushResult
	err    error
}

func (m *mockPushDocsUseCase) Execute(ctx context.Context, cmd uc.PushDocsCommand) (domain.DocsPushResult, error) {
	return m.result, m.err
}

func TestDocsPushHandler(t *testing.T) {
	newHead := domain.CommitHash("newhead123")

	tests := []struct {
		name           string
		method         string
		body           dto.DocsPushRequest
		mockUC         *mockPushDocsUseCase
		wantStatus     int
		wantOk         bool
		wantPushStatus string
		wantError      string
	}{
		{
			name:   "Success - Updated",
			method: http.MethodPost,
			body: dto.DocsPushRequest{
				WorkspaceID:  "ws1",
				BaseDocsHash: "base123",
				DocsSnapshot: base64.StdEncoding.EncodeToString([]byte("snapshot")),
				ActorEmail:   "user@test.com",
			},
			mockUC: &mockPushDocsUseCase{
				result: domain.DocsPushResult{
					Status:  domain.DocsPushStatusUpdated,
					NewHead: &newHead,
				},
			},
			wantStatus:     http.StatusOK,
			wantOk:         true,
			wantPushStatus: "updated",
		},
		{
			name:   "Success - NoChange",
			method: http.MethodPost,
			body: dto.DocsPushRequest{
				WorkspaceID:  "ws1",
				BaseDocsHash: "base123",
				DocsSnapshot: base64.StdEncoding.EncodeToString([]byte("snapshot")),
				ActorEmail:   "user@test.com",
			},
			mockUC: &mockPushDocsUseCase{
				result: domain.DocsPushResult{
					Status:      domain.DocsPushStatusNoChange,
					CurrentHead: "currenthead123",
				},
			},
			wantStatus:     http.StatusOK,
			wantOk:         true,
			wantPushStatus: "nochange",
		},
		{
			name:   "Success - Outdated",
			method: http.MethodPost,
			body: dto.DocsPushRequest{
				WorkspaceID:  "ws1",
				BaseDocsHash: "oldbase",
				DocsSnapshot: base64.StdEncoding.EncodeToString([]byte("snapshot")),
				ActorEmail:   "user@test.com",
			},
			mockUC: &mockPushDocsUseCase{
				result: domain.DocsPushResult{
					Status:      domain.DocsPushStatusOutdated,
					CurrentHead: "latesthead123",
				},
			},
			wantStatus:     http.StatusOK,
			wantOk:         true,
			wantPushStatus: "outdated",
		},
		{
			name:   "Error - Unknown Workspace",
			method: http.MethodPost,
			body: dto.DocsPushRequest{
				WorkspaceID:  "unknown",
				BaseDocsHash: "base123",
				DocsSnapshot: base64.StdEncoding.EncodeToString([]byte("snapshot")),
				ActorEmail:   "user@test.com",
			},
			mockUC: &mockPushDocsUseCase{
				err: domain.ErrUnknownWorkspace,
			},
			wantStatus: http.StatusBadRequest,
			wantOk:     false,
			wantError:  "unknown_workspace",
		},
		{
			name:   "Error - Docs Repo Busy",
			method: http.MethodPost,
			body: dto.DocsPushRequest{
				WorkspaceID:  "ws1",
				BaseDocsHash: "base123",
				DocsSnapshot: base64.StdEncoding.EncodeToString([]byte("snapshot")),
				ActorEmail:   "user@test.com",
			},
			mockUC: &mockPushDocsUseCase{
				err: domain.ErrDocsRepoBusy,
			},
			wantStatus: http.StatusConflict,
			wantOk:     false,
			wantError:  "docs_repo_busy",
		},
		{
			name:   "Error - Unknown Docs Commit",
			method: http.MethodPost,
			body: dto.DocsPushRequest{
				WorkspaceID:  "ws1",
				BaseDocsHash: "badcommit",
				DocsSnapshot: base64.StdEncoding.EncodeToString([]byte("snapshot")),
				ActorEmail:   "user@test.com",
			},
			mockUC: &mockPushDocsUseCase{
				err: domain.ErrUnknownDocsCommit,
			},
			wantStatus: http.StatusBadRequest,
			wantOk:     false,
			wantError:  "unknown_docs_commit",
		},
		{
			name:   "Error - Missing WorkspaceID",
			method: http.MethodPost,
			body: dto.DocsPushRequest{
				WorkspaceID:  "",
				BaseDocsHash: "base123",
				DocsSnapshot: base64.StdEncoding.EncodeToString([]byte("snapshot")),
				ActorEmail:   "user@test.com",
			},
			mockUC:     &mockPushDocsUseCase{},
			wantStatus: http.StatusBadRequest,
			wantError:  "missing_workspace_id",
		},
		{
			name:   "Error - Missing BaseDocsHash",
			method: http.MethodPost,
			body: dto.DocsPushRequest{
				WorkspaceID:  "ws1",
				BaseDocsHash: "",
				DocsSnapshot: base64.StdEncoding.EncodeToString([]byte("snapshot")),
				ActorEmail:   "user@test.com",
			},
			mockUC:     &mockPushDocsUseCase{},
			wantStatus: http.StatusBadRequest,
			wantError:  "missing_base_docs_hash",
		},
		{
			name:   "Error - Missing DocsSnapshot",
			method: http.MethodPost,
			body: dto.DocsPushRequest{
				WorkspaceID:  "ws1",
				BaseDocsHash: "base123",
				DocsSnapshot: "",
				ActorEmail:   "user@test.com",
			},
			mockUC:     &mockPushDocsUseCase{},
			wantStatus: http.StatusBadRequest,
			wantError:  "missing_docs_snapshot",
		},
		{
			name:   "Error - Invalid ActorEmail Format",
			method: http.MethodPost,
			body: dto.DocsPushRequest{
				WorkspaceID:  "ws1",
				BaseDocsHash: "base123",
				DocsSnapshot: base64.StdEncoding.EncodeToString([]byte("snapshot")),
				ActorEmail:   "invalid-email",
			},
			mockUC:     &mockPushDocsUseCase{},
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid_actor_email_format",
		},
		{
			name:   "Error - Invalid Base64 Snapshot",
			method: http.MethodPost,
			body: dto.DocsPushRequest{
				WorkspaceID:  "ws1",
				BaseDocsHash: "base123",
				DocsSnapshot: "not-valid-base64!!!",
				ActorEmail:   "user@test.com",
			},
			mockUC:     &mockPushDocsUseCase{},
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid_docs_snapshot_encoding",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := handler.NewDocsPushHandler(tt.mockUC)

			bodyBytes, _ := json.Marshal(tt.body)
			req := httptest.NewRequest(tt.method, "/docs/push", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			h.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("Status code = %d, want %d", w.Code, tt.wantStatus)
			}

			var resp dto.DocsPushResponse
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				t.Fatalf("Failed to decode response: %v", err)
			}

			if resp.Ok != tt.wantOk {
				t.Errorf("Response Ok = %v, want %v", resp.Ok, tt.wantOk)
			}

			if tt.wantPushStatus != "" && resp.Status != tt.wantPushStatus {
				t.Errorf("Response Status = %v, want %v", resp.Status, tt.wantPushStatus)
			}

			if tt.wantError != "" && resp.Error != tt.wantError {
				t.Errorf("Response Error = %v, want %v", resp.Error, tt.wantError)
			}
		})
	}
}

func TestDocsPushHandler_MethodNotAllowed(t *testing.T) {
	h := handler.NewDocsPushHandler(&mockPushDocsUseCase{})

	req := httptest.NewRequest(http.MethodGet, "/docs/push", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Status code = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}
