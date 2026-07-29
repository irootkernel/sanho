package state

import (
	"context"

	"github.com/irootkernel/sanho/internal/domain/docs"
	"github.com/irootkernel/sanho/internal/domain/workspace"
)

type FileWorkspaceRepository struct {
	stateRepo *FileStateRepository
}

func NewFileWorkspaceRepository(stateRepo *FileStateRepository) *FileWorkspaceRepository {
	return &FileWorkspaceRepository{stateRepo: stateRepo}
}

func (r *FileWorkspaceRepository) Save(_ context.Context, ws *workspace.Workspace) error {
	wsState := WorkspaceState{
		ID:             string(ws.ID),
		Project:        string(ws.Project),
		DocsRepoID:     string(ws.DocsRepoID),
		LocalPath:      ws.LocalPath,
		RepoURL:        ws.RepoURL,
		DocsHash:       string(ws.DocsHash),
		LastReportedAt: ws.LastReportedAt,
		OwnerEmail:     ws.OwnerEmail,
		LastActorEmail: ws.LastActorEmail,
	}
	return r.stateRepo.AddWorkspace(wsState)
}

func (r *FileWorkspaceRepository) Get(_ context.Context, id workspace.WorkspaceID) (*workspace.Workspace, error) {
	wsState, ok := r.stateRepo.GetWorkspace(string(id))
	if !ok {
		return nil, nil // Not found
	}
	return &workspace.Workspace{
		ID:             workspace.WorkspaceID(wsState.ID),
		Project:        docs.ProjectName(wsState.Project),
		DocsRepoID:     docs.DocsRepoID(wsState.DocsRepoID),
		LocalPath:      wsState.LocalPath,
		RepoURL:        wsState.RepoURL,
		DocsHash:       docs.CommitHash(wsState.DocsHash),
		LastReportedAt: wsState.LastReportedAt,
		OwnerEmail:     wsState.OwnerEmail,
		LastActorEmail: wsState.LastActorEmail,
	}, nil
}

func (r *FileWorkspaceRepository) UpdateDocsHash(_ context.Context, id workspace.WorkspaceID, newHash docs.CommitHash, actorEmail string) error {
	return r.stateRepo.UpdateWorkspaceDocsHash(string(id), string(newHash), actorEmail)
}

// List returns all registered workspaces.
func (r *FileWorkspaceRepository) List(_ context.Context) ([]*workspace.Workspace, error) {
	wsStates := r.stateRepo.ListWorkspaces()
	workspaces := make([]*workspace.Workspace, 0, len(wsStates))
	for _, wsState := range wsStates {
		workspaces = append(workspaces, &workspace.Workspace{
			ID:             workspace.WorkspaceID(wsState.ID),
			Project:        docs.ProjectName(wsState.Project),
			DocsRepoID:     docs.DocsRepoID(wsState.DocsRepoID),
			LocalPath:      wsState.LocalPath,
			RepoURL:        wsState.RepoURL,
			DocsHash:       docs.CommitHash(wsState.DocsHash),
			LastReportedAt: wsState.LastReportedAt,
			OwnerEmail:     wsState.OwnerEmail,
			LastActorEmail: wsState.LastActorEmail,
		})
	}
	return workspaces, nil
}
