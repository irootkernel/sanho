package e2e_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/SeventeenthEarth/kkachi/internal/interface/http/dto"
)

func TestE2E_WorkspaceDeletionEnablesProjectDeletion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	baseURL := requireServer(t, ctx)
	client := &http.Client{Timeout: 10 * time.Second}

	originPath, _ := createOriginRepo(t, map[string]string{
		"docs/index.md": "# Workspace Flow\n",
	})

	projectName := uniqueName("workspace-flow-project")
	repoID := uniqueName("workspace-flow-repo")

	addProject(t, client, baseURL, projectName, repoID, originPath)
	t.Cleanup(func() {
		req, _ := http.NewRequest(http.MethodDelete, baseURL+"/projects/"+projectName+"?force=true", nil)
		if resp, err := client.Do(req); err == nil {
			resp.Body.Close()
		}
	})

	wsID, _ := registerWorkspace(t, client, baseURL, dto.RegisterWorkspaceRequest{
		Project:    projectName,
		LocalPath:  "/tmp/ws-flow",
		RepoURL:    originPath,
		ActorEmail: "flow@example.com",
	})

	// Project delete should fail while workspace exists.
	reqNoForce, _ := http.NewRequest(http.MethodDelete, baseURL+"/projects/"+projectName, nil)
	resp, err := client.Do(reqNoForce)
	if err != nil {
		t.Fatalf("delete without force failed: %v", err)
	}
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected conflict, got %d", resp.StatusCode)
	}
	var conflictResp map[string]string
	json.NewDecoder(resp.Body).Decode(&conflictResp)
	resp.Body.Close()
	if conflictResp["error"] != "project_has_workspaces" {
		t.Fatalf("expected project_has_workspaces, got %s", conflictResp["error"])
	}

	// Delete workspace then delete project without force.
	deleteWorkspace(t, client, baseURL, wsID)
	deleteProject(t, client, baseURL, projectName, false)

	// Confirm project is gone.
	resp, err = client.Get(baseURL + "/docs/head?project=" + projectName)
	if err != nil {
		t.Fatalf("head after deletion failed: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected unknown_project 400, got %d", resp.StatusCode)
	}
	var errResp map[string]string
	json.NewDecoder(resp.Body).Decode(&errResp)
	resp.Body.Close()
	if errResp["error"] != "unknown_project" {
		t.Fatalf("expected unknown_project, got %s", errResp["error"])
	}
}
