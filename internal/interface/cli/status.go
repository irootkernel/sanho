package cli

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/irootkernel/sanho/internal/domain/client"
	"github.com/irootkernel/sanho/internal/domain/docs"
	"github.com/irootkernel/sanho/internal/domain/merge"
	"github.com/irootkernel/sanho/internal/infra/fs"
	infraGit "github.com/irootkernel/sanho/internal/infra/git"
	"github.com/irootkernel/sanho/internal/infra/httpclient"
)

// newStatusCmd creates the status command.
func newStatusCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show the current docs synchronization status",
		Long: `Display the current workspace docs status including:
- Workspace ID
- Local docs base hash
- Daemon docs HEAD hash
- Exact docs commit relation to HEAD
- Same-project workspace comparisons
- Synchronization status (up_to_date, outdated, unknown)
- Pending fix status`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			// Get current working directory
			cwd, err := os.Getwd()
			if err != nil {
				return withErrorCode("internal_error", fmt.Errorf("failed to get current directory: %w", err))
			}

			// Step 1: Load .sanho.json
			configLoader := fs.NewFileConfigLoader()
			config, err := configLoader.Load(cwd)
			if err != nil {
				if errors.Is(err, fs.ErrConfigNotFound) {
					return withErrorCode(
						"not_in_workspace",
						errors.New("this directory is not a sanho workspace. Run 'sanho init' first"),
					)
				}
				return withErrorCode("invalid_workspace_config", fmt.Errorf("failed to load config: %w", err))
			}
			pullCommit, err := newPullCommitEngine(nil).assessTransaction(ctx, cwd, config.DocsDir)
			if err != nil {
				return withErrorCode("pull_commit_state_failed", fmt.Errorf("failed to inspect pull-commit state: %w", err))
			}
			gitOperation := infraGit.GitOperation{
				Type:           infraGit.OperationNone,
				Classification: infraGit.OperationClear,
				NextCommands:   make([]string, 0),
			}
			gitDetector := infraGit.NewDetector()
			if gitDetector.HasGitDir(cwd) {
				gitOperation, err = gitDetector.DetectOperation(ctx, cwd)
				if err != nil {
					return withErrorCode(
						"git_operation_detection_failed",
						fmt.Errorf("failed to inspect Git operation state: %w", err),
					)
				}
			}
			mainPublication, err := assessMainPublication(ctx, cwd, mainPublicationAssessmentOptions{
				RefreshOrigin: true,
				ReadOnly:      true,
			})
			if err != nil {
				return withErrorCode(
					"main_publication_state_failed",
					fmt.Errorf("failed to inspect main publication state: %w", err),
				)
			}

			// Step 2: Load .sanho_docs_hash
			hashStore := fs.NewFileDocsHashStore()
			hashPath := filepath.Join(cwd, config.DocsHashFile)
			localHash, err := hashStore.Read(hashPath)
			if err != nil {
				if errors.Is(err, fs.ErrHashFileNotFound) {
					return withErrorCode(
						"docs_hash_not_found",
						errors.New("docs hash file not found. Workspace may be corrupted. Try 'sanho init --force'"),
					)
				}
				return withErrorCode("docs_hash_read_failed", fmt.Errorf("failed to read docs hash file: %w", err))
			}

			// Step 3: Check pending fix state
			pendingFixStore := fs.NewFilePendingFixStore()
			pendingFixPath := filepath.Join(cwd, config.PendingFixFile)
			pendingFixState, hasPendingFix, err := pendingFixStore.Read(pendingFixPath)
			if err != nil {
				return withErrorCode("pending_fix_read_failed", fmt.Errorf("failed to read pending fix state: %w", err))
			}

			// Step 4: Check for conflict markers in docs
			var hasConflicts bool
			conflictFiles := make([]string, 0)
			conflictScanStatus := "complete"
			docsPath := filepath.Join(cwd, config.DocsDir)
			if _, err := os.Stat(docsPath); err == nil {
				conflictDetector := merge.NewFileConflictDetector()
				conflictFiles, err = conflictDetector.DetectConflicts(docsPath)
				if err != nil {
					conflictScanStatus = "unavailable"
					conflictFiles = make([]string, 0)
					// Log warning but continue
					if !jsonOutput {
						cmd.PrintErrf("Warning: failed to scan for conflicts: %v\n", err)
					}
				}
				hasConflicts = len(conflictFiles) > 0
			}

			// Step 5: Get docs HEAD and compare project workspaces.
			httpClient, err := newDaemonClient(config.SocketPath)
			if err != nil {
				return err
			}
			projectStatus, err := httpClient.GetProjectStatus(
				ctx,
				config.Project,
				config.WorkspaceID,
				localHash,
			)
			comparisonAvailable := true
			legacyDifferent := false
			if errors.Is(err, httpclient.ErrEndpointNotFound) {
				comparisonAvailable = false
				daemonHead, headErr := httpClient.DocsHead(ctx, config.Project)
				err = headErr
				if headErr == nil {
					projectStatus = httpclient.ProjectStatusResponse{
						Project:              string(config.Project),
						ReferenceWorkspaceID: string(config.WorkspaceID),
						ReferenceDocsHash:    string(localHash),
						DocsHead:             string(daemonHead),
					}
					if localHash == daemonHead {
						projectStatus.ReferenceToHead.Status = docs.CommitRelationSame
					} else {
						projectStatus.ReferenceToHead.Status = docs.CommitRelationUnknown
						legacyDifferent = true
					}
				}
			}
			headReconciliation := assessHeadReconciliation(
				ctx,
				cwd,
				config,
				localHash,
				gitOperation,
				httpClient,
			)
			// Determine status
			var status client.DocsStatus
			var daemonHeadStr string
			relation := httpclient.CommitRelation{Status: docs.CommitRelationUnknown}
			var daemonError error // Track daemon error to return at the end

			if err != nil {
				status = client.DocsStatusUnknown
				daemonHeadStr = "(unavailable)"
				switch {
				case errors.Is(err, httpclient.ErrUnknownProject):
					if !jsonOutput {
						cmd.PrintErrf("Warning: project '%s' is not registered on daemon\n", config.Project)
					}
					daemonError = withErrorCode(
						"unknown_project",
						fmt.Errorf("project '%s' is not registered on daemon", config.Project),
					)
				case errors.Is(err, httpclient.ErrUnknownWorkspace):
					if !jsonOutput {
						cmd.PrintErrf("Warning: workspace '%s' is not registered on daemon\n", config.WorkspaceID)
					}
					daemonError = withErrorCode(
						"unknown_workspace",
						fmt.Errorf("workspace '%s' is not registered on daemon", config.WorkspaceID),
					)
				case errors.Is(err, httpclient.ErrWorkspaceProjectMismatch):
					if !jsonOutput {
						cmd.PrintErrf("Warning: workspace '%s' is registered under another project\n", config.WorkspaceID)
					}
					daemonError = withErrorCode(
						"workspace_project_mismatch",
						fmt.Errorf("workspace '%s' belongs to another project", config.WorkspaceID),
					)
				case errors.Is(err, httpclient.ErrUnknownDocsCommit):
					if !jsonOutput {
						cmd.PrintErrf("Warning: local docs commit '%s' is not available on daemon\n", localHash)
					}
					daemonError = withErrorCode(
						"unknown_docs_commit",
						fmt.Errorf("local docs commit '%s' is unknown", localHash),
					)
				default:
					if !jsonOutput {
						cmd.PrintErrf("Warning: failed to connect to daemon: %v\n", err)
					}
					daemonError = withErrorCode(
						"daemon_request_failed",
						fmt.Errorf("failed to fetch project status: %w", err),
					)
				}
			} else {
				daemonHeadStr = projectStatus.DocsHead
				relation = projectStatus.ReferenceToHead
				effectiveRelation := effectiveStatusRelation(relation, headReconciliation)
				if effectiveRelation.Status == docs.CommitRelationSame {
					status = client.DocsStatusUpToDate
				} else if effectiveRelation.Status == docs.CommitRelationUnknown && !legacyDifferent {
					status = client.DocsStatusUnknown
				} else {
					status = client.DocsStatusOutdated
				}
			}

			if jsonOutput {
				if daemonError != nil {
					return daemonError
				}
				output := buildStatusJSONOutput(
					config,
					string(localHash),
					status,
					projectStatus,
					pendingFixState,
					hasPendingFix,
					conflictScanStatus,
					conflictFiles,
					comparisonAvailable,
					pullCommit,
					mainPublication,
					gitOperation,
					headReconciliation,
				)
				if err := writeJSON(cmd.OutOrStdout(), output); err != nil {
					return withErrorCode("internal_error", errors.Join(ErrInternal, err))
				}
				return nil
			}

			// Print status
			cmd.Println("sanho status")
			cmd.Printf("  project       : %s\n", config.Project)
			cmd.Printf("  workspace     : %s\n", config.WorkspaceID)
			cmd.Printf("  docs base     : %s\n", localHash)
			cmd.Printf("  docs head     : %s\n", daemonHeadStr)
			cmd.Printf("  status        : %s\n", status)
			cmd.Printf("  docs relation : %s\n", formatCommitRelation(relation))
			if hasPendingFix {
				cmd.Println("  pending_fix   : yes")
			} else {
				cmd.Println("  pending_fix   : no")
			}
			if pullCommit.Exists {
				cmd.Printf("  pull_commit  : %s (%s)\n", pullCommit.Classification, pullCommit.State.Phase)
				cmd.Printf("  recovery     : %s\n", pullCommit.NextCommand)
			} else {
				cmd.Println("  pull_commit  : none")
			}
			if mainPublication.Exists {
				cmd.Printf("  main_publish : %s\n", mainPublication.Classification)
				cmd.Printf("  publish head : %s\n", shortHash(mainPublication.LocalMain))
				cmd.Printf("  publish note : %s\n", mainPublication.Reason)
			} else {
				cmd.Println("  main_publish : none")
			}
			printStatusGitOperation(cmd, gitOperation)
			cmd.Printf(
				"  head_reconcile: %s (pending: %t)\n",
				headReconciliation.Classification,
				headReconciliation.Pending,
			)
			if daemonError == nil {
				cmd.Println()
				if comparisonAvailable {
					if err := printProjectWorkspaces(cmd, projectStatus); err != nil {
						return withErrorCode("internal_error", errors.Join(ErrInternal, err))
					}
				} else {
					cmd.Println("project workspaces:")
					cmd.Println("  (daemon upgrade required for workspace comparisons)")
				}
			}

			cmd.Println()

			// Additional messages based on status
			if daemonError != nil {
				cmd.Println("sanho: unable to determine sync status")
			} else if status == client.DocsStatusOutdated {
				cmd.Println("sanho: docs base and daemon HEAD are different.")
				cmd.Println("sanho: a merge may occur during pre-commit.")
			} else if headReconciliation.Pending {
				cmd.Println("sanho: canonical docs are up to date; valid HEAD awaits local reconciliation.")
			} else if status == client.DocsStatusUpToDate {
				cmd.Println("sanho: docs are up to date.")
			}

			// Pending fix messages
			if hasPendingFix {
				if hasConflicts {
					cmd.Println()
					cmd.Println("sanho: pending fix detected with conflict markers in docs.")
					cmd.Println("sanho: please resolve conflicts and run 'sanho fix'.")
					cmd.Printf("sanho: files with conflicts: %v\n", conflictFiles)
				} else {
					cmd.Println()
					cmd.Println("sanho: pending fix detected but no conflict markers found.")
					cmd.Println("sanho: if you have resolved all conflicts, run 'sanho fix' to finalize.")
					cmd.Printf("sanho: pending fix was created at: %s\n", pendingFixState.CreatedAt.Format(time.RFC3339))
				}
			}

			// Return error if daemon was unreachable (roadmap: sanho status should exit 1 on daemon error)
			return daemonError
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Print machine-readable JSON")
	return cmd
}

func printStatusGitOperation(cmd *cobra.Command, operation infraGit.GitOperation) {
	if !operation.Active {
		cmd.Println("  git_operation: none")
		return
	}
	cmd.Printf("  git_operation: %s (%s)\n", operation.Type, operation.Classification)
	cmd.Printf("  git_reason   : %s\n", operation.Reason)
	if operation.Backend != "" {
		cmd.Printf("  git_backend  : %s\n", operation.Backend)
	}
	cmd.Printf("  git_orphaned : %t\n", operation.Orphaned)
	if operation.RecoveryClassification != "" {
		cmd.Printf("  git_recovery_class: %s\n", operation.RecoveryClassification)
	}
	for _, path := range operation.MetadataPaths {
		cmd.Printf("  git_metadata : %s\n", path)
	}
	if operation.MetadataOID != "" {
		cmd.Printf("  git_oid      : %s\n", operation.MetadataOID)
	}
	cmd.Println("  git_recovery :")
	for _, command := range operation.NextCommands {
		cmd.Printf("    - %s\n", command)
	}
	if operation.Type == infraGit.OperationRebase && !operation.Orphaned {
		cmd.Println("  git_note     : --abort restores the pre-rebase state; --quit keeps the current HEAD and working state.")
	}
}

func effectiveStatusRelation(
	reference httpclient.CommitRelation,
	reconciliation headReconciliationAssessment,
) httpclient.CommitRelation {
	if reconciliation.Classification == headReconciliationPending ||
		reconciliation.Classification == headReconciliationReconciled {
		return reconciliation.DocsRelation
	}
	return reference
}

func formatCommitRelation(relation httpclient.CommitRelation) string {
	switch relation.Status {
	case docs.CommitRelationSame:
		return "same"
	case docs.CommitRelationAhead:
		return fmt.Sprintf("ahead %d", relation.Ahead)
	case docs.CommitRelationBehind:
		return fmt.Sprintf("behind %d", relation.Behind)
	case docs.CommitRelationDiverged:
		return fmt.Sprintf("diverged +%d/-%d", relation.Ahead, relation.Behind)
	default:
		return "unknown"
	}
}

func printProjectWorkspaces(cmd *cobra.Command, status httpclient.ProjectStatusResponse) error {
	rows := sortedProjectWorkspaces(status.Workspaces)
	out := cmd.OutOrStdout()
	if _, err := fmt.Fprintln(out, "project workspaces:"); err != nil {
		return fmt.Errorf("write project workspaces heading: %w", err)
	}
	if len(rows) == 0 {
		if _, err := fmt.Fprintln(out, "  (none)"); err != nil {
			return fmt.Errorf("write empty project workspaces: %w", err)
		}
		return nil
	}
	table := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(table, "  REPOSITORY\tWORKSPACE\tDOCS HASH\tVS CURRENT\tVS HEAD"); err != nil {
		return fmt.Errorf("write project workspaces header: %w", err)
	}
	for _, row := range rows {
		label := repositoryLabel(row.RepoURL, row.LocalPath)
		if row.WorkspaceID == status.ReferenceWorkspaceID {
			label += " (current)"
		}
		if _, err := fmt.Fprintf(
			table,
			"  %s\t%s\t%s\t%s\t%s\n",
			label,
			row.WorkspaceID,
			shortHash(row.DocsHash),
			formatCommitRelation(row.RelativeToReference),
			formatCommitRelation(row.RelativeToHead),
		); err != nil {
			return fmt.Errorf("write project workspace row: %w", err)
		}
	}
	if err := table.Flush(); err != nil {
		return fmt.Errorf("flush project workspaces: %w", err)
	}
	return nil
}

func sortedProjectWorkspaces(workspaces []httpclient.ProjectStatusWorkspace) []httpclient.ProjectStatusWorkspace {
	rows := append([]httpclient.ProjectStatusWorkspace(nil), workspaces...)
	sort.SliceStable(rows, func(i, j int) bool {
		left := rows[i].RelativeToReference
		right := rows[j].RelativeToReference
		if relationRank(left.Status) != relationRank(right.Status) {
			return relationRank(left.Status) < relationRank(right.Status)
		}
		switch left.Status {
		case docs.CommitRelationAhead:
			if left.Ahead != right.Ahead {
				return left.Ahead > right.Ahead
			}
		case docs.CommitRelationBehind:
			if left.Behind != right.Behind {
				return left.Behind < right.Behind
			}
		}
		leftRepo := repositoryLabel(rows[i].RepoURL, rows[i].LocalPath)
		rightRepo := repositoryLabel(rows[j].RepoURL, rows[j].LocalPath)
		if leftRepo != rightRepo {
			return leftRepo < rightRepo
		}
		return rows[i].WorkspaceID < rows[j].WorkspaceID
	})
	return rows
}

func relationRank(status docs.CommitRelationStatus) int {
	switch status {
	case docs.CommitRelationAhead:
		return 0
	case docs.CommitRelationSame:
		return 1
	case docs.CommitRelationBehind:
		return 2
	case docs.CommitRelationDiverged:
		return 3
	default:
		return 4
	}
}

func repositoryLabel(repoURL, localPath string) string {
	candidate := strings.TrimSpace(repoURL)
	if parsed, err := url.Parse(candidate); err == nil && parsed.Path != "" {
		candidate = parsed.Path
	} else if colon := strings.LastIndex(candidate, ":"); colon >= 0 {
		candidate = candidate[colon+1:]
	}
	candidate = strings.TrimSuffix(filepath.Base(strings.TrimRight(candidate, "/")), ".git")
	if candidate == "" || candidate == "." {
		candidate = filepath.Base(filepath.Clean(localPath))
	}
	if candidate == "" || candidate == "." {
		return "(unknown)"
	}
	return candidate
}

func shortHash(hash string) string {
	const length = 12
	if len(hash) <= length {
		return hash
	}
	return hash[:length]
}
