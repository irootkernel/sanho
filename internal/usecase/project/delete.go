package project

import (
	"context"
	"errors"
)

var (
	ErrProjectHasWorkspaces = errors.New("project_has_workspaces")
	ErrUnknownProject       = errors.New("unknown_project")
)

type DeleteProjectUseCase struct {
	stateRepo  DeleteProjectStateRepository
	gitManager DocsRepoManager
}

func NewDeleteProjectUseCase(stateRepo DeleteProjectStateRepository, gitManager DocsRepoManager) *DeleteProjectUseCase {
	return &DeleteProjectUseCase{
		stateRepo:  stateRepo,
		gitManager: gitManager,
	}
}

func (uc *DeleteProjectUseCase) Execute(project string, force bool) error {
	ctx := context.Background()
	repoID, ok := uc.stateRepo.GetDocsRepoID(project)
	if !ok {
		return ErrUnknownProject
	}

	// Check if project has registered workspaces
	if !force && uc.stateRepo.HasWorkspacesForProject(project) {
		return ErrProjectHasWorkspaces
	}

	// With force, proactively remove all workspaces tied to the project.
	if force {
		if err := uc.stateRepo.DeleteWorkspacesByProject(project); err != nil {
			return err
		}
	}

	if err := uc.stateRepo.DeleteProject(project); err != nil {
		return err
	}

	// Check usage
	usage := uc.stateRepo.GetRepoUsage()
	if usage[repoID] == 0 {
		// Repo not used anymore
		if repoConfig, ok := uc.stateRepo.GetDocsRepo(repoID); ok {
			if err := uc.gitManager.DeleteRepo(ctx, repoID, repoConfig.Path); err != nil {
				return err
			}
		}
		if err := uc.stateRepo.DeleteDocsRepo(repoID); err != nil {
			return err
		}
	}

	return nil
}
