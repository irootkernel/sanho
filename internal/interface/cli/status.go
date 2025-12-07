package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/SeventeenthEarth/kkachi/internal/domain/client"
	"github.com/SeventeenthEarth/kkachi/internal/domain/merge"
	"github.com/SeventeenthEarth/kkachi/internal/infra/fs"
	"github.com/SeventeenthEarth/kkachi/internal/infra/httpclient"
)

// newStatusCmd creates the status command.
func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the current docs synchronization status",
		Long: `Display the current workspace docs status including:
- Workspace ID
- Local docs base hash
- Server docs HEAD hash
- Synchronization status (up_to_date, outdated)
- Pending fix status`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			// Get current working directory
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("failed to get current directory: %w", err)
			}

			// Step 1: Load .kkachi.json
			configLoader := fs.NewFileConfigLoader()
			config, err := configLoader.Load(cwd)
			if err != nil {
				if errors.Is(err, fs.ErrConfigNotFound) {
					return errors.New("this directory is not a kkachi workspace. Run 'kkachi init' first")
				}
				return fmt.Errorf("failed to load config: %w", err)
			}

			// Step 2: Load .kkachi_docs_hash
			hashStore := fs.NewFileDocsHashStore()
			hashPath := filepath.Join(cwd, config.DocsHashFile)
			localHash, err := hashStore.Read(hashPath)
			if err != nil {
				if errors.Is(err, fs.ErrHashFileNotFound) {
					return errors.New("docs hash file not found. Workspace may be corrupted. Try 'kkachi init --force'")
				}
				return fmt.Errorf("failed to read docs hash file: %w", err)
			}

			// Step 3: Check pending fix state
			pendingFixStore := fs.NewFilePendingFixStore()
			pendingFixPath := filepath.Join(cwd, config.PendingFixFile)
			pendingFixState, hasPendingFix, err := pendingFixStore.Read(pendingFixPath)
			if err != nil {
				return fmt.Errorf("failed to read pending fix state: %w", err)
			}

			// Step 4: Check for conflict markers in docs
			var hasConflicts bool
			var conflictFiles []string
			docsPath := filepath.Join(cwd, config.DocsDir)
			if _, err := os.Stat(docsPath); err == nil {
				conflictDetector := merge.NewFileConflictDetector()
				conflictFiles, err = conflictDetector.DetectConflicts(docsPath)
				if err != nil {
					// Log warning but continue
					cmd.PrintErrf("Warning: failed to scan for conflicts: %v\n", err)
				}
				hasConflicts = len(conflictFiles) > 0
			}

			// Step 5: Call /docs/head to get server HEAD
			httpClient := httpclient.NewHTTPClient(config.ServerURL)
			serverHead, err := httpClient.DocsHead(ctx, config.Project)

			// Determine status
			var status client.DocsStatus
			var serverHeadStr string
			var serverError error // Track server error to return at the end

			if err != nil {
				status = client.DocsStatusUnknown
				serverHeadStr = "(unavailable)"
				if errors.Is(err, httpclient.ErrUnknownProject) {
					cmd.PrintErrf("Warning: project '%s' is not registered on server\n", config.Project)
					serverError = fmt.Errorf("project '%s' is not registered on server", config.Project)
				} else {
					cmd.PrintErrf("Warning: failed to connect to server: %v\n", err)
					serverError = fmt.Errorf("failed to fetch docs head: %w", err)
				}
			} else {
				serverHeadStr = string(serverHead)
				if string(localHash) == string(serverHead) {
					status = client.DocsStatusUpToDate
				} else {
					status = client.DocsStatusOutdated
				}
			}

			// Print status
			fmt.Println("kkachi status")
			fmt.Printf("  workspace     : %s\n", config.WorkspaceID)
			fmt.Printf("  docs base     : %s\n", localHash)
			fmt.Printf("  docs head     : %s\n", serverHeadStr)
			fmt.Printf("  status        : %s\n", status)
			if hasPendingFix {
				fmt.Printf("  pending_fix   : yes\n")
			} else {
				fmt.Printf("  pending_fix   : no\n")
			}

			fmt.Println()

			// Additional messages based on status
			if serverError != nil {
				fmt.Println("kkachi: unable to determine sync status (server unreachable)")
			} else if status == client.DocsStatusOutdated {
				fmt.Println("kkachi: docs base and server HEAD are different.")
				fmt.Println("kkachi: a merge may occur during pre-commit.")
			} else if status == client.DocsStatusUpToDate {
				fmt.Println("kkachi: docs are up to date.")
			}

			// Pending fix messages
			if hasPendingFix {
				if hasConflicts {
					fmt.Println()
					fmt.Println("kkachi: pending fix detected with conflict markers in docs.")
					fmt.Println("kkachi: please resolve conflicts and run 'kkachi fix'.")
					fmt.Printf("kkachi: files with conflicts: %v\n", conflictFiles)
				} else {
					fmt.Println()
					fmt.Println("kkachi: pending fix detected but no conflict markers found.")
					fmt.Println("kkachi: if you have resolved all conflicts, run 'kkachi fix' to finalize.")
					fmt.Printf("kkachi: pending fix was created at: %s\n", pendingFixState.CreatedAt.Format(time.RFC3339))
				}
			}

			// Return error if server was unreachable (roadmap: kkachi status should exit 1 on server error)
			return serverError
		},
	}
}
