package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/SeventeenthEarth/kkachi/internal/domain/docs"
	kkachihttp "github.com/SeventeenthEarth/kkachi/internal/interface/http"
	"github.com/SeventeenthEarth/kkachi/internal/interface/http/handler"
)

type mockGetDocsSnapshotUseCase struct {
	snapshot docs.DocsSnapshot
	commit   docs.CommitHash
	err      error
}

func (m *mockGetDocsSnapshotUseCase) Execute(ctx context.Context, project docs.ProjectName, commit docs.CommitHash) (docs.DocsSnapshot, docs.CommitHash, error) {
	return m.snapshot, m.commit, m.err
}

func TestDocsSnapshot(t *testing.T) {
	tests := []struct {
		name           string
		queryProject   string
		queryCommit    string
		mockUC         *mockGetDocsSnapshotUseCase
		wantStatus     int
		wantBodyFields map[string]interface{}
	}{
		{
			name:         "Success",
			queryProject: "proj1",
			queryCommit:  "abc",
			mockUC: &mockGetDocsSnapshotUseCase{
				snapshot: []byte("tar-content"),
				commit:   "abc",
				err:      nil,
			},
			wantStatus: http.StatusOK,
			wantBodyFields: map[string]interface{}{
				"commit":   "abc",
				"snapshot": "dGFyLWNvbnRlbnQ=", // base64 of "tar-content"
			},
		},
		{
			name:         "Missing Project",
			queryProject: "",
			mockUC:       &mockGetDocsSnapshotUseCase{},
			wantStatus:   http.StatusBadRequest,
			wantBodyFields: map[string]interface{}{
				"error": "missing_project",
			},
		},
		{
			name:         "Unknown Project",
			queryProject: "unknown",
			mockUC: &mockGetDocsSnapshotUseCase{
				err: docs.ErrUnknownProject,
			},
			wantStatus: http.StatusBadRequest,
			wantBodyFields: map[string]interface{}{
				"error": "unknown_project",
			},
		},
		{
			name:         "Unknown Commit",
			queryProject: "proj1",
			queryCommit:  "bad",
			mockUC: &mockGetDocsSnapshotUseCase{
				err: docs.ErrUnknownDocsCommit,
			},
			wantStatus: http.StatusBadRequest,
			wantBodyFields: map[string]interface{}{
				"error": "unknown_docs_commit",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := handler.NewDocsSnapshotHandler(tt.mockUC)
			srv := kkachihttp.NewHTTPServer(":0", nil, nil, nil, handler, nil, nil)

			req := httptest.NewRequest("GET", "/docs/snapshot?project="+tt.queryProject+"&commit="+tt.queryCommit, nil)
			w := httptest.NewRecorder()

			srv.Handler.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %v, want %v", w.Code, tt.wantStatus)
			}

			var body map[string]interface{}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Errorf("failed to unmarshal body: %v", err)
			}

			for k, v := range tt.wantBodyFields {
				if gotV, ok := body[k]; !ok || gotV != v {
					t.Errorf("body field %s = %v, want %v", k, gotV, v)
				}
			}
		})
	}
}
