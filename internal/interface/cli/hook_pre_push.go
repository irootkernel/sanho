package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/SeventeenthEarth/kkachi/internal/domain/merge"
	"github.com/SeventeenthEarth/kkachi/internal/infra/fs"
	"github.com/SeventeenthEarth/kkachi/internal/usecase/hook"
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
		cmd.PrintErrf("kkachi hook pre-push: failed to get current directory: %v\n", err)
		return err
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
			cmd.PrintErrf("kkachi hook pre-push: %v\n", err)
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
	fmt.Fprintf(o.cmd.OutOrStdout(), "kkachi: %s\n", msg)
}

func (o *cliPrePushOutput) Warning(msg string) {
	fmt.Fprintf(o.cmd.OutOrStdout(), "kkachi: %s\n", msg)
}

func (o *cliPrePushOutput) Error(msg string) {
	o.cmd.PrintErrf("kkachi: %s\n", msg)
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
