package handler

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/mail"

	domain "github.com/SeventeenthEarth/kkachi/internal/domain/docs"
	"github.com/SeventeenthEarth/kkachi/internal/interface/http/dto"
	usecase "github.com/SeventeenthEarth/kkachi/internal/usecase/docs"
)

type DocsPushHandler struct {
	uc usecase.PushDocsUseCase
}

func NewDocsPushHandler(uc usecase.PushDocsUseCase) *DocsPushHandler {
	return &DocsPushHandler{uc: uc}
}

func (h *DocsPushHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeErrorResponse(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}

	var req dto.DocsPushRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeErrorResponse(w, http.StatusBadRequest, "invalid_request_body")
		return
	}

	if req.WorkspaceID == "" {
		h.writeErrorResponse(w, http.StatusBadRequest, "missing_workspace_id")
		return
	}
	if req.BaseDocsHash == "" {
		h.writeErrorResponse(w, http.StatusBadRequest, "missing_base_docs_hash")
		return
	}
	if req.DocsSnapshot == "" {
		h.writeErrorResponse(w, http.StatusBadRequest, "missing_docs_snapshot")
		return
	}
	if req.ActorEmail == "" {
		h.writeErrorResponse(w, http.StatusBadRequest, "missing_actor_email")
		return
	}
	if _, err := mail.ParseAddress(req.ActorEmail); err != nil {
		h.writeErrorResponse(w, http.StatusBadRequest, "invalid_actor_email_format")
		return
	}

	// Decode base64 snapshot
	snapshotBytes, err := base64.StdEncoding.DecodeString(req.DocsSnapshot)
	if err != nil {
		h.writeErrorResponse(w, http.StatusBadRequest, "invalid_docs_snapshot_encoding")
		return
	}

	result, err := h.uc.Execute(r.Context(), usecase.PushDocsCommand{
		WorkspaceID:  req.WorkspaceID,
		BaseDocsHash: domain.CommitHash(req.BaseDocsHash),
		DocsSnapshot: domain.DocsSnapshot(snapshotBytes),
		ActorEmail:   req.ActorEmail,
	})
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrUnknownWorkspace):
			h.writeErrorResponse(w, http.StatusBadRequest, "unknown_workspace")
		case errors.Is(err, domain.ErrDocsRepoBusy):
			h.writeErrorResponse(w, http.StatusConflict, "docs_repo_busy")
		case errors.Is(err, domain.ErrUnknownDocsCommit):
			h.writeErrorResponse(w, http.StatusBadRequest, "unknown_docs_commit")
		default:
			log.Printf("push docs failed: %v", err)
			h.writeErrorResponse(w, http.StatusInternalServerError, "internal_server_error")
		}
		return
	}

	resp := dto.DocsPushResponse{
		Ok:     true,
		Status: string(result.Status),
	}

	if result.NewHead != nil {
		resp.NewDocsHash = string(*result.NewHead)
	}
	if !result.CurrentHead.IsZero() {
		resp.CurrentDocsHash = string(result.CurrentHead)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("failed to write push response: %v", err)
	}
}

func (h *DocsPushHandler) writeErrorResponse(w http.ResponseWriter, status int, errMsg string) {
	resp := dto.DocsPushResponse{
		Ok:    false,
		Error: errMsg,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("failed to write error response: %v", err)
	}
}
