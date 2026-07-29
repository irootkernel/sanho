package docs

import (
	"context"
	"errors"
	"log"

	domain "github.com/irootkernel/sanho/internal/domain/docs"
	"github.com/irootkernel/sanho/internal/domain/workspace"
)

// PushDocsUseCase defines the interface for pushing docs changes.
type PushDocsUseCase interface {
	Execute(ctx context.Context, cmd PushDocsCommand) (domain.DocsPushResult, error)
}

// PushDocsCommand contains the parameters for a docs push operation.
type PushDocsCommand struct {
	WorkspaceID  string
	BaseDocsHash domain.CommitHash
	DocsSnapshot domain.DocsSnapshot
	ActorEmail   string
}

type DocsRepoMutexManager interface {
	TryLock(docsRepoID domain.DocsRepoID) bool
	Unlock(docsRepoID domain.DocsRepoID)
}

type pushDocsUseCase struct {
	workspaceRepo workspace.WorkspaceRepository
	docsWriteRepo domain.DocsWriteRepository
	mutexManager  DocsRepoMutexManager
}

// NewPushDocsUseCase creates a new PushDocsUseCase instance.
func NewPushDocsUseCase(
	workspaceRepo workspace.WorkspaceRepository,
	docsWriteRepo domain.DocsWriteRepository,
	mutexManager DocsRepoMutexManager,
) PushDocsUseCase {
	return &pushDocsUseCase{
		workspaceRepo: workspaceRepo,
		docsWriteRepo: docsWriteRepo,
		mutexManager:  mutexManager,
	}
}

func (u *pushDocsUseCase) Execute(ctx context.Context, cmd PushDocsCommand) (domain.DocsPushResult, error) {
	// 1. Get workspace to verify it exists and get project info
	ws, err := u.workspaceRepo.Get(ctx, workspace.WorkspaceID(cmd.WorkspaceID))
	if err != nil {
		return domain.DocsPushResult{}, err
	}
	if ws == nil {
		return domain.DocsPushResult{}, domain.ErrUnknownWorkspace
	}

	// 2. Try to acquire lock for the docs repo
	if !u.mutexManager.TryLock(ws.DocsRepoID) {
		return domain.DocsPushResult{}, domain.ErrDocsRepoBusy
	}
	defer u.mutexManager.Unlock(ws.DocsRepoID)

	previousHash := ws.DocsHash

	// 3. Perform the push operation (commit locally, push later)
	result, err := u.docsWriteRepo.PushSnapshot(ctx, ws.Project, cmd.BaseDocsHash, cmd.DocsSnapshot, cmd.ActorEmail)
	if err != nil {
		return domain.DocsPushResult{}, errors.Join(err, u.docsWriteRepo.Reset(ctx, ws.Project))
	}

	// 4. Update workspace docs hash based on result
	var newHash domain.CommitHash
	switch result.Status {
	case domain.DocsPushStatusUpdated:
		if result.NewHead == nil {
			log.Printf("CRITICAL: PushSnapshot returned 'updated' status with nil NewHead for workspace %s", cmd.WorkspaceID)
			return domain.DocsPushResult{}, errors.Join(domain.ErrInconsistentPush, u.docsWriteRepo.Reset(ctx, ws.Project))
		}
		newHash = *result.NewHead
	case domain.DocsPushStatusNoChange, domain.DocsPushStatusOutdated:
		newHash = result.CurrentHead
	}

	if err := u.workspaceRepo.UpdateDocsHash(ctx, workspace.WorkspaceID(cmd.WorkspaceID), newHash, cmd.ActorEmail); err != nil {
		if result.Status == domain.DocsPushStatusUpdated {
			return domain.DocsPushResult{}, errors.Join(err, u.docsWriteRepo.Reset(ctx, ws.Project))
		}
		return domain.DocsPushResult{}, err
	}

	// 5. Push to remote only after state update to avoid remote/state drift.
	if result.Status == domain.DocsPushStatusUpdated {
		if err := u.docsWriteRepo.Push(ctx, ws.Project); err != nil {
			rollbackErr := u.workspaceRepo.UpdateDocsHash(ctx, workspace.WorkspaceID(cmd.WorkspaceID), previousHash, cmd.ActorEmail)
			if rollbackErr != nil {
				log.Printf("CRITICAL: failed to rollback workspace hash for %s after push failure. Error: %v", cmd.WorkspaceID, rollbackErr)
			}
			resetErr := u.docsWriteRepo.Reset(ctx, ws.Project)
			if resetErr != nil {
				log.Printf("CRITICAL: failed to reset docs repo for %s after push failure. Error: %v", cmd.WorkspaceID, resetErr)
			}
			return domain.DocsPushResult{}, errors.Join(err, rollbackErr, resetErr)
		}
	}

	return result, nil
}
