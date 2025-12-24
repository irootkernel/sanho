package pty

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"sync"

	"github.com/SeventeenthEarth/kkachi/internal/domain/guardrail"
)

// SessionManager manages active PTY sessions in memory.
type SessionManager struct {
	mu        sync.RWMutex
	sessions  map[string]*Session
	guardrail guardrail.Guardrail
}

// NewSessionManager creates a new session manager.
func NewSessionManager(g guardrail.Guardrail) *SessionManager {
	return &SessionManager{
		sessions:  make(map[string]*Session),
		guardrail: g,
	}
}

// CreateSession creates and registers a new PTY session.
func (m *SessionManager) CreateSession(cfg CreateSessionConfig) (*Session, error) {
	// Generate session ID if not provided
	if cfg.ID == "" {
		cfg.ID = generateSessionID()
	}

	// Use manager's guardrail if not provided in config
	if cfg.Guardrail == nil {
		cfg.Guardrail = m.guardrail
	}

	session, err := SpawnSession(cfg)
	if err != nil {
		slog.Error("pty_session_create_failed", "error", err, "workspace_id", cfg.WorkspaceID)
		return nil, err
	}

	m.mu.Lock()
	m.sessions[session.ID] = session
	m.mu.Unlock()

	slog.Info("pty_session_created", "id", session.ID, "workspace_id", cfg.WorkspaceID, "shell", cfg.Shell)

	return session, nil
}

// AddSession directly adds a session to the manager.
// This is primarily used for testing or internal registration.
func (m *SessionManager) AddSession(session *Session) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[session.ID] = session
	slog.Debug("pty_session_added", "id", session.ID, "workspace_id", session.WorkspaceID)
}

// GetSession retrieves a session by ID.
// Returns nil and false if the session does not exist.
func (m *SessionManager) GetSession(id string) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	session, exists := m.sessions[id]
	return session, exists
}

// TerminateSession terminates and removes a session by ID.
// This method is idempotent - calling it for a non-existent session returns nil.
func (m *SessionManager) TerminateSession(id string) error {
	m.mu.Lock()
	session, exists := m.sessions[id]
	if exists {
		delete(m.sessions, id)
	}
	m.mu.Unlock()

	if !exists {
		// Idempotent: already terminated or never existed
		return nil
	}

	err := session.Terminate()
	if err != nil {
		slog.Error("pty_session_termination_failed", "id", id, "error", err)
	} else {
		slog.Info("pty_session_terminated", "id", id)
	}
	return err
}

// ListSessions returns all active session IDs.
func (m *SessionManager) ListSessions() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ids := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	return ids
}

// SessionCount returns the number of active sessions.
func (m *SessionManager) SessionCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

// Close terminates all sessions and cleans up resources.
func (m *SessionManager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var firstErr error
	for id, session := range m.sessions {
		if err := session.Terminate(); err != nil && firstErr == nil {
			firstErr = err
		}
		delete(m.sessions, id)
	}
	return firstErr
}

// generateSessionID generates a random session ID.
func generateSessionID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to less secure but available randomness
		// This should never happen in practice
		return "fallback-" + hex.EncodeToString(bytes[:8])
	}
	return hex.EncodeToString(bytes)
}
