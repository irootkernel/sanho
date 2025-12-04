package project

import "errors"

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
	repoID, ok := uc.stateRepo.GetDocsRepoID(project)
	if !ok {
		return ErrUnknownProject
	}

	// TODO: Check workspaces
	// if hasWorkspaces(project) && !force {
	// 	return ErrProjectHasWorkspaces
	// }

	if err := uc.stateRepo.DeleteProject(project); err != nil {
		return err
	}

	// Check usage
	usage := uc.stateRepo.GetRepoUsage()
	if usage[repoID] == 0 {
		// Repo not used anymore
		if repoConfig, ok := uc.stateRepo.GetDocsRepo(repoID); ok {
			if err := uc.gitManager.DeleteRepo(repoConfig.Path); err != nil {
				return err
			}
		}
		if err := uc.stateRepo.DeleteDocsRepo(repoID); err != nil {
			return err
		}
	}

	return nil
}
