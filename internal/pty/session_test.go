package pty

import (
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
)

// skipIfPTYUnavailable skips the test if PTY is not available in this environment.
// Some CI environments and sandboxed environments may not support PTY operations.
func skipIfPTYUnavailable(t *testing.T) {
	t.Helper()

	// PTY is not supported on Windows
	if runtime.GOOS == "windows" {
		t.Skip("PTY not supported on Windows")
	}

	// Check if /bin/sh exists
	shellPath, err := exec.LookPath("/bin/sh")
	if err != nil {
		t.Skip("Shell /bin/sh not available")
	}

	// Actually try to spawn a PTY to verify it works in this environment
	// Use the same configuration as SpawnSession for accurate detection
	cmd := exec.Command(shellPath)
	cmd.Dir = os.TempDir()
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: 80, Rows: 24})
	if err != nil {
		t.Skipf("PTY spawn not available in this environment: %v", err)
	}

	// Clean up the test PTY
	cmd.Process.Kill()
	cmd.Wait()
	ptmx.Close()
}

// TestSpawnSession_Success tests successful PTY session creation
func TestSpawnSession_Success(t *testing.T) {
	skipIfPTYUnavailable(t)

	// Create a temp directory for the CWD
	tempDir, err := os.MkdirTemp("", "pty-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	cfg := CreateSessionConfig{
		ID:          "test-session",
		WorkspaceID: "test-ws",
		Shell:       "/bin/sh",
		ResolvedCWD: tempDir,
		Cols:        80,
		Rows:        24,
	}

	session, err := SpawnSession(cfg)
	if err != nil {
		t.Fatalf("SpawnSession failed: %v", err)
	}
	defer session.Terminate()

	// Verify session fields
	if session.ID != "test-session" {
		t.Errorf("Expected ID 'test-session', got '%s'", session.ID)
	}
	if session.Shell != "/bin/sh" {
		t.Errorf("Expected shell '/bin/sh', got '%s'", session.Shell)
	}
	if session.ResolvedCWD != tempDir {
		t.Errorf("Expected CWD '%s', got '%s'", tempDir, session.ResolvedCWD)
	}
	if session.Cols != 80 || session.Rows != 24 {
		t.Errorf("Expected 80x24, got %dx%d", session.Cols, session.Rows)
	}
	if session.PTY == nil {
		t.Error("Expected PTY file to be set")
	}
	if session.Cmd == nil || session.Cmd.Process == nil {
		t.Error("Expected process to be started")
	}
}

// TestSpawnSession_DefaultSize tests default terminal size
func TestSpawnSession_DefaultSize(t *testing.T) {
	skipIfPTYUnavailable(t)

	tempDir, err := os.MkdirTemp("", "pty-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	cfg := CreateSessionConfig{
		ID:          "test-session",
		WorkspaceID: "test-ws",
		Shell:       "/bin/sh",
		ResolvedCWD: tempDir,
		Cols:        0, // Should default to 80
		Rows:        0, // Should default to 24
	}

	session, err := SpawnSession(cfg)
	if err != nil {
		t.Fatalf("SpawnSession failed: %v", err)
	}
	defer session.Terminate()

	if session.Cols != 80 {
		t.Errorf("Expected default cols 80, got %d", session.Cols)
	}
	if session.Rows != 24 {
		t.Errorf("Expected default rows 24, got %d", session.Rows)
	}
}

// TestSession_Terminate tests session termination cleans up properly
func TestSession_Terminate(t *testing.T) {
	skipIfPTYUnavailable(t)

	tempDir, err := os.MkdirTemp("", "pty-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	cfg := CreateSessionConfig{
		ID:          "test-session",
		WorkspaceID: "test-ws",
		Shell:       "/bin/sh",
		ResolvedCWD: tempDir,
	}

	session, err := SpawnSession(cfg)
	if err != nil {
		t.Fatalf("SpawnSession failed: %v", err)
	}

	pid := session.Cmd.Process.Pid

	// Terminate the session
	if err := session.Terminate(); err != nil {
		t.Errorf("Terminate failed: %v", err)
	}

	// Give a moment for process cleanup
	time.Sleep(100 * time.Millisecond)

	// Verify process is gone
	process, err := os.FindProcess(pid)
	if err != nil {
		// Process not found, which is expected
		return
	}

	// Try to signal the process - should fail if it's really gone
	err = process.Signal(syscall.Signal(0))
	if err == nil {
		t.Error("Process should be terminated but is still running")
	}
}

// TestSession_TerminateIdempotent tests that Terminate can be called multiple times
func TestSession_TerminateIdempotent(t *testing.T) {
	skipIfPTYUnavailable(t)

	tempDir, err := os.MkdirTemp("", "pty-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	cfg := CreateSessionConfig{
		ID:          "test-session",
		WorkspaceID: "test-ws",
		Shell:       "/bin/sh",
		ResolvedCWD: tempDir,
	}

	session, err := SpawnSession(cfg)
	if err != nil {
		t.Fatalf("SpawnSession failed: %v", err)
	}

	// First terminate
	if err := session.Terminate(); err != nil {
		t.Errorf("First Terminate failed: %v", err)
	}

	// Second terminate should also succeed (idempotent)
	if err := session.Terminate(); err != nil {
		t.Errorf("Second Terminate should succeed (idempotent), got: %v", err)
	}

	// Third terminate
	if err := session.Terminate(); err != nil {
		t.Errorf("Third Terminate should succeed (idempotent), got: %v", err)
	}
}

// TestSessionManager_CreateAndTerminate tests the full lifecycle via manager
func TestSessionManager_CreateAndTerminate(t *testing.T) {
	skipIfPTYUnavailable(t)

	tempDir, err := os.MkdirTemp("", "pty-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	m := NewSessionManager()
	defer m.Close()

	// Create session
	session, err := m.CreateSession(CreateSessionConfig{
		WorkspaceID: "test-ws",
		Shell:       "/bin/sh",
		ResolvedCWD: tempDir,
	})
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// Session ID should be auto-generated
	if session.ID == "" {
		t.Error("Expected session ID to be generated")
	}

	// Session should be retrievable
	retrieved, exists := m.GetSession(session.ID)
	if !exists {
		t.Error("Session should exist after creation")
	}
	if retrieved != session {
		t.Error("Retrieved session should match created session")
	}

	// Session count should be 1
	if m.SessionCount() != 1 {
		t.Errorf("Expected 1 session, got %d", m.SessionCount())
	}

	pid := session.Cmd.Process.Pid

	// Terminate
	if err := m.TerminateSession(session.ID); err != nil {
		t.Errorf("TerminateSession failed: %v", err)
	}

	// Session should no longer exist
	_, exists = m.GetSession(session.ID)
	if exists {
		t.Error("Session should not exist after termination")
	}

	// Session count should be 0
	if m.SessionCount() != 0 {
		t.Errorf("Expected 0 sessions, got %d", m.SessionCount())
	}

	// Process should be gone
	time.Sleep(100 * time.Millisecond)
	process, _ := os.FindProcess(pid)
	if err := process.Signal(syscall.Signal(0)); err == nil {
		t.Error("Process should be terminated")
	}
}

// TestSessionManager_MultipleSessionsCleanup tests cleanup of multiple sessions
func TestSessionManager_MultipleSessionsCleanup(t *testing.T) {
	skipIfPTYUnavailable(t)

	tempDir, err := os.MkdirTemp("", "pty-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	m := NewSessionManager()

	pids := make([]int, 0, 3)

	// Create 3 sessions
	for i := 0; i < 3; i++ {
		session, err := m.CreateSession(CreateSessionConfig{
			WorkspaceID: "test-ws",
			Shell:       "/bin/sh",
			ResolvedCWD: tempDir,
		})
		if err != nil {
			t.Fatalf("CreateSession %d failed: %v", i, err)
		}
		pids = append(pids, session.Cmd.Process.Pid)
	}

	if m.SessionCount() != 3 {
		t.Errorf("Expected 3 sessions, got %d", m.SessionCount())
	}

	// Close manager - should terminate all sessions
	if err := m.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}

	if m.SessionCount() != 0 {
		t.Errorf("Expected 0 sessions after close, got %d", m.SessionCount())
	}

	// All processes should be gone
	time.Sleep(100 * time.Millisecond)
	for i, pid := range pids {
		process, _ := os.FindProcess(pid)
		if err := process.Signal(syscall.Signal(0)); err == nil {
			t.Errorf("Process %d (pid %d) should be terminated", i, pid)
		}
	}
}
