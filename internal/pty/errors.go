// Package pty provides PTY session management for the Kkachi server.
// It handles terminal session creation, lifecycle management, and CWD validation.
package pty

import "errors"

// Error codes for PTY operations.
var (
	// ErrUnknownWorkspace indicates the workspace_id does not exist.
	ErrUnknownWorkspace = errors.New("unknown_workspace")

	// ErrCWDTraversal indicates a path traversal attempt was detected.
	ErrCWDTraversal = errors.New("cwd_traversal_attempt")

	// ErrSessionNotFound indicates the session does not exist.
	ErrSessionNotFound = errors.New("session_not_found")

	// ErrPTYSpawnFailed indicates PTY creation failed.
	ErrPTYSpawnFailed = errors.New("pty_spawn_failed")

	// ErrShellNotAllowed indicates the requested shell is not in the allowlist.
	ErrShellNotAllowed = errors.New("shell_not_allowed")

	// ErrAbsolutePathNotAllowed indicates an absolute path was provided for cwd_rel.
	ErrAbsolutePathNotAllowed = errors.New("absolute_path_not_allowed")

	// ErrSessionTerminated indicates the session has already been terminated.
	ErrSessionTerminated = errors.New("session_terminated")

	// ErrSessionAlreadyAttached indicates the session is already attached to another client.
	ErrSessionAlreadyAttached = errors.New("session_already_attached")
)
