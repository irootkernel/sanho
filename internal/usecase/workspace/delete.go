package workspace

import (
	"errors"
	"os"
)

var ErrUnknownWorkspace = errors.New("unknown_workspace")

type DeleteWorkspaceRepository interface {
	DeleteWorkspace(id string) error
}

type DeleteWorkspaceUseCase struct {
	stateRepo DeleteWorkspaceRepository
}

func NewDeleteWorkspaceUseCase(stateRepo DeleteWorkspaceRepository) *DeleteWorkspaceUseCase {
	return &DeleteWorkspaceUseCase{stateRepo: stateRepo}
}

func (uc *DeleteWorkspaceUseCase) Execute(id string) error {
	err := uc.stateRepo.DeleteWorkspace(id)
	if errors.Is(err, os.ErrNotExist) {
		return ErrUnknownWorkspace
	}
	return err
}
