package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/irootkernel/sanho/internal/domain/docs"
	"github.com/irootkernel/sanho/internal/domain/workspace"
	"github.com/irootkernel/sanho/internal/interface/http/dto"
	"github.com/irootkernel/sanho/internal/usecase/project"
)

type ProjectStatusHandler struct {
	useCase ProjectStatusUseCase
}

type ProjectStatusUseCase interface {
	Execute(
		ctx context.Context,
		projectName docs.ProjectName,
		referenceWorkspaceID workspace.WorkspaceID,
		referenceDocsHash docs.CommitHash,
	) (project.ProjectStatusResult, error)
}

func NewProjectStatusHandler(useCase ProjectStatusUseCase) *ProjectStatusHandler {
	return &ProjectStatusHandler{useCase: useCase}
}

func (h *ProjectStatusHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	projectName := r.PathValue("project")
	workspaceID := r.URL.Query().Get("workspace_id")
	docsHash := r.URL.Query().Get("docs_hash")
	if projectName == "" || workspaceID == "" || docsHash == "" {
		jsonError(w, "missing_required_parameter", http.StatusBadRequest)
		return
	}

	result, err := h.useCase.Execute(
		r.Context(),
		docs.ProjectName(projectName),
		workspace.WorkspaceID(workspaceID),
		docs.CommitHash(docsHash),
	)
	if err != nil {
		switch {
		case errors.Is(err, docs.ErrUnknownProject):
			jsonError(w, "unknown_project", http.StatusBadRequest)
		case errors.Is(err, docs.ErrUnknownWorkspace):
			jsonError(w, "unknown_workspace", http.StatusNotFound)
		case errors.Is(err, docs.ErrWorkspaceProjectMismatch):
			jsonError(w, "workspace_project_mismatch", http.StatusBadRequest)
		case errors.Is(err, docs.ErrUnknownDocsCommit):
			jsonError(w, "unknown_docs_commit", http.StatusBadRequest)
		default:
			log.Printf("get project status failed: %v", err)
			jsonError(w, "internal_server_error", http.StatusInternalServerError)
		}
		return
	}

	response := dto.ProjectStatusResponse{
		Project:              string(result.Project),
		ReferenceWorkspaceID: string(result.ReferenceWorkspaceID),
		ReferenceDocsHash:    string(result.ReferenceDocsHash),
		DocsHead:             string(result.DocsHead),
		ReferenceToHead:      relationDTO(result.ReferenceToHead),
		Workspaces:           make([]dto.ProjectStatusWorkspace, 0, len(result.Workspaces)),
	}
	for _, item := range result.Workspaces {
		ws := item.Workspace
		var lastReportedAt *string
		if !ws.LastReportedAt.IsZero() {
			formatted := ws.LastReportedAt.Format(time.RFC3339)
			lastReportedAt = &formatted
		}
		response.Workspaces = append(response.Workspaces, dto.ProjectStatusWorkspace{
			WorkspaceID:         string(ws.ID),
			Project:             string(ws.Project),
			DocsRepoID:          string(ws.DocsRepoID),
			LocalPath:           ws.LocalPath,
			RepoURL:             ws.RepoURL,
			DocsHash:            string(ws.DocsHash),
			LastReportedAt:      lastReportedAt,
			LastActorEmail:      ws.LastActorEmail,
			RelativeToReference: relationDTO(item.RelativeToReference),
			RelativeToHead:      relationDTO(item.RelativeToHead),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("failed to write project status response: %v", err)
	}
}

func relationDTO(relation docs.CommitRelation) dto.CommitRelation {
	return dto.CommitRelation{
		Status: string(relation.Status),
		Ahead:  relation.Ahead,
		Behind: relation.Behind,
	}
}
