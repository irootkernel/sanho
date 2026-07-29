package project

import (
	"context"

	"github.com/irootkernel/sanho/internal/domain/docs"
)

// State access needed by AddProject use case.
type AddProjectStateRepository interface {
	AddDocsRepo(docs.RepositoryConfig) error
	DeleteDocsRepo(id string) error
	AddProject(project, repoID string) error
}

// State access needed by DeleteProject use case.
type DeleteProjectStateRepository interface {
	GetDocsRepoID(project string) (string, bool)
	DeleteProject(project string) error
	GetRepoUsage() map[string]int
	GetDocsRepo(id string) (docs.RepositoryConfig, bool)
	DeleteDocsRepo(id string) error
	HasWorkspacesForProject(project string) bool
	// DeleteWorkspacesByProject removes all workspaces registered to the project.
	DeleteWorkspacesByProject(project string) error
}

type DocsRepoManager interface {
	Sync(ctx context.Context, repos []docs.RepositoryConfig) error
	DeleteRepo(ctx context.Context, repoID string, path string) error
}
