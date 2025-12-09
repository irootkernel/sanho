package handler

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/SeventeenthEarth/kkachi/internal/domain/docs"
	"github.com/SeventeenthEarth/kkachi/internal/interface/http/dto"
	"github.com/SeventeenthEarth/kkachi/internal/usecase/workspace"
)

type WorkspaceHandler struct {
	deleteUC   *workspace.DeleteWorkspaceUseCase
	registerUC workspace.RegisterWorkspaceUseCase
}

func NewWorkspaceHandler(deleteUC *workspace.DeleteWorkspaceUseCase, registerUC workspace.RegisterWorkspaceUseCase) *WorkspaceHandler {
	return &WorkspaceHandler{
		deleteUC:   deleteUC,
		registerUC: registerUC,
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
