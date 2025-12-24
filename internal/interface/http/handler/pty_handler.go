package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"

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
}

// NewPTYHandler creates a new PTY handler.
func NewPTYHandler(
	sessionManager *pty.SessionManager,
	workspaceLookup WorkspaceLookup,
	config pty.Config,
) *PTYHandler {
	return &PTYHandler{
		sessionManager:  sessionManager,
		workspaceLookup: workspaceLookup,
		config:          config,
	}
}

// Create handles POST /api/pty/sessions - creates a new PTY session.
func (h *PTYHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req dto.CreatePTYSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, "invalid_request_body", "Failed to parse request body", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.WorkspaceID == "" {
		h.writeError(w, "missing_workspace_id", "workspace_id is required", http.StatusBadRequest)
		return
	}

	// Lookup workspace
	ws, err := h.workspaceLookup.Get(r.Context(), workspace.WorkspaceID(req.WorkspaceID))
	if err != nil {
		log.Printf("PTY: workspace lookup error: %v", err)
		h.writeError(w, "internal_server_error", "Failed to lookup workspace", http.StatusInternalServerError)
		return
	}
	if ws == nil {
		h.writeError(w, "unknown_workspace", fmt.Sprintf("Workspace '%s' not found", req.WorkspaceID), http.StatusBadRequest)
		return
	}

	// Resolve CWD
	resolvedCWD, err := pty.ResolveCWD(ws.LocalPath, req.CwdRel)
	if err != nil {
		if errors.Is(err, pty.ErrAbsolutePathNotAllowed) {
			h.writeError(w, "absolute_path_not_allowed", "cwd_rel must be a relative path", http.StatusBadRequest)
			return
		}
		if errors.Is(err, pty.ErrCWDTraversal) {
			h.writeError(w, "cwd_traversal_attempt", "Path traversal not allowed", http.StatusBadRequest)
			return
		}
		h.writeError(w, "invalid_cwd", err.Error(), http.StatusBadRequest)
		return
	}

	// Determine shell
	shell := req.Shell
	if shell == "" {
		shell = h.config.DefaultShell
	}

	// Validate shell
	if err := pty.ValidateShell(shell, h.config.AllowedShells); err != nil {
		h.writeError(w, "shell_not_allowed", fmt.Sprintf("Shell '%s' is not allowed", shell), http.StatusBadRequest)
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
			"invalid_terminal_size",
			fmt.Sprintf("cols must be between %d and %d, rows between %d and %d", pty.MinCols, pty.MaxCols, pty.MinRows, pty.MaxRows),
			http.StatusBadRequest,
		)
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
		if errors.Is(err, pty.ErrPTYSpawnFailed) {
			h.writeError(w, "pty_spawn_failed", "Failed to create PTY session", http.StatusInternalServerError)
			return
		}
		log.Printf("PTY: session creation error: %v", err)
		h.writeError(w, "internal_server_error", "Failed to create session", http.StatusInternalServerError)
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
		log.Printf("PTY: failed to write response: %v", err)
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
	sessionID := r.PathValue("id")
	if sessionID == "" {
		h.writeError(w, "missing_session_id", "Session ID is required", http.StatusBadRequest)
		return
	}

	session, exists := h.sessionManager.GetSession(sessionID)
	if !exists {
		h.writeError(w, "session_not_found", "Session not found", http.StatusNotFound)
		return
	}

	// Enforce single-attach policy
	if !session.Attach() {
		h.writeError(w, "session_already_attached", "Session is already attached to another client", http.StatusConflict)
		return
	}
	defer session.Detach()

	// Upgrade to WebSocket
	rawConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("PTY: failed to upgrade to WS: %v", err)
		return
	}
	conn := &safeWSConn{conn: rawConn}
	defer conn.Close()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// PTY -> WS loop
	go func() {
		defer cancel()
		buf := make([]byte, 8192)
		for {
			n, err := session.PTY.Read(buf)
			if err != nil {
				// Process might have exited
				return
			}
			if err := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
				return
			}
		}
	}()

	// WS -> PTY loop
	for {
		messageType, p, err := conn.ReadMessage()
		if err != nil {
			break
		}

		select {
		case <-ctx.Done():
			return
		default:
		}

		if messageType == websocket.BinaryMessage {
			// Raw input to PTY
			if _, err := session.PTY.Write(p); err != nil {
				break
			}
		} else if messageType == websocket.TextMessage {
			// Control messages (JSON)
			var ctrl dto.PTYWSControlMessage
			if err := json.Unmarshal(p, &ctrl); err != nil {
				continue
			}

			if ctrl.Type == "resize" {
				var resize dto.PTYWSResizeMessage
				if err := json.Unmarshal(p, &resize); err == nil {
					_ = session.Resize(resize.Cols, resize.Rows)
				}
			}
		}
	}
}

// Terminate handles DELETE /api/pty/sessions/{id} - terminates a PTY session.
func (h *PTYHandler) Terminate(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if sessionID == "" {
		h.writeError(w, "missing_session_id", "Session ID is required", http.StatusBadRequest)
		return
	}

	// Terminate is idempotent - success even if session doesn't exist
	if err := h.sessionManager.TerminateSession(sessionID); err != nil {
		log.Printf("PTY: session termination error: %v", err)
		// Still return success for idempotency
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]bool{"ok": true}); err != nil {
		log.Printf("PTY: failed to write response: %v", err)
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
		log.Printf("PTY: failed to write error response: %v", err)
	}
}
