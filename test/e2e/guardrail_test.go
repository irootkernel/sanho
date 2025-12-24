package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/SeventeenthEarth/kkachi/internal/interface/http/dto"
	"github.com/gorilla/websocket"
)

func TestE2E_Guardrail_Blocking(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	baseURL := requireServer(t, ctx)
	client := &http.Client{Timeout: 10 * time.Second}

	// Setup: Create a project and workspace
	projectName := uniqueName("guardrail-test")
	originPath, _ := createOriginRepo(t, map[string]string{
		"docs/README.md": "# Guardrail Test\n",
	})
	addProject(t, client, baseURL, projectName, "guardrail-docs-repo", originPath)
	t.Cleanup(func() {
		deleteProject(t, client, baseURL, projectName, true)
	})

	workspaceDir := sharedRepoTempDir(t)
	workspaceID, _ := registerWorkspace(t, client, baseURL, dto.RegisterWorkspaceRequest{
		Project:    projectName,
		LocalPath:  workspaceDir,
		RepoURL:    originPath,
		ActorEmail: "guardrail-test@example.com",
	})
	t.Cleanup(func() {
		deleteWorkspace(t, client, baseURL, workspaceID)
	})

	// 1. Create session
	reqBody := dto.CreatePTYSessionRequest{
		WorkspaceID: workspaceID,
	}
	body, _ := json.Marshal(reqBody)
	resp, err := client.Post(baseURL+"/api/pty/sessions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Skip("PTY spawn failed, skipping guardrail E2E test")
		return
	}

	var createResp dto.CreatePTYSessionResponse
	json.NewDecoder(resp.Body).Decode(&createResp)
	sessionID := createResp.SessionID
	wsURL := "ws" + baseURL[4:] + createResp.WsURL

	t.Cleanup(func() {
		terminateReq, _ := http.NewRequest(http.MethodDelete, baseURL+"/api/pty/sessions/"+sessionID, nil)
		client.Do(terminateReq)
	})

	// 2. Connect via WebSocket
	dialer := websocket.Dialer{}
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect to WebSocket: %v", err)
	}
	defer conn.Close()

	// 3. Try to run a blocked command
	// Note: config/security_rules.yaml should have "rm -rf . *" as blocked.
	blockedCmd := "rm -rf /test\n"
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte(blockedCmd)); err != nil {
		t.Fatalf("Failed to write to WS: %v", err)
	}

	// 4. Expect "Blocked by security policy" in output
	found := false
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	for !found {
		_, p, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("Failed to read from WS (timeout?): %v", err)
		}
		if bytes.Contains(p, []byte("Blocked by security policy")) {
			found = true
		}
	}
}
