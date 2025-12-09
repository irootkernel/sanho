package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/SeventeenthEarth/kkachi/internal/infra/state"
)

func TestDeleteWorkspaceUseCase_Success(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "kkachi-delete-ws-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	statePath := filepath.Join(tempDir, "state.json")
	stateRepo, err := state.NewFileStateRepository(statePath)
	if err != nil {
		t.Fatalf("failed to create state repo: %v", err)
	}

	wsID := "proj:/path/ws1"
	if err := stateRepo.AddWorkspace(state.WorkspaceState{
		ID:         wsID,
		Project:    "proj",
		DocsRepoID: "repo",
	}); err != nil {
		t.Fatalf("failed to seed workspace: %v", err)
	}

	uc := NewDeleteWorkspaceUseCase(stateRepo)
	if err := uc.Execute(wsID); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if _, ok := stateRepo.GetWorkspace(wsID); ok {
		t.Fatalf("workspace %s should be deleted", wsID)
	}
}

func TestDeleteWorkspaceUseCase_UnknownWorkspace(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "kkachi-delete-ws-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	statePath := filepath.Join(tempDir, "state.json")
	stateRepo, err := state.NewFileStateRepository(statePath)
	if err != nil {
		t.Fatalf("failed to create state repo: %v", err)
	}

	uc := NewDeleteWorkspaceUseCase(stateRepo)
	if err := uc.Execute("missing"); err != ErrUnknownWorkspace {
		t.Fatalf("expected ErrUnknownWorkspace, got %v", err)
	}
}
