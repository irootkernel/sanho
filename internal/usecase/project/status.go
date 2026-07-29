package project

import (
	"context"

	"github.com/irootkernel/sanho/internal/domain/docs"
	"github.com/irootkernel/sanho/internal/domain/workspace"
)

type ProjectStatusWorkspace struct {
	Workspace           *workspace.Workspace
	RelativeToReference docs.CommitRelation
	RelativeToHead      docs.CommitRelation
}

type ProjectStatusResult struct {
	Project              docs.ProjectName
	ReferenceWorkspaceID workspace.WorkspaceID
	ReferenceDocsHash    docs.CommitHash
	DocsHead             docs.CommitHash
	ReferenceToHead      docs.CommitRelation
	Workspaces           []ProjectStatusWorkspace
}

type GetProjectStatusUseCase struct {
	workspaceRepo workspace.WorkspaceRepository
	docsRepo      docs.DocsStatusRepository
}

func NewGetProjectStatusUseCase(
	workspaceRepo workspace.WorkspaceRepository,
	docsRepo docs.DocsStatusRepository,
) *GetProjectStatusUseCase {
	return &GetProjectStatusUseCase{
		workspaceRepo: workspaceRepo,
		docsRepo:      docsRepo,
	}
}

func (u *GetProjectStatusUseCase) Execute(
	ctx context.Context,
	project docs.ProjectName,
	referenceWorkspaceID workspace.WorkspaceID,
	referenceDocsHash docs.CommitHash,
) (ProjectStatusResult, error) {
	referenceWorkspace, err := u.workspaceRepo.Get(ctx, referenceWorkspaceID)
	if err != nil {
		return ProjectStatusResult{}, err
	}
	if referenceWorkspace == nil {
		return ProjectStatusResult{}, docs.ErrUnknownWorkspace
	}
	if referenceWorkspace.Project != project {
		return ProjectStatusResult{}, docs.ErrWorkspaceProjectMismatch
	}

	allWorkspaces, err := u.workspaceRepo.List(ctx)
	if err != nil {
		return ProjectStatusResult{}, err
	}
	projectWorkspaces := make([]*workspace.Workspace, 0, len(allWorkspaces))
	workspaceCommits := make([]docs.CommitHash, 0, len(allWorkspaces))
	for _, ws := range allWorkspaces {
		if ws.Project != project {
			continue
		}
		projectWorkspaces = append(projectWorkspaces, ws)
		workspaceCommits = append(workspaceCommits, ws.DocsHash)
	}

	comparison, err := u.docsRepo.CompareProjectCommits(ctx, project, referenceDocsHash, workspaceCommits)
	if err != nil {
		return ProjectStatusResult{}, err
	}
	result := ProjectStatusResult{
		Project:              project,
		ReferenceWorkspaceID: referenceWorkspaceID,
		ReferenceDocsHash:    referenceDocsHash,
		DocsHead:             comparison.Head,
		ReferenceToHead:      comparison.ReferenceToHead,
		Workspaces:           make([]ProjectStatusWorkspace, 0, len(projectWorkspaces)),
	}
	for _, ws := range projectWorkspaces {
		relations, ok := comparison.WorkspaceComparisons[ws.DocsHash]
		if !ok {
			unknown := docs.CommitRelation{Status: docs.CommitRelationUnknown}
			relations = docs.CommitComparison{
				RelativeToReference: unknown,
				RelativeToHead:      unknown,
			}
		}
		result.Workspaces = append(result.Workspaces, ProjectStatusWorkspace{
			Workspace:           ws,
			RelativeToReference: relations.RelativeToReference,
			RelativeToHead:      relations.RelativeToHead,
		})
	}
	return result, nil
}
