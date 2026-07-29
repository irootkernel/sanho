package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/irootkernel/sanho/internal/interface/http/dto"
)

// TestE2E_State tests the /state endpoint with real daemon and Git operations.
// Per F5-Data requirement: "서버에서 /workspaces/register를 여러 번 호출한 뒤 /state 응답이 이를 반영하는지 확인"
func TestE2E_State(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	originPath, expectedHead := createOriginRepo(t, map[string]string{
		"README.md": "# Test Repo\n",
	})

	projectName := uniqueName("state-test-project")
	repoID := uniqueName("state-test-repo")

	baseURL, client, _ := requireDaemon(t, ctx)

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

	// Test 1: Initial /state - no workspaces
	t.Run("Initial state - project with no workspaces", func(t *testing.T) {
		resp, err := client.Get(baseURL + "/state")
		if err != nil {
			t.Fatalf("get state request failed: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("get state status = %d", resp.StatusCode)
		}

		var stateResp map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&stateResp)
		resp.Body.Close()

		// Verify docs_heads contains our project
		docsHeads, ok := stateResp["docs_heads"].(map[string]interface{})
		if !ok {
			t.Fatalf("docs_heads not in response")
		}
		if docsHeads[projectName] != expectedHead {
			t.Errorf("expected docs_heads[%s] = %s, got %v", projectName, expectedHead, docsHeads[projectName])
		}

		// Verify workspaces is empty (for our project)
		workspaces, ok := stateResp["workspaces"].([]interface{})
		if !ok {
			t.Fatalf("workspaces not in response")
		}
		projectWSCount := 0
		for _, ws := range workspaces {
			wsMap := ws.(map[string]interface{})
			if wsMap["project"] == projectName {
				projectWSCount++
			}
		}
		if projectWSCount != 0 {
			t.Errorf("expected 0 workspaces for project %s, got %d", projectName, projectWSCount)
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
		t.Fatalf("register workspace 1 failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("register workspace 1 status = %d", resp.StatusCode)
	}
	var wsResp1 map[string]string
	json.NewDecoder(resp.Body).Decode(&wsResp1)
	resp.Body.Close()
	wsID1 := wsResp1["workspace_id"]

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
		t.Fatalf("register workspace 2 failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("register workspace 2 status = %d", resp.StatusCode)
	}
	var wsResp2 map[string]string
	json.NewDecoder(resp.Body).Decode(&wsResp2)
	resp.Body.Close()
	wsID2 := wsResp2["workspace_id"]

	// Test 2: /state after registering workspaces
	t.Run("State with multiple workspaces", func(t *testing.T) {
		resp, err := client.Get(baseURL + "/state")
		if err != nil {
			t.Fatalf("get state request failed: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("get state status = %d", resp.StatusCode)
		}

		var stateResp map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&stateResp)
		resp.Body.Close()

		// Verify docs_heads
		docsHeads, _ := stateResp["docs_heads"].(map[string]interface{})
		if docsHeads[projectName] != expectedHead {
			t.Errorf("expected docs_heads[%s] = %s, got %v", projectName, expectedHead, docsHeads[projectName])
		}

		// Verify workspaces contains our registered workspaces
		workspaces, _ := stateResp["workspaces"].([]interface{})
		foundWS1 := false
		foundWS2 := false
		for _, ws := range workspaces {
			wsMap := ws.(map[string]interface{})
			if wsMap["workspace_id"] == wsID1 {
				foundWS1 = true
				if wsMap["project"] != projectName {
					t.Errorf("ws1 project = %v, want %s", wsMap["project"], projectName)
				}
				if wsMap["local_path"] != "/tmp/ws1" {
					t.Errorf("ws1 local_path = %v, want /tmp/ws1", wsMap["local_path"])
				}
				if wsMap["last_actor_email"] != "dev1@example.com" {
					t.Errorf("ws1 last_actor_email = %v, want dev1@example.com", wsMap["last_actor_email"])
				}
			}
			if wsMap["workspace_id"] == wsID2 {
				foundWS2 = true
				if wsMap["project"] != projectName {
					t.Errorf("ws2 project = %v, want %s", wsMap["project"], projectName)
				}
				if wsMap["local_path"] != "/tmp/ws2" {
					t.Errorf("ws2 local_path = %v, want /tmp/ws2", wsMap["local_path"])
				}
				if wsMap["last_actor_email"] != "dev2@example.com" {
					t.Errorf("ws2 last_actor_email = %v, want dev2@example.com", wsMap["last_actor_email"])
				}
			}
		}
		if !foundWS1 {
			t.Errorf("workspace %s not found in /state response", wsID1)
		}
		if !foundWS2 {
			t.Errorf("workspace %s not found in /state response", wsID2)
		}
	})

	// Test 3: Verify workspace data contains required fields
	t.Run("Workspace data has all required fields", func(t *testing.T) {
		resp, err := client.Get(baseURL + "/state")
		if err != nil {
			t.Fatalf("get state request failed: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("get state status = %d", resp.StatusCode)
		}

		var stateResp map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&stateResp)
		resp.Body.Close()

		workspaces, _ := stateResp["workspaces"].([]interface{})
		requiredFields := []string{"workspace_id", "project", "docs_repo_id", "local_path", "repo_url", "docs_hash", "last_reported_at", "last_actor_email"}

		for _, ws := range workspaces {
			wsMap := ws.(map[string]interface{})
			for _, field := range requiredFields {
				if _, ok := wsMap[field]; !ok {
					t.Errorf("missing field %s in workspace response", field)
				}
			}
		}
	})

	// Test 4: Verify /docs/head consistency with /state.docs_heads
	// STASK-1 requirement: GET /docs/head?project=<project> should return the same value as docs_heads[project]
	t.Run("DocsHead consistency with State", func(t *testing.T) {
		// Get /docs/head response
		docsHeadResp, err := client.Get(baseURL + "/docs/head?project=" + projectName)
		if err != nil {
			t.Fatalf("get docs/head request failed: %v", err)
		}
		if docsHeadResp.StatusCode != http.StatusOK {
			t.Fatalf("get docs/head status = %d", docsHeadResp.StatusCode)
		}
		var docsHeadJSON map[string]string
		json.NewDecoder(docsHeadResp.Body).Decode(&docsHeadJSON)
		docsHeadResp.Body.Close()
		docsHeadValue := docsHeadJSON["head"]

		// Get /state response
		stateResp, err := client.Get(baseURL + "/state")
		if err != nil {
			t.Fatalf("get state request failed: %v", err)
		}
		if stateResp.StatusCode != http.StatusOK {
			t.Fatalf("get state status = %d", stateResp.StatusCode)
		}
		var stateJSON map[string]interface{}
		json.NewDecoder(stateResp.Body).Decode(&stateJSON)
		stateResp.Body.Close()

		docsHeads, ok := stateJSON["docs_heads"].(map[string]interface{})
		if !ok {
			t.Fatalf("docs_heads not found in /state response")
		}
		stateDocsHeadValue, ok := docsHeads[projectName].(string)
		if !ok {
			t.Fatalf("docs_heads[%s] not found in /state response", projectName)
		}

		// Verify consistency
		if docsHeadValue != stateDocsHeadValue {
			t.Errorf("Inconsistency detected: /docs/head returned %q, but /state.docs_heads[%s] returned %q",
				docsHeadValue, projectName, stateDocsHeadValue)
		}
	})

	// Test 5: Verify last_reported_at is RFC3339 formatted
	t.Run("LastReportedAt is RFC3339 formatted", func(t *testing.T) {
		resp, err := client.Get(baseURL + "/state")
		if err != nil {
			t.Fatalf("get state request failed: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("get state status = %d", resp.StatusCode)
		}

		var stateResp map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&stateResp)
		resp.Body.Close()

		workspaces, _ := stateResp["workspaces"].([]interface{})
		for _, ws := range workspaces {
			wsMap := ws.(map[string]interface{})
			if wsMap["project"] != projectName {
				continue
			}
			lastReportedAt, ok := wsMap["last_reported_at"].(string)
			if !ok || lastReportedAt == "" {
				t.Errorf("last_reported_at is missing or empty for workspace %v", wsMap["workspace_id"])
				continue
			}
			if _, err := time.Parse(time.RFC3339, lastReportedAt); err != nil {
				t.Errorf("last_reported_at %q is not RFC3339 formatted: %v", lastReportedAt, err)
			}
		}
	})

}
