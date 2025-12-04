package handler

import (
	"encoding/json"
	"net/http"

	"github.com/SeventeenthEarth/kkachi/internal/usecase/workspace"
)

type WorkspaceHandler struct {
	deleteUC *workspace.DeleteWorkspaceUseCase
}

func NewWorkspaceHandler(deleteUC *workspace.DeleteWorkspaceUseCase) *WorkspaceHandler {
	return &WorkspaceHandler{deleteUC: deleteUC}
}

func (h *WorkspaceHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("workspace_id")

	if err := h.deleteUC.Execute(id); err != nil {
		w.Header().Set("Content-Type", "application/json")
		if err == workspace.ErrWorkspaceNotFound {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "workspace_not_found"})
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}
