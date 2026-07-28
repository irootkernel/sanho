package state_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SeventeenthEarth/kkachi/internal/domain/docs"
	"github.com/SeventeenthEarth/kkachi/internal/domain/workspace"
	stateuc "github.com/SeventeenthEarth/kkachi/internal/usecase/state"
)

// Mock DocsReadRepository
type mockDocsRepo struct {
	heads map[docs.ProjectName]docs.CommitHash
	err   error
}

func (m *mockDocsRepo) GetHead(ctx context.Context, project docs.ProjectName) (docs.CommitHash, error) {
	if m.err != nil {
		return "", m.err
	}
	head, ok := m.heads[project]
	if !ok {
		return "", docs.ErrUnknownProject
	}
	return head, nil
}

func (m *mockDocsRepo) GetSnapshot(ctx context.Context, project docs.ProjectName, commit docs.CommitHash) (docs.DocsSnapshot, docs.CommitHash, error) {
	return nil, "", errors.New("not implemented")
}

// Mock WorkspaceRepository
type mockWorkspaceRepo struct {
	workspaces []*workspace.Workspace
	listErr    error
}

func (m *mockWorkspaceRepo) Save(ctx context.Context, ws *workspace.Workspace) error {
	return nil
}

func (m *mockWorkspaceRepo) Get(ctx context.Context, id workspace.WorkspaceID) (*workspace.Workspace, error) {
	return nil, nil
}

func (m *mockWorkspaceRepo) UpdateDocsHash(ctx context.Context, id workspace.WorkspaceID, newHash docs.CommitHash, actorEmail string) error {
	return nil
}

func (m *mockWorkspaceRepo) List(ctx context.Context) ([]*workspace.Workspace, error) {
	return m.workspaces, m.listErr
}

// Mock ProjectLister
type mockProjectLister struct {
	projects []string
}

func (m *mockProjectLister) ListProjects() []string {
	return m.projects
}

func TestGetStateUseCase_Execute(t *testing.T) {
	tests := []struct {
		name           string
		docsRepo       *mockDocsRepo
		wsRepo         *mockWorkspaceRepo
		projectLister  *mockProjectLister
		wantErr        bool
		wantHeads      int
		wantWorkspaces int
	}{
		{
			name: "Empty state",
			docsRepo: &mockDocsRepo{
				heads: map[docs.ProjectName]docs.CommitHash{},
			},
			wsRepo: &mockWorkspaceRepo{
				workspaces: []*workspace.Workspace{},
			},
			projectLister:  &mockProjectLister{projects: []string{}},
			wantErr:        false,
			wantHeads:      0,
			wantWorkspaces: 0,
		},
		{
			name: "With projects and workspaces",
			docsRepo: &mockDocsRepo{
				heads: map[docs.ProjectName]docs.CommitHash{
					"project1": "hash1",
					"project2": "hash2",
				},
			},
			wsRepo: &mockWorkspaceRepo{
				workspaces: []*workspace.Workspace{
					{ID: "ws1", Project: "project1"},
					{ID: "ws2", Project: "project2"},
				},
			},
			projectLister: &mockProjectLister{
				projects: []string{"project1", "project2"},
			},
			wantErr:        false,
			wantHeads:      2,
			wantWorkspaces: 2,
		},
		{
			name: "Project without HEAD fails closed",
			docsRepo: &mockDocsRepo{
				heads: map[docs.ProjectName]docs.CommitHash{
					"project1": "hash1",
					// project2 missing - GetHead will return error
				},
			},
			wsRepo: &mockWorkspaceRepo{
				workspaces: []*workspace.Workspace{
					{ID: "ws1", Project: "project1"},
				},
			},
			projectLister: &mockProjectLister{
				projects: []string{"project1", "project2"},
			},
			wantErr: true,
		},
		{
			name:     "List workspace error",
			docsRepo: &mockDocsRepo{},
			wsRepo: &mockWorkspaceRepo{
				listErr: errors.New("database error"),
			},
			projectLister: &mockProjectLister{},
			wantErr:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uc := stateuc.NewGetStateUseCase(tt.docsRepo, tt.wsRepo, tt.projectLister)
			result, err := uc.Execute(context.Background())

			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if len(result.DocsHeads) != tt.wantHeads {
					t.Errorf("DocsHeads count = %d, want %d", len(result.DocsHeads), tt.wantHeads)
				}
				if len(result.Workspaces) != tt.wantWorkspaces {
					t.Errorf("Workspaces count = %d, want %d", len(result.Workspaces), tt.wantWorkspaces)
				}
			}
		})
	}
}
