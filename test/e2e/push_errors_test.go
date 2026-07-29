package e2e_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/irootkernel/sanho/internal/interface/http/dto"
)

func TestE2E_DocsPushNoChangeAndErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	baseURL, client, _ := requireServer(t, ctx)

	originPath, initialHead := createOriginRepo(t, map[string]string{
		"docs/index.md": "# Initial\n",
	})

	projectName := uniqueName("push-errors-project")
	repoID := uniqueName("push-errors-repo")

	addProject(t, client, baseURL, projectName, repoID, originPath)
	t.Cleanup(func() {
		req, _ := http.NewRequest(http.MethodDelete, baseURL+"/projects/"+projectName+"?force=true", nil)
		if resp, err := client.Do(req); err == nil {
			resp.Body.Close()
		}
	})

	wsID, currentHead := registerWorkspace(t, client, baseURL, dto.RegisterWorkspaceRequest{
		Project:    projectName,
		LocalPath:  "/tmp/ws-nochange",
		RepoURL:    originPath,
		ActorEmail: "dev@example.com",
	})

	if currentHead != initialHead {
		t.Fatalf("workspace head mismatch: got %s want %s", currentHead, initialHead)
	}

	snapshot := createSnapshotE2E(t, map[string]string{
		"docs/index.md": "# Initial\n",
	})

	pushReq := dto.DocsPushRequest{
		WorkspaceID:  wsID,
		BaseDocsHash: currentHead,
		DocsSnapshot: base64.StdEncoding.EncodeToString(snapshot),
		ActorEmail:   "dev@example.com",
	}

	// No change push should return status=nochange and current hash.
	pushBody, _ := json.Marshal(pushReq)
	resp, err := client.Post(baseURL+"/docs/push", "application/json", bytes.NewReader(pushBody))
	if err != nil {
		t.Fatalf("nochange push failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("nochange push status = %d", resp.StatusCode)
	}
	var pushResp dto.DocsPushResponse
	json.NewDecoder(resp.Body).Decode(&pushResp)
	resp.Body.Close()

	if pushResp.Status != "nochange" {
		t.Fatalf("expected status nochange, got %s", pushResp.Status)
	}
	if pushResp.CurrentDocsHash != currentHead {
		t.Fatalf("expected current_docs_hash %s, got %s", currentHead, pushResp.CurrentDocsHash)
	}

	// Unknown workspace should return error.
	pushReq.WorkspaceID = "missing-workspace"
	pushBody, _ = json.Marshal(pushReq)
	resp, err = client.Post(baseURL+"/docs/push", "application/json", bytes.NewReader(pushBody))
	if err != nil {
		t.Fatalf("unknown workspace push failed: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown workspace status = %d", resp.StatusCode)
	}
	json.NewDecoder(resp.Body).Decode(&pushResp)
	resp.Body.Close()
	if pushResp.Error != "unknown_workspace" {
		t.Fatalf("expected unknown_workspace, got %s", pushResp.Error)
	}

	// Unknown base commit should return unknown_docs_commit.
	pushReq.WorkspaceID = wsID
	pushReq.BaseDocsHash = strings.Repeat("0", 40)
	pushBody, _ = json.Marshal(pushReq)
	resp, err = client.Post(baseURL+"/docs/push", "application/json", bytes.NewReader(pushBody))
	if err != nil {
		t.Fatalf("unknown commit push failed: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown commit status = %d", resp.StatusCode)
	}
	json.NewDecoder(resp.Body).Decode(&pushResp)
	resp.Body.Close()
	if pushResp.Error != "unknown_docs_commit" {
		t.Fatalf("expected unknown_docs_commit, got %s", pushResp.Error)
	}
}
