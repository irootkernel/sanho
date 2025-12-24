package pty

import (
	"testing"
)

func TestNewSessionManager(t *testing.T) {
	m := NewSessionManager(nil)
	if m == nil {
		t.Fatal("NewSessionManager(nil) returned nil")
	}
	if m.SessionCount() != 0 {
		t.Errorf("Expected 0 sessions, got %d", m.SessionCount())
	}
}

func TestSessionManager_TerminateIdempotent(t *testing.T) {
	m := NewSessionManager(nil)

	// Terminating non-existent session should succeed (idempotent)
	err := m.TerminateSession("nonexistent")
	if err != nil {
		t.Errorf("TerminateSession for non-existent should succeed, got error: %v", err)
	}

	// Multiple terminates should all succeed
	for i := 0; i < 3; i++ {
		err := m.TerminateSession("nonexistent")
		if err != nil {
			t.Errorf("Repeated TerminateSession should succeed, got error: %v", err)
		}
	}
}

func TestSessionManager_GetSession_NotFound(t *testing.T) {
	m := NewSessionManager(nil)

	session, exists := m.GetSession("nonexistent")
	if exists {
		t.Error("GetSession should return false for non-existent session")
	}
	if session != nil {
		t.Error("GetSession should return nil for non-existent session")
	}
}

func TestSessionManager_ListSessions_Empty(t *testing.T) {
	m := NewSessionManager(nil)

	sessions := m.ListSessions()
	if len(sessions) != 0 {
		t.Errorf("ListSessions should return empty list, got %d sessions", len(sessions))
	}
}

func TestSessionManager_Close_Empty(t *testing.T) {
	m := NewSessionManager(nil)

	// Close should succeed on empty manager
	err := m.Close()
	if err != nil {
		t.Errorf("Close on empty manager should succeed, got error: %v", err)
	}
}

func TestGenerateSessionID(t *testing.T) {
	ids := make(map[string]bool)

	// Generate multiple IDs and ensure they're unique
	for i := 0; i < 100; i++ {
		id := generateSessionID()
		if id == "" {
			t.Error("generateSessionID returned empty string")
		}
		if len(id) != 32 { // 16 bytes = 32 hex chars
			t.Errorf("Expected 32 char ID, got %d chars", len(id))
		}
		if ids[id] {
			t.Errorf("Duplicate ID generated: %s", id)
		}
		ids[id] = true
	}
}
