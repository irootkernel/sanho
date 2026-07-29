package handler_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/irootkernel/sanho/internal/config"
	"github.com/irootkernel/sanho/internal/infra/git"
	"github.com/irootkernel/sanho/internal/infra/state"
	"github.com/irootkernel/sanho/internal/interface/http/handler"
	"github.com/irootkernel/sanho/internal/usecase/project"
)

func TestDeleteProject(t *testing.T) {
	// Setup temp dir
	tempDir, err := os.MkdirTemp("", "sanho-test-project-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	statePath := filepath.Join(tempDir, "state.json")
	repoPath := filepath.Join(tempDir, "repo")
	os.Mkdir(repoPath, 0755)

	// Init State
	stateRepo, _ := state.NewFileStateRepository(statePath)
	stateRepo.AddDocsRepo(config.DocsRepoConfig{ID: "repo1", Path: repoPath})
	stateRepo.AddProject("proj1", "repo1")

	// Init Components
	gitClient := git.NewClient()
	gitManager := git.NewDocsRepoManager(gitClient, git.NewRepoCoordinator())
	uc := project.NewDeleteProjectUseCase(stateRepo, gitManager)
	addUC := project.NewAddProjectUseCase(stateRepo, gitManager)
	h := handler.NewProjectHandler(uc, addUC)

	// Test: Delete success and cleanup
	req := httptest.NewRequest(http.MethodDelete, "/projects/proj1?force=true", nil)
	req.SetPathValue("project", "proj1")

	w := httptest.NewRecorder()
	h.Delete(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}

	// Check state
	if _, ok := stateRepo.GetDocsRepoID("proj1"); ok {
		t.Error("Project should be removed from state")
	}

	// Check repo dir
	if _, err := os.Stat(repoPath); !os.IsNotExist(err) {
		t.Error("Repo directory should be deleted")
	}
}

func TestDeleteProjectWithWorkspaces(t *testing.T) {
	// Setup temp dir
	tempDir, err := os.MkdirTemp("", "sanho-test-project-ws-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	statePath := filepath.Join(tempDir, "state.json")
	repoPath := filepath.Join(tempDir, "repo")
	os.Mkdir(repoPath, 0755)

	// Init State
	stateRepo, _ := state.NewFileStateRepository(statePath)
	stateRepo.AddDocsRepo(config.DocsRepoConfig{ID: "repo1", Path: repoPath})
	stateRepo.AddProject("proj1", "repo1")
	// Add a workspace for proj1
	stateRepo.AddWorkspace(state.WorkspaceState{
		ID:        "proj1:/some/path",
		Project:   "proj1",
		LocalPath: "/some/path",
	})

	// Init Components
	gitClient := git.NewClient()
	gitManager := git.NewDocsRepoManager(gitClient, git.NewRepoCoordinator())
	uc := project.NewDeleteProjectUseCase(stateRepo, gitManager)
	addUC := project.NewAddProjectUseCase(stateRepo, gitManager)
	h := handler.NewProjectHandler(uc, addUC)

	t.Run("delete without force should return 409", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/projects/proj1", nil)
		req.SetPathValue("project", "proj1")

		w := httptest.NewRecorder()
		h.Delete(w, req)

		if w.Code != http.StatusConflict {
			t.Errorf("Expected 409, got %d", w.Code)
		}

		// Verify project still exists
		if _, ok := stateRepo.GetDocsRepoID("proj1"); !ok {
			t.Error("Project should still exist")
		}
	})

	t.Run("delete with force should succeed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/projects/proj1?force=true", nil)
		req.SetPathValue("project", "proj1")

		w := httptest.NewRecorder()
		h.Delete(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected 200, got %d", w.Code)
		}

		// Verify project removed
		if _, ok := stateRepo.GetDocsRepoID("proj1"); ok {
			t.Error("Project should be removed from state")
		}
	})
}

func TestAddProject(t *testing.T) {
	// TODO: Implement integration test for AddProject
}
