package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/irootkernel/sanho/internal/infra/fs"
	"github.com/irootkernel/sanho/internal/infra/git"
	"github.com/irootkernel/sanho/internal/infra/httpclient"
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
		Short: "Remove sanho configuration and unlink workspace",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := getWorkingDirectory()
			if err != nil {
				return err
			}

			loader := fs.NewFileConfigLoader()
			config, err := loader.Load(cwd)
			if err != nil {
				return fmt.Errorf("failed to load .sanho.json: %w", err)
			}
			config.ApplyDefaults()
			hasPullCommit, err := newPullCommitEngine(nil).hasTransaction(cmd.Context(), cwd)
			if err != nil {
				return fmt.Errorf("failed to check pull-commit state: %w", err)
			}
			if hasPullCommit {
				return errors.New("cannot clean an incomplete pull-commit transaction; use 'sanho pull-commit --continue' or '--abort' first")
			}
			hasPulledDocs, err := hasPulledDocsBaseline(cmd.Context(), cwd)
			if err != nil {
				return fmt.Errorf("failed to check pulled docs baseline: %w", err)
			}
			if hasPulledDocs {
				return errors.New("cannot clean while pulled docs await a base commit; run 'sanho pull-commit' first")
			}
			hasPendingReport, err := hasPendingWorkspaceReport(cmd.Context(), cwd)
			if err != nil {
				return fmt.Errorf("failed to check pending workspace report: %w", err)
			}
			if hasPendingReport {
				return errors.New("cannot clean while a workspace docs-hash report is pending; restore daemon access and retry a guarded command first")
			}
			mainPublication, err := assessMainPublication(cmd.Context(), cwd, true)
			if err != nil {
				return fmt.Errorf("failed to check main publication state: %w", err)
			}
			if mainPublication.Exists {
				return fmt.Errorf(
					"cannot clean while origin/main publication is %s: %s; run 'git push origin main' or another origin branch push first",
					mainPublication.Classification,
					mainPublication.Reason,
				)
			}

			configPath := filepath.Join(cwd, fs.ConfigFileName)
			docsHashPath := filepath.Join(cwd, config.DocsHashFile)
			pendingFixPath := filepath.Join(cwd, config.PendingFixFile)
			docsPath := filepath.Join(cwd, config.DocsDir)

			cmd.Printf("sanho clean target:\n")
			cmd.Printf("  socket    : %s\n", config.SocketPath)
			cmd.Printf("  project   : %s\n", config.Project)
			cmd.Printf("  workspace : %s\n", config.WorkspaceID)
			cmd.Printf("  remove files: %s, %s, %s\n", configPath, docsHashPath, pendingFixPath)
			if removeDocs {
				cmd.Printf("  remove docs dir: %s\n", docsPath)
			}
			cmd.Printf("  hooks to clean: pre-commit, post-checkout, post-merge, post-rewrite, pre-push, commit-msg, post-commit\n")
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

				client, err := newDaemonClient(config.SocketPath)
				if err != nil {
					return err
				}
				if err := client.DeleteWorkspace(ctx, config.WorkspaceID); err != nil {
					if errors.Is(err, httpclient.ErrUnknownWorkspace) {
						cmd.Println("sanho: workspace already removed on daemon (unknown_workspace). Continuing...")
					} else {
						return fmt.Errorf("failed to delete workspace on daemon: %w", err)
					}
				} else {
					cmd.Println("sanho: workspace removed from daemon.")
				}
			} else if offline && !dryRun {
				cmd.Println("sanho: offline mode - skipping daemon workspace deletion.")
			}

			removePath := func(p string, allowMissing bool) error {
				if dryRun {
					cmd.Printf("dry-run: would remove %s\n", p)
					return nil
				}
				if err := os.Remove(p); err != nil {
					if os.IsNotExist(err) && allowMissing {
						cmd.Printf("sanho: %s not found, skipping.\n", p)
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

			// Clean sanho hook lines
			if !dryRun {
				cleaner := git.NewHookInstaller()
				hookLines := map[string]string{
					"pre-commit":    "sanho hook pre-commit",
					"post-checkout": "sanho hook post-checkout",
					"post-merge":    "sanho hook post-merge",
					"post-rewrite":  "sanho hook post-rewrite \"$@\"",
					"pre-push":      "sanho hook pre-push \"$@\"",
					"commit-msg":    "sanho hook commit-msg \"$1\"",
					"post-commit":   "sanho hook post-commit",
				}
				for hookName, line := range hookLines {
					if err := cleaner.RemoveHookLine(cmd.Context(), cwd, hookName, line); err != nil {
						return fmt.Errorf("failed to clean hook %s: %w", hookName, err)
					}
					if err := cleaner.RemoveHookLine(cmd.Context(), cwd, "pre-push", "sanho hook pre-push"); err != nil {
						return fmt.Errorf("failed to clean legacy pre-push hook: %w", err)
					}
				}
			} else {
				cmd.Println("dry-run: skipping hook cleanup")
			}

			cmd.Println("sanho: clean completed.")
			return nil
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
	cmd.Flags().BoolVar(&offline, "offline", false, "Skip daemon workspace deletion")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show actions without making changes")
	cmd.Flags().BoolVar(&removeDocs, "remove-docs", false, "Remove docs directory as well")

	return cmd
}
