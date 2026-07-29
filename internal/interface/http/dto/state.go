package dto

// DaemonStateResponse is the response DTO for GET /state.
type DaemonStateResponse struct {
	DocsHeads  map[string]string  `json:"docs_heads"`
	Workspaces []WorkspaceSummary `json:"workspaces"`
}

// WorkspaceSummary contains the summary of a registered workspace.
type WorkspaceSummary struct {
	WorkspaceID    string  `json:"workspace_id"`
	Project        string  `json:"project"`
	DocsRepoID     string  `json:"docs_repo_id"`
	LocalPath      string  `json:"local_path"`
	RepoURL        string  `json:"repo_url"`
	DocsHash       string  `json:"docs_hash"`
	LastReportedAt *string `json:"last_reported_at,omitempty"`
	LastActorEmail string  `json:"last_actor_email"`
}
