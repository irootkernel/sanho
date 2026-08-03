package cli

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/irootkernel/sanho/internal/domain/docs"
	"github.com/irootkernel/sanho/internal/domain/merge"
	"github.com/irootkernel/sanho/internal/domain/workspace"
	"github.com/irootkernel/sanho/internal/infra/fs"
	infraGit "github.com/irootkernel/sanho/internal/infra/git"
	"github.com/irootkernel/sanho/internal/infra/httpclient"
	"github.com/irootkernel/sanho/internal/usecase/hook"
)

// fixTimeout is the timeout for fix operations.
const fixTimeout = 60 * time.Second

// newFixCmd creates the fix command.
func newFixCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "fix",
		Short: "Complete pending docs merge and push to daemon",
		Long: `After resolving merge conflicts in the docs directory,
run this command to push the merged documentation to the daemon.

This command will:
- Verify all conflict markers are resolved
- Push the merged docs to the daemon
- Clear the pending fix state`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFixCommand(cmd)
		},
	}
}

// runFixCommand executes the sanho fix logic.
func runFixCommand(cmd *cobra.Command) error {
	ctx, cancel := context.WithTimeout(context.Background(), fixTimeout)
	defer cancel()

	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		cmd.PrintErrf("sanho fix: failed to get current directory: %v\n", err)
		return err
	}

	// Create dependencies
	configLoader := fs.NewFileConfigLoader()
	docsHashStore := fs.NewFileDocsHashStore()
	pendingFixStore := newFixPendingFixStoreAdapter(fs.NewFilePendingFixStore())
	conflictDetector := merge.NewFileConflictDetector()
	snapshotBuilder := fs.NewSnapshotBuilder()
	output := newCLIFixOutput(cmd)

	// Load config first to get daemon socket path
	config, err := configLoader.Load(cwd)
	if err != nil {
		cmd.PrintErrf("sanho fix: %v\n", err)
		cmd.PrintErrf("sanho configuration is broken or missing (.sanho.json / .sanho_docs_hash).\n")
		return err
	}
	if err := requireWorkspaceMutationSafe(ctx, cwd); err != nil {
		return wrapGitOperationGuard("sanho fix", err)
	}

	rawHTTPClient, err := newDaemonClient(config.SocketPath)
	if err != nil {
		return err
	}
	httpClient := newFixHTTPClientAdapter(rawHTTPClient)
	gitClient := newFixGitClientAdapter(infraGit.NewDetector())

	// Create usecase
	usecase := hook.NewFixUseCase(
		configLoader,
		docsHashStore,
		pendingFixStore,
		conflictDetector,
		snapshotBuilder,
		httpClient,
		gitClient,
		output,
	)

	// Execute
	if err := usecase.Execute(ctx, cwd); err != nil {
		// Handle specific errors with appropriate messages
		switch {
		case errors.Is(err, hook.ErrConfigBroken):
			cmd.PrintErrf("sanho: configuration is broken. Please run 'sanho init' to reinitialize.\n")
		case errors.Is(err, hook.ErrNoPendingFix):
			// Message already printed by output
		case errors.Is(err, hook.ErrConflictMarkerFound):
			// Message already printed by output
		case errors.Is(err, hook.ErrFixHeadChanged):
			// Message already printed by output
		case errors.Is(err, hook.ErrDocsRepoBusy):
			// Message already printed by output
		case errors.Is(err, hook.ErrUnknownDocsCommit):
			// Message already printed by output
		case errors.Is(err, hook.ErrActorEmailRequired):
			// Message already printed by output
		default:
			cmd.PrintErrf("sanho fix: %v\n", err)
		}
		return err
	}

	return nil
}

// cliFixOutput implements hook.FixOutput for CLI.
type cliFixOutput struct {
	cmd *cobra.Command
}

func newCLIFixOutput(cmd *cobra.Command) *cliFixOutput {
	return &cliFixOutput{cmd: cmd}
}

func (o *cliFixOutput) Info(msg string) {
	o.cmd.Printf("sanho: %s\n", msg)
}

func (o *cliFixOutput) Warning(msg string) {
	o.cmd.Printf("sanho: %s\n", msg)
}

func (o *cliFixOutput) Error(msg string) {
	o.cmd.PrintErrf("sanho: %s\n", msg)
}

// fixPendingFixStoreAdapter adapts fs.FilePendingFixStore to hook.FixPendingFixStore interface.
type fixPendingFixStoreAdapter struct {
	store *fs.FilePendingFixStore
}

func newFixPendingFixStoreAdapter(store *fs.FilePendingFixStore) *fixPendingFixStoreAdapter {
	return &fixPendingFixStoreAdapter{store: store}
}

func (a *fixPendingFixStoreAdapter) Read(path string) (bool, error) {
	_, exists, err := a.store.Read(path)
	return exists, err
}

func (a *fixPendingFixStoreAdapter) Remove(path string) error {
	return a.store.Remove(path)
}

// fixHTTPClientAdapter adapts httpclient.HTTPClient to hook.FixHTTPClient interface.
type fixHTTPClientAdapter struct {
	client *httpclient.HTTPClient
}

func newFixHTTPClientAdapter(client *httpclient.HTTPClient) *fixHTTPClientAdapter {
	return &fixHTTPClientAdapter{client: client}
}

func (a *fixHTTPClientAdapter) DocsHead(ctx context.Context, project docs.ProjectName) (docs.CommitHash, error) {
	return a.client.DocsHead(ctx, project)
}

func (a *fixHTTPClientAdapter) DocsPush(ctx context.Context, req hook.DocsPushRequest) (hook.DocsPushResponse, error) {
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

// fixGitClientAdapter adapts git.Detector to hook.FixGitClient interface.
type fixGitClientAdapter struct {
	detector *infraGit.Detector
}

func newFixGitClientAdapter(detector *infraGit.Detector) *fixGitClientAdapter {
	return &fixGitClientAdapter{detector: detector}
}

func (a *fixGitClientAdapter) GetUserEmail(ctx context.Context, path string) (string, error) {
	return a.detector.GetUserEmail(ctx, path)
}
