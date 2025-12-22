package pty

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
)

// SessionManager manages active PTY sessions in memory.
type SessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

// NewSessionManager creates a new session manager.
func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessions: make(map[string]*Session),
	}
}

// CreateSession creates and registers a new PTY session.
func (m *SessionManager) CreateSession(cfg CreateSessionConfig) (*Session, error) {
	// Generate session ID if not provided
	if cfg.ID == "" {
		cfg.ID = generateSessionID()
	}

	session, err := SpawnSession(cfg)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.sessions[session.ID] = session
	m.mu.Unlock()

	return session, nil
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

	return session.Terminate()
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
