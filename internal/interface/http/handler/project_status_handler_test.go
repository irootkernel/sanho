package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/SeventeenthEarth/kkachi/internal/domain/docs"
	"github.com/SeventeenthEarth/kkachi/internal/domain/workspace"
	"github.com/SeventeenthEarth/kkachi/internal/interface/http/dto"
	"github.com/SeventeenthEarth/kkachi/internal/interface/http/handler"
	"github.com/SeventeenthEarth/kkachi/internal/usecase/project"
)

type projectStatusUseCase struct {
	result      project.ProjectStatusResult
	err         error
	project     docs.ProjectName
	workspaceID workspace.WorkspaceID
	docsHash    docs.CommitHash
}

func (u *projectStatusUseCase) Execute(
	_ context.Context,
	projectName docs.ProjectName,
	referenceWorkspaceID workspace.WorkspaceID,
	referenceDocsHash docs.CommitHash,
) (project.ProjectStatusResult, error) {
	u.project = projectName
	u.workspaceID = referenceWorkspaceID
	u.docsHash = referenceDocsHash
	return u.result, u.err
}

func TestProjectStatusHandlerSuccess(t *testing.T) {
	useCase := &projectStatusUseCase{result: project.ProjectStatusResult{
		Project:              "alpha",
		ReferenceWorkspaceID: "ws-current",
		ReferenceDocsHash:    "base",
		DocsHead:             "head",
		ReferenceToHead:      docs.CommitRelation{Status: docs.CommitRelationBehind, Behind: 1},
		Workspaces: []project.ProjectStatusWorkspace{{
			Workspace: &workspace.Workspace{
				ID:         "ws-peer",
				Project:    "alpha",
				DocsRepoID: "docs",
				LocalPath:  "/tmp/peer",
				RepoURL:    "git@github.com:org/peer.git",
				DocsHash:   "head",
			},
			RelativeToReference: docs.CommitRelation{Status: docs.CommitRelationAhead, Ahead: 1},
			RelativeToHead:      docs.CommitRelation{Status: docs.CommitRelationSame},
		}},
	}}
	statusHandler := handler.NewProjectStatusHandler(useCase)
	request := httptest.NewRequest(http.MethodGet, "/projects/alpha/status?workspace_id=ws-current&docs_hash=base", nil)
	request.SetPathValue("project", "alpha")
	recorder := httptest.NewRecorder()

	statusHandler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response dto.ProjectStatusResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if useCase.project != "alpha" || useCase.workspaceID != "ws-current" || useCase.docsHash != "base" {
		t.Fatalf("input project=%q workspace=%q hash=%q", useCase.project, useCase.workspaceID, useCase.docsHash)
	}
	if response.DocsHead != "head" || len(response.Workspaces) != 1 {
		t.Fatalf("response = %#v", response)
	}
	if response.Workspaces[0].RelativeToReference.Status != "ahead" {
		t.Fatalf("workspace relation = %#v", response.Workspaces[0].RelativeToReference)
	}
}

func TestProjectStatusHandlerMissingParameter(t *testing.T) {
	statusHandler := handler.NewProjectStatusHandler(&projectStatusUseCase{})
	request := httptest.NewRequest(http.MethodGet, "/projects/alpha/status", nil)
	request.SetPathValue("project", "alpha")
	recorder := httptest.NewRecorder()

	statusHandler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestProjectStatusHandlerErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		code int
		body string
	}{
		{"unknown project", docs.ErrUnknownProject, http.StatusBadRequest, "unknown_project"},
		{"unknown workspace", docs.ErrUnknownWorkspace, http.StatusNotFound, "unknown_workspace"},
		{"project mismatch", docs.ErrWorkspaceProjectMismatch, http.StatusBadRequest, "workspace_project_mismatch"},
		{"unknown commit", docs.ErrUnknownDocsCommit, http.StatusBadRequest, "unknown_docs_commit"},
		{"internal", errors.New("boom"), http.StatusInternalServerError, "internal_server_error"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			statusHandler := handler.NewProjectStatusHandler(&projectStatusUseCase{err: test.err})
			request := httptest.NewRequest(http.MethodGet, "/projects/alpha/status?workspace_id=ws&docs_hash=hash", nil)
			request.SetPathValue("project", "alpha")
			recorder := httptest.NewRecorder()

			statusHandler.ServeHTTP(recorder, request)

			if recorder.Code != test.code {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
			var response map[string]string
			if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
				t.Fatal(err)
			}
			if response["error"] != test.body {
				t.Fatalf("error = %q, want %q", response["error"], test.body)
			}
		})
	}
}
