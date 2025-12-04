package git

import (
	"context"
	"errors"

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
	repoID, ok := r.stateRepo.GetDocsRepoID(string(project))
	if !ok {
		return "", docs.ErrUnknownProject
	}
	repoConfig, ok := r.stateRepo.GetDocsRepo(repoID)
	if !ok {
		return "", ErrRepoConfigMissing
	}

	head, err := r.git.RevParseHead(ctx, repoConfig.Path)
	if err != nil {
		return "", err
	}
	return docs.CommitHash(head), nil
}
