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
	"github.com/SeventeenthEarth/kkachi/internal/interface/http/dto"
	"github.com/SeventeenthEarth/kkachi/internal/interface/http/handler"
	"github.com/SeventeenthEarth/kkachi/internal/usecase/docs"
	"github.com/SeventeenthEarth/kkachi/internal/usecase/project"
	stateuc "github.com/SeventeenthEarth/kkachi/internal/usecase/state"
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
	runCmd(t, originPath, "git", "init")
	runCmd(t, originPath, "git", "config", "user.email", "test@example.com")
	runCmd(t, originPath, "git", "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(originPath, "README.md"), []byte("# Test Repo"), 0644); err != nil {
		t.Fatalf("failed to write README: %v", err)
	}
	runCmd(t, originPath, "git", "add", ".")
	runCmd(t, originPath, "git", "commit", "-m", "Initial commit")

	// Get HEAD hash
	out := runCmd(t, originPath, "git", "rev-parse", "HEAD")
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

	getDocsSnapshotUC := docs.NewGetDocsSnapshotUseCase(docsRepo)

	workspaceRepo := state.NewFileWorkspaceRepository(stateRepo)
	registerWorkspaceUC := workspace.NewRegisterWorkspaceUseCase(docsRepo, workspaceRepo, stateRepo, nil)

	projectHandler := handler.NewProjectHandler(deleteProjectUC, addProjectUC)
	workspaceHandler := handler.NewWorkspaceHandler(deleteWorkspaceUC, registerWorkspaceUC)
	docsHeadHandler := handler.NewDocsHeadHandler(getDocsHeadUC)
	docsSnapshotHandler := handler.NewDocsSnapshotHandler(getDocsSnapshotUC)

	srv := kkachihttp.NewHTTPServer(kkachihttp.ServerConfig{Addr: ":0"}, projectHandler, workspaceHandler, docsHeadHandler, docsSnapshotHandler, nil, nil, nil)
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
		var body bytes.Buffer
		body.ReadFrom(resp.Body)
		t.Errorf("AddProject status: %d, body: %s", resp.StatusCode, body.String())
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

	// 5. Test: Register Workspace (P2)
	regReq := dto.RegisterWorkspaceRequest{
		Project:    "test-project",
		LocalPath:  "/tmp/test-ws",
		RepoURL:    originPath,
		ActorEmail: "dev@example.com",
	}
	regReqBody, _ := json.Marshal(regReq)
	resp, err = client.Post(ts.URL+"/workspaces/register", "application/json", bytes.NewReader(regReqBody))
	if err != nil {
		t.Fatalf("Failed to register workspace: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("RegisterWorkspace status: %d", resp.StatusCode)
	}
	var regResp map[string]string
	json.NewDecoder(resp.Body).Decode(&regResp)
	resp.Body.Close()

	if regResp["current_docs_head"] != expectedHead {
		t.Errorf("Expected current_docs_head %s, got %s", expectedHead, regResp["current_docs_head"])
	}
	if regResp["workspace_id"] == "" {
		t.Error("Expected workspace_id to be present")
	}

	// 7. Test: Get Snapshot (P3)
	// Create a dummy docs file in origin
	if err := os.MkdirAll(filepath.Join(originPath, "docs"), 0755); err != nil {
		t.Fatalf("failed to create docs directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(originPath, "docs", "index.md"), []byte("Hello Kkachi"), 0644); err != nil {
		t.Fatalf("failed to write docs file: %v", err)
	}
	runCmd(t, originPath, "git", "add", ".")
	runCmd(t, originPath, "git", "commit", "-m", "Add docs")

	// Manually sync server repos to pick up the new commit (since we don't have bg sync yet)
	if err := gitManager.Sync(context.Background(), stateRepo.ListDocsRepos()); err != nil {
		t.Fatalf("failed to sync repos: %v", err)
	}

	// Get new HEAD
	out = runCmd(t, originPath, "git", "rev-parse", "HEAD")
	newHead := strings.TrimSpace(string(out))

	// Request Snapshot (using HEAD implicit)
	resp, err = client.Get(ts.URL + "/docs/snapshot?project=test-project")
	if err != nil {
		t.Fatalf("Failed to get snapshot: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GetSnapshot status: %d", resp.StatusCode)
	}

	var snapResp map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&snapResp); err != nil {
		t.Fatalf("failed to decode snapshot response: %v", err)
	}
	resp.Body.Close()

	if snapResp["commit"] != newHead {
		t.Errorf("Expected snapshot commit %s, got %s", newHead, snapResp["commit"])
	}
	if snapResp["snapshot"] == "" {
		t.Fatal("Expected snapshot data, got empty")
	}

	// 8. Test: Delete Project without force (should fail due to workspace)
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/projects/test-project", nil)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("Failed to delete project: %v", err)
	}
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("DeleteProject without force should return 409, got %d", resp.StatusCode)
	}
	var conflictResp map[string]string
	json.NewDecoder(resp.Body).Decode(&conflictResp)
	resp.Body.Close()
	if conflictResp["error"] != "project_has_workspaces" {
		t.Errorf("Expected error project_has_workspaces, got %s", conflictResp["error"])
	}

	// Verify project still exists
	resp, err = client.Get(ts.URL + "/docs/head?project=test-project")
	if err != nil {
		t.Fatalf("Failed to get head after blocked delete: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Project should still exist, got status %d", resp.StatusCode)
	}
	resp.Body.Close()

	// 9. Test: Delete Project with force (P0-4)
	req, _ = http.NewRequest(http.MethodDelete, ts.URL+"/projects/test-project?force=true", nil)
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

	// 9. Test: OpenAPI & Swagger UI (S1, S2)
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

	// 10. Test: /healthz endpoint (STASK-2 - already exists)
	resp, err = client.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("Failed to get /healthz: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/healthz status: %d", resp.StatusCode)
	}
	var healthResp map[string]bool
	json.NewDecoder(resp.Body).Decode(&healthResp)
	resp.Body.Close()
	if !healthResp["ok"] {
		t.Errorf("Expected /healthz ok: true, got %v", healthResp)
	}
}

// TestIntegration_APIStateEndpoint tests the /api/state endpoint (STASK-2)
func TestIntegration_APIStateEndpoint(t *testing.T) {
	// Setup: Create temp directory for state
	tempDir, err := os.MkdirTemp("", "kkachi-apistate-integration-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	// Change to temp directory so docs_repos is created in isolation
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tempDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)

	// Create "Remote" Git Repo
	originPath := filepath.Join(tempDir, "origin")
	if err := os.Mkdir(originPath, 0755); err != nil {
		t.Fatal(err)
	}
	runCmd(t, originPath, "git", "init")
	runCmd(t, originPath, "git", "config", "user.email", "test@example.com")
	runCmd(t, originPath, "git", "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(originPath, "README.md"), []byte("# Test Repo"), 0644); err != nil {
		t.Fatalf("failed to write README: %v", err)
	}
	runCmd(t, originPath, "git", "add", ".")
	runCmd(t, originPath, "git", "commit", "-m", "Initial commit")

	// Get HEAD
	out := runCmd(t, originPath, "git", "rev-parse", "HEAD")
	expectedHead := strings.TrimSpace(string(out))

	// Setup server
	statePath := filepath.Join(tempDir, "state.json")
	stateRepo, err := state.NewFileStateRepository(statePath)
	if err != nil {
		t.Fatal(err)
	}

	gitClient := git.NewClient()
	gitManager := git.NewDocsRepoManager(gitClient)
	docsRepo := git.NewGitDocsRepository(gitClient, stateRepo)

	if err := gitManager.Sync(context.Background(), stateRepo.ListDocsRepos()); err != nil {
		t.Fatalf("initial sync failed: %v", err)
	}

	addProjectUC := project.NewAddProjectUseCase(stateRepo, gitManager)
	getDocsHeadUC := docs.NewGetDocsHeadUseCase(docsRepo)
	workspaceRepo := state.NewFileWorkspaceRepository(stateRepo)
	getStateUC := stateuc.NewGetStateUseCase(docsRepo, workspaceRepo, stateRepo)

	docsHeadHandler := handler.NewDocsHeadHandler(getDocsHeadUC)
	projectHandler := handler.NewProjectHandler(nil, addProjectUC)
	stateHandler := handler.NewStateHandler(getStateUC)

	srv := kkachihttp.NewHTTPServer(kkachihttp.ServerConfig{Addr: ":0"}, projectHandler, nil, docsHeadHandler, nil, nil, stateHandler, nil)
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	client := ts.Client()

	// Add a project
	addReqBody, _ := json.Marshal(map[string]string{
		"project":       "api-test-project",
		"docs_repo_id":  "api-test-repo",
		"docs_repo_url": originPath,
		"actor_email":   "admin@example.com",
	})
	resp, err := client.Post(ts.URL+"/projects", "application/json", bytes.NewReader(addReqBody))
	if err != nil {
		t.Fatalf("Failed to add project: %v", err)
	}
	resp.Body.Close()

	// Test 1: /api/state returns 200 OK
	t.Run("API State returns 200", func(t *testing.T) {
		resp, err := client.Get(ts.URL + "/api/state")
		if err != nil {
			t.Fatalf("Failed to get /api/state: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 for /api/state, got %d", resp.StatusCode)
		}
	})

	// Test 2: /api/state and /state return identical responses
	t.Run("API State matches State", func(t *testing.T) {
		// Get /state
		stateResp, err := client.Get(ts.URL + "/state")
		if err != nil {
			t.Fatalf("Failed to get /state: %v", err)
		}
		var stateJSON map[string]interface{}
		json.NewDecoder(stateResp.Body).Decode(&stateJSON)
		stateResp.Body.Close()

		// Get /api/state
		apiResp, err := client.Get(ts.URL + "/api/state")
		if err != nil {
			t.Fatalf("Failed to get /api/state: %v", err)
		}
		var apiJSON map[string]interface{}
		json.NewDecoder(apiResp.Body).Decode(&apiJSON)
		apiResp.Body.Close()

		// Compare docs_heads
		stateHeads := stateJSON["docs_heads"].(map[string]interface{})
		apiHeads := apiJSON["docs_heads"].(map[string]interface{})

		if stateHeads["api-test-project"] != apiHeads["api-test-project"] {
			t.Errorf("docs_heads mismatch: /state=%v, /api/state=%v",
				stateHeads["api-test-project"], apiHeads["api-test-project"])
		}

		if stateHeads["api-test-project"] != expectedHead {
			t.Errorf("Expected docs_heads to be %s, got %v", expectedHead, stateHeads["api-test-project"])
		}
	})
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
