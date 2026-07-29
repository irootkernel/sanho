package handler

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/irootkernel/sanho/internal/domain/docs"
	workspaceDomain "github.com/irootkernel/sanho/internal/domain/workspace"
	"github.com/irootkernel/sanho/internal/interface/http/dto"
	"github.com/irootkernel/sanho/internal/usecase/workspace"
)

type WorkspaceHandler struct {
	deleteUC   *workspace.DeleteWorkspaceUseCase
	registerUC workspace.RegisterWorkspaceUseCase
	reportUC   workspace.ReportDocsHashUseCase
}

func NewWorkspaceHandler(
	deleteUC *workspace.DeleteWorkspaceUseCase,
	registerUC workspace.RegisterWorkspaceUseCase,
	reportUC workspace.ReportDocsHashUseCase,
) *WorkspaceHandler {
	return &WorkspaceHandler{
		deleteUC:   deleteUC,
		registerUC: registerUC,
		reportUC:   reportUC,
	}
}

func jsonError(w http.ResponseWriter, errorMsg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": errorMsg}); err != nil {
		log.Printf("failed to write json error response: %v", err)
	}
}

func (h *WorkspaceHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req dto.RegisterWorkspaceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid_request_body", http.StatusBadRequest)
		return
	}
	if req.Project == "" || req.LocalPath == "" || req.RepoURL == "" || req.ActorEmail == "" {
		jsonError(w, "invalid_request_body: missing required fields", http.StatusBadRequest)
		return
	}

	ws, err := h.registerUC.Execute(r.Context(), workspace.RegisterWorkspaceCommand{
		Project:    req.Project,
		LocalPath:  req.LocalPath,
		RepoURL:    req.RepoURL,
		ActorEmail: req.ActorEmail,
	})
	if err != nil {
		if errors.Is(err, docs.ErrUnknownProject) {
			jsonError(w, "unknown_project", http.StatusBadRequest)
			return
		}
		log.Printf("register workspace failed: %v", err)
		jsonError(w, "internal_server_error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(dto.RegisterWorkspaceResponse{
		WorkspaceID:     string(ws.ID),
		CurrentDocsHead: string(ws.DocsHash),
	}); err != nil {
		log.Printf("failed to write register workspace response: %v", err)
	}
}

func (h *WorkspaceHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("workspace_id")

	if err := h.deleteUC.Execute(id); err != nil {
		w.Header().Set("Content-Type", "application/json")
		if err == workspace.ErrUnknownWorkspace {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "unknown_workspace"})
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func (h *WorkspaceHandler) ReportDocsHash(w http.ResponseWriter, r *http.Request) {
	if h.reportUC == nil {
		jsonError(w, "not_found", http.StatusNotFound)
		return
	}

	var req dto.ReportWorkspaceDocsHashRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.DocsHash == "" || req.ActorEmail == "" {
		jsonError(w, "invalid_request_body", http.StatusBadRequest)
		return
	}

	err := h.reportUC.Execute(r.Context(), workspace.ReportDocsHashCommand{
		WorkspaceID: workspaceDomain.WorkspaceID(r.PathValue("workspace_id")),
		DocsHash:    docs.CommitHash(req.DocsHash),
		ActorEmail:  req.ActorEmail,
	})
	if err != nil {
		switch {
		case errors.Is(err, workspace.ErrUnknownWorkspace):
			jsonError(w, "unknown_workspace", http.StatusNotFound)
		case errors.Is(err, docs.ErrUnknownDocsCommit):
			jsonError(w, "unknown_docs_commit", http.StatusBadRequest)
		case errors.Is(err, workspace.ErrDocsHashNotInCurrentHistory):
			jsonError(w, "docs_hash_not_in_current_history", http.StatusConflict)
		default:
			log.Printf("report workspace docs hash failed: %v", err)
			jsonError(w, "internal_server_error", http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}
