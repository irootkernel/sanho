package docs

import (
	"context"

	domain "github.com/irootkernel/sanho/internal/domain/docs"
)

type GetDocsHeadUseCase interface {
	Execute(ctx context.Context, project domain.ProjectName) (domain.CommitHash, error)
}

type getDocsHeadUseCase struct {
	docsRepo domain.DocsReadRepository
}

func NewGetDocsHeadUseCase(docsRepo domain.DocsReadRepository) GetDocsHeadUseCase {
	return &getDocsHeadUseCase{
		docsRepo: docsRepo,
	}
}

func (u *getDocsHeadUseCase) Execute(ctx context.Context, project domain.ProjectName) (domain.CommitHash, error) {
	return u.docsRepo.GetHead(ctx, project)
}
