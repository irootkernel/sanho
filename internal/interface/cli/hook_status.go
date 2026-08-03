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
	"github.com/irootkernel/sanho/internal/domain/merge"
	"github.com/irootkernel/sanho/internal/infra/fs"
	"github.com/irootkernel/sanho/internal/infra/httpclient"
)

// hookStatusTimeout is the timeout for hook status operations.
// Kept short to avoid slowing down Git operations.
const hookStatusTimeout = 10 * time.Second

// runHookStatus executes status check for read-only hooks.
// It always returns nil to not block Git operations.
// Errors and status are printed to output, but never cause exit code != 0.
func runHookStatus(cmd *cobra.Command, hookName string, reconcile bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), hookStatusTimeout)
	defer cancel()

	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		cmd.PrintErrf("sanho %s: warning: failed to get current directory: %v\n", hookName, err)
		return nil // Always exit 0
	}
	if skipMutationHookDuringGitOperation(ctx, cwd, hookName, cmd) {
		return nil
	}

	// Step 1: Load .sanho.json
	configLoader := fs.NewFileConfigLoader()
	config, err := configLoader.Load(cwd)
	if err != nil {
		if errors.Is(err, fs.ErrConfigNotFound) {
			// Not a sanho workspace - silently ignore
			return nil
		}
		cmd.PrintErrf("sanho %s: warning: failed to load config: %v\n", hookName, err)
		return nil // Always exit 0
	}
	if reconcile {
		if _, err := reconcileWorkspaceDocsFromHEAD(ctx, cwd, config); err != nil {
			cmd.PrintErrf("sanho %s: warning: failed to reconcile docs hash from HEAD: %v\n", hookName, err)
		}
	}

	// Step 2: Load .sanho_docs_hash
	hashStore := fs.NewFileDocsHashStore()
	hashPath := filepath.Join(cwd, config.DocsHashFile)
	localHash, err := hashStore.Read(hashPath)
	if err != nil {
		if errors.Is(err, fs.ErrHashFileNotFound) {
			cmd.PrintErrf("sanho %s: warning: docs hash file not found\n", hookName)
		} else {
			cmd.PrintErrf("sanho %s: warning: failed to read docs hash: %v\n", hookName, err)
		}
		return nil // Always exit 0
	}

	// Step 3: Check pending fix state
	pendingFixStore := fs.NewFilePendingFixStore()
	pendingFixPath := filepath.Join(cwd, config.PendingFixFile)
	_, hasPendingFix, err := pendingFixStore.Read(pendingFixPath)
	if err != nil {
		cmd.PrintErrf("sanho %s: warning: failed to read pending fix state: %v\n", hookName, err)
		return nil // Always exit 0
	}

	// Step 4: Check for conflict markers in docs
	var hasConflicts bool
	docsPath := filepath.Join(cwd, config.DocsDir)
	if _, err := os.Stat(docsPath); err == nil {
		conflictDetector := merge.NewFileConflictDetector()
		conflictFiles, detectErr := conflictDetector.DetectConflicts(docsPath)
		if detectErr != nil {
			// Log warning but continue
			cmd.PrintErrf("sanho %s: warning: failed to scan for conflicts: %v\n", hookName, detectErr)
		}
		hasConflicts = len(conflictFiles) > 0
	}

	// Step 5: Call /docs/head to get daemon HEAD
	httpClient, err := newDaemonClient(config.SocketPath)
	if err != nil {
		return err
	}
	daemonHead, err := httpClient.DocsHead(ctx, config.Project)

	// Determine status
	var status client.DocsStatus
	var daemonHeadStr string

	if err != nil {
		status = client.DocsStatusUnknown
		daemonHeadStr = "(unavailable)"
		if errors.Is(err, httpclient.ErrUnknownProject) {
			cmd.PrintErrf("sanho %s: warning: project '%s' is not registered on daemon\n", hookName, config.Project)
		} else {
			cmd.PrintErrf("sanho %s: warning: failed to connect to daemon: %v\n", hookName, err)
		}
	} else {
		daemonHeadStr = string(daemonHead)
		if string(localHash) == string(daemonHead) {
			status = client.DocsStatusUpToDate
		} else {
			status = client.DocsStatusOutdated
		}
	}

	// Print concise status
	fmt.Printf("sanho %s: docs status: %s\n", hookName, status)
	fmt.Printf("  base: %s\n", localHash)
	fmt.Printf("  head: %s\n", daemonHeadStr)

	// Additional warnings
	if hasPendingFix {
		fmt.Printf("sanho %s: pending fix detected. Run 'sanho fix' to finalize.\n", hookName)
	}
	if hasConflicts {
		fmt.Printf("sanho %s: conflict markers detected in docs.\n", hookName)
	}
	if status == client.DocsStatusOutdated {
		fmt.Printf("sanho %s: docs are outdated. A merge may occur during pre-commit.\n", hookName)
	}

	return nil // Always exit 0
}
