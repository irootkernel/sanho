package docs

import "errors"

var (
	ErrUnknownProject           = errors.New("unknown_project")
	ErrUnknownDocsCommit        = errors.New("unknown_docs_commit")
	ErrUnknownWorkspace         = errors.New("unknown_workspace")
	ErrWorkspaceProjectMismatch = errors.New("workspace_project_mismatch")
	ErrDocsRepoBusy             = errors.New("docs_repo_busy")
	ErrInconsistentPush         = errors.New("internal error: inconsistent push result from repository")
)
