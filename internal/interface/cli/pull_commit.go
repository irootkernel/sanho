package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/irootkernel/sanho/internal/infra/fs"
	"github.com/irootkernel/sanho/internal/infra/httpclient"
)

const pullCommitTimeout = 2 * time.Minute

func newPullCommitCmd() *cobra.Command {
	var continueTransaction bool
	var abortTransaction bool

	cmd := &cobra.Command{
		Use:   "pull-commit",
		Short: "Merge remote docs into the local base while preserving dirty changes",
		Long: `Create a docs-only commit from the latest server snapshot on the
latest acceptable main while preserving staged and unstaged changes as separate
layers. Unpublished linear feature branches are rebased onto the system commit.

This command is also executed automatically by the pre-commit hook when the
central docs version changed.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if continueTransaction && abortTransaction {
				return errors.New("--continue and --abort cannot be used together")
			}
			return runPullCommitCommand(cmd, continueTransaction, abortTransaction)
		},
	}
	cmd.Flags().BoolVar(&continueTransaction, "continue", false, "Continue after resolving and staging docs conflicts")
	cmd.Flags().BoolVar(&abortTransaction, "abort", false, "Restore the original staged and unstaged docs state")
	return cmd
}

func runPullCommitCommand(cmd *cobra.Command, continueTransaction, abortTransaction bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), pullCommitTimeout)
	defer cancel()

	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("sanho pull-commit: get current directory: %w", err)
	}
	configLoader := fs.NewFileConfigLoader()
	config, err := configLoader.Load(workDir)
	if err != nil {
		return fmt.Errorf("sanho pull-commit: %w", err)
	}
	httpClient := httpclient.NewHTTPClient(config.ServerURL)
	engine := newPullCommitEngine(httpClient)

	if abortTransaction {
		if err := engine.abort(ctx, workDir, config); err != nil {
			return fmt.Errorf("sanho pull-commit --abort: %w", err)
		}
		cmd.Println("sanho: pull-commit aborted; original staged and unstaged docs were restored.")
		return nil
	}
	if err := retryPendingWorkspaceReport(ctx, workDir, config); err != nil {
		return fmt.Errorf("sanho pull-commit: %w", err)
	}

	if continueTransaction {
		state, exists, err := engine.resume(ctx, workDir, config)
		if !exists {
			return errors.New("sanho pull-commit --continue: no transaction exists")
		}
		if errors.Is(err, errPullCommitConflict) {
			printPullCommitConflicts(cmd, state.ConflictFiles)
			return err
		}
		if err != nil && !errors.Is(err, errPullCommitRetry) {
			return fmt.Errorf("sanho pull-commit --continue: %w", err)
		}
		if err := engine.finishManual(ctx, workDir, config); err != nil {
			return fmt.Errorf("sanho pull-commit --continue: %w", err)
		}
		cmd.Printf("sanho: created docs base commit %s and restored staged/unstaged docs layers.\n", state.SyncCommit)
		return nil
	}

	hasTransaction, err := engine.hasTransaction(ctx, workDir)
	if err != nil {
		return err
	}
	if hasTransaction {
		return errors.New("a pull-commit transaction already exists; use --continue or --abort")
	}
	_, hasPendingFix, err := fs.NewFilePendingFixStore().Read(filepath.Join(workDir, config.PendingFixFile))
	if err != nil {
		return fmt.Errorf("check legacy pending-fix state: %w", err)
	}
	if hasPendingFix {
		return errors.New("legacy pending-fix state exists; run 'sanho fix' first")
	}
	conflicts, err := engine.conflictDetector.DetectConflicts(filepath.Join(workDir, config.DocsDir))
	if err != nil {
		return fmt.Errorf("check docs conflicts: %w", err)
	}
	if len(conflicts) > 0 {
		cmd.PrintErrln("sanho: existing conflict markers found in docs:")
		for _, file := range conflicts {
			cmd.PrintErrf("  - %s\n", file)
		}
		cmd.PrintErrln("Resolve the existing conflicts before starting pull-commit.")
		return errPullCommitConflict
	}
	hash, err := fs.NewFileDocsHashStore().Read(filepath.Join(workDir, config.DocsHashFile))
	if err != nil {
		return fmt.Errorf("read local docs version: %w", err)
	}
	remoteHash, err := httpClient.DocsHead(ctx, config.Project)
	if err != nil {
		return fmt.Errorf("read server docs version: %w", err)
	}
	hasPulledDocs, err := engine.hasPulledDocs(ctx, workDir)
	if err != nil {
		return fmt.Errorf("check pulled docs baseline: %w", err)
	}
	if hash == remoteHash && !hasPulledDocs {
		cmd.Println("sanho: docs base is already up to date.")
		return nil
	}

	state, err := engine.start(ctx, workDir, config, hash, remoteHash)
	if errors.Is(err, errPullCommitConflict) {
		printPullCommitConflicts(cmd, state.ConflictFiles)
		return err
	}
	if err != nil && !errors.Is(err, errPullCommitRetry) {
		return fmt.Errorf("sanho pull-commit: %w", err)
	}
	if err := engine.finishManual(ctx, workDir, config); err != nil {
		return fmt.Errorf("sanho pull-commit: %w", err)
	}
	cmd.Printf("sanho: created docs base commit %s and restored staged/unstaged docs layers.\n", state.SyncCommit)
	return nil
}

func printPullCommitConflicts(cmd *cobra.Command, files []string) {
	cmd.PrintErrln("sanho: docs merge has conflicts:")
	for _, file := range files {
		cmd.PrintErrf("  - %s\n", file)
	}
	cmd.PrintErrln("Resolve the files, stage them, then run 'sanho pull-commit --continue'.")
	cmd.PrintErrln("Use 'sanho pull-commit --abort' to restore the original docs state.")
}
