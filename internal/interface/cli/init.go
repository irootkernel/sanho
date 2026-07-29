package cli

import (
	"context"
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

	type initMode int
	const (
		modeFresh initMode = iota // download snapshot
		modeReuse                 // reuse existing docs
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
- Add workspace metadata files to .gitignore
- Install Git hooks for document synchronization

Prerequisites:
- Current directory must be a Git repository
- .kkachi.json must not exist (unless --force is used)
- docs directory must not exist unless this repo already has kkachi-managed docs (docs-version commits)`,
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

			// Prepare git client for repo inspection
			gitClient := git.NewClient()

			// Decide init mode (fresh download vs reuse existing docs)
			initMode := modeFresh
			var reuseBaseHash string

			if !force {
				if _, err := os.Stat(docsPath); err == nil {
					hasDocsVersion, err := gitClient.HasDocsVersionCommits(context.Background(), cwd)
					if err != nil {
						return fmt.Errorf("failed to inspect git history for docs-version commits: %w", err)
					}
					if !hasDocsVersion {
						return errors.New("existing docs directory detected, but this repo has no docs-version commits.\nkkachi does not overwrite legacy/manual docs automatically.\nPlease backup/remove the docs directory first or use --force to replace it.")
					}

					clean, err := gitClient.IsPathClean(context.Background(), cwd, docsDir)
					if err != nil {
						return fmt.Errorf("failed to check docs directory cleanliness: %w", err)
					}
					if !clean {
						return errors.New("현재 docs 디렉토리에 commit 되지 않은 변경이 있습니다.\nkkachi 를 연결하기 전에 먼저 변경을 커밋하거나, 백업 후 되돌린 뒤 다시 `kkachi-cli init` 을 실행해 주세요.")
					}

					hash, err := gitClient.GetLastDocsVersionHash(context.Background(), cwd)
					if err != nil {
						if errors.Is(err, git.ErrNoDocsVersionCommits) {
							return errors.New("repo 에 docs-version 커밋이 있지만 기준 hash 를 찾지 못했습니다. kkachi commit-msg hook 이 올바르게 동작했는지 확인해 주세요.")
						}
						return fmt.Errorf("failed to read last docs-version hash: %w", err)
					}
					initMode = modeReuse
					reuseBaseHash = hash
				}
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
					return fmt.Errorf("project '%s' is not registered on server. Run 'kkachi-cli project add' first", projectName)
				}
				return fmt.Errorf("failed to register workspace: %w", err)
			}

			var docsBaseHash docs.CommitHash

			if initMode == modeFresh {
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

				if commitHash == "" {
					return errors.New("server returned empty docs HEAD hash; init cannot proceed")
				}
				docsBaseHash = docs.CommitHash(commitHash)
			} else {
				// Reuse existing docs directory without touching its contents.
				docsBaseHash = docs.CommitHash(reuseBaseHash)
				if docsBaseHash.IsZero() {
					return errors.New("failed to determine docs base hash from git log")
				}
			}

			// Step 5: Write .kkachi.json
			config := &client.WorkspaceConfig{
				ServerURL:             serverURL,
				WorkspaceID:           workspaceResp.WorkspaceID,
				Project:               docs.ProjectName(projectName),
				ActorEmail:            actorEmail,
				DocsDir:               docsDir,
				DocsHashFile:          client.DefaultDocsHashFile,
				PendingFixFile:        client.DefaultPendingFixFile,
				DocsSyncCommitMessage: client.DefaultDocsSyncCommitMessage,
			}
			if err := configWriter.Write(cwd, config); err != nil {
				return fmt.Errorf("failed to write .kkachi.json: %w", err)
			}

			// Step 6: Write docs hash file
			hashStore := fs.NewFileDocsHashStore()
			hashPath := filepath.Join(cwd, client.DefaultDocsHashFile)
			if err := hashStore.Write(hashPath, docsBaseHash); err != nil {
				return fmt.Errorf("failed to write docs hash file: %w", err)
			}

			// Step 7: Remove pending fix file if exists
			pendingFixPath := filepath.Join(cwd, client.DefaultPendingFixFile)
			_ = os.Remove(pendingFixPath)

			// Step 8: Ensure .gitignore excludes kkachi workspace metadata
			gitignoreManager := fs.NewGitignoreManager()
			if err := gitignoreManager.EnsureEntries(
				cwd,
				"# Kkachi",
				[]string{client.DefaultDocsHashFile, fs.ConfigFileName, fs.WorkspaceReportFallbackFile},
			); err != nil {
				return fmt.Errorf("failed to update .gitignore: %w", err)
			}

			// Step 9: Install Git hooks
			fmt.Println("Installing Git hooks...")
			hookInstaller := git.NewHookInstaller()
			if err := hookInstaller.InstallAllHooks(ctx, cwd); err != nil {
				return fmt.Errorf("failed to install Git hooks: %w", err)
			}

			// Success message
			fmt.Println()
			if initMode == modeReuse {
				status := "up_to_date"
				if string(docsBaseHash) != string(workspaceResp.CurrentDocsHead) {
					status = "outdated"
				}
				fmt.Println("kkachi: 기존 docs 디렉토리를 그대로 사용하여 workspace 를 초기화했습니다.")
				fmt.Printf("  workspace_id : %s\n", workspaceResp.WorkspaceID)
				fmt.Printf("  docs_base    : %s\n", docsBaseHash)
				fmt.Printf("  server_head  : %s\n", workspaceResp.CurrentDocsHead)
				fmt.Printf("  status       : %s\n", status)
			} else {
				fmt.Println("kkachi: workspace initialized.")
				fmt.Printf("  workspace_id : %s\n", workspaceResp.WorkspaceID)
				fmt.Printf("  docs_head    : %s\n", docsBaseHash)
			}

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
