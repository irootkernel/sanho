package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/SeventeenthEarth/kkachi/internal/config"
	"github.com/SeventeenthEarth/kkachi/internal/infra/state"
	kkachihttp "github.com/SeventeenthEarth/kkachi/internal/interface/http"
	"github.com/SeventeenthEarth/kkachi/internal/interface/http/handler"
	"github.com/SeventeenthEarth/kkachi/internal/pty"
	"github.com/gorilla/websocket"
)

func TestPTY_WS_Auth(t *testing.T) {
	// Setup state
	tempDir, err := os.MkdirTemp("", "kkachi-pty-auth-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	statePath := filepath.Join(tempDir, "state.json")
	stateRepo, err := state.NewFileStateRepository(statePath)
	if err != nil {
		t.Fatal(err)
	}
	workspaceRepo := state.NewFileWorkspaceRepository(stateRepo)

	sessionManager := pty.NewSessionManager(nil)
	defer sessionManager.Close()

	ptyConfig := pty.Config{}
	authConfig := config.AuthConfig{
		AuthEnabled: true,
		AuthToken:   "secret-token",
	}

	ptyHandler := handler.NewPTYHandler(sessionManager, workspaceRepo, ptyConfig, authConfig)

	// We only need the PTY handler for this test
	srv := kkachihttp.NewHTTPServer(
		kkachihttp.ServerConfig{Addr: ":0", AuthConfig: authConfig},
		nil, nil, nil, nil, nil, nil, ptyHandler,
	)
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	client := ts.Client()

	t.Run("WS Auth missing cookie", func(t *testing.T) {
		req, _ := http.NewRequest("GET", ts.URL+"/api/pty/sessions/dummy-id/ws", nil)
		req.Header.Set("Origin", ts.URL)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("Expected 401, got %d", resp.StatusCode)
		}
		var errResp map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}
		if errResp["error"] != "unauthorized" {
			t.Errorf("Expected error 'unauthorized', got %q", errResp["error"])
		}
	})

	t.Run("WS Auth invalid cookie", func(t *testing.T) {
		req, _ := http.NewRequest("GET", ts.URL+"/api/pty/sessions/dummy-id/ws", nil)
		req.AddCookie(&http.Cookie{Name: "auth_token", Value: "wrong-token"})
		req.Header.Set("Origin", ts.URL)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("Expected 401, got %d", resp.StatusCode)
		}
		var errResp map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}
		if errResp["error"] != "unauthorized" {
			t.Errorf("Expected error 'unauthorized', got %q", errResp["error"])
		}
	})

	t.Run("WS Auth success (Session not found)", func(t *testing.T) {
		req, _ := http.NewRequest("GET", ts.URL+"/api/pty/sessions/dummy-id/ws", nil)
		req.AddCookie(&http.Cookie{Name: "auth_token", Value: "secret-token"})
		req.Header.Set("Origin", ts.URL)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		// Should pass auth and fail at session lookup (404)
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("Expected 404 (Auth passed), got %d", resp.StatusCode)
		}
	})

	t.Run("WS Auth success (Connected)", func(t *testing.T) {
		// Create a dummy session with a fake PTY (pipe)
		ptmx, _, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		// NOTE: ptmx will be closed by sessionManager.Close() via Session.Terminate()
		// or explicitly below if something fails before session creation.

		sessionID := "auth-success-id"
		session := &pty.Session{
			ID:          sessionID,
			WorkspaceID: "test-ws",
			PTY:         ptmx,
		}
		sessionManager.AddSession(session)

		// Connect to WebSocket with cookie
		wsURL := "ws" + ts.URL[4:] + "/api/pty/sessions/" + sessionID + "/ws"

		header := http.Header{}
		header.Add("Cookie", "auth_token=secret-token")
		header.Set("Origin", ts.URL)

		ws, resp, err := websocket.DefaultDialer.Dial(wsURL, header)
		if err != nil {
			if resp != nil {
				t.Fatalf("Failed to connect to WS: %v, status: %d", err, resp.StatusCode)
			}
			t.Fatalf("Failed to connect to WS: %v", err)
		}
		defer ws.Close()

		if resp.StatusCode != http.StatusSwitchingProtocols {
			t.Errorf("Expected 101 Switching Protocols, got %d", resp.StatusCode)
		}
	})

	t.Run("HTTP Auth check (sanity check)", func(t *testing.T) {
		req, _ := http.NewRequest("POST", ts.URL+"/api/pty/sessions", nil)
		// Missing Bearer token
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("Expected 401 for HTTP, got %d", resp.StatusCode)
		}
		var errResp map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}
		if errResp["error"] != "unauthorized" {
			t.Errorf("Expected error 'unauthorized', got %q", errResp["error"])
		}
		if errResp["message"] != "missing_authorization_header" {
			t.Errorf("Expected message 'missing_authorization_header', got %q", errResp["message"])
		}
	})
}
