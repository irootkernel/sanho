package e2e_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/SeventeenthEarth/kkachi/internal/interface/http/dto"
)

// TestE2E_DocsPush tests the full /docs/push flow with a running server
func TestE2E_DocsPush(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	originPath, initialHead := createOriginRepo(t, map[string]string{
		"docs/index.md": "# Initial\n",
	})

	projectName := uniqueName("test-push")
	repoID := uniqueName("test-repo")

	baseURL := requireServer(t, ctx)

	client := &http.Client{Timeout: 10 * time.Second}

	// Add project
	body, _ := json.Marshal(map[string]string{
		"project":       projectName,
		"docs_repo_id":  repoID,
		"docs_repo_url": originPath,
		"actor_email":   "admin@example.com",
	})
	resp, err := client.Post(baseURL+"/projects", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("add project failed: %v", err)
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

	// Register first workspace
	wsReq1 := dto.RegisterWorkspaceRequest{
		Project:    projectName,
		LocalPath:  "/tmp/ws1",
		RepoURL:    originPath,
		ActorEmail: "dev1@example.com",
	}
	wsBody1, _ := json.Marshal(wsReq1)
	resp, err = client.Post(baseURL+"/workspaces/register", "application/json", bytes.NewReader(wsBody1))
	if err != nil {
		t.Fatalf("register ws1 failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("register ws1 status = %d", resp.StatusCode)
	}
	var wsResp1 map[string]string
	json.NewDecoder(resp.Body).Decode(&wsResp1)
	resp.Body.Close()
	ws1ID := wsResp1["workspace_id"]

	// Register second workspace
	wsReq2 := dto.RegisterWorkspaceRequest{
		Project:    projectName,
		LocalPath:  "/tmp/ws2",
		RepoURL:    originPath,
		ActorEmail: "dev2@example.com",
	}
	wsBody2, _ := json.Marshal(wsReq2)
	resp, err = client.Post(baseURL+"/workspaces/register", "application/json", bytes.NewReader(wsBody2))
	if err != nil {
		t.Fatalf("register ws2 failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("register ws2 status = %d", resp.StatusCode)
	}
	var wsResp2 map[string]string
	json.NewDecoder(resp.Body).Decode(&wsResp2)
	resp.Body.Close()
	ws2ID := wsResp2["workspace_id"]

	// WS1 pushes changes (should succeed with "updated")
	t.Run("WS1 Push - Updated", func(t *testing.T) {
		snapshot := createSnapshotE2E(t, map[string]string{
			"index.md": "# Updated by WS1\n",
		})
		pushReq := dto.DocsPushRequest{
			WorkspaceID:  ws1ID,
			BaseDocsHash: initialHead,
			DocsSnapshot: base64.StdEncoding.EncodeToString(snapshot),
			ActorEmail:   "dev1@example.com",
		}
		pushBody, _ := json.Marshal(pushReq)
		resp, err := client.Post(baseURL+"/docs/push", "application/json", bytes.NewReader(pushBody))
		if err != nil {
			t.Fatalf("push failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		var pushResp dto.DocsPushResponse
		json.NewDecoder(resp.Body).Decode(&pushResp)

		if !pushResp.Ok {
			t.Errorf("expected Ok=true, got false. Error: %s", pushResp.Error)
		}
		if pushResp.Status != "updated" {
			t.Errorf("expected status 'updated', got '%s'", pushResp.Status)
		}
		if pushResp.NewDocsHash == "" {
			t.Error("expected NewDocsHash to be set")
		}
	})

	// WS2 pushes with old base (should get "outdated")
	t.Run("WS2 Push - Outdated", func(t *testing.T) {
		snapshot := createSnapshotE2E(t, map[string]string{
			"index.md": "# Updated by WS2\n",
		})
		pushReq := dto.DocsPushRequest{
			WorkspaceID:  ws2ID,
			BaseDocsHash: initialHead, // Still using old base!
			DocsSnapshot: base64.StdEncoding.EncodeToString(snapshot),
			ActorEmail:   "dev2@example.com",
		}
		pushBody, _ := json.Marshal(pushReq)
		resp, err := client.Post(baseURL+"/docs/push", "application/json", bytes.NewReader(pushBody))
		if err != nil {
			t.Fatalf("push failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		var pushResp dto.DocsPushResponse
		json.NewDecoder(resp.Body).Decode(&pushResp)

		if !pushResp.Ok {
			t.Errorf("expected Ok=true, got false. Error: %s", pushResp.Error)
		}
		if pushResp.Status != "outdated" {
			t.Errorf("expected status 'outdated', got '%s'", pushResp.Status)
		}
		if pushResp.CurrentDocsHash == "" {
			t.Error("expected CurrentDocsHash to be set")
		}
	})
}
