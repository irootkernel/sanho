package workspace_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SeventeenthEarth/kkachi/internal/domain/docs"
	"github.com/SeventeenthEarth/kkachi/internal/domain/workspace"
	uc "github.com/SeventeenthEarth/kkachi/internal/usecase/workspace"
)

// Mocks
type mockDocsRepo struct {
	head docs.CommitHash
	err  error
}

func (m *mockDocsRepo) GetHead(ctx context.Context, project docs.ProjectName) (docs.CommitHash, error) {
	return m.head, m.err
}

func (m *mockDocsRepo) GetSnapshot(ctx context.Context, project docs.ProjectName, commit docs.CommitHash) (docs.DocsSnapshot, docs.CommitHash, error) {
	return nil, "", errors.New("not implemented")
}

type mockWorkspaceRepo struct {
	saved   *workspace.Workspace
	get     *workspace.Workspace
	getErr  error
	saveErr error
}

func (m *mockWorkspaceRepo) Save(ctx context.Context, ws *workspace.Workspace) error {
	m.saved = ws
	return m.saveErr
}

func (m *mockWorkspaceRepo) Get(ctx context.Context, id workspace.WorkspaceID) (*workspace.Workspace, error) {
	return m.get, m.getErr
}

type mockProjectMapper struct {
	repoID string
	ok     bool
}

func (m *mockProjectMapper) GetDocsRepoID(project string) (string, bool) {
	return m.repoID, m.ok
}

type mockClock struct {
	now time.Time
}

func (m mockClock) Now() time.Time { return m.now }

func TestRegisterWorkspaceUseCase_Execute(t *testing.T) {
	fixedNow := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)

	tests := []struct {
		name           string
		project        string
		localPath      string
		repoURL        string
		actorEmail     string
		mockDocs       *mockDocsRepo
		mockWS         *mockWorkspaceRepo
		mockMapper     *mockProjectMapper
		clock          uc.Clock
		wantErr        bool
		wantHead       docs.CommitHash
		wantSavedID    workspace.WorkspaceID
		wantSavedEmail string
		wantReportedAt time.Time
		wantRepoURL    string
	}{
		{
			name:           "Success - New Workspace",
			project:        "proj1",
			localPath:      "/tmp/ws",
			repoURL:        "git@github.com:org/repo.git",
			actorEmail:     "user@example.com",
			mockDocs:       &mockDocsRepo{head: "hash123", err: nil},
			mockWS:         &mockWorkspaceRepo{get: nil, getErr: nil},
			mockMapper:     &mockProjectMapper{repoID: "docs_repo_1", ok: true},
			clock:          mockClock{now: fixedNow},
			wantErr:        false,
			wantHead:       "hash123",
			wantSavedID:    "proj1:/tmp/ws",
			wantSavedEmail: "user@example.com",
			wantReportedAt: fixedNow,
			wantRepoURL:    "git@github.com:org/repo.git",
		},
		{
			name:       "Success - Update Workspace",
			project:    "proj1",
			localPath:  "/tmp/ws",
			repoURL:    "git@github.com:org/repo.git",
			actorEmail: "new_user@example.com",
			mockDocs:   &mockDocsRepo{head: "hash456", err: nil},
			mockWS: &mockWorkspaceRepo{
				get: &workspace.Workspace{
					ID:         "proj1:/tmp/ws",
					OwnerEmail: "original_owner@example.com",
					RepoURL:    "old_url",
				},
				getErr: nil,
			},
			mockMapper:     &mockProjectMapper{repoID: "docs_repo_1", ok: true},
			clock:          mockClock{now: fixedNow},
			wantErr:        false,
			wantHead:       "hash456",
			wantSavedID:    "proj1:/tmp/ws",
			wantSavedEmail: "original_owner@example.com", // Should preserve owner
			wantReportedAt: fixedNow,
			wantRepoURL:    "git@github.com:org/repo.git",
		},
		{
			name:       "Error - Unknown Project",
			project:    "unknown",
			mockMapper: &mockProjectMapper{ok: false},
			wantErr:    true,
		},
		{
			name:       "Error - Docs Repo Failure",
			project:    "proj1",
			mockMapper: &mockProjectMapper{repoID: "docs_repo_1", ok: true},
			mockDocs:   &mockDocsRepo{err: errors.New("git error")},
			wantErr:    true,
		},
		{
			name:       "Error - WorkspaceRepo Get",
			project:    "proj1",
			mockMapper: &mockProjectMapper{repoID: "docs_repo_1", ok: true},
			mockDocs:   &mockDocsRepo{head: "hash123"},
			mockWS:     &mockWorkspaceRepo{getErr: errors.New("get failed")},
			clock:      mockClock{now: fixedNow},
			wantErr:    true,
		},
		{
			name:       "Error - WorkspaceRepo Save",
			project:    "proj1",
			localPath:  "/tmp/ws",
			repoURL:    "git@github.com:org/repo.git",
			actorEmail: "user@example.com",
			mockMapper: &mockProjectMapper{repoID: "docs_repo_1", ok: true},
			mockDocs:   &mockDocsRepo{head: "hash999"},
			mockWS:     &mockWorkspaceRepo{saveErr: errors.New("save failed")},
			clock:      mockClock{now: fixedNow},
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock := tt.clock
			if clock == nil {
				clock = mockClock{now: fixedNow}
			}
			u := uc.NewRegisterWorkspaceUseCase(tt.mockDocs, tt.mockWS, tt.mockMapper, clock)
			ws, err := u.Execute(context.Background(), uc.RegisterWorkspaceCommand{
				Project:    tt.project,
				LocalPath:  tt.localPath,
				RepoURL:    tt.repoURL,
				ActorEmail: tt.actorEmail,
			})

			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if ws.DocsHash != tt.wantHead {
					t.Errorf("Execute() head = %v, want %v", ws.DocsHash, tt.wantHead)
				}
				if tt.mockWS.saved.ID != tt.wantSavedID {
					t.Errorf("Saved Workspace ID = %v, want %v", tt.mockWS.saved.ID, tt.wantSavedID)
				}
				if tt.mockWS.saved.OwnerEmail != tt.wantSavedEmail {
					t.Errorf("Saved Workspace OwnerEmail = %v, want %v", tt.mockWS.saved.OwnerEmail, tt.wantSavedEmail)
				}
				if tt.mockWS.saved.LastActorEmail != tt.actorEmail {
					t.Errorf("Saved Workspace LastActorEmail = %v, want %v", tt.mockWS.saved.LastActorEmail, tt.actorEmail)
				}
				if !tt.wantReportedAt.IsZero() && tt.mockWS.saved.LastReportedAt != tt.wantReportedAt {
					t.Errorf("Saved Workspace LastReportedAt = %v, want %v", tt.mockWS.saved.LastReportedAt, tt.wantReportedAt)
				}
				if tt.wantRepoURL != "" && tt.mockWS.saved.RepoURL != tt.wantRepoURL {
					t.Errorf("Saved Workspace RepoURL = %v, want %v", tt.mockWS.saved.RepoURL, tt.wantRepoURL)
				}
			}
		})
	}
}
