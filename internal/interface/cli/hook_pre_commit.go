package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/irootkernel/sanho/internal/domain/client"
	"github.com/irootkernel/sanho/internal/domain/docs"
	"github.com/irootkernel/sanho/internal/domain/merge"
	"github.com/irootkernel/sanho/internal/domain/workspace"
	"github.com/irootkernel/sanho/internal/infra/fs"
	infraGit "github.com/irootkernel/sanho/internal/infra/git"
	"github.com/irootkernel/sanho/internal/infra/httpclient"
	"github.com/irootkernel/sanho/internal/usecase/hook"
)

// preCommitTimeout is the timeout for pre-commit operations.
const preCommitTimeout = 60 * time.Second

// runPreCommitHook executes the pre-commit hook logic.
func runPreCommitHook(cmd *cobra.Command) error {
	ctx, cancel := context.WithTimeout(context.Background(), preCommitTimeout)
	defer cancel()

	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		cmd.PrintErrf("sanho hook pre-commit: failed to get current directory: %v\n", err)
		return err
	}

	// Create dependencies
	configLoader := fs.NewFileConfigLoader()
	docsHashStore := fs.NewFileDocsHashStore()
	pendingFixStore := fs.NewFilePendingFixStore()
	conflictDetector := merge.NewFileConflictDetector()
	gitClient := infraGit.NewClient()
	snapshotBuilder := fs.NewSnapshotBuilder()
	snapshotApplier := fs.NewSnapshotApplier()
	output := newCLIPreCommitOutput(cmd)

	// Load config first to get daemon socket path
	config, err := configLoader.Load(cwd)
	if err != nil {
		cmd.PrintErrf("sanho hook pre-commit: %v\n", err)
		cmd.PrintErrf("sanho configuration is broken or missing (.sanho.json / .sanho_docs_hash).\n")
		return err
	}

	rawHTTPClient, err := newDaemonClient(config.SocketPath)
	if err != nil {
		return err
	}
	httpClient := newPreCommitHTTPClientAdapter(rawHTTPClient)
	workspaceSync := infraGit.NewWorkspaceSync(snapshotBuilder, snapshotApplier)
	pullCommit := newPullCommitEngine(rawHTTPClient)
	if err := retryPendingWorkspaceReport(ctx, cwd, config); err != nil {
		return fmt.Errorf("sanho hook pre-commit: %w", err)
	}

	state, hasTransaction, err := pullCommit.resume(ctx, cwd, config)
	if hasTransaction {
		switch {
		case errors.Is(err, errPullCommitConflict):
			printPullCommitConflicts(cmd, state.ConflictFiles)
		case errors.Is(err, errPullCommitRetry):
			cmd.Printf("sanho: created docs base commit %s.\n", state.SyncCommit)
			cmd.PrintErrln("sanho: staged and unstaged docs were preserved. Run the same git commit command again.")
		case err != nil:
			cmd.PrintErrf("sanho hook pre-commit: %v\n", err)
		}
		if err != nil {
			return err
		}
	}

	if !hasTransaction {
		conflictFiles, err := conflictDetector.DetectConflicts(filepath.Join(cwd, config.DocsDir))
		if err != nil {
			return fmt.Errorf("sanho hook pre-commit: check docs conflicts: %w", err)
		}
		if len(conflictFiles) > 0 {
			cmd.PrintErrln("sanho: conflict markers found in docs files:")
			for _, file := range conflictFiles {
				cmd.PrintErrf("  - %s\n", file)
			}
			return hook.ErrConflictMarkerFound
		}
		_, hasPendingFix, err := pendingFixStore.Read(filepath.Join(cwd, config.PendingFixFile))
		if err != nil {
			return fmt.Errorf("sanho hook pre-commit: check pending fix state: %w", err)
		}
		if hasPendingFix {
			cmd.PrintErrln("sanho: legacy pending-fix state exists. Run 'sanho fix' before committing.")
			return hook.ErrPendingFixExists
		}

		baseHash, err := docsHashStore.Read(filepath.Join(cwd, config.DocsHashFile))
		if err != nil {
			return fmt.Errorf("sanho hook pre-commit: read docs hash: %w", err)
		}
		remoteHash, err := rawHTTPClient.DocsHead(ctx, config.Project)
		if err != nil {
			return fmt.Errorf("sanho hook pre-commit: read daemon docs version: %w", err)
		}
		hasPulledDocs, err := pullCommit.hasPulledDocs(ctx, cwd)
		if err != nil {
			return fmt.Errorf("sanho hook pre-commit: check pulled docs baseline: %w", err)
		}
		if baseHash != remoteHash || hasPulledDocs {
			state, err := pullCommit.start(ctx, cwd, config, baseHash, remoteHash)
			switch {
			case errors.Is(err, errPullCommitConflict):
				printPullCommitConflicts(cmd, state.ConflictFiles)
			case errors.Is(err, errPullCommitRetry):
				cmd.Printf("sanho: created docs base commit %s.\n", state.SyncCommit)
				cmd.PrintErrln("sanho: staged and unstaged docs were preserved. Run the same git commit command again.")
			case err != nil:
				cmd.PrintErrf("sanho hook pre-commit: %v\n", err)
			}
			if err != nil {
				return err
			}
		}
	}

	// Create usecase
	usecase := hook.NewPreCommitUseCase(
		configLoader,
		docsHashStore,
		pendingFixStore,
		conflictDetector,
		newPreCommitGitClientAdapter(gitClient),
		&indexPreCommitSnapshotBuilder{
			ctx:           ctx,
			workDir:       cwd,
			docsDir:       config.DocsDir,
			workspaceSync: workspaceSync,
		},
		snapshotApplier,
		httpClient,
		output,
		hook.WithPreCommitOutdatedHandler(func(
			ctx context.Context,
			workDir string,
			config *client.WorkspaceConfig,
			baseHash, remoteHash docs.CommitHash,
		) error {
			state, err := pullCommit.restartAfterOutdated(ctx, workDir, config, baseHash, remoteHash)
			if errors.Is(err, errPullCommitConflict) {
				printPullCommitConflicts(cmd, state.ConflictFiles)
			}
			if errors.Is(err, errPullCommitRetry) {
				cmd.Printf("sanho: created docs base commit %s.\n", state.SyncCommit)
				cmd.PrintErrln("sanho: staged and unstaged docs were preserved. Run the same git commit command again.")
			}
			return err
		}),
	)

	// Execute
	if err := usecase.Execute(ctx, cwd); err != nil {
		// Handle specific errors with appropriate messages
		switch {
		case errors.Is(err, hook.ErrConfigBroken):
			cmd.PrintErrf("sanho: configuration is broken. Please run 'sanho init' to reinitialize.\n")
		case errors.Is(err, hook.ErrConflictMarkerFound):
			// Message already printed by output
		case errors.Is(err, hook.ErrPendingFixExists):
			// Message already printed by output
		case errors.Is(err, hook.ErrOutdated):
			// Message already printed by output
		case errors.Is(err, hook.ErrDocsRepoBusy):
			// Message already printed by output
		case errors.Is(err, hook.ErrUnknownDocsCommit):
			// Message already printed by output
		default:
			cmd.PrintErrf("sanho hook pre-commit: %v\n", err)
		}
		return err
	}

	return nil
}

type indexPreCommitSnapshotBuilder struct {
	ctx           context.Context
	workDir       string
	docsDir       string
	workspaceSync *infraGit.WorkspaceSync
}

func (b *indexPreCommitSnapshotBuilder) Build(_ string) ([]byte, error) {
	return b.workspaceSync.BuildIndexDocsSnapshot(b.ctx, b.workDir, b.docsDir)
}

// cliPreCommitOutput implements hook.PreCommitOutput for CLI.
type cliPreCommitOutput struct {
	cmd *cobra.Command
}

func newCLIPreCommitOutput(cmd *cobra.Command) *cliPreCommitOutput {
	return &cliPreCommitOutput{cmd: cmd}
}

func (o *cliPreCommitOutput) Info(msg string) {
	o.cmd.Printf("sanho: %s\n", msg)
}

func (o *cliPreCommitOutput) Warning(msg string) {
	o.cmd.Printf("sanho: %s\n", msg)
}

func (o *cliPreCommitOutput) Error(msg string) {
	o.cmd.PrintErrf("sanho: %s\n", msg)
}

// preCommitGitClientAdapter adapts git.Client to hook.PreCommitGitClient interface.
type preCommitGitClientAdapter struct {
	client *infraGit.Client
}

func newPreCommitGitClientAdapter(client *infraGit.Client) *preCommitGitClientAdapter {
	return &preCommitGitClientAdapter{client: client}
}

func (a *preCommitGitClientAdapter) HasDocsChangeForCommit(ctx context.Context, repoPath, docsDir string) (bool, error) {
	return a.client.HasDocsChangeForCommit(ctx, repoPath, docsDir)
}

func (a *preCommitGitClientAdapter) MergeFile(ctx context.Context, baseContent, localContent, remoteContent []byte) (hook.MergeResult, error) {
	result, err := a.client.MergeFile(ctx, baseContent, localContent, remoteContent)
	if err != nil {
		return hook.MergeResult{}, err
	}
	return hook.MergeResult{
		Content:      result.Content,
		HasConflicts: result.HasConflicts,
	}, nil
}

// preCommitHTTPClientAdapter adapts httpclient.HTTPClient to hook.PreCommitHTTPClient interface.
type preCommitHTTPClientAdapter struct {
	client *httpclient.HTTPClient
}

func newPreCommitHTTPClientAdapter(client *httpclient.HTTPClient) *preCommitHTTPClientAdapter {
	return &preCommitHTTPClientAdapter{client: client}
}

func (a *preCommitHTTPClientAdapter) DocsPush(ctx context.Context, req hook.DocsPushRequest) (hook.DocsPushResponse, error) {
	httpReq := httpclient.DocsPushRequest{
		WorkspaceID:  workspace.WorkspaceID(req.WorkspaceID),
		BaseDocsHash: docs.CommitHash(req.BaseDocsHash),
		DocsSnapshot: req.DocsSnapshot,
		ActorEmail:   req.ActorEmail,
	}

	resp, err := a.client.DocsPush(ctx, httpReq)
	if err != nil {
		// Map httpclient errors to hook errors for proper handling in usecase
		if errors.Is(err, httpclient.ErrUnknownDocsCommit) {
			return hook.DocsPushResponse{}, hook.ErrUnknownDocsCommit
		}
		if errors.Is(err, httpclient.ErrDocsRepoBusy) {
			return hook.DocsPushResponse{}, hook.ErrDocsRepoBusy
		}
		return hook.DocsPushResponse{}, err
	}

	return hook.DocsPushResponse{
		Ok:              resp.Ok,
		Status:          resp.Status,
		NewDocsHash:     resp.NewDocsHash,
		CurrentDocsHash: resp.CurrentDocsHash,
		Error:           resp.Error,
	}, nil
}

func (a *preCommitHTTPClientAdapter) DocsSnapshot(ctx context.Context, project docs.ProjectName, commit docs.CommitHash) (docs.DocsSnapshot, docs.CommitHash, error) {
	return a.client.DocsSnapshot(ctx, project, commit)
}
