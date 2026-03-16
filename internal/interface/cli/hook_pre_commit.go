package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/SeventeenthEarth/kkachi/internal/domain/docs"
	"github.com/SeventeenthEarth/kkachi/internal/domain/merge"
	"github.com/SeventeenthEarth/kkachi/internal/domain/workspace"
	"github.com/SeventeenthEarth/kkachi/internal/infra/fs"
	infraGit "github.com/SeventeenthEarth/kkachi/internal/infra/git"
	"github.com/SeventeenthEarth/kkachi/internal/infra/httpclient"
	"github.com/SeventeenthEarth/kkachi/internal/usecase/hook"
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
		cmd.PrintErrf("kkachi-cli hook pre-commit: failed to get current directory: %v\n", err)
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

	// Load config first to get server URL
	config, err := configLoader.Load(cwd)
	if err != nil {
		cmd.PrintErrf("kkachi-cli hook pre-commit: %v\n", err)
		cmd.PrintErrf("kkachi configuration is broken or missing (.kkachi.json / .kkachi_docs_hash).\n")
		return err
	}

	httpClient := newPreCommitHTTPClientAdapter(httpclient.NewHTTPClient(config.ServerURL))

	// Create usecase
	usecase := hook.NewPreCommitUseCase(
		configLoader,
		docsHashStore,
		pendingFixStore,
		conflictDetector,
		newPreCommitGitClientAdapter(gitClient),
		snapshotBuilder,
		snapshotApplier,
		httpClient,
		output,
	)

	// Execute
	if err := usecase.Execute(ctx, cwd); err != nil {
		// Handle specific errors with appropriate messages
		switch {
		case errors.Is(err, hook.ErrConfigBroken):
			cmd.PrintErrf("kkachi: configuration is broken. Please run 'kkachi init' to reinitialize.\n")
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
			cmd.PrintErrf("kkachi-cli hook pre-commit: %v\n", err)
		}
		return err
	}

	return nil
}

// cliPreCommitOutput implements hook.PreCommitOutput for CLI.
type cliPreCommitOutput struct {
	cmd *cobra.Command
}

func newCLIPreCommitOutput(cmd *cobra.Command) *cliPreCommitOutput {
	return &cliPreCommitOutput{cmd: cmd}
}

func (o *cliPreCommitOutput) Info(msg string) {
	fmt.Fprintf(o.cmd.OutOrStdout(), "kkachi: %s\n", msg)
}

func (o *cliPreCommitOutput) Warning(msg string) {
	fmt.Fprintf(o.cmd.OutOrStdout(), "kkachi: %s\n", msg)
}

func (o *cliPreCommitOutput) Error(msg string) {
	o.cmd.PrintErrf("kkachi: %s\n", msg)
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
