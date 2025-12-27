package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os/exec"
	"sync"

	"github.com/SeventeenthEarth/kkachi/internal/config"
	"github.com/SeventeenthEarth/kkachi/internal/domain/workspace"
	"github.com/SeventeenthEarth/kkachi/internal/interface/http/dto"
	"github.com/SeventeenthEarth/kkachi/internal/pty"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // For now, allow all origins
	},
}

// WorkspaceLookup provides workspace lookup capability.
type WorkspaceLookup interface {
	Get(ctx context.Context, id workspace.WorkspaceID) (*workspace.Workspace, error)
}

// PTYHandler handles PTY session HTTP endpoints.
type PTYHandler struct {
	sessionManager  *pty.SessionManager
	workspaceLookup WorkspaceLookup
	config          pty.Config
	authConfig      config.AuthConfig
}

// NewPTYHandler creates a new PTY handler.
func NewPTYHandler(
	sessionManager *pty.SessionManager,
	workspaceLookup WorkspaceLookup,
	config pty.Config,
	authConfig config.AuthConfig,
) *PTYHandler {
	return &PTYHandler{
		sessionManager:  sessionManager,
		workspaceLookup: workspaceLookup,
		config:          config,
		authConfig:      authConfig,
	}
}

// Create handles POST /api/pty/sessions - creates a new PTY session.
func (h *PTYHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req dto.CreatePTYSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, pty.CodeInvalidRequestBody, "Failed to parse request body", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.WorkspaceID == "" {
		h.writeError(w, pty.CodeMissingWorkspaceID, "workspace_id is required", http.StatusBadRequest)
		return
	}

	// Lookup workspace
	ws, err := h.workspaceLookup.Get(r.Context(), workspace.WorkspaceID(req.WorkspaceID))
	if err != nil {
		slog.Error("pty_workspace_lookup_failed", "error", err, "workspace_id", req.WorkspaceID)
		h.writeError(w, pty.CodeInternalServerError, "Failed to lookup workspace", http.StatusInternalServerError)
		return
	}
	if ws == nil {
		h.writeError(w, pty.CodeUnknownWorkspace, fmt.Sprintf("Workspace '%s' not found", req.WorkspaceID), http.StatusBadRequest)
		return
	}

	// Resolve CWD
	resolvedCWD, err := pty.ResolveCWD(ws.LocalPath, req.CwdRel)
	if err != nil {
		if errors.Is(err, pty.ErrAbsolutePathNotAllowed) {
			h.writeError(w, pty.CodeAbsolutePathNotAllowed, "cwd_rel must be a relative path", http.StatusBadRequest)
			return
		}
		if errors.Is(err, pty.ErrCWDTraversal) {
			h.writeError(w, pty.CodeCWDTraversal, "Path traversal not allowed", http.StatusBadRequest)
			return
		}
		h.writeError(w, pty.CodeInvalidCWD, err.Error(), http.StatusBadRequest)
		return
	}

	// Determine shell
	shell := req.Shell
	if shell == "" {
		shell = h.config.DefaultShell
	}

	// Validate shell
	if err := pty.ValidateShell(shell, h.config.AllowedShells); err != nil {
		h.writeError(w, pty.CodeShellNotAllowed, fmt.Sprintf("Shell '%s' is not allowed", shell), http.StatusBadRequest)
		return
	}

	// Determine terminal size
	cols := req.Cols
	rows := req.Rows
	if cols == 0 {
		cols = int(h.config.DefaultCols)
	}
	if rows == 0 {
		rows = int(h.config.DefaultRows)
	}
	if cols < pty.MinCols || cols > pty.MaxCols || rows < pty.MinRows || rows > pty.MaxRows {
		h.writeError(
			w,
			pty.CodeInvalidTerminalSize,
			fmt.Sprintf("cols must be between %d and %d, rows between %d and %d", pty.MinCols, pty.MaxCols, pty.MinRows, pty.MaxRows),
			http.StatusBadRequest,
		)
		return
	}

	// Check session limits
	if h.config.MaxSessions > 0 && h.sessionManager.SessionCount() >= h.config.MaxSessions {
		h.writeError(w, pty.CodeSessionLimitExceeded, "Maximum number of concurrent sessions reached", http.StatusTooManyRequests)
		return
	}

	// Create session
	session, err := h.sessionManager.CreateSession(pty.CreateSessionConfig{
		WorkspaceID: req.WorkspaceID,
		Shell:       shell,
		ResolvedCWD: resolvedCWD,
		Cols:        uint16(cols),
		Rows:        uint16(rows),
	})
	if err != nil {
		slog.Error("pty_session_creation_failed", "error", err, "workspace_id", req.WorkspaceID)
		if errors.Is(err, pty.ErrPTYSpawnFailed) {
			h.writeError(w, pty.CodePTYSpawnFailed, "Failed to create PTY session", http.StatusInternalServerError)
			return
		}
		h.writeError(w, pty.CodeInternalServerError, "Failed to create session", http.StatusInternalServerError)
		return
	}
	// Build WebSocket URL (will be implemented in STASK-2)
	wsURL := fmt.Sprintf("/api/pty/sessions/%s/ws", session.ID)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(dto.CreatePTYSessionResponse{
		SessionID:   session.ID,
		WsURL:       wsURL,
		ResolvedCWD: resolvedCWD,
	}); err != nil {
		slog.Error("pty_create_response_write_failed", "error", err, "session_id", session.ID)
	}
}

// safeWSConn wraps a websocket connection with a mutex to ensure thread-safe writes.
type safeWSConn struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (s *safeWSConn) WriteMessage(messageType int, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conn.WriteMessage(messageType, data)
}

func (s *safeWSConn) ReadMessage() (messageType int, p []byte, err error) {
	// ReadMessage does not need a lock as it's typically called from a single loop,
	// but gorilla/websocket documentation says "Connections support one concurrent reader and one concurrent writer."
	// Since we only have one reader loop, this is fine.
	return s.conn.ReadMessage()
}

func (s *safeWSConn) Close() error {
	return s.conn.Close()
}

// WS handles GET /api/pty/sessions/{id}/ws - attaches to a PTY session via WebSocket.
func (h *PTYHandler) WS(w http.ResponseWriter, r *http.Request) {
	// Authentication (STASK-5)
	if h.authConfig.AuthEnabled {
		cookie, err := r.Cookie("auth_token")
		if err != nil {
			// Cookie missing
			slog.Warn("pty_ws_auth_failed", "reason", "missing_cookie", "remote_addr", r.RemoteAddr)
			h.writeError(w, pty.CodeUnauthorized, "Missing authentication cookie", http.StatusUnauthorized)
			return
		}
		if cookie.Value != h.authConfig.AuthToken {
			// Invalid token
			slog.Warn("pty_ws_auth_failed", "reason", "invalid_token", "remote_addr", r.RemoteAddr)
			h.writeError(w, pty.CodeUnauthorized, "Invalid authentication token", http.StatusUnauthorized)
			return
		}
	}

	sessionID := r.PathValue("id")
	if sessionID == "" {
		h.writeError(w, pty.CodeMissingSessionID, "Session ID is required", http.StatusBadRequest)
		return
	}

	session, exists := h.sessionManager.GetSession(sessionID)
	if !exists {
		h.writeError(w, pty.CodeSessionNotFound, "Session not found", http.StatusNotFound)
		return
	}

	// Enforce single-attach policy
	if !session.Attach() {
		h.writeError(w, pty.CodeSessionAlreadyAttached, "Session is already attached to another client", http.StatusConflict)
		return
	}
	defer session.Detach()

	// Upgrade to WebSocket
	rawConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("pty_ws_upgrade_failed", "error", err, "session_id", sessionID)
		return
	}
	conn := &safeWSConn{conn: rawConn}
	defer conn.Close()

	slog.Info("pty_ws_attached", "id", sessionID, "remote_addr", r.RemoteAddr)
	defer slog.Info("pty_ws_detached", "id", sessionID)

	// Safe exit channel access
	exitCh := session.ExitCh
	if exitCh == nil {
		slog.Warn("pty_session_exit_ch_nil", "id", sessionID)
		exitCh = make(chan error) // Dummy channel that never receives
	}

	// NOTE on defer order:
	// 1. cancel() is called first (LIFO), stopping internal goroutines.
	// 2. disconnect policy is applied, potentially terminating the session.
	// 3. log "pty_ws_detached".
	// 4. conn.Close() closes the WebSocket.
	// 5. session.Detach() unlocks the session attachment.

	// Apply disconnect policy when the handler exits
	defer func() {
		if h.config.DisconnectPolicy == pty.DisconnectPolicyTerminate {
			_ = h.sessionManager.TerminateSession(session.ID)
		}
	}()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Channel to receive messages from WebSocket
	type wsMsg struct {
		msgType int
		data    []byte
	}
	wsReadCh := make(chan wsMsg)
	wsErrCh := make(chan error, 1)

	// WS -> Handler loop
	go func() {
		defer close(wsReadCh)
		for {
			messageType, p, err := conn.ReadMessage()
			if err != nil {
				wsErrCh <- err
				return
			}
			if messageType == websocket.TextMessage || messageType == websocket.BinaryMessage {
				select {
				case wsReadCh <- wsMsg{msgType: messageType, data: p}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	// PTY -> WS loop
	go func() {
		buf := make([]byte, 8192)
		for {
			n, err := session.PTY.Read(buf)
			if err != nil {
				// Process might have exited or PTY closed.
				return
			}
			if err := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
				return
			}
		}
	}()

	// Main event loop
	for {
		select {
		case <-ctx.Done():
			return
		case err := <-wsErrCh:
			// WebSocket error or closed by client
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				slog.Warn("pty_ws_read_error", "error", err, "id", sessionID)
			}
			return
		case msg := <-wsReadCh:
			if msg.msgType == websocket.BinaryMessage {
				// Raw input to PTY (STASK-4: with Guardrail)
				blocked, reason, err := session.HandleInput(msg.data)
				if err != nil {
					slog.Error("pty_write_error", "error", err, "id", sessionID)
					return
				}

				if blocked {
					slog.Warn("pty_command_blocked", "session_id", sessionID, "reason", reason)
					// Send warning back to the terminal (as output)
					warning := fmt.Sprintf("\r\nBlocked by security policy: %s\r\n", reason)
					if err := conn.WriteMessage(websocket.BinaryMessage, []byte(warning)); err != nil {
						return
					}
					continue
				}
			} else if msg.msgType == websocket.TextMessage {
				// Control messages (JSON)
				var ctrl dto.PTYWSControlMessage
				if err := json.Unmarshal(msg.data, &ctrl); err != nil {
					continue
				}

				if ctrl.Type == "resize" {
					var resize dto.PTYWSResizeMessage
					if err := json.Unmarshal(msg.data, &resize); err == nil {
						if err := session.Resize(resize.Cols, resize.Rows); err != nil {
							slog.Error("pty_resize_failed", "error", err, "id", sessionID)
						}
					}
				}
			}
		case err := <-exitCh:
			// Process exited
			exitCode := 0
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				exitCode = exitErr.ExitCode()
			} else if err != nil {
				// Some other error
				exitCode = 1
				slog.Error("pty_process_wait_error", "error", err, "id", sessionID)
			}

			slog.Info("pty_process_exited", "id", sessionID, "exit_code", exitCode)

			// Notify client
			msg := dto.PTYWSEventMessage{
				Type:     "exit",
				ExitCode: exitCode,
			}
			payload, _ := json.Marshal(msg)
			_ = conn.WriteMessage(websocket.TextMessage, payload)
			return
		}
	}
}

// Terminate handles DELETE /api/pty/sessions/{id} - terminates a PTY session.
func (h *PTYHandler) Terminate(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if sessionID == "" {
		h.writeError(w, pty.CodeMissingSessionID, "Session ID is required", http.StatusBadRequest)
		return
	}

	// Terminate is idempotent - success even if session doesn't exist
	if err := h.sessionManager.TerminateSession(sessionID); err != nil {
		// Log error but continue
		slog.Error("pty_terminate_handler_failed", "error", err, "id", sessionID)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]bool{"ok": true}); err != nil {
		slog.Error("pty_terminate_response_write_failed", "error", err, "id", sessionID)
	}
}

// writeError writes a JSON error response.
func (h *PTYHandler) writeError(w http.ResponseWriter, errorCode, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(dto.PTYErrorResponse{
		Error:   errorCode,
		Message: message,
	}); err != nil {
		slog.Error("pty_error_response_write_failed", "error", err, "error_code", errorCode)
	}
}
