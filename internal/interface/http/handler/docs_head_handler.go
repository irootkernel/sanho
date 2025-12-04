package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	domain "github.com/SeventeenthEarth/kkachi/internal/domain/docs"
	usecase "github.com/SeventeenthEarth/kkachi/internal/usecase/docs"
)

type DocsHeadHandler struct {
	uc usecase.GetDocsHeadUseCase
}

func NewDocsHeadHandler(uc usecase.GetDocsHeadUseCase) *DocsHeadHandler {
	return &DocsHeadHandler{uc: uc}
}

func (h *DocsHeadHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	if project == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "missing_project"})
		return
	}

	head, err := h.uc.Execute(r.Context(), domain.ProjectName(project))

	if err != nil {
		if errors.Is(err, domain.ErrUnknownProject) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "unknown_project"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	resp := map[string]string{"head": string(head)}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
