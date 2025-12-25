// Package pty provides PTY session management for the Kkachi server.
// It handles terminal session creation, lifecycle management, and CWD validation.
package pty

import "errors"

// PTY Error Codes (used in API responses)
const (
	CodeUnknownWorkspace       = "unknown_workspace"
	CodeCWDTraversal           = "cwd_traversal_attempt"
	CodeSessionNotFound        = "session_not_found"
	CodePTYSpawnFailed         = "pty_spawn_failed"
	CodeShellNotAllowed        = "shell_not_allowed"
	CodeAbsolutePathNotAllowed = "absolute_path_not_allowed"
	CodeSessionTerminated      = "session_terminated"
	CodeSessionAlreadyAttached = "session_already_attached"
	CodeSessionLimitExceeded   = "session_limit_exceeded"
	CodeCommandBlocked         = "command_blocked"
	CodeMissingWorkspaceID     = "missing_workspace_id"
	CodeInvalidRequestBody     = "invalid_request_body"
	CodeInvalidCWD             = "invalid_cwd"
	CodeInvalidTerminalSize    = "invalid_terminal_size"
	CodeMissingSessionID       = "missing_session_id"
	CodeUnauthorized           = "unauthorized"
	CodeInternalServerError    = "internal_server_error"
)

// Error definitions
var (
	// ErrUnknownWorkspace indicates the workspace_id does not exist.
	ErrUnknownWorkspace = errors.New(CodeUnknownWorkspace)

	// ErrCWDTraversal indicates a path traversal attempt was detected.
	ErrCWDTraversal = errors.New(CodeCWDTraversal)

	// ErrSessionNotFound indicates the session does not exist.
	ErrSessionNotFound = errors.New(CodeSessionNotFound)

	// ErrPTYSpawnFailed indicates PTY creation failed.
	ErrPTYSpawnFailed = errors.New(CodePTYSpawnFailed)

	// ErrShellNotAllowed indicates the requested shell is not in the allowlist.
	ErrShellNotAllowed = errors.New(CodeShellNotAllowed)

	// ErrAbsolutePathNotAllowed indicates an absolute path was provided for cwd_rel.
	ErrAbsolutePathNotAllowed = errors.New(CodeAbsolutePathNotAllowed)

	// ErrSessionTerminated indicates the session has already been terminated.
	ErrSessionTerminated = errors.New(CodeSessionTerminated)

	// ErrSessionAlreadyAttached indicates the session is already attached to another client.
	ErrSessionAlreadyAttached = errors.New(CodeSessionAlreadyAttached)

	// ErrSessionLimitExceeded indicates the maximum number of sessions has been reached.
	ErrSessionLimitExceeded = errors.New(CodeSessionLimitExceeded)
)
