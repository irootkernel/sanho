package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/SeventeenthEarth/kkachi/internal/domain/client"
	"github.com/SeventeenthEarth/kkachi/internal/domain/docs"
	"github.com/SeventeenthEarth/kkachi/internal/infra/fs"
	"github.com/SeventeenthEarth/kkachi/internal/infra/git"
	"github.com/SeventeenthEarth/kkachi/internal/infra/httpclient"
)

// newInitCmd creates the init command.
func newInitCmd() *cobra.Command {
	var (
		serverURL   string
		projectName string
		docsDir     string
		docsRepoURL string
		force       bool
	)

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a workspace for kkachi",
		Long: `Initialize the current directory as a kkachi workspace.

This command will:
- Verify this is a Git repository
- Register the project if not already registered
- Register this workspace with the kkachi-server
- Download the current docs snapshot from the server
- Create .kkachi.json configuration file
- Create .kkachi_docs_hash file
- Install Git hooks for document synchronization

Prerequisites:
- Current directory must be a Git repository
- .kkachi.json must not exist (unless --force is used)
- docs directory must not exist (snapshot will be downloaded)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Get current working directory
			cwd, err := getWorkingDirectory()
			if err != nil {
				return err
			}

			// Collect required values interactively if not provided via flags.
			// This keeps kkachi init primarily conversational, as designed.
			if serverURL == "" {
				input, err := promptForInput("Enter kkachi-server URL: ")
				if err != nil {
					return err
				}
				if input == "" {
					return errors.New("server URL is required")
				}
				serverURL = input
			}
			if projectName == "" {
				input, err := promptForInput("Enter project name: ")
				if err != nil {
					return err
				}
				if input == "" {
					return errors.New("project name is required")
				}
				projectName = input
			}

			// Check if this is a Git repository first (needed for prompts below)
			detector := git.NewDetector()
			if !detector.HasGitDir(cwd) {
				return errors.New("current directory is not a Git repository. Run 'git init' first")
			}

			// Get current repo's origin URL for context (short-lived timeout)
			gitCtx, gitCancel := createContext(DefaultTimeout)
			currentRepoURL, _ := detector.GetRemoteOriginURL(gitCtx, cwd)
			gitCancel()

			// Collect docs repository URL interactively if not provided via flag.
			if docsRepoURL == "" {
				// Show current repo URL as reference
				prompt := "Enter docs repository Git SSH URL"
				if currentRepoURL != "" {
					prompt += fmt.Sprintf(" (current: %s)", currentRepoURL)
				}
				prompt += ": "
				input, err := promptForInput(prompt)
				if err != nil {
					return err
				}
				if input == "" {
					return errors.New("docs repository URL is required")
				}
				docsRepoURL = input
			}

			// Set default docs dir
			if docsDir == "" {
				docsDir = client.DefaultDocsDir
			}

			// Check if .kkachi.json already exists
			configWriter := fs.NewFileConfigWriter()
			if configWriter.Exists(cwd) && !force {
				return errors.New("this directory is already a kkachi workspace (.kkachi.json exists). Use --force to reinitialize")
			}

			// Resolve docs path safely within the workspace to avoid deleting
			// arbitrary paths when using --docs-dir with --force.
			docsPath, err := resolveDocsPath(cwd, docsDir)
			if err != nil {
				return err
			}

			// Check if docs directory already exists
			if _, err := os.Stat(docsPath); err == nil && !force {
				return fmt.Errorf("docs directory '%s' already exists. Please backup/remove it before init, or use --force", docsDir)
			}

			// Get actor email from git config or prompt (with its own timeout)
			emailCtx, emailCancel := createContext(DefaultTimeout)
			actorEmail, err := promptForEmail(emailCtx, cwd)
			emailCancel()
			if err != nil {
				return err
			}

			// Create context for server interactions and Git hook installation
			ctx, cancel := createContext(LongTimeout)
			defer cancel()

			// repoURL is the same as currentRepoURL (already fetched above)
			repoURL := currentRepoURL

			// Extract docs_repo_id from URL
			docsRepoID := client.ExtractDocsRepoID(docsRepoURL)
			if docsRepoID == "" {
				return errors.New("failed to extract docs_repo_id from docs-repo-url")
			}

			// Create HTTP client
			httpClient := httpclient.NewHTTPClient(serverURL)

			// Step 1: Register project (idempotent)
			fmt.Printf("Registering project '%s'...\n", projectName)
			projectReq := httpclient.CreateProjectRequest{
				Project:     docs.ProjectName(projectName),
				DocsRepoID:  docsRepoID,
				DocsRepoURL: docsRepoURL,
				ActorEmail:  actorEmail,
			}
			if err := httpClient.CreateOrUpdateProject(ctx, projectReq); err != nil {
				return fmt.Errorf("failed to register project: %w", err)
			}

			// Step 2: Register workspace
			fmt.Println("Registering workspace...")
			workspaceReq := httpclient.RegisterWorkspaceRequest{
				Project:    docs.ProjectName(projectName),
				LocalPath:  cwd,
				RepoURL:    repoURL,
				ActorEmail: actorEmail,
			}
			workspaceResp, err := httpClient.RegisterWorkspace(ctx, workspaceReq)
			if err != nil {
				if errors.Is(err, httpclient.ErrUnknownProject) {
					return fmt.Errorf("project '%s' is not registered on server. Run 'kkachi project add' first", projectName)
				}
				return fmt.Errorf("failed to register workspace: %w", err)
			}

			// Step 3: Download docs snapshot
			fmt.Println("Downloading docs snapshot...")
			snapshot, commitHash, err := httpClient.DocsSnapshot(ctx, docs.ProjectName(projectName), "")
			if err != nil {
				return fmt.Errorf("failed to download docs snapshot: %w", err)
			}

			// Step 4: Apply snapshot, ensuring docs directory reflects the server state
			// When --force is used, always clear any existing docs to avoid stale content,
			// regardless of whether the snapshot is empty or not.
			if force {
				if err := os.RemoveAll(docsPath); err != nil {
					return fmt.Errorf("failed to remove docs directory '%s': %w", docsDir, err)
				}
			}

			if len(snapshot) > 0 {
				applier := fs.NewSnapshotApplier()
				if err := applier.Apply(snapshot, cwd, docsDir); err != nil {
					return fmt.Errorf("failed to apply docs snapshot: %w", err)
				}
			} else {
				// Create empty docs directory
				if err := os.MkdirAll(docsPath, 0755); err != nil {
					return fmt.Errorf("failed to create docs directory: %w", err)
				}
			}

			// Step 5: Write .kkachi.json
			config := &client.WorkspaceConfig{
				ServerURL:      serverURL,
				WorkspaceID:    workspaceResp.WorkspaceID,
				Project:        docs.ProjectName(projectName),
				ActorEmail:     actorEmail,
				DocsDir:        docsDir,
				DocsHashFile:   client.DefaultDocsHashFile,
				PendingFixFile: client.DefaultPendingFixFile,
			}
			if err := configWriter.Write(cwd, config); err != nil {
				return fmt.Errorf("failed to write .kkachi.json: %w", err)
			}

			// Step 6: Write docs hash file
			// Validate that commitHash is not empty to avoid creating invalid state
			if commitHash == "" {
				return errors.New("server returned empty docs HEAD hash; init cannot proceed")
			}
			hashStore := fs.NewFileDocsHashStore()
			hashPath := filepath.Join(cwd, client.DefaultDocsHashFile)
			if err := hashStore.Write(hashPath, docs.CommitHash(commitHash)); err != nil {
				return fmt.Errorf("failed to write docs hash file: %w", err)
			}

			// Step 7: Remove pending fix file if exists
			pendingFixPath := filepath.Join(cwd, client.DefaultPendingFixFile)
			_ = os.Remove(pendingFixPath)

			// Step 8: Install Git hooks
			fmt.Println("Installing Git hooks...")
			hookInstaller := git.NewHookInstaller()
			if err := hookInstaller.InstallAllHooks(ctx, cwd); err != nil {
				return fmt.Errorf("failed to install Git hooks: %w", err)
			}

			// Success message
			fmt.Println()
			fmt.Println("kkachi: workspace initialized.")
			fmt.Printf("  workspace_id : %s\n", workspaceResp.WorkspaceID)
			fmt.Printf("  docs_head    : %s\n", commitHash)

			return nil
		},
	}

	cmd.Flags().StringVar(&serverURL, "server-url", "", "kkachi-server URL (required)")
	cmd.Flags().StringVar(&projectName, "project", "", "Project name (required)")
	cmd.Flags().StringVar(&docsDir, "docs-dir", "", "Local docs directory (default: docs)")
	cmd.Flags().StringVar(&docsRepoURL, "docs-repo-url", "", "Docs repository Git URL (required)")
	cmd.Flags().BoolVar(&force, "force", false, "Force reinitialize even if already initialized")

	return cmd
}
