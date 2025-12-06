package dto

// DocsPushRequest is the request body for POST /docs/push
type DocsPushRequest struct {
	WorkspaceID  string `json:"workspace_id"`
	BaseDocsHash string `json:"base_docs_hash"`
	DocsSnapshot string `json:"docs_snapshot"` // base64-encoded tar.gz
	ActorEmail   string `json:"actor_email"`
}

// DocsPushResponse is the response body for POST /docs/push
type DocsPushResponse struct {
	Ok              bool   `json:"ok"`
	Status          string `json:"status"`
	NewDocsHash     string `json:"new_docs_hash,omitempty"`
	CurrentDocsHash string `json:"current_docs_hash,omitempty"`
	Error           string `json:"error,omitempty"`
}
