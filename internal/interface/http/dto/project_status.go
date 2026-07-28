package dto

type CommitRelation struct {
	Status string `json:"status"`
	Ahead  int    `json:"ahead"`
	Behind int    `json:"behind"`
}

type ProjectStatusWorkspace struct {
	WorkspaceID         string         `json:"workspace_id"`
	Project             string         `json:"project"`
	DocsRepoID          string         `json:"docs_repo_id"`
	LocalPath           string         `json:"local_path"`
	RepoURL             string         `json:"repo_url"`
	DocsHash            string         `json:"docs_hash"`
	LastReportedAt      *string        `json:"last_reported_at,omitempty"`
	LastActorEmail      string         `json:"last_actor_email"`
	RelativeToReference CommitRelation `json:"relative_to_reference"`
	RelativeToHead      CommitRelation `json:"relative_to_head"`
}

type ProjectStatusResponse struct {
	Project              string                   `json:"project"`
	ReferenceWorkspaceID string                   `json:"reference_workspace_id"`
	ReferenceDocsHash    string                   `json:"reference_docs_hash"`
	DocsHead             string                   `json:"docs_head"`
	ReferenceToHead      CommitRelation           `json:"reference_to_head"`
	Workspaces           []ProjectStatusWorkspace `json:"workspaces"`
}
