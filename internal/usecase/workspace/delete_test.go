package workspace

import (
	"errors"
	"os"
	"testing"
)

type deleteWorkspaceRepository struct {
	deletedID string
	err       error
}

func (r *deleteWorkspaceRepository) DeleteWorkspace(id string) error {
	r.deletedID = id
	return r.err
}

func TestDeleteWorkspaceUseCaseSuccess(t *testing.T) {
	repository := &deleteWorkspaceRepository{}
	uc := NewDeleteWorkspaceUseCase(repository)

	if err := uc.Execute("proj:/path/ws1"); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if repository.deletedID != "proj:/path/ws1" {
		t.Fatalf("deleted workspace = %q, want %q", repository.deletedID, "proj:/path/ws1")
	}
}

func TestDeleteWorkspaceUseCaseUnknownWorkspace(t *testing.T) {
	repository := &deleteWorkspaceRepository{err: os.ErrNotExist}
	uc := NewDeleteWorkspaceUseCase(repository)

	if err := uc.Execute("missing"); !errors.Is(err, ErrUnknownWorkspace) {
		t.Fatalf("Execute error = %v, want %v", err, ErrUnknownWorkspace)
	}
}

func TestDeleteWorkspaceUseCasePropagatesRepositoryError(t *testing.T) {
	repositoryErr := errors.New("delete failed")
	repository := &deleteWorkspaceRepository{err: repositoryErr}
	uc := NewDeleteWorkspaceUseCase(repository)

	if err := uc.Execute("workspace"); !errors.Is(err, repositoryErr) {
		t.Fatalf("Execute error = %v, want %v", err, repositoryErr)
	}
}
