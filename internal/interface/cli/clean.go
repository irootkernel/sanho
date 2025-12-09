package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/SeventeenthEarth/kkachi/internal/infra/fs"
	"github.com/SeventeenthEarth/kkachi/internal/infra/git"
	"github.com/SeventeenthEarth/kkachi/internal/infra/httpclient"
)

// newCleanCmd creates the clean command.
func newCleanCmd() *cobra.Command {
	var (
		yes        bool
		offline    bool
		dryRun     bool
		removeDocs bool
	)

	cmd := &cobra.Command{
		Use:   "clean",
		Short: "Remove kkachi configuration and unlink workspace",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := getWorkingDirectory()
			if err != nil {
				return err
			}

			loader := fs.NewFileConfigLoader()
			config, err := loader.Load(cwd)
			if err != nil {
				return fmt.Errorf("failed to load .kkachi.json: %w", err)
			}
			config.ApplyDefaults()

			configPath := filepath.Join(cwd, fs.ConfigFileName)
			docsHashPath := filepath.Join(cwd, config.DocsHashFile)
			pendingFixPath := filepath.Join(cwd, config.PendingFixFile)
			docsPath := filepath.Join(cwd, config.DocsDir)

			cmd.Printf("kkachi clean target:\n")
			cmd.Printf("  server    : %s\n", config.ServerURL)
			cmd.Printf("  project   : %s\n", config.Project)
			cmd.Printf("  workspace : %s\n", config.WorkspaceID)
			cmd.Printf("  remove files: %s, %s, %s\n", configPath, docsHashPath, pendingFixPath)
			if removeDocs {
				cmd.Printf("  remove docs dir: %s\n", docsPath)
			}
			cmd.Printf("  hooks to clean: pre-commit, post-checkout, post-merge, post-rewrite, pre-push, commit-msg\n")
			if dryRun {
				cmd.Println("  dry-run: no changes will be made")
			}

			if !yes && !dryRun {
				confirmed, err := promptForConfirmation("Proceed? (y/N): ")
				if err != nil {
					return err
				}
				if !confirmed {
					return nil
				}
			}

			if !offline && !dryRun {
				ctx, cancel := createContext(DefaultTimeout)
				defer cancel()

				client := httpclient.NewHTTPClient(config.ServerURL)
				if err := client.DeleteWorkspace(ctx, config.WorkspaceID); err != nil {
					if errors.Is(err, httpclient.ErrUnknownWorkspace) {
						cmd.Println("kkachi: workspace already removed on server (unknown_workspace). Continuing...")
					} else {
						return fmt.Errorf("failed to delete workspace on server: %w", err)
					}
				} else {
					cmd.Println("kkachi: workspace removed from server.")
				}
			} else if offline && !dryRun {
				cmd.Println("kkachi: offline mode - skipping server workspace deletion.")
			}

			removePath := func(p string, allowMissing bool) error {
				if dryRun {
					cmd.Printf("dry-run: would remove %s\n", p)
					return nil
				}
				if err := os.Remove(p); err != nil {
					if os.IsNotExist(err) && allowMissing {
						cmd.Printf("kkachi: %s not found, skipping.\n", p)
						return nil
					}
					return fmt.Errorf("failed to remove %s: %w", p, err)
				}
				return nil
			}

			if err := removePath(configPath, true); err != nil {
				return err
			}
			if err := removePath(docsHashPath, true); err != nil {
				return err
			}
			if err := removePath(pendingFixPath, true); err != nil {
				return err
			}

			if removeDocs {
				if dryRun {
					cmd.Printf("dry-run: would remove directory %s\n", docsPath)
				} else if err := os.RemoveAll(docsPath); err != nil && !os.IsNotExist(err) {
					return fmt.Errorf("failed to remove docs directory: %w", err)
				}
			}

			// Clean kkachi hook lines
			if !dryRun {
				cleaner := git.NewHookInstaller()
				hookLines := map[string]string{
					"pre-commit":    "kkachi hook pre-commit",
					"post-checkout": "kkachi hook post-checkout",
					"post-merge":    "kkachi hook post-merge",
					"post-rewrite":  "kkachi hook post-rewrite \"$@\"",
					"pre-push":      "kkachi hook pre-push",
					"commit-msg":    "kkachi hook commit-msg \"$1\"",
				}
				for hookName, line := range hookLines {
					if err := cleaner.RemoveHookLine(cmd.Context(), cwd, hookName, line); err != nil {
						return fmt.Errorf("failed to clean hook %s: %w", hookName, err)
					}
				}
			} else {
				cmd.Println("dry-run: skipping hook cleanup")
			}

			cmd.Println("kkachi: clean completed.")
			return nil
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
	cmd.Flags().BoolVar(&offline, "offline", false, "Skip server workspace deletion")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show actions without making changes")
	cmd.Flags().BoolVar(&removeDocs, "remove-docs", false, "Remove docs directory as well")

	return cmd
}
