package workspace

import (
	"context"
	"errors"
	"testing"

	"github.com/SeventeenthEarth/kkachi/internal/domain/docs"
	domainWorkspace "github.com/SeventeenthEarth/kkachi/internal/domain/workspace"
)

func TestReportDocsHashAcceptsCurrentOrAncestorAndUpdatesWorkspace(t *testing.T) {
	for _, status := range []docs.CommitRelationStatus{
		docs.CommitRelationSame,
		docs.CommitRelationBehind,
	} {
		t.Run(string(status), func(t *testing.T) {
			workspaceRepo := &reportWorkspaceRepo{
				workspace: &domainWorkspace.Workspace{
					ID:      "workspace-1",
					Project: "project-1",
				},
			}
			docsRepo := &reportDocsRepo{status: status}
			usecase := NewReportDocsHashUseCase(workspaceRepo, docsRepo)

			err := usecase.Execute(context.Background(), ReportDocsHashCommand{
				WorkspaceID: "workspace-1",
				DocsHash:    "docs-1",
				ActorEmail:  "actor@example.com",
			})
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if workspaceRepo.updatedHash != "docs-1" || workspaceRepo.actorEmail != "actor@example.com" {
				t.Fatalf("unexpected update: hash=%q actor=%q", workspaceRepo.updatedHash, workspaceRepo.actorEmail)
			}
		})
	}
}

func TestReportDocsHashRejectsCommitOutsideCurrentHistory(t *testing.T) {
	workspaceRepo := &reportWorkspaceRepo{
		workspace: &domainWorkspace.Workspace{ID: "workspace-1", Project: "project-1"},
	}
	usecase := NewReportDocsHashUseCase(
		workspaceRepo,
		&reportDocsRepo{status: docs.CommitRelationDiverged},
	)

	err := usecase.Execute(context.Background(), ReportDocsHashCommand{
		WorkspaceID: "workspace-1",
		DocsHash:    "docs-1",
		ActorEmail:  "actor@example.com",
	})
	if !errors.Is(err, ErrDocsHashNotInCurrentHistory) {
		t.Fatalf("error=%v want %v", err, ErrDocsHashNotInCurrentHistory)
	}
	if workspaceRepo.updatedHash != "" {
		t.Fatalf("workspace was updated to %q", workspaceRepo.updatedHash)
	}
}

func TestReportDocsHashRejectsUnknownWorkspace(t *testing.T) {
	usecase := NewReportDocsHashUseCase(&reportWorkspaceRepo{}, &reportDocsRepo{})
	err := usecase.Execute(context.Background(), ReportDocsHashCommand{WorkspaceID: "missing"})
	if !errors.Is(err, ErrUnknownWorkspace) {
		t.Fatalf("error=%v want %v", err, ErrUnknownWorkspace)
	}
}

type reportWorkspaceRepo struct {
	workspace   *domainWorkspace.Workspace
	updatedHash docs.CommitHash
	actorEmail  string
}

func (r *reportWorkspaceRepo) Save(context.Context, *domainWorkspace.Workspace) error {
	return nil
}

func (r *reportWorkspaceRepo) Get(
	context.Context,
	domainWorkspace.WorkspaceID,
) (*domainWorkspace.Workspace, error) {
	return r.workspace, nil
}

func (r *reportWorkspaceRepo) UpdateDocsHash(
	_ context.Context,
	_ domainWorkspace.WorkspaceID,
	hash docs.CommitHash,
	actorEmail string,
) error {
	r.updatedHash = hash
	r.actorEmail = actorEmail
	return nil
}

func (r *reportWorkspaceRepo) List(context.Context) ([]*domainWorkspace.Workspace, error) {
	return nil, nil
}

type reportDocsRepo struct {
	status docs.CommitRelationStatus
	err    error
}

func (r *reportDocsRepo) CompareProjectCommits(
	context.Context,
	docs.ProjectName,
	docs.CommitHash,
	[]docs.CommitHash,
) (docs.ProjectCommitComparison, error) {
	if r.err != nil {
		return docs.ProjectCommitComparison{}, r.err
	}
	return docs.ProjectCommitComparison{
		ReferenceToHead: docs.CommitRelation{Status: r.status},
	}, nil
}
