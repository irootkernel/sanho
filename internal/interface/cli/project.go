package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/irootkernel/sanho/internal/domain/client"
	"github.com/irootkernel/sanho/internal/domain/docs"
	"github.com/irootkernel/sanho/internal/infra/httpclient"
)

// newProjectCmd creates the project parent command.
func newProjectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Manage projects on the sanhod",
		Long:  `Commands for managing projects registered with the sanhod.`,
	}

	cmd.AddCommand(newProjectAddCmd())
	cmd.AddCommand(newProjectDeleteCmd())

	return cmd
}

// newProjectAddCmd creates the project add command.
func newProjectAddCmd() *cobra.Command {
	var (
		serverURL   string
		projectName string
		docsRepoURL string
	)

	cmd := &cobra.Command{
		Use:   "add",
		Short: "Register a project with the sanhod",
		Long: `Register a new project and its associated docs repository with the sanhod.

This command requires:
- Project name
- Docs repository URL

The docs_repo_id is automatically extracted from the docs repository URL.
For example: git@github.com:org/my_docs.git -> docs_repo_id = "my_docs"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := createContext(DefaultTimeout)
			defer cancel()

			// Validate required flags
			if err := validateRequiredFlag("server-url", serverURL); err != nil {
				return err
			}
			if err := validateRequiredFlag("project", projectName); err != nil {
				return err
			}
			if err := validateRequiredFlag("docs-repo-url", docsRepoURL); err != nil {
				return err
			}

			// Extract docs_repo_id from URL
			docsRepoID := client.ExtractDocsRepoID(docsRepoURL)
			if docsRepoID == "" {
				return errors.New("failed to extract docs_repo_id from docs-repo-url")
			}

			// Get actor email from git config or prompt
			cwd, _ := getWorkingDirectory()
			if cwd == "" {
				cwd = "."
			}
			actorEmail, err := promptForEmail(ctx, cwd)
			if err != nil {
				return err
			}

			// Create HTTP client and call API
			httpClient := httpclient.NewHTTPClient(serverURL)
			req := httpclient.CreateProjectRequest{
				Project:     docs.ProjectName(projectName),
				DocsRepoID:  docsRepoID,
				DocsRepoURL: docsRepoURL,
				ActorEmail:  actorEmail,
			}

			if err := httpClient.CreateOrUpdateProject(ctx, req); err != nil {
				if errors.Is(err, httpclient.ErrUnknownProject) {
					return fmt.Errorf("failed to create project: server returned unknown_project error")
				}
				return fmt.Errorf("failed to create/update project: %w", err)
			}

			fmt.Printf("sanho: project '%s' registered successfully.\n", projectName)
			fmt.Printf("  docs_repo_id  : %s\n", docsRepoID)
			fmt.Printf("  docs_repo_url : %s\n", docsRepoURL)

			return nil
		},
	}

	cmd.Flags().StringVar(&serverURL, "server-url", "", "sanhod URL (required)")
	cmd.Flags().StringVar(&projectName, "project", "", "Project name (required)")
	cmd.Flags().StringVar(&docsRepoURL, "docs-repo-url", "", "Docs repository Git URL (required)")

	return cmd
}

// newProjectDeleteCmd creates the project delete command.
func newProjectDeleteCmd() *cobra.Command {
	var (
		serverURL   string
		projectName string
		force       bool
		yes         bool
	)

	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Remove a project from the sanhod",
		Long: `Remove a project from the sanhod.

Note: This does not delete any local directories or files.
Workspaces associated with this project will no longer function.

Use --force to delete a project even if it has registered workspaces.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := createContext(DefaultTimeout)
			defer cancel()

			// Validate required flags
			if err := validateRequiredFlag("server-url", serverURL); err != nil {
				return err
			}
			if err := validateRequiredFlag("project", projectName); err != nil {
				return err
			}

			// Confirmation prompt unless --yes is provided
			if !yes {
				fmt.Printf("You are about to delete project '%s' from the server.\n", projectName)
				fmt.Println("Registered workspaces will no longer function.")
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

			if err := httpClient.DeleteProject(ctx, docs.ProjectName(projectName), force); err != nil {
				if errors.Is(err, httpclient.ErrUnknownProject) {
					return fmt.Errorf("project '%s' does not exist on the server", projectName)
				}
				if errors.Is(err, httpclient.ErrProjectHasWorkspaces) {
					return fmt.Errorf("project '%s' has registered workspaces. Use --force to delete anyway, or unregister workspaces first", projectName)
				}
				return fmt.Errorf("failed to delete project: %w", err)
			}

			fmt.Printf("sanho: project '%s' deleted successfully.\n", projectName)
			fmt.Println("Workspaces connected to this project will no longer communicate with sanho.")

			return nil
		},
	}

	cmd.Flags().StringVar(&serverURL, "server-url", "", "sanhod URL (required)")
	cmd.Flags().StringVar(&projectName, "project", "", "Project name to delete (required)")
	cmd.Flags().BoolVar(&force, "force", false, "Force delete even if workspaces exist")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")

	return cmd
}
