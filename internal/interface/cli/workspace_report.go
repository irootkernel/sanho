package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/irootkernel/sanho/internal/domain/client"
	"github.com/irootkernel/sanho/internal/domain/docs"
	"github.com/irootkernel/sanho/internal/infra/fs"
	infraGit "github.com/irootkernel/sanho/internal/infra/git"
	"github.com/irootkernel/sanho/internal/infra/httpclient"
)

func workspaceReportStore(ctx context.Context, workDir string) (*fs.WorkspaceReportStore, error) {
	syncer := infraGit.NewWorkspaceSync(fs.NewSnapshotBuilder(), fs.NewSnapshotApplier())
	isRepository, err := syncer.IsRepository(ctx, workDir)
	if err != nil {
		return nil, fmt.Errorf("check Git repository for workspace report: %w", err)
	}
	path := filepath.Join(workDir, fs.WorkspaceReportFallbackFile)
	if isRepository {
		path, err = syncer.ResolveWorkspaceReportPath(ctx, workDir)
		if err != nil {
			return nil, fmt.Errorf("resolve workspace report path: %w", err)
		}
	}
	return fs.NewWorkspaceReportStore(path), nil
}

func reportWorkspaceDocsHash(
	ctx context.Context,
	workDir string,
	config *client.WorkspaceConfig,
	hash docs.CommitHash,
) error {
	store, err := workspaceReportStore(ctx, workDir)
	if err != nil {
		return err
	}
	state := fs.WorkspaceReportState{
		Version:     1,
		WorkspaceID: config.WorkspaceID,
		DocsHash:    hash,
		ActorEmail:  config.ActorEmail,
		CreatedAt:   time.Now(),
	}
	if err := store.Save(state); err != nil {
		return fmt.Errorf("persist pending workspace report: %w", err)
	}
	if err := sendWorkspaceReport(ctx, config.SocketPath, state); err != nil {
		return fmt.Errorf("report docs hash to daemon; report remains pending: %w", err)
	}
	if err := store.Remove(); err != nil {
		return fmt.Errorf("remove completed workspace report: %w", err)
	}
	return nil
}

func retryPendingWorkspaceReport(
	ctx context.Context,
	workDir string,
	config *client.WorkspaceConfig,
) error {
	store, err := workspaceReportStore(ctx, workDir)
	if err != nil {
		return err
	}
	state, exists, err := store.Load()
	if err != nil {
		return fmt.Errorf("load pending workspace report: %w", err)
	}
	if !exists {
		return nil
	}
	if state.WorkspaceID != config.WorkspaceID {
		return fmt.Errorf("pending workspace report belongs to workspace %s", state.WorkspaceID)
	}
	if err := sendWorkspaceReport(ctx, config.SocketPath, state); err != nil {
		return fmt.Errorf("retry pending workspace report: %w", err)
	}
	if err := store.Remove(); err != nil {
		return fmt.Errorf("remove completed workspace report: %w", err)
	}
	return nil
}

func hasPendingWorkspaceReport(ctx context.Context, workDir string) (bool, error) {
	store, err := workspaceReportStore(ctx, workDir)
	if err != nil {
		return false, err
	}
	_, exists, err := store.Load()
	return exists, err
}

func sendWorkspaceReport(ctx context.Context, socketPath string, state fs.WorkspaceReportState) error {
	client, err := newDaemonClient(socketPath)
	if err != nil {
		return err
	}
	return client.ReportWorkspaceDocsHash(
		ctx,
		state.WorkspaceID,
		httpclient.ReportWorkspaceDocsHashRequest{
			DocsHash:   state.DocsHash,
			ActorEmail: state.ActorEmail,
		},
	)
}
