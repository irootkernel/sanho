package handler

import (
	"encoding/json"
	"net/http"

	"github.com/irootkernel/sanho/internal/interface/http/httputil"
	"github.com/irootkernel/sanho/internal/usecase/project"
)

type ProjectHandler struct {
	deleteUC *project.DeleteProjectUseCase
	addUC    *project.AddProjectUseCase
}

func NewProjectHandler(deleteUC *project.DeleteProjectUseCase, addUC *project.AddProjectUseCase) *ProjectHandler {
	return &ProjectHandler{
		deleteUC: deleteUC,
		addUC:    addUC,
	}
}

func (h *ProjectHandler) Add(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Project     string `json:"project"`
		DocsRepoID  string `json:"docs_repo_id"`
		DocsRepoURL string `json:"docs_repo_url"`
		ActorEmail  string `json:"actor_email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	input := project.AddProjectInput{
		Project:     req.Project,
		DocsRepoID:  req.DocsRepoID,
		DocsRepoURL: req.DocsRepoURL,
		ActorEmail:  req.ActorEmail,
	}

	if err := h.addUC.Execute(r.Context(), input); err != nil {
		httputil.WriteJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *ProjectHandler) Delete(w http.ResponseWriter, r *http.Request) {
	projectName := r.PathValue("project")
	force := r.URL.Query().Get("force") == "true"

	err := h.deleteUC.Execute(projectName, force)
	if err != nil {
		if err == project.ErrUnknownProject {
			httputil.WriteJSONError(w, http.StatusNotFound, "unknown_project")
			return
		}
		if err == project.ErrProjectHasWorkspaces {
			httputil.WriteJSONError(w, http.StatusConflict, "project_has_workspaces")
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
