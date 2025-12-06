package docs_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SeventeenthEarth/kkachi/internal/domain/docs"
	"github.com/SeventeenthEarth/kkachi/internal/domain/workspace"
	uc "github.com/SeventeenthEarth/kkachi/internal/usecase/docs"
)

// Mock implementations
type mockWorkspaceRepo struct {
	getResult        *workspace.Workspace
	getErr           error
	updateHashCalled bool
	updateHashID     workspace.WorkspaceID
	updateHashValue  docs.CommitHash
	updateHashActor  string
	updateHashErr    error
}

func (m *mockWorkspaceRepo) Save(ctx context.Context, ws *workspace.Workspace) error {
	return nil
}

func (m *mockWorkspaceRepo) Get(ctx context.Context, id workspace.WorkspaceID) (*workspace.Workspace, error) {
	return m.getResult, m.getErr
}

func (m *mockWorkspaceRepo) UpdateDocsHash(ctx context.Context, id workspace.WorkspaceID, newHash docs.CommitHash, actorEmail string) error {
	m.updateHashCalled = true
	m.updateHashID = id
	m.updateHashValue = newHash
	m.updateHashActor = actorEmail
	return m.updateHashErr
}

func (m *mockWorkspaceRepo) List(ctx context.Context) ([]*workspace.Workspace, error) {
	return nil, nil
}

type mockDocsWriteRepo struct {
	pushResult      docs.DocsPushResult
	pushSnapshotErr error
	pushErr         error
	pushCalled      bool
}

func (m *mockDocsWriteRepo) PushSnapshot(ctx context.Context, project docs.ProjectName, base docs.CommitHash, snapshot docs.DocsSnapshot, actorEmail string) (docs.DocsPushResult, error) {
	return m.pushResult, m.pushSnapshotErr
}

func (m *mockDocsWriteRepo) Push(ctx context.Context, project docs.ProjectName) error {
	m.pushCalled = true
	return m.pushErr
}

type mockMutexManager struct {
	tryLockResult bool
	unlockCalled  bool
}

func (m *mockMutexManager) TryLock(docsRepoID docs.DocsRepoID) bool {
	return m.tryLockResult
}

func (m *mockMutexManager) Unlock(docsRepoID docs.DocsRepoID) {
	m.unlockCalled = true
}

func TestPushDocsUseCase_Execute(t *testing.T) {
	newHead := docs.CommitHash("newhead123")

	tests := []struct {
		name               string
		cmd                uc.PushDocsCommand
		wsRepo             *mockWorkspaceRepo
		docsWriteRepo      *mockDocsWriteRepo
		mutexManager       *mockMutexManager
		wantErr            error
		wantUpdateHashVal  docs.CommitHash
		wantUpdateHashCall bool
		wantStatus         docs.DocsPushStatus
		wantPushCall       bool
	}{
		{
			name: "Updated - UpdateDocsHash called with NewHead",
			cmd: uc.PushDocsCommand{
				WorkspaceID:  "ws1",
				BaseDocsHash: "base123",
				DocsSnapshot: []byte("snapshot"),
				ActorEmail:   "user@test.com",
			},
			wsRepo: &mockWorkspaceRepo{
				getResult: &workspace.Workspace{
					ID:         "ws1",
					Project:    "proj1",
					DocsRepoID: "repo1",
				},
			},
			docsWriteRepo: &mockDocsWriteRepo{
				pushResult: docs.DocsPushResult{
					Status:  docs.DocsPushStatusUpdated,
					NewHead: &newHead,
				},
			},
			mutexManager:       &mockMutexManager{tryLockResult: true},
			wantErr:            nil,
			wantUpdateHashVal:  "newhead123",
			wantUpdateHashCall: true,
			wantStatus:         docs.DocsPushStatusUpdated,
			wantPushCall:       true,
		},
		{
			name: "NoChange - UpdateDocsHash called with CurrentHead",
			cmd: uc.PushDocsCommand{
				WorkspaceID:  "ws1",
				BaseDocsHash: "base123",
				DocsSnapshot: []byte("snapshot"),
				ActorEmail:   "user@test.com",
			},
			wsRepo: &mockWorkspaceRepo{
				getResult: &workspace.Workspace{
					ID:         "ws1",
					Project:    "proj1",
					DocsRepoID: "repo1",
				},
			},
			docsWriteRepo: &mockDocsWriteRepo{
				pushResult: docs.DocsPushResult{
					Status:      docs.DocsPushStatusNoChange,
					CurrentHead: "currenthead123",
				},
			},
			mutexManager:       &mockMutexManager{tryLockResult: true},
			wantErr:            nil,
			wantUpdateHashVal:  "currenthead123",
			wantUpdateHashCall: true,
			wantStatus:         docs.DocsPushStatusNoChange,
			wantPushCall:       false,
		},
		{
			name: "Outdated - UpdateDocsHash called with CurrentHead",
			cmd: uc.PushDocsCommand{
				WorkspaceID:  "ws1",
				BaseDocsHash: "oldbase",
				DocsSnapshot: []byte("snapshot"),
				ActorEmail:   "user@test.com",
			},
			wsRepo: &mockWorkspaceRepo{
				getResult: &workspace.Workspace{
					ID:         "ws1",
					Project:    "proj1",
					DocsRepoID: "repo1",
				},
			},
			docsWriteRepo: &mockDocsWriteRepo{
				pushResult: docs.DocsPushResult{
					Status:      docs.DocsPushStatusOutdated,
					CurrentHead: "latesthead123",
				},
			},
			mutexManager:       &mockMutexManager{tryLockResult: true},
			wantErr:            nil,
			wantUpdateHashVal:  "latesthead123",
			wantUpdateHashCall: true,
			wantStatus:         docs.DocsPushStatusOutdated,
			wantPushCall:       false,
		},
		{
			name: "Error - Unknown Workspace",
			cmd: uc.PushDocsCommand{
				WorkspaceID: "unknown",
			},
			wsRepo: &mockWorkspaceRepo{
				getResult: nil,
			},
			docsWriteRepo:      &mockDocsWriteRepo{},
			mutexManager:       &mockMutexManager{tryLockResult: true},
			wantErr:            docs.ErrUnknownWorkspace,
			wantUpdateHashCall: false,
			wantPushCall:       false,
		},
		{
			name: "Error - Docs Repo Busy",
			cmd: uc.PushDocsCommand{
				WorkspaceID: "ws1",
			},
			wsRepo: &mockWorkspaceRepo{
				getResult: &workspace.Workspace{
					ID:         "ws1",
					Project:    "proj1",
					DocsRepoID: "repo1",
				},
			},
			docsWriteRepo:      &mockDocsWriteRepo{},
			mutexManager:       &mockMutexManager{tryLockResult: false},
			wantErr:            docs.ErrDocsRepoBusy,
			wantUpdateHashCall: false,
			wantPushCall:       false,
		},
		{
			name: "Error - Push Failed",
			cmd: uc.PushDocsCommand{
				WorkspaceID:  "ws1",
				BaseDocsHash: "base123",
			},
			wsRepo: &mockWorkspaceRepo{
				getResult: &workspace.Workspace{
					ID:         "ws1",
					Project:    "proj1",
					DocsRepoID: "repo1",
				},
			},
			docsWriteRepo: &mockDocsWriteRepo{
				pushResult: docs.DocsPushResult{
					Status:  docs.DocsPushStatusUpdated,
					NewHead: &newHead,
				},
				pushErr: errors.New("push failed"),
			},
			mutexManager:       &mockMutexManager{tryLockResult: true},
			wantErr:            errors.New("push failed"),
			wantUpdateHashVal:  "base123", // rolled back
			wantUpdateHashCall: true,
			wantPushCall:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			usecase := uc.NewPushDocsUseCase(tt.wsRepo, tt.docsWriteRepo, tt.mutexManager)

			result, err := usecase.Execute(context.Background(), tt.cmd)

			// Check error
			if tt.wantErr != nil {
				if err == nil {
					t.Errorf("Expected error %v, got nil", tt.wantErr)
					return
				}
				if !errors.Is(err, tt.wantErr) && err.Error() != tt.wantErr.Error() {
					t.Errorf("Expected error %v, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			// Check UpdateDocsHash was called
			if tt.wantUpdateHashCall != tt.wsRepo.updateHashCalled {
				t.Errorf("UpdateDocsHash called = %v, want %v", tt.wsRepo.updateHashCalled, tt.wantUpdateHashCall)
			}

			// Check UpdateDocsHash value
			if tt.wantUpdateHashVal != "" && tt.wsRepo.updateHashValue != tt.wantUpdateHashVal {
				t.Errorf("UpdateDocsHash value = %v, want %v", tt.wsRepo.updateHashValue, tt.wantUpdateHashVal)
			}

			// Check status
			if result.Status != tt.wantStatus {
				t.Errorf("Result status = %v, want %v", result.Status, tt.wantStatus)
			}

			// Check mutex was unlocked (when lock was acquired)
			if tt.mutexManager.tryLockResult && !tt.mutexManager.unlockCalled {
				t.Error("Mutex was not unlocked after acquiring lock")
			}

			if tt.docsWriteRepo.pushCalled != tt.wantPushCall {
				t.Errorf("Push called = %v, want %v", tt.docsWriteRepo.pushCalled, tt.wantPushCall)
			}
		})
	}
}

func TestDocsPushStatus_String(t *testing.T) {
	tests := []struct {
		status docs.DocsPushStatus
		want   string
	}{
		{docs.DocsPushStatusUpdated, "updated"},
		{docs.DocsPushStatusNoChange, "nochange"},
		{docs.DocsPushStatusOutdated, "outdated"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := string(tt.status); got != tt.want {
				t.Errorf("DocsPushStatus string = %v, want %v", got, tt.want)
			}
		})
	}
}
