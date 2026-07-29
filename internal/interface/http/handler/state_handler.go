package handler

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/irootkernel/sanho/internal/interface/http/dto"
	"github.com/irootkernel/sanho/internal/usecase/state"
)

// StateHandler handles GET /state requests.
type StateHandler struct {
	useCase state.GetStateUseCase
}

// NewStateHandler creates a new StateHandler instance.
func NewStateHandler(uc state.GetStateUseCase) *StateHandler {
	return &StateHandler{useCase: uc}
}

// ServeHTTP implements http.Handler for GET /state.
func (h *StateHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	result, err := h.useCase.Execute(ctx)
	if err != nil {
		// TODO: replace with structured logger if/when available
		log.Printf("StateHandler: failed to get state: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "internal_server_error"})
		return
	}

	// Convert domain types to DTOs
	docsHeads := make(map[string]string)
	for project, head := range result.DocsHeads {
		docsHeads[string(project)] = string(head)
	}

	workspaceSummaries := make([]dto.WorkspaceSummary, 0, len(result.Workspaces))
	for _, ws := range result.Workspaces {
		var lastReportedAt *string
		if !ws.LastReportedAt.IsZero() {
			formatted := ws.LastReportedAt.Format(time.RFC3339)
			lastReportedAt = &formatted
		}

		workspaceSummaries = append(workspaceSummaries, dto.WorkspaceSummary{
			WorkspaceID:    string(ws.ID),
			Project:        string(ws.Project),
			DocsRepoID:     string(ws.DocsRepoID),
			LocalPath:      ws.LocalPath,
			RepoURL:        ws.RepoURL,
			DocsHash:       string(ws.DocsHash),
			LastReportedAt: lastReportedAt,
			LastActorEmail: ws.LastActorEmail,
		})
	}

	resp := dto.ServerStateResponse{
		DocsHeads:  docsHeads,
		Workspaces: workspaceSummaries,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
