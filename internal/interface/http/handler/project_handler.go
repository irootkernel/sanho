package handler

import (
	"encoding/json"
	"net/http"

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
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func (h *ProjectHandler) Delete(w http.ResponseWriter, r *http.Request) {
	projectName := r.PathValue("project")
	force := r.URL.Query().Get("force") == "true"

	err := h.deleteUC.Execute(projectName, force)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		if err == project.ErrUnknownProject {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "unknown_project"})
			return
		}
		if err == project.ErrProjectHasWorkspaces {
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]string{"error": "project_has_workspaces"})
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}
