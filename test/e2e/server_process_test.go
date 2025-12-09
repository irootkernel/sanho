package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/SeventeenthEarth/kkachi/internal/interface/http/dto"
)

// E2E: Prefer hitting a running server via KKACHI_E2E_BASE_URL; otherwise build & launch locally.
func TestE2E_ServerProcess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	originPath, expectedHead := createOriginRepo(t, map[string]string{
		"README.md": "# Test Repo\n",
	})

	projectName := uniqueName("test-project")
	repoID := uniqueName("test-repo")

	baseURL := requireServer(t, ctx)

	client := &http.Client{Timeout: 5 * time.Second}

	// Add project.
	body, _ := json.Marshal(map[string]string{
		"project":       projectName,
		"docs_repo_id":  repoID,
		"docs_repo_url": originPath,
		"actor_email":   "admin@example.com",
	})
	resp, err := client.Post(baseURL+"/projects", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("add project request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("add project status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	t.Cleanup(func() {
		req, _ := http.NewRequest(http.MethodDelete, baseURL+"/projects/"+projectName+"?force=true", nil)
		if r, err := client.Do(req); err == nil {
			r.Body.Close()
		}
	})

	// Get head.
	resp, err = client.Get(baseURL + "/docs/head?project=" + projectName)
	if err != nil {
		t.Fatalf("get head request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get head status = %d", resp.StatusCode)
	}
	var headResp map[string]string
	json.NewDecoder(resp.Body).Decode(&headResp)
	resp.Body.Close()
	if headResp["head"] != expectedHead {
		t.Fatalf("expected head %s, got %s", expectedHead, headResp["head"])
	}

	// Register Workspace.
	wsReq := dto.RegisterWorkspaceRequest{
		Project:    projectName,
		LocalPath:  "/tmp/test-ws",
		RepoURL:    originPath,
		ActorEmail: "dev@example.com",
	}
	wsBody, _ := json.Marshal(wsReq)
	resp, err = client.Post(baseURL+"/workspaces/register", "application/json", bytes.NewReader(wsBody))
	if err != nil {
		t.Fatalf("register workspace request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("register workspace status = %d", resp.StatusCode)
	}
	var wsResp map[string]string
	json.NewDecoder(resp.Body).Decode(&wsResp)
	resp.Body.Close()

	if wsResp["current_docs_head"] != expectedHead {
		t.Fatalf("expected current_docs_head %s, got %s", expectedHead, wsResp["current_docs_head"])
	}
	if wsResp["workspace_id"] == "" {
		t.Fatal("expected workspace_id to be present")
	}

	// Verify State File Persistence
	// Test Delete without force (should fail due to workspace)
	reqNoForce, _ := http.NewRequest(http.MethodDelete, baseURL+"/projects/"+projectName, nil)
	respNoForce, err := client.Do(reqNoForce)
	if err != nil {
		t.Fatalf("delete without force request failed: %v", err)
	}
	if respNoForce.StatusCode != http.StatusConflict {
		t.Fatalf("delete without force should return 409, got %d", respNoForce.StatusCode)
	}
	var conflictResp map[string]string
	json.NewDecoder(respNoForce.Body).Decode(&conflictResp)
	respNoForce.Body.Close()
	if conflictResp["error"] != "project_has_workspaces" {
		t.Fatalf("expected error project_has_workspaces, got %s", conflictResp["error"])
	}

	// Verify project still exists
	respCheck, err := client.Get(baseURL + "/docs/head?project=" + projectName)
	if err != nil {
		t.Fatalf("get head check failed: %v", err)
	}
	if respCheck.StatusCode != http.StatusOK {
		t.Fatalf("project should still exist, got status %d", respCheck.StatusCode)
	}
	respCheck.Body.Close()

	// Delete project with force.
	req, _ := http.NewRequest(http.MethodDelete, baseURL+"/projects/"+projectName+"?force=true", nil)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("delete project request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete project status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Confirm project gone.
	resp, err = client.Get(baseURL + "/docs/head?project=" + projectName)
	if err != nil {
		t.Fatalf("get head after delete failed: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 unknown_project, got %d", resp.StatusCode)
	}
	var errResp map[string]string
	json.NewDecoder(resp.Body).Decode(&errResp)
	resp.Body.Close()
	if errResp["error"] != "unknown_project" {
		t.Fatalf("expected error unknown_project, got %s", errResp["error"])
	}
}
