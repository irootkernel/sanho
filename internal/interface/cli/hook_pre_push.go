package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/irootkernel/sanho/internal/domain/merge"
	"github.com/irootkernel/sanho/internal/infra/fs"
	"github.com/irootkernel/sanho/internal/usecase/hook"
)

// prePushTimeout is the timeout for pre-push operations.
const prePushTimeout = 30 * time.Second

// runPrePushHook executes the pre-push hook logic.
func runPrePushHook(cmd *cobra.Command) error {
	ctx, cancel := context.WithTimeout(context.Background(), prePushTimeout)
	defer cancel()

	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		cmd.PrintErrf("sanho hook pre-push: failed to get current directory: %v\n", err)
		return err
	}
	config, err := fs.NewFileConfigLoader().Load(cwd)
	if err != nil {
		return fmt.Errorf("sanho hook pre-push: load configuration: %w", err)
	}
	if err := retryPendingWorkspaceReport(ctx, cwd, config); err != nil {
		return fmt.Errorf("sanho hook pre-push: %w", err)
	}
	hasPullCommit, err := newPullCommitEngine(nil).hasTransaction(ctx, cwd)
	if err != nil {
		return fmt.Errorf("sanho hook pre-push: check pull-commit state: %w", err)
	}
	if hasPullCommit {
		cmd.PrintErrln("sanho: an incomplete pull-commit transaction exists.")
		cmd.PrintErrln("Complete the pending commit or resolve it with 'sanho pull-commit --continue'.")
		cmd.PrintErrln("Push is blocked so the docs base commit cannot be published alone.")
		return errors.New("pull-commit transaction exists - push blocked")
	}
	hasPulledDocs, err := hasPulledDocsBaseline(ctx, cwd)
	if err != nil {
		return fmt.Errorf("sanho hook pre-push: check pulled docs baseline: %w", err)
	}
	if hasPulledDocs {
		cmd.PrintErrln("sanho: pulled docs have not been materialized in application history.")
		cmd.PrintErrln("Run 'sanho pull-commit' before pushing.")
		return errors.New("pulled docs baseline exists - push blocked")
	}

	// Create dependencies
	configLoader := fs.NewFileConfigLoader()
	pendingFixStore := newPrePushPendingFixStoreAdapter(fs.NewFilePendingFixStore())
	conflictDetector := merge.NewFileConflictDetector()
	output := newCLIPrePushOutput(cmd)

	// Create usecase
	usecase := hook.NewPrePushUseCase(
		configLoader,
		pendingFixStore,
		conflictDetector,
		output,
	)

	// Execute
	if err := usecase.Execute(ctx, cwd); err != nil {
		// Handle specific errors with appropriate messages
		switch {
		case errors.Is(err, hook.ErrConfigBroken):
			// Message already printed by output
		case errors.Is(err, hook.ErrPrePushConflictMarkerFound):
			// Message already printed by output
		case errors.Is(err, hook.ErrPrePushPendingFixExists):
			// Message already printed by output
		default:
			cmd.PrintErrf("sanho hook pre-push: %v\n", err)
		}
		return err
	}

	return nil
}

// cliPrePushOutput implements hook.PrePushOutput for CLI.
type cliPrePushOutput struct {
	cmd *cobra.Command
}

func newCLIPrePushOutput(cmd *cobra.Command) *cliPrePushOutput {
	return &cliPrePushOutput{cmd: cmd}
}

func (o *cliPrePushOutput) Info(msg string) {
	fmt.Fprintf(o.cmd.OutOrStdout(), "sanho: %s\n", msg)
}

func (o *cliPrePushOutput) Warning(msg string) {
	fmt.Fprintf(o.cmd.OutOrStdout(), "sanho: %s\n", msg)
}

func (o *cliPrePushOutput) Error(msg string) {
	o.cmd.PrintErrf("sanho: %s\n", msg)
}

// prePushPendingFixStoreAdapter adapts fs.FilePendingFixStore to hook.PrePushPendingFixStore interface.
type prePushPendingFixStoreAdapter struct {
	store *fs.FilePendingFixStore
}

func newPrePushPendingFixStoreAdapter(store *fs.FilePendingFixStore) *prePushPendingFixStoreAdapter {
	return &prePushPendingFixStoreAdapter{store: store}
}

func (a *prePushPendingFixStoreAdapter) Exists(path string) (bool, error) {
	_, exists, err := a.store.Read(path)
	return exists, err
}
