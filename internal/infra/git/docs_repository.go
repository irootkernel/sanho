package git

import (
	"context"
	"errors"

	"github.com/SeventeenthEarth/kkachi/internal/config"
	"github.com/SeventeenthEarth/kkachi/internal/domain/docs"
	"github.com/SeventeenthEarth/kkachi/internal/infra/state"
)

var ErrRepoConfigMissing = errors.New("repo_config_missing")

type GitDocsRepository struct {
	git       *Client
	stateRepo *state.FileStateRepository
}

func NewGitDocsRepository(git *Client, stateRepo *state.FileStateRepository) *GitDocsRepository {
	return &GitDocsRepository{
		git:       git,
		stateRepo: stateRepo,
	}
}

func (r *GitDocsRepository) GetHead(ctx context.Context, project docs.ProjectName) (docs.CommitHash, error) {
	repoConfig, err := r.getRepoConfig(project)
	if err != nil {
		return "", err
	}

	head, err := r.git.RevParseHead(ctx, repoConfig.Path)
	if err != nil {
		return "", err
	}
	return docs.CommitHash(head), nil
}

func (r *GitDocsRepository) GetSnapshot(ctx context.Context, project docs.ProjectName, commit docs.CommitHash) (docs.DocsSnapshot, docs.CommitHash, error) {
	repoConfig, err := r.getRepoConfig(project)
	if err != nil {
		return nil, "", err
	}

	resolvedCommit, err := r.git.ResolveCommit(ctx, repoConfig.Path, string(commit))
	if err != nil {
		if errors.Is(err, ErrUnknownCommit) {
			return nil, "", docs.ErrUnknownDocsCommit
		}
		return nil, "", err
	}

	snapshot, err := r.git.ArchiveDocs(ctx, repoConfig.Path, resolvedCommit)
	if err != nil {
		return nil, "", err
	}
	return docs.DocsSnapshot(snapshot), docs.CommitHash(resolvedCommit), nil
}

func (r *GitDocsRepository) getRepoConfig(project docs.ProjectName) (config.DocsRepoConfig, error) {
	repoID, ok := r.stateRepo.GetDocsRepoID(string(project))
	if !ok {
		return config.DocsRepoConfig{}, docs.ErrUnknownProject
	}
	repoConfig, ok := r.stateRepo.GetDocsRepo(repoID)
	if !ok {
		return config.DocsRepoConfig{}, ErrRepoConfigMissing
	}
	return repoConfig, nil
}
