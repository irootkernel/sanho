package project

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/irootkernel/sanho/internal/domain/docs"
)

type AddProjectUseCase struct {
	stateRepo    AddProjectStateRepository
	gitManager   DocsRepoManager
	docsReposDir string
}

func NewAddProjectUseCase(
	stateRepo AddProjectStateRepository,
	gitManager DocsRepoManager,
	docsReposDir string,
) *AddProjectUseCase {
	return &AddProjectUseCase{
		stateRepo:    stateRepo,
		gitManager:   gitManager,
		docsReposDir: docsReposDir,
	}
}

type AddProjectInput struct {
	Project     string
	DocsRepoID  string
	DocsRepoURL string
	ActorEmail  string
}

func (uc *AddProjectUseCase) Execute(ctx context.Context, input AddProjectInput) error {
	if input.DocsRepoID == "" ||
		input.DocsRepoID == "." ||
		input.DocsRepoID == ".." ||
		filepath.Base(input.DocsRepoID) != input.DocsRepoID {
		return fmt.Errorf("invalid docs_repo_id: %q", input.DocsRepoID)
	}
	repoPath := filepath.Join(uc.docsReposDir, input.DocsRepoID)

	repoConfig := docs.RepositoryConfig{
		ID:      input.DocsRepoID,
		Path:    repoPath,
		RepoURL: input.DocsRepoURL,
	}

	// Update State (DocsRepo)
	if err := uc.stateRepo.AddDocsRepo(repoConfig); err != nil {
		return err
	}

	// Trigger Sync for this repo
	if err := uc.gitManager.Sync(ctx, []docs.RepositoryConfig{repoConfig}); err != nil {
		// Rollback docs repo entry to keep state consistent if sync fails
		if rollbackErr := uc.stateRepo.DeleteDocsRepo(repoConfig.ID); rollbackErr != nil {
			return fmt.Errorf("failed to sync repo: %w (rollback failed: %v)", err, rollbackErr)
		}
		_ = uc.gitManager.DeleteRepo(ctx, repoConfig.ID, repoConfig.Path)
		return fmt.Errorf("failed to sync repo: %w", err)
	}

	// Update State (Project Mapping)
	if err := uc.stateRepo.AddProject(input.Project, input.DocsRepoID); err != nil {
		return err
	}

	return nil
}
