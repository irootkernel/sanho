package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/SeventeenthEarth/kkachi/internal/domain/docs"
	"github.com/SeventeenthEarth/kkachi/internal/infra/fs"
	infraGit "github.com/SeventeenthEarth/kkachi/internal/infra/git"
	"github.com/SeventeenthEarth/kkachi/internal/infra/httpclient"
	docsUsecase "github.com/SeventeenthEarth/kkachi/internal/usecase/docs"
)

// pullTimeout is the timeout for pull operations.
const pullTimeout = 60 * time.Second

// newPullCmd creates the pull command.
func newPullCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pull",
		Short: "Pull latest docs snapshot from server",
		Long: `Pull the latest documentation snapshot from the kkachi server
and apply it to the local docs directory.

This command will:
- Check the server for the latest docs version
- Download the snapshot if outdated
- Apply the snapshot to the local docs directory
- Update the local docs hash

If local changes exist, use --force to overwrite them.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			force, _ := cmd.Flags().GetBool("force")
			return runPullCommand(cmd, force)
		},
	}

	cmd.Flags().Bool("force", false, "Overwrite local changes without prompting")

	return cmd
}

// runPullCommand executes the kkachi pull logic.
func runPullCommand(cmd *cobra.Command, force bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), pullTimeout)
	defer cancel()

	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		cmd.PrintErrf("kkachi pull: failed to get current directory: %v\n", err)
		return err
	}

	// Create dependencies
	configLoader := fs.NewFileConfigLoader()
	docsHashStore := fs.NewFileDocsHashStore()
	pendingFixStore := newPullPendingFixStoreAdapter(fs.NewFilePendingFixStore())
	snapshotApplier := fs.NewSnapshotApplier()
	output := newCLIPullOutput(cmd)

	// Load config first to get server URL
	config, err := configLoader.Load(cwd)
	if err != nil {
		cmd.PrintErrf("kkachi pull: %v\n", err)
		cmd.PrintErrf("This directory is not a kkachi workspace.\n")
		cmd.PrintErrf("Run 'kkachi init' first to initialize.\n")
		return err
	}

	httpClient := newPullHTTPClientAdapter(httpclient.NewHTTPClient(config.ServerURL))
	gitClient := newPullGitClientAdapter(infraGit.NewClient())

	// Create usecase
	usecase := docsUsecase.NewPullUseCase(
		configLoader,
		docsHashStore,
		pendingFixStore,
		httpClient,
		snapshotApplier,
		gitClient,
		output,
	)

	// Execute
	input := docsUsecase.PullInput{
		WorkDir: cwd,
		Force:   force,
	}

	if err := usecase.Execute(ctx, input); err != nil {
		// Handle specific errors with appropriate messages
		switch {
		case errors.Is(err, docsUsecase.ErrPullConfigBroken):
			cmd.PrintErrf("kkachi: configuration is broken. Please run 'kkachi init' to reinitialize.\n")
		case errors.Is(err, docsUsecase.ErrPullPendingFix):
			// Message already printed by output
		case errors.Is(err, docsUsecase.ErrPullLocalChanges):
			// Message already printed by output
		case errors.Is(err, docsUsecase.ErrPullAlreadyUpToDate):
			// This is not really an error, but we return nil for success
			return nil
		case errors.Is(err, docsUsecase.ErrPullUnknownProject):
			cmd.PrintErrf("kkachi: project '%s' is not registered on server.\n", config.Project)
			cmd.PrintErrf("kkachi: run 'kkachi init' or 'kkachi project add' to register it.\n")
		case errors.Is(err, docsUsecase.ErrPullUnknownWorkspace):
			cmd.PrintErrf("kkachi: workspace '%s' is not registered on server.\n", config.WorkspaceID)
			cmd.PrintErrf("kkachi: run 'kkachi init' or 'kkachi workspace register' to register it.\n")
		default:
			cmd.PrintErrf("kkachi pull: %v\n", err)
		}
		return err
	}

	return nil
}

// cliPullOutput implements docsUsecase.PullOutput for CLI.
type cliPullOutput struct {
	cmd *cobra.Command
}

func newCLIPullOutput(cmd *cobra.Command) *cliPullOutput {
	return &cliPullOutput{cmd: cmd}
}

func (o *cliPullOutput) Info(msg string) {
	fmt.Fprintf(o.cmd.OutOrStdout(), "kkachi: %s\n", msg)
}

func (o *cliPullOutput) Warning(msg string) {
	fmt.Fprintf(o.cmd.OutOrStdout(), "kkachi: %s\n", msg)
}

func (o *cliPullOutput) Error(msg string) {
	o.cmd.PrintErrf("kkachi: %s\n", msg)
}

// pullPendingFixStoreAdapter adapts fs.FilePendingFixStore to docsUsecase.PullPendingFixStore.
type pullPendingFixStoreAdapter struct {
	store *fs.FilePendingFixStore
}

func newPullPendingFixStoreAdapter(store *fs.FilePendingFixStore) *pullPendingFixStoreAdapter {
	return &pullPendingFixStoreAdapter{store: store}
}

func (a *pullPendingFixStoreAdapter) Exists(path string) (bool, error) {
	_, exists, err := a.store.Read(path)
	return exists, err
}

// pullHTTPClientAdapter adapts httpclient.HTTPClient to docsUsecase.PullHTTPClient.
type pullHTTPClientAdapter struct {
	client *httpclient.HTTPClient
}

func newPullHTTPClientAdapter(client *httpclient.HTTPClient) *pullHTTPClientAdapter {
	return &pullHTTPClientAdapter{client: client}
}

func (a *pullHTTPClientAdapter) DocsHead(ctx context.Context, project docs.ProjectName) (docs.CommitHash, error) {
	hash, err := a.client.DocsHead(ctx, project)
	if err != nil {
		// Map httpclient errors to usecase errors for proper handling
		if errors.Is(err, httpclient.ErrUnknownProject) {
			return "", docsUsecase.ErrPullUnknownProject
		}
		if errors.Is(err, httpclient.ErrUnknownWorkspace) {
			return "", docsUsecase.ErrPullUnknownWorkspace
		}
		return "", err
	}
	return hash, nil
}

func (a *pullHTTPClientAdapter) DocsSnapshot(ctx context.Context, project docs.ProjectName, commit docs.CommitHash) (docs.DocsSnapshot, docs.CommitHash, error) {
	snapshot, hash, err := a.client.DocsSnapshot(ctx, project, commit)
	if err != nil {
		// Map httpclient errors to usecase errors for proper handling
		if errors.Is(err, httpclient.ErrUnknownProject) {
			return nil, "", docsUsecase.ErrPullUnknownProject
		}
		if errors.Is(err, httpclient.ErrUnknownWorkspace) {
			return nil, "", docsUsecase.ErrPullUnknownWorkspace
		}
		return nil, "", err
	}
	return snapshot, hash, nil
}

// pullGitClientAdapter adapts infraGit.Client to docsUsecase.PullGitClient.
type pullGitClientAdapter struct {
	client *infraGit.Client
}

func newPullGitClientAdapter(client *infraGit.Client) *pullGitClientAdapter {
	return &pullGitClientAdapter{client: client}
}

func (a *pullGitClientAdapter) HasLocalDocsChanges(ctx context.Context, repoPath, docsDir string) (bool, error) {
	return a.client.HasLocalDocsChanges(ctx, repoPath, docsDir)
}
