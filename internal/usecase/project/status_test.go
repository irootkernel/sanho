package project

import (
	"context"
	"errors"
	"testing"

	"github.com/irootkernel/sanho/internal/domain/docs"
	"github.com/irootkernel/sanho/internal/domain/workspace"
)

type statusWorkspaceRepository struct {
	workspaces []*workspace.Workspace
	getErr     error
	listErr    error
}

func (r *statusWorkspaceRepository) Save(context.Context, *workspace.Workspace) error {
	return nil
}

func (r *statusWorkspaceRepository) Get(_ context.Context, id workspace.WorkspaceID) (*workspace.Workspace, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	for _, ws := range r.workspaces {
		if ws.ID == id {
			return ws, nil
		}
	}
	return nil, nil
}

func (r *statusWorkspaceRepository) UpdateDocsHash(context.Context, workspace.WorkspaceID, docs.CommitHash, string) error {
	return nil
}

func (r *statusWorkspaceRepository) List(context.Context) ([]*workspace.Workspace, error) {
	return r.workspaces, r.listErr
}

type statusDocsRepository struct {
	result  docs.ProjectCommitComparison
	err     error
	project docs.ProjectName
	ref     docs.CommitHash
	commits []docs.CommitHash
}

func (r *statusDocsRepository) CompareProjectCommits(
	_ context.Context,
	project docs.ProjectName,
	reference docs.CommitHash,
	commits []docs.CommitHash,
) (docs.ProjectCommitComparison, error) {
	r.project = project
	r.ref = reference
	r.commits = append([]docs.CommitHash(nil), commits...)
	return r.result, r.err
}

func TestGetProjectStatusUseCaseFiltersProjectAndMapsRelations(t *testing.T) {
	workspaceRepo := &statusWorkspaceRepository{workspaces: []*workspace.Workspace{
		{ID: "current", Project: "alpha", DocsHash: "base"},
		{ID: "peer", Project: "alpha", DocsHash: "head"},
		{ID: "other", Project: "beta", DocsHash: "other"},
	}}
	docsRepo := &statusDocsRepository{result: docs.ProjectCommitComparison{
		Head:            "head",
		ReferenceToHead: docs.CommitRelation{Status: docs.CommitRelationBehind, Behind: 1},
		WorkspaceComparisons: map[docs.CommitHash]docs.CommitComparison{
			"base": {
				RelativeToReference: docs.CommitRelation{Status: docs.CommitRelationSame},
				RelativeToHead:      docs.CommitRelation{Status: docs.CommitRelationBehind, Behind: 1},
			},
			"head": {
				RelativeToReference: docs.CommitRelation{Status: docs.CommitRelationAhead, Ahead: 1},
				RelativeToHead:      docs.CommitRelation{Status: docs.CommitRelationSame},
			},
		},
	}}

	useCase := NewGetProjectStatusUseCase(workspaceRepo, docsRepo)
	result, err := useCase.Execute(context.Background(), "alpha", "current", "base")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.DocsHead != "head" || len(result.Workspaces) != 2 {
		t.Fatalf("result = %#v", result)
	}
	if docsRepo.project != "alpha" || docsRepo.ref != "base" {
		t.Fatalf("comparison input project=%q ref=%q", docsRepo.project, docsRepo.ref)
	}
	if len(docsRepo.commits) != 2 || docsRepo.commits[0] != "base" || docsRepo.commits[1] != "head" {
		t.Fatalf("comparison commits = %v", docsRepo.commits)
	}
	if result.Workspaces[1].RelativeToReference.Status != docs.CommitRelationAhead {
		t.Fatalf("peer relation = %#v", result.Workspaces[1].RelativeToReference)
	}
}

func TestGetProjectStatusUseCaseRejectsUnknownWorkspace(t *testing.T) {
	useCase := NewGetProjectStatusUseCase(
		&statusWorkspaceRepository{},
		&statusDocsRepository{},
	)
	_, err := useCase.Execute(context.Background(), "alpha", "missing", "base")
	if !errors.Is(err, docs.ErrUnknownWorkspace) {
		t.Fatalf("Execute() error = %v, want unknown workspace", err)
	}
}

func TestGetProjectStatusUseCaseRejectsWorkspaceProjectMismatch(t *testing.T) {
	useCase := NewGetProjectStatusUseCase(
		&statusWorkspaceRepository{workspaces: []*workspace.Workspace{
			{ID: "current", Project: "beta", DocsHash: "base"},
		}},
		&statusDocsRepository{},
	)
	_, err := useCase.Execute(context.Background(), "alpha", "current", "base")
	if !errors.Is(err, docs.ErrWorkspaceProjectMismatch) {
		t.Fatalf("Execute() error = %v, want workspace project mismatch", err)
	}
}
