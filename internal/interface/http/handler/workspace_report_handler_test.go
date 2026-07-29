package handler

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	workspaceUsecase "github.com/SeventeenthEarth/kkachi/internal/usecase/workspace"
)

func TestWorkspaceHandlerReportDocsHash(t *testing.T) {
	reporter := &fakeReportDocsHashUseCase{}
	handler := NewWorkspaceHandler(nil, nil, reporter)
	req := httptest.NewRequest(
		http.MethodPut,
		"/workspaces/workspace-1/docs-hash",
		bytes.NewBufferString(`{"docs_hash":"docs-2","actor_email":"actor@example.com"}`),
	)
	req.SetPathValue("workspace_id", "workspace-1")
	recorder := httptest.NewRecorder()

	handler.ReportDocsHash(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if reporter.command.WorkspaceID != "workspace-1" ||
		reporter.command.DocsHash != "docs-2" ||
		reporter.command.ActorEmail != "actor@example.com" {
		t.Fatalf("command=%+v", reporter.command)
	}
}

func TestWorkspaceHandlerReportDocsHashMapsDivergedCommit(t *testing.T) {
	handler := NewWorkspaceHandler(
		nil,
		nil,
		&fakeReportDocsHashUseCase{err: workspaceUsecase.ErrDocsHashNotInCurrentHistory},
	)
	req := httptest.NewRequest(
		http.MethodPut,
		"/workspaces/workspace-1/docs-hash",
		bytes.NewBufferString(`{"docs_hash":"diverged","actor_email":"actor@example.com"}`),
	)
	req.SetPathValue("workspace_id", "workspace-1")
	recorder := httptest.NewRecorder()

	handler.ReportDocsHash(recorder, req)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if !bytes.Contains(recorder.Body.Bytes(), []byte("docs_hash_not_in_current_history")) {
		t.Fatalf("body=%s", recorder.Body.String())
	}
}

type fakeReportDocsHashUseCase struct {
	command workspaceUsecase.ReportDocsHashCommand
	err     error
}

func (f *fakeReportDocsHashUseCase) Execute(
	_ context.Context,
	command workspaceUsecase.ReportDocsHashCommand,
) error {
	f.command = command
	return f.err
}

var _ workspaceUsecase.ReportDocsHashUseCase = (*fakeReportDocsHashUseCase)(nil)
