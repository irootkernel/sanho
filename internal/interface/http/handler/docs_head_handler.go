package handler

import (
	"errors"
	"net/http"

	domain "github.com/irootkernel/sanho/internal/domain/docs"
	"github.com/irootkernel/sanho/internal/interface/http/httputil"
	usecase "github.com/irootkernel/sanho/internal/usecase/docs"
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
		httputil.WriteJSONError(w, http.StatusBadRequest, "missing_project")
		return
	}

	head, err := h.uc.Execute(r.Context(), domain.ProjectName(project))

	if err != nil {
		if errors.Is(err, domain.ErrUnknownProject) {
			httputil.WriteJSONError(w, http.StatusBadRequest, "unknown_project")
			return
		}
		httputil.WriteJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]string{"head": string(head)})
}
