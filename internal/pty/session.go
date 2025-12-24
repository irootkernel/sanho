package pty

import (
	"errors"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/SeventeenthEarth/kkachi/internal/domain/guardrail"
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
	ExitCh      chan error
	Guardrail   guardrail.Guardrail

	terminateMu sync.Mutex
	terminated  bool

	attachMu sync.Mutex
	attached bool

	inputMu  sync.Mutex
	inputBuf []byte
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
		if _, err := s.Cmd.Process.Wait(); err != nil {
			// Ignore "wait: no child processes" error which can happen
			// if the process already exited
			if !isNoChildError(err) && firstErr == nil {
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
	Guardrail   guardrail.Guardrail
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

	exitCh := make(chan error, 1)
	go func() {
		// Wait for the process to exit and send the result to ExitCh.
		// If Wait returns an error, it might be an ExitError (non-zero exit) or something else.
		exitCh <- cmd.Wait()
		close(exitCh)
	}()

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
		ExitCh:      exitCh,
		Guardrail:   cfg.Guardrail,
	}, nil
}

// Attach flags the session as attached.
// It returns false if the session is already attached.
func (s *Session) Attach() bool {
	s.attachMu.Lock()
	defer s.attachMu.Unlock()

	if s.attached {
		return false
	}
	s.attached = true
	return true
}

// Detach flags the session as detached.
func (s *Session) Detach() {
	s.attachMu.Lock()
	defer s.attachMu.Unlock()
	s.attached = false
}

// IsAttached returns whether the session is currently attached.
func (s *Session) IsAttached() bool {
	s.attachMu.Lock()
	defer s.attachMu.Unlock()
	return s.attached
}

// HandleInput processes raw input from the client.
// It buffers input until a newline character is encountered, then validates
// the command against the Guardrail.
// Returns (blocked, reason, error).
func (s *Session) HandleInput(data []byte) (bool, string, error) {
	s.inputMu.Lock()
	defer s.inputMu.Unlock()

	for _, b := range data {
		if b == '\r' || b == '\n' {
			cmd := string(s.inputBuf)

			if s.Guardrail != nil {
				result := s.Guardrail.Validate(cmd)
				if result.Blocked {
					s.inputBuf = nil // Clear buffer on block
					return true, result.Reason, nil
				}
			}

			s.inputBuf = nil // Clear buffer for next command
			if _, err := s.PTY.Write([]byte{b}); err != nil {
				return false, "", err
			}
		} else if b == '\b' || b == 0x7f {
			if len(s.inputBuf) > 0 {
				s.inputBuf = s.inputBuf[:len(s.inputBuf)-1]
			}
			if _, err := s.PTY.Write([]byte{b}); err != nil {
				return false, "", err
			}
		} else {
			s.inputBuf = append(s.inputBuf, b)
			if _, err := s.PTY.Write([]byte{b}); err != nil {
				return false, "", err
			}
		}
	}

	return false, "", nil
}

// Resize resizes the PTY window.
func (s *Session) Resize(cols, rows uint16) error {
	s.terminateMu.Lock()
	terminated := s.terminated
	s.terminateMu.Unlock()

	if terminated {
		return ErrSessionTerminated
	}

	if s.PTY == nil {
		return errors.New("PTY not initialized")
	}

	return pty.Setsize(s.PTY, &pty.Winsize{
		Cols: cols,
		Rows: rows,
	})
}
