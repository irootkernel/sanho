package project

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/SeventeenthEarth/kkachi/internal/config"
)

type AddProjectUseCase struct {
	stateRepo  AddProjectStateRepository
	gitManager DocsRepoManager
}

func NewAddProjectUseCase(stateRepo AddProjectStateRepository, gitManager DocsRepoManager) *AddProjectUseCase {
	return &AddProjectUseCase{
		stateRepo:  stateRepo,
		gitManager: gitManager,
	}
}

type AddProjectInput struct {
	Project     string
	DocsRepoID  string
	DocsRepoURL string
	ActorEmail  string
}

func (uc *AddProjectUseCase) Execute(ctx context.Context, input AddProjectInput) error {
	// Determine local path
	// Use relative path "docs_repos/<id>" for now, or absolute if we had a base dir config.
	// I'll use "docs_repos/<id>" relative to CWD.
	repoPath, err := filepath.Abs(filepath.Join("docs_repos", input.DocsRepoID))
	if err != nil {
		return fmt.Errorf("could not determine absolute path for repo: %w", err)
	}

	repoConfig := config.DocsRepoConfig{
		ID:      input.DocsRepoID,
		Path:    repoPath,
		RepoURL: input.DocsRepoURL,
	}

	// Update State (DocsRepo)
	if err := uc.stateRepo.AddDocsRepo(repoConfig); err != nil {
		return err
	}

	// Trigger Sync for this repo
	if err := uc.gitManager.Sync(ctx, []config.DocsRepoConfig{repoConfig}); err != nil {
		// Rollback docs repo entry to keep state consistent if sync fails
		if rollbackErr := uc.stateRepo.DeleteDocsRepo(repoConfig.ID); rollbackErr != nil {
			return fmt.Errorf("failed to sync repo: %w (rollback failed: %v)", err, rollbackErr)
		}
		_ = uc.gitManager.DeleteRepo(repoConfig.Path)
		return fmt.Errorf("failed to sync repo: %w", err)
	}

	// Update State (Project Mapping)
	if err := uc.stateRepo.AddProject(input.Project, input.DocsRepoID); err != nil {
		return err
	}

	return nil
}
