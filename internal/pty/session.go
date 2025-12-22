package pty

import (
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
)

// Session represents an active PTY session.
type Session struct {
	ID          string
	WorkspaceID string
	Shell       string
	ResolvedCWD string
	Cols        uint16
	Rows        uint16
	Cmd         *exec.Cmd
	PTY         *os.File
	CreatedAt   time.Time
	terminateMu sync.Mutex
	terminated  bool
}

// Terminate gracefully terminates the PTY session.
// It kills the process (and its process group), waits for exit, and closes the PTY FD.
// This method is idempotent - calling it multiple times is safe.
func (s *Session) Terminate() error {
	s.terminateMu.Lock()
	if s.terminated {
		s.terminateMu.Unlock()
		return nil
	}
	s.terminated = true
	s.terminateMu.Unlock()

	var firstErr error

	// Kill the process group to ensure all child processes are terminated
	if s.Cmd != nil && s.Cmd.Process != nil {
		// Try to kill the process group first (negative PID)
		if pgid, err := syscall.Getpgid(s.Cmd.Process.Pid); err == nil {
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
		} else {
			// Fall back to killing just the process
			_ = s.Cmd.Process.Kill()
		}

		// Wait for process to exit (prevents zombie)
		if _, err := s.Cmd.Process.Wait(); err != nil && firstErr == nil {
			// Ignore "wait: no child processes" error which can happen
			// if the process already exited
			if !isNoChildError(err) {
				firstErr = err
			}
		}
	}

	// Close the PTY file descriptor
	if s.PTY != nil {
		if err := s.PTY.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}

// isNoChildError checks if the error is a "no child processes" error.
func isNoChildError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return errStr == "wait: no child processes" ||
		errStr == "waitid: no child processes"
}

// CreateSessionConfig holds configuration for creating a new PTY session.
type CreateSessionConfig struct {
	ID          string
	WorkspaceID string
	Shell       string
	ResolvedCWD string
	Cols        uint16
	Rows        uint16
}

// SpawnSession creates a new PTY session with the given configuration.
// It starts the shell process in a new PTY with the specified working directory.
func SpawnSession(cfg CreateSessionConfig) (*Session, error) {
	// Set defaults
	cols := cfg.Cols
	if cols == 0 {
		cols = 80
	}
	rows := cfg.Rows
	if rows == 0 {
		rows = 24
	}

	// Create the command
	cmd := exec.Command(cfg.Shell)
	cmd.Dir = cfg.ResolvedCWD
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	// Start the command with a PTY
	// Use Setpgid to create a new process group (for clean termination)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{
		Cols: cols,
		Rows: rows,
	})
	if err != nil {
		return nil, ErrPTYSpawnFailed
	}

	return &Session{
		ID:          cfg.ID,
		WorkspaceID: cfg.WorkspaceID,
		Shell:       cfg.Shell,
		ResolvedCWD: cfg.ResolvedCWD,
		Cols:        cols,
		Rows:        rows,
		Cmd:         cmd,
		PTY:         ptmx,
		CreatedAt:   time.Now(),
	}, nil
}
