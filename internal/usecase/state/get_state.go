package state

import (
	"context"

	"github.com/SeventeenthEarth/kkachi/internal/domain/docs"
	"github.com/SeventeenthEarth/kkachi/internal/domain/workspace"
)

// ProjectLister provides a list of registered projects.
type ProjectLister interface {
	ListProjects() []string
}

// GetStateUseCase defines the interface for retrieving server state.
type GetStateUseCase interface {
	Execute(ctx context.Context) (*StateResult, error)
}

// StateResult contains the aggregated server state.
type StateResult struct {
	DocsHeads  map[docs.ProjectName]docs.CommitHash
	Workspaces []*workspace.Workspace
}

type getStateUseCase struct {
	docsRepo      docs.DocsReadRepository
	workspaceRepo workspace.WorkspaceRepository
	projectLister ProjectLister
}

// NewGetStateUseCase creates a new GetStateUseCase instance.
func NewGetStateUseCase(
	docsRepo docs.DocsReadRepository,
	workspaceRepo workspace.WorkspaceRepository,
	projectLister ProjectLister,
) GetStateUseCase {
	return &getStateUseCase{
		docsRepo:      docsRepo,
		workspaceRepo: workspaceRepo,
		projectLister: projectLister,
	}
}

// Execute retrieves the current server state including docs heads and workspaces.
func (u *getStateUseCase) Execute(ctx context.Context) (*StateResult, error) {
	// Get all workspaces
	workspaces, err := u.workspaceRepo.List(ctx)
	if err != nil {
		return nil, err
	}

	// Get docs heads for all registered projects
	projects := u.projectLister.ListProjects()
	docsHeads := make(map[docs.ProjectName]docs.CommitHash)
	for _, project := range projects {
		projectName := docs.ProjectName(project)
		head, err := u.docsRepo.GetHead(ctx, projectName)
		if err != nil {
			return nil, err
		}
		docsHeads[projectName] = head
	}

	return &StateResult{
		DocsHeads:  docsHeads,
		Workspaces: workspaces,
	}, nil
}
