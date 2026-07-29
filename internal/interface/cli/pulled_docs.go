package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/SeventeenthEarth/kkachi/internal/domain/docs"
	"github.com/SeventeenthEarth/kkachi/internal/infra/fs"
	infraGit "github.com/SeventeenthEarth/kkachi/internal/infra/git"
)

func recordPulledDocsBaseline(
	ctx context.Context,
	workDir string,
	previousHash, adoptedHash docs.CommitHash,
	originalIndex, adoptedSnapshot []byte,
	resetOriginalIndex bool,
) error {
	syncer := infraGit.NewWorkspaceSync(fs.NewSnapshotBuilder(), fs.NewSnapshotApplier())
	dir, err := syncer.ResolvePulledDocsDir(ctx, workDir)
	if err != nil {
		return err
	}
	store := fs.NewPulledDocsStore(dir)
	state, exists, err := store.Load()
	if err != nil {
		return err
	}
	if !exists {
		head, err := syncer.Head(ctx, workDir)
		if err != nil {
			return err
		}
		state = fs.PulledDocsState{
			Version:      1,
			OriginalHead: head,
			PreviousHash: previousHash,
			CreatedAt:    time.Now(),
		}
	}
	if !exists || resetOriginalIndex {
		if err := store.WriteArtifact(fs.PulledDocsOriginalIndexSnapshot, originalIndex); err != nil {
			_ = store.Remove()
			return err
		}
	}
	state.AdoptedHash = adoptedHash
	if err := store.WriteArtifact(fs.PulledDocsAdoptedSnapshot, adoptedSnapshot); err != nil {
		if !exists {
			_ = store.Remove()
		}
		return err
	}
	if err := store.Save(state); err != nil {
		if !exists {
			_ = store.Remove()
		}
		return fmt.Errorf("save pulled docs baseline: %w", err)
	}
	return nil
}

func hasPulledDocsBaseline(ctx context.Context, workDir string) (bool, error) {
	engine := newPullCommitEngine(nil)
	isRepository, err := engine.workspaceSync.IsRepository(ctx, workDir)
	if err != nil || !isRepository {
		return false, err
	}
	return engine.hasPulledDocs(ctx, workDir)
}
