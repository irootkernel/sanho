package workspace

import (
	"context"
	"errors"

	"github.com/irootkernel/sanho/internal/domain/docs"
	domainWorkspace "github.com/irootkernel/sanho/internal/domain/workspace"
)

var ErrDocsHashNotInCurrentHistory = errors.New("docs hash is not in current history")

type ReportDocsHashCommand struct {
	WorkspaceID domainWorkspace.WorkspaceID
	DocsHash    docs.CommitHash
	ActorEmail  string
}

type ReportDocsHashUseCase interface {
	Execute(ctx context.Context, cmd ReportDocsHashCommand) error
}

type reportDocsHashUseCase struct {
	workspaceRepo domainWorkspace.WorkspaceRepository
	docsRepo      docs.DocsStatusRepository
}

func NewReportDocsHashUseCase(
	workspaceRepo domainWorkspace.WorkspaceRepository,
	docsRepo docs.DocsStatusRepository,
) ReportDocsHashUseCase {
	return &reportDocsHashUseCase{
		workspaceRepo: workspaceRepo,
		docsRepo:      docsRepo,
	}
}

func (u *reportDocsHashUseCase) Execute(ctx context.Context, cmd ReportDocsHashCommand) error {
	ws, err := u.workspaceRepo.Get(ctx, cmd.WorkspaceID)
	if err != nil {
		return err
	}
	if ws == nil {
		return ErrUnknownWorkspace
	}

	comparison, err := u.docsRepo.CompareProjectCommits(ctx, ws.Project, cmd.DocsHash, nil)
	if err != nil {
		return err
	}
	switch comparison.ReferenceToHead.Status {
	case docs.CommitRelationSame, docs.CommitRelationBehind:
		return u.workspaceRepo.UpdateDocsHash(ctx, ws.ID, cmd.DocsHash, cmd.ActorEmail)
	default:
		return ErrDocsHashNotInCurrentHistory
	}
}
