package cli

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/irootkernel/sanho/internal/domain/docs"
	"github.com/irootkernel/sanho/internal/domain/workspace"
	"github.com/irootkernel/sanho/internal/infra/git"
	"github.com/irootkernel/sanho/internal/infra/httpclient"
)

// newWorkspaceCmd creates the workspace parent command.
func newWorkspaceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workspace",
		Short: "Manage workspaces on the sanhod",
		Long:  `Commands for managing workspaces registered with the sanhod.`,
	}

	cmd.AddCommand(newWorkspaceRegisterCmd())
	cmd.AddCommand(newWorkspaceUnregisterCmd())

	return cmd
}

// newWorkspaceRegisterCmd creates the workspace register command.
func newWorkspaceRegisterCmd() *cobra.Command {
	var (
		serverURL   string
		projectName string
		yes         bool
	)

	cmd := &cobra.Command{
		Use:   "register [path]",
		Short: "Register a workspace with the sanhod",
		Long: `Register a workspace directory with the sanhod.

This is an alternative to 'sanho init' for registering workspaces
without creating local configuration files.

If path is not specified, the current directory is used.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Determine workspace path
			workspacePath := "."
			if len(args) > 0 {
				workspacePath = args[0]
			}

			// Get absolute path
			absPath, err := filepath.Abs(workspacePath)
			if err != nil {
				return fmt.Errorf("failed to get absolute path: %w", err)
			}

			// Validate required flags
			if err := validateRequiredFlag("server-url", serverURL); err != nil {
				return err
			}
			if err := validateRequiredFlag("project", projectName); err != nil {
				return err
			}

			// Check if it's a git repository
			detector := git.NewDetector()
			if !detector.HasGitDir(absPath) {
				return fmt.Errorf("'%s' is not a Git repository", absPath)
			}

			// Get actor email from git config or prompt
			emailCtx, emailCancel := createContext(DefaultTimeout)
			actorEmail, err := promptForEmail(emailCtx, absPath)
			emailCancel()
			if err != nil {
				return err
			}

			// Get repo URL from git config
			gitCtx, gitCancel := createContext(DefaultTimeout)
			repoURL, _ := detector.GetRemoteOriginURL(gitCtx, absPath)
			gitCancel()

			// Confirmation prompt unless --yes is provided
			if !yes {
				fmt.Printf("Registering workspace:\n")
				fmt.Printf("  path    : %s\n", absPath)
				fmt.Printf("  project : %s\n", projectName)
				fmt.Printf("  repo_url: %s\n", repoURL)
				confirmed, err := promptForConfirmation("Proceed? (y/N): ")
				if err != nil {
					return err
				}
				if !confirmed {
					return nil
				}
			}

			// Create HTTP client and call API
			httpClient := httpclient.NewHTTPClient(serverURL)

			// Create context for the registration request after all interactive input
			ctx, cancel := createContext(DefaultTimeout)
			defer cancel()

			req := httpclient.RegisterWorkspaceRequest{
				Project:    docs.ProjectName(projectName),
				LocalPath:  absPath,
				RepoURL:    repoURL,
				ActorEmail: actorEmail,
			}

			resp, err := httpClient.RegisterWorkspace(ctx, req)
			if err != nil {
				if errors.Is(err, httpclient.ErrUnknownProject) {
					return fmt.Errorf("project '%s' is not registered on the server. Run 'sanho project add' first", projectName)
				}
				return fmt.Errorf("failed to register workspace: %w", err)
			}

			fmt.Println("sanho: workspace registered successfully.")
			fmt.Printf("  workspace_id     : %s\n", resp.WorkspaceID)
			fmt.Printf("  current_docs_head: %s\n", resp.CurrentDocsHead)

			return nil
		},
	}

	cmd.Flags().StringVar(&serverURL, "server-url", "", "sanhod URL (required)")
	cmd.Flags().StringVar(&projectName, "project", "", "Project name (required)")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")

	return cmd
}

// newWorkspaceUnregisterCmd creates the workspace unregister command.
func newWorkspaceUnregisterCmd() *cobra.Command {
	var (
		serverURL   string
		workspaceID string
		yes         bool
	)

	cmd := &cobra.Command{
		Use:   "unregister",
		Short: "Remove a workspace registration from the sanhod",
		Long: `Remove a workspace registration from the sanhod.

Note: This does not delete any local files. The local .kkachi.json
and other configuration files will remain.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := createContext(DefaultTimeout)
			defer cancel()

			// Validate required flags
			if err := validateRequiredFlag("server-url", serverURL); err != nil {
				return err
			}
			if err := validateRequiredFlag("workspace-id", workspaceID); err != nil {
				return err
			}

			// Confirmation prompt unless --yes is provided
			if !yes {
				fmt.Printf("Unregistering workspace '%s' from the server.\n", workspaceID)
				fmt.Println("Local directories will not be deleted.")
				confirmed, err := promptForConfirmation("Proceed? (y/N): ")
				if err != nil {
					return err
				}
				if !confirmed {
					return nil
				}
			}

			// Create HTTP client and call API
			httpClient := httpclient.NewHTTPClient(serverURL)

			if err := httpClient.DeleteWorkspace(ctx, workspace.WorkspaceID(workspaceID)); err != nil {
				if errors.Is(err, httpclient.ErrUnknownWorkspace) {
					return fmt.Errorf("workspace '%s' is not registered on the server", workspaceID)
				}
				return fmt.Errorf("failed to unregister workspace: %w", err)
			}

			fmt.Printf("sanho: workspace '%s' unregistered successfully.\n", workspaceID)

			return nil
		},
	}

	cmd.Flags().StringVar(&serverURL, "server-url", "", "sanhod URL (required)")
	cmd.Flags().StringVar(&workspaceID, "workspace-id", "", "Workspace ID to unregister (required)")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")

	return cmd
}
