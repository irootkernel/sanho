package project

import (
	"context"

	"github.com/SeventeenthEarth/kkachi/internal/config"
)

// State access needed by AddProject use case.
type AddProjectStateRepository interface {
	AddDocsRepo(config.DocsRepoConfig) error
	DeleteDocsRepo(id string) error
	AddProject(project, repoID string) error
}

// State access needed by DeleteProject use case.
type DeleteProjectStateRepository interface {
	GetDocsRepoID(project string) (string, bool)
	DeleteProject(project string) error
	GetRepoUsage() map[string]int
	GetDocsRepo(id string) (config.DocsRepoConfig, bool)
	DeleteDocsRepo(id string) error
	HasWorkspacesForProject(project string) bool
	// DeleteWorkspacesByProject removes all workspaces registered to the project.
	DeleteWorkspacesByProject(project string) error
}

type DocsRepoManager interface {
	Sync(ctx context.Context, repos []config.DocsRepoConfig) error
	DeleteRepo(ctx context.Context, repoID string, path string) error
}
