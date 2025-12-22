package dto

// CreatePTYSessionRequest represents the request body for creating a PTY session.
type CreatePTYSessionRequest struct {
	WorkspaceID string `json:"workspace_id"`
	CwdRel      string `json:"cwd_rel,omitempty"` // Relative path within workspace
	Shell       string `json:"shell,omitempty"`   // Shell to use (default: from config)
	Cols        int    `json:"cols,omitempty"`    // Terminal columns (default: 80)
	Rows        int    `json:"rows,omitempty"`    // Terminal rows (default: 24)
}

// CreatePTYSessionResponse represents the response for a successful session creation.
type CreatePTYSessionResponse struct {
	SessionID   string `json:"session_id"`
	WsURL       string `json:"ws_url"`
	ResolvedCWD string `json:"resolved_cwd"`
}

// PTYErrorResponse represents an error response from PTY operations.
type PTYErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}
