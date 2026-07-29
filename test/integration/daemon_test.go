package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/irootkernel/sanho/internal/infra/git"
	"github.com/irootkernel/sanho/internal/infra/state"
	sanhohttp "github.com/irootkernel/sanho/internal/interface/http"
	"github.com/irootkernel/sanho/internal/interface/http/dto"
	"github.com/irootkernel/sanho/internal/interface/http/handler"
	"github.com/irootkernel/sanho/internal/usecase/docs"
	"github.com/irootkernel/sanho/internal/usecase/project"
	stateUsecase "github.com/irootkernel/sanho/internal/usecase/state"
	"github.com/irootkernel/sanho/internal/usecase/workspace"
)

// This test spins up the HTTP daemon in-process to validate handler wiring.
func TestIntegration_Daemon(t *testing.T) {
	// 1. Setup Environment
	tempDir := t.TempDir()

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

	// Daemon State Path
	statePath := filepath.Join(tempDir, "state.json")

	// 2. Wire up Daemon
	stateRepo, err := state.NewFileStateRepository(statePath)
	if err != nil {
		t.Fatal(err)
	}

	gitClient := git.NewClient()
	repoCoordinator := git.NewRepoCoordinator()
	gitManager := git.NewDocsRepoManager(gitClient, repoCoordinator)
	docsRepo := git.NewGitDocsRepository(gitClient, stateRepo, repoCoordinator)

	// Initial Sync (Empty)
	if err := gitManager.Sync(context.Background(), stateRepo.ListDocsRepos()); err != nil {
		t.Fatalf("initial sync failed: %v", err)
	}

	deleteProjectUC := project.NewDeleteProjectUseCase(stateRepo, gitManager)
	addProjectUC := project.NewAddProjectUseCase(stateRepo, gitManager, t.TempDir())
	deleteWorkspaceUC := workspace.NewDeleteWorkspaceUseCase(stateRepo)
	getDocsHeadUC := docs.NewGetDocsHeadUseCase(docsRepo)

	getDocsSnapshotUC := docs.NewGetDocsSnapshotUseCase(docsRepo)

	workspaceRepo := state.NewFileWorkspaceRepository(stateRepo)
	registerWorkspaceUC := workspace.NewRegisterWorkspaceUseCase(docsRepo, workspaceRepo, stateRepo, nil)
	reportDocsHashUC := workspace.NewReportDocsHashUseCase(workspaceRepo, docsRepo)
	getProjectStatusUC := project.NewGetProjectStatusUseCase(workspaceRepo, docsRepo)
	getStateUC := stateUsecase.NewGetStateUseCase(docsRepo, workspaceRepo, stateRepo)

	projectHandler := handler.NewProjectHandler(deleteProjectUC, addProjectUC)
	workspaceHandler := handler.NewWorkspaceHandler(deleteWorkspaceUC, registerWorkspaceUC, reportDocsHashUC)
	docsHeadHandler := handler.NewDocsHeadHandler(getDocsHeadUC)
	docsSnapshotHandler := handler.NewDocsSnapshotHandler(getDocsSnapshotUC)
	projectStatusHandler := handler.NewProjectStatusHandler(getProjectStatusUC)
	stateHandler := handler.NewStateHandler(getStateUC)

	srv := sanhohttp.NewHTTPServer(projectHandler, workspaceHandler, docsHeadHandler, docsSnapshotHandler, nil, stateHandler, projectStatusHandler)
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
		if _, err := body.ReadFrom(resp.Body); err != nil {
			t.Fatalf("read add project response: %v", err)
		}
		t.Errorf("AddProject status: %d, body: %s", resp.StatusCode, body.String())
	}
	_ = resp.Body.Close()

	// 4. Test: Get Head (P1)
	resp, err = client.Get(ts.URL + "/docs/head?project=test-project")
	if err != nil {
		t.Fatalf("Failed to get head: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GetHead status: %d", resp.StatusCode)
	}

	var headResp map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&headResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	_ = resp.Body.Close()

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
	if err := json.NewDecoder(resp.Body).Decode(&regResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	_ = resp.Body.Close()

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
	if err := os.WriteFile(filepath.Join(originPath, "docs", "index.md"), []byte("Hello Sanho"), 0644); err != nil {
		t.Fatalf("failed to write docs file: %v", err)
	}
	runCmd(t, originPath, "git", "add", ".")
	runCmd(t, originPath, "git", "commit", "-m", "Add docs")

	// Manually sync daemon repos to pick up the new commit (since we don't have bg sync yet)
	if err := gitManager.Sync(context.Background(), stateRepo.ListDocsRepos()); err != nil {
		t.Fatalf("failed to sync repos: %v", err)
	}

	// Get new HEAD
	out = runCmd(t, originPath, "git", "rev-parse", "HEAD")
	newHead := strings.TrimSpace(string(out))

	// Report the newly adopted docs commit and verify /state uses daemon state.
	reportReqBody, _ := json.Marshal(map[string]string{
		"docs_hash":   newHead,
		"actor_email": "sync@example.com",
	})
	reportURL := ts.URL + "/workspaces/" + url.PathEscape(regResp["workspace_id"]) + "/docs-hash"
	reportReq, err := http.NewRequest(http.MethodPut, reportURL, bytes.NewReader(reportReqBody))
	if err != nil {
		t.Fatal(err)
	}
	reportReq.Header.Set("Content-Type", "application/json")
	resp, err = client.Do(reportReq)
	if err != nil {
		t.Fatalf("Failed to report workspace docs hash: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		var body bytes.Buffer
		_, _ = body.ReadFrom(resp.Body)
		t.Fatalf("ReportDocsHash status: %d body: %s", resp.StatusCode, body.String())
	}
	_ = resp.Body.Close()

	resp, err = client.Get(ts.URL + "/state")
	if err != nil {
		t.Fatalf("Failed to get state after docs hash report: %v", err)
	}
	var stateResp dto.DaemonStateResponse
	if err := json.NewDecoder(resp.Body).Decode(&stateResp); err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if len(stateResp.Workspaces) != 1 ||
		stateResp.Workspaces[0].DocsHash != newHead ||
		stateResp.Workspaces[0].LastActorEmail != "sync@example.com" {
		t.Fatalf("workspace state was not updated: %+v", stateResp.Workspaces)
	}

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
	_ = resp.Body.Close()

	if snapResp["commit"] != newHead {
		t.Errorf("Expected snapshot commit %s, got %s", newHead, snapResp["commit"])
	}
	if snapResp["snapshot"] == "" {
		t.Fatal("Expected snapshot data, got empty")
	}

	// 8. Test: Compare the registered workspace's base with the new docs HEAD.
	statusURL := ts.URL + "/projects/test-project/status?workspace_id=" +
		url.QueryEscape(regResp["workspace_id"]) + "&docs_hash=" + url.QueryEscape(expectedHead)
	resp, err = client.Get(statusURL)
	if err != nil {
		t.Fatalf("Failed to get project status: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		var body bytes.Buffer
		if _, err := body.ReadFrom(resp.Body); err != nil {
			t.Fatalf("read project status response: %v", err)
		}
		t.Fatalf("ProjectStatus status: %d, body: %s", resp.StatusCode, body.String())
	}
	var statusResp dto.ProjectStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&statusResp); err != nil {
		t.Fatalf("failed to decode project status response: %v", err)
	}
	_ = resp.Body.Close()
	if statusResp.DocsHead != newHead {
		t.Errorf("Expected project status head %s, got %s", newHead, statusResp.DocsHead)
	}
	if statusResp.ReferenceToHead.Status != "behind" || statusResp.ReferenceToHead.Behind != 1 {
		t.Errorf("Expected reference behind HEAD by 1, got %#v", statusResp.ReferenceToHead)
	}
	if len(statusResp.Workspaces) != 1 ||
		statusResp.Workspaces[0].RelativeToReference.Status != "ahead" ||
		statusResp.Workspaces[0].RelativeToReference.Ahead != 1 ||
		statusResp.Workspaces[0].RelativeToHead.Status != "same" {
		t.Errorf("Unexpected workspace comparisons: %#v", statusResp.Workspaces)
	}

	// 9. Test: Delete Project without force (should fail due to workspace)
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/projects/test-project", nil)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("Failed to delete project: %v", err)
	}
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("DeleteProject without force should return 409, got %d", resp.StatusCode)
	}
	var conflictResp map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&conflictResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	_ = resp.Body.Close()
	if conflictResp["error"] != "project_has_workspaces" {
		t.Errorf("Expected error project_has_workspaces, got %s", conflictResp["error"])
	}

	// Verify project still exists
	resp, err = client.Get(ts.URL + "/docs/head?project=test-project")
	if err != nil {
		t.Fatalf("Failed to verify project deletion: %v", err)
	}
	if err != nil {
		t.Fatalf("Failed to get head after blocked delete: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Project should still exist, got status %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// 10. Test: Delete Project with force (P0-4)
	req, _ = http.NewRequest(http.MethodDelete, ts.URL+"/projects/test-project?force=true", nil)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("Failed to delete project: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("DeleteProject status: %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// Verify Deletion
	resp, err = client.Get(ts.URL + "/docs/head?project=test-project")
	if err != nil {
		t.Fatalf("Failed to verify project deletion: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest { // Should be 400 unknown_project
		t.Errorf("Expected 400 after delete, got %d", resp.StatusCode)
	}
	var errResp map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	_ = resp.Body.Close()
	if errResp["error"] != "unknown_project" {
		t.Errorf("Expected error unknown_project, got %s", errResp["error"])
	}

	// 11. Test: /healthz endpoint
	resp, err = client.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("Failed to get /healthz: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/healthz status: %d", resp.StatusCode)
	}
	var healthResp map[string]bool
	if err := json.NewDecoder(resp.Body).Decode(&healthResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	_ = resp.Body.Close()
	if !healthResp["ok"] {
		t.Errorf("Expected /healthz ok: true, got %v", healthResp)
	}
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
