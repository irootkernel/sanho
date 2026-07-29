package workspace

import (
	"context"
	"fmt"
	"time"

	"github.com/irootkernel/sanho/internal/domain/docs"
	"github.com/irootkernel/sanho/internal/domain/workspace"
)

type RegisterWorkspaceUseCase interface {
	Execute(ctx context.Context, cmd RegisterWorkspaceCommand) (*workspace.Workspace, error)
}

type RegisterWorkspaceCommand struct {
	Project    string
	LocalPath  string
	RepoURL    string
	ActorEmail string
}

type ProjectDocsRepoMapper interface {
	GetDocsRepoID(project string) (string, bool)
}

type registerWorkspaceUseCase struct {
	docsRepo      docs.DocsReadRepository
	workspaceRepo workspace.WorkspaceRepository
	projectMapper ProjectDocsRepoMapper
	clock         Clock
}

type Clock interface {
	Now() time.Time
}

type RealClock struct{}

func (RealClock) Now() time.Time { return time.Now() }

func NewRegisterWorkspaceUseCase(docsRepo docs.DocsReadRepository, workspaceRepo workspace.WorkspaceRepository, projectMapper ProjectDocsRepoMapper, clock Clock) RegisterWorkspaceUseCase {
	if clock == nil {
		clock = RealClock{}
	}
	return &registerWorkspaceUseCase{
		docsRepo:      docsRepo,
		workspaceRepo: workspaceRepo,
		projectMapper: projectMapper,
		clock:         clock,
	}
}

func (u *registerWorkspaceUseCase) Execute(ctx context.Context, cmd RegisterWorkspaceCommand) (*workspace.Workspace, error) {
	// 1. Get DocsRepoID
	repoIDStr, ok := u.projectMapper.GetDocsRepoID(cmd.Project)
	if !ok {
		return nil, docs.ErrUnknownProject
	}
	repoID := docs.DocsRepoID(repoIDStr)

	// 2. Get HEAD
	head, err := u.docsRepo.GetHead(ctx, docs.ProjectName(cmd.Project))
	if err != nil {
		return nil, err
	}

	// 3. Generate WorkspaceID
	// Rule: project:localPath
	id := workspace.WorkspaceID(fmt.Sprintf("%s:%s", cmd.Project, cmd.LocalPath))

	// 4. Check existence
	ws, err := u.workspaceRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	now := u.clock.Now()

	if ws == nil {
		// Create new with fields that should not be overridden on updates
		ws = &workspace.Workspace{
			ID:         id,
			Project:    docs.ProjectName(cmd.Project),
			LocalPath:  cmd.LocalPath,
			OwnerEmail: cmd.ActorEmail,
		}
	}

	// Common updates for both new and existing workspaces
	ws.RepoURL = cmd.RepoURL
	ws.DocsHash = head
	ws.LastReportedAt = now
	ws.DocsRepoID = repoID
	ws.LastActorEmail = cmd.ActorEmail

	// 5. Save
	if err := u.workspaceRepo.Save(ctx, ws); err != nil {
		return nil, err
	}

	return ws, nil
}
