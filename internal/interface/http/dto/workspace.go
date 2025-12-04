package dto

type RegisterWorkspaceRequest struct {
	Project    string `json:"project"`
	LocalPath  string `json:"local_path"`
	RepoURL    string `json:"repo_url"`
	ActorEmail string `json:"actor_email"`
}

type RegisterWorkspaceResponse struct {
	WorkspaceID     string `json:"workspace_id"`
	CurrentDocsHead string `json:"current_docs_head"`
}
