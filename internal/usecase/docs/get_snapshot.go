package docs

import (
	"context"

	domain "github.com/irootkernel/sanho/internal/domain/docs"
)

type GetDocsSnapshotUseCase interface {
	Execute(ctx context.Context, project domain.ProjectName, commit domain.CommitHash) (domain.DocsSnapshot, domain.CommitHash, error)
}

type getDocsSnapshotUseCase struct {
	repo domain.DocsReadRepository
}

func NewGetDocsSnapshotUseCase(repo domain.DocsReadRepository) GetDocsSnapshotUseCase {
	return &getDocsSnapshotUseCase{repo: repo}
}

func (u *getDocsSnapshotUseCase) Execute(ctx context.Context, project domain.ProjectName, commit domain.CommitHash) (domain.DocsSnapshot, domain.CommitHash, error) {
	if commit.IsZero() {
		head, err := u.repo.GetHead(ctx, project)
		if err != nil {
			return nil, "", err
		}
		commit = head
	}

	snapshot, resolvedCommit, err := u.repo.GetSnapshot(ctx, project, commit)
	if err != nil {
		return nil, "", err
	}
	return snapshot, resolvedCommit, nil
}
