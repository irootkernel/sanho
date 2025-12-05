package handler

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	domain "github.com/SeventeenthEarth/kkachi/internal/domain/docs"
	"github.com/SeventeenthEarth/kkachi/internal/interface/http/dto"
	"github.com/SeventeenthEarth/kkachi/internal/interface/http/httputil"
	usecase "github.com/SeventeenthEarth/kkachi/internal/usecase/docs"
)

type DocsSnapshotHandler struct {
	uc usecase.GetDocsSnapshotUseCase
}

func NewDocsSnapshotHandler(uc usecase.GetDocsSnapshotUseCase) *DocsSnapshotHandler {
	return &DocsSnapshotHandler{uc: uc}
}

func (h *DocsSnapshotHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	if project == "" {
		httputil.WriteJSONError(w, http.StatusBadRequest, "missing_project")
		return
	}
	commit := r.URL.Query().Get("commit")

	snapshot, head, err := h.uc.Execute(r.Context(), domain.ProjectName(project), domain.CommitHash(commit))
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrUnknownProject):
			httputil.WriteJSONError(w, http.StatusBadRequest, "unknown_project")
		case errors.Is(err, domain.ErrUnknownDocsCommit):
			httputil.WriteJSONError(w, http.StatusBadRequest, "unknown_docs_commit")
		default:
			log.Printf("get docs snapshot failed: %v", err)
			httputil.WriteJSONError(w, http.StatusInternalServerError, "internal_server_error")
		}
		return
	}

	resp := dto.GetSnapshotResponse{
		Commit:   string(head),
		Snapshot: base64.StdEncoding.EncodeToString(snapshot),
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("failed to write snapshot response: %v", err)
	}
}
