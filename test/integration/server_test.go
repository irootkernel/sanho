package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SeventeenthEarth/kkachi/internal/infra/git"
	"github.com/SeventeenthEarth/kkachi/internal/infra/state"
	kkachihttp "github.com/SeventeenthEarth/kkachi/internal/interface/http"
	"github.com/SeventeenthEarth/kkachi/internal/interface/http/handler"
	"github.com/SeventeenthEarth/kkachi/internal/usecase/docs"
	"github.com/SeventeenthEarth/kkachi/internal/usecase/project"
	"github.com/SeventeenthEarth/kkachi/internal/usecase/workspace"
)

// This test spins up the HTTP server in-process to validate handler wiring.
func TestIntegration_Server(t *testing.T) {
	// 1. Setup Environment
	tempDir, err := os.MkdirTemp("", "kkachi-integration-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	// Create "Remote" Git Repo
	originPath := filepath.Join(tempDir, "origin")
	if err := os.Mkdir(originPath, 0755); err != nil {
		t.Fatal(err)
	}
	runCmd(t, "", "git", "init", originPath)
	runCmd(t, "", "git", "-C", originPath, "config", "user.email", "test@example.com")
	runCmd(t, "", "git", "-C", originPath, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(originPath, "README.md"), []byte("# Test Repo"), 0644); err != nil {
		t.Fatalf("failed to write README: %v", err)
	}
	runCmd(t, "", "git", "-C", originPath, "add", ".")
	runCmd(t, "", "git", "-C", originPath, "commit", "-m", "Initial commit")

	// Get HEAD hash
	out := runCmd(t, "", "git", "-C", originPath, "rev-parse", "HEAD")
	expectedHead := strings.TrimSpace(string(out))

	// Server State Path
	statePath := filepath.Join(tempDir, "state.json")

	// 2. Wire up Server
	stateRepo, err := state.NewFileStateRepository(statePath)
	if err != nil {
		t.Fatal(err)
	}

	gitClient := git.NewClient()
	gitManager := git.NewDocsRepoManager(gitClient)
	docsRepo := git.NewGitDocsRepository(gitClient, stateRepo)

	// Initial Sync (Empty)
	if err := gitManager.Sync(context.Background(), stateRepo.ListDocsRepos()); err != nil {
		t.Fatalf("initial sync failed: %v", err)
	}

	deleteProjectUC := project.NewDeleteProjectUseCase(stateRepo, gitManager)
	addProjectUC := project.NewAddProjectUseCase(stateRepo, gitManager)
	deleteWorkspaceUC := workspace.NewDeleteWorkspaceUseCase(stateRepo)
	getDocsHeadUC := docs.NewGetDocsHeadUseCase(docsRepo)

	projectHandler := handler.NewProjectHandler(deleteProjectUC, addProjectUC)
	workspaceHandler := handler.NewWorkspaceHandler(deleteWorkspaceUC)
	docsHeadHandler := handler.NewDocsHeadHandler(getDocsHeadUC)

	srv := kkachihttp.NewHTTPServer(":0", projectHandler, workspaceHandler, docsHeadHandler)
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	client := ts.Client()

	// 3. Test: Add Project (P1-1)
	addReqBody, _ := json.Marshal(map[string]string{
		"project":       "test-project",
		"docs_repo_id":  "test-repo",
		"docs_repo_url": originPath,
		"actor_email":   "admin@example.com",
	})
	resp, err := client.Post(ts.URL+"/projects", "application/json", bytes.NewReader(addReqBody))
	if err != nil {
		t.Fatalf("Failed to add project: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("AddProject status: %d", resp.StatusCode)
	}
	resp.Body.Close()

	// 4. Test: Get Head (P1)
	resp, err = client.Get(ts.URL + "/docs/head?project=test-project")
	if err != nil {
		t.Fatalf("Failed to get head: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GetHead status: %d", resp.StatusCode)
	}

	var headResp map[string]string
	json.NewDecoder(resp.Body).Decode(&headResp)
	resp.Body.Close()

	if headResp["head"] != expectedHead {
		t.Errorf("Expected head %s, got %s", expectedHead, headResp["head"])
	}

	// 5. Test: Delete Project (P0-4)
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/projects/test-project?force=true", nil)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("Failed to delete project: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("DeleteProject status: %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Verify Deletion
	resp, err = client.Get(ts.URL + "/docs/head?project=test-project")
	if resp.StatusCode != http.StatusBadRequest { // Should be 400 unknown_project
		t.Errorf("Expected 400 after delete, got %d", resp.StatusCode)
	}
	var errResp map[string]string
	json.NewDecoder(resp.Body).Decode(&errResp)
	resp.Body.Close()
	if errResp["error"] != "unknown_project" {
		t.Errorf("Expected error unknown_project, got %s", errResp["error"])
	}

	// 6. Test: OpenAPI & Swagger UI (S1, S2)
	// OpenAPI Spec
	resp, err = client.Get(ts.URL + "/openapi.yaml")
	if err != nil {
		t.Fatalf("Failed to get openapi.yaml: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("OpenAPI status: %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Swagger UI
	resp, err = client.Get(ts.URL + "/docs")
	if err != nil {
		t.Fatalf("Failed to get swagger ui: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Swagger UI status: %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func runCmd(t *testing.T, dir string, name string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command %s %v failed: %v\noutput:\n%s", name, args, err, string(out))
	}
	return out
}
