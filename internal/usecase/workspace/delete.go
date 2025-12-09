package workspace

import (
	"errors"
	"os"

	"github.com/SeventeenthEarth/kkachi/internal/infra/state"
)

var ErrUnknownWorkspace = errors.New("unknown_workspace")

type DeleteWorkspaceUseCase struct {
	stateRepo *state.FileStateRepository
}

func NewDeleteWorkspaceUseCase(stateRepo *state.FileStateRepository) *DeleteWorkspaceUseCase {
	return &DeleteWorkspaceUseCase{stateRepo: stateRepo}
}

func (uc *DeleteWorkspaceUseCase) Execute(id string) error {
	err := uc.stateRepo.DeleteWorkspace(id)
	if errors.Is(err, os.ErrNotExist) {
		return ErrUnknownWorkspace
	}
	return err
}
