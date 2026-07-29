package handler_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/irootkernel/sanho/internal/domain/docs"
	"github.com/irootkernel/sanho/internal/infra/git"
	"github.com/irootkernel/sanho/internal/infra/state"
	"github.com/irootkernel/sanho/internal/interface/http/handler"
	"github.com/irootkernel/sanho/internal/usecase/project"
)

func TestDeleteProject(t *testing.T) {
	// Setup temp dir
	tempDir := t.TempDir()

	statePath := filepath.Join(tempDir, "state.json")
	repoPath := filepath.Join(tempDir, "repo")
	if err := os.Mkdir(repoPath, 0755); err != nil {
		t.Fatal(err)
	}

	// Init State
	stateRepo, err := state.NewFileStateRepository(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := stateRepo.AddDocsRepo(docs.RepositoryConfig{ID: "repo1", Path: repoPath}); err != nil {
		t.Fatal(err)
	}
	if err := stateRepo.AddProject("proj1", "repo1"); err != nil {
		t.Fatal(err)
	}

	// Init Components
	gitClient := git.NewClient()
	gitManager := git.NewDocsRepoManager(gitClient, git.NewRepoCoordinator())
	uc := project.NewDeleteProjectUseCase(stateRepo, gitManager)
	addUC := project.NewAddProjectUseCase(stateRepo, gitManager, t.TempDir())
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
	tempDir := t.TempDir()

	statePath := filepath.Join(tempDir, "state.json")
	repoPath := filepath.Join(tempDir, "repo")
	if err := os.Mkdir(repoPath, 0755); err != nil {
		t.Fatal(err)
	}

	// Init State
	stateRepo, err := state.NewFileStateRepository(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := stateRepo.AddDocsRepo(docs.RepositoryConfig{ID: "repo1", Path: repoPath}); err != nil {
		t.Fatal(err)
	}
	if err := stateRepo.AddProject("proj1", "repo1"); err != nil {
		t.Fatal(err)
	}
	// Add a workspace for proj1
	if err := stateRepo.AddWorkspace(state.WorkspaceState{
		ID:        "proj1:/some/path",
		Project:   "proj1",
		LocalPath: "/some/path",
	}); err != nil {
		t.Fatal(err)
	}

	// Init Components
	gitClient := git.NewClient()
	gitManager := git.NewDocsRepoManager(gitClient, git.NewRepoCoordinator())
	uc := project.NewDeleteProjectUseCase(stateRepo, gitManager)
	addUC := project.NewAddProjectUseCase(stateRepo, gitManager, t.TempDir())
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
