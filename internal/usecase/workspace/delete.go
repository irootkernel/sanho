package workspace

import (
	"errors"
	"os"

	"github.com/SeventeenthEarth/kkachi/internal/infra/state"
)

var ErrWorkspaceNotFound = errors.New("workspace_not_found")

type DeleteWorkspaceUseCase struct {
	stateRepo *state.FileStateRepository
}

func NewDeleteWorkspaceUseCase(stateRepo *state.FileStateRepository) *DeleteWorkspaceUseCase {
	return &DeleteWorkspaceUseCase{stateRepo: stateRepo}
}

func (uc *DeleteWorkspaceUseCase) Execute(id string) error {
	err := uc.stateRepo.DeleteWorkspace(id)
	if errors.Is(err, os.ErrNotExist) {
		return ErrWorkspaceNotFound
	}
	return err
}
