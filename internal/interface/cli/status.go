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

	"github.com/SeventeenthEarth/kkachi/internal/domain/client"
	"github.com/SeventeenthEarth/kkachi/internal/domain/docs"
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

			// Step 5: Get docs HEAD and compare project workspaces.
			httpClient := httpclient.NewHTTPClient(config.ServerURL)
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
				serverHead, headErr := httpClient.DocsHead(ctx, config.Project)
				err = headErr
				if headErr == nil {
					projectStatus = httpclient.ProjectStatusResponse{
						Project:              string(config.Project),
						ReferenceWorkspaceID: string(config.WorkspaceID),
						ReferenceDocsHash:    string(localHash),
						DocsHead:             string(serverHead),
					}
					if localHash == serverHead {
						projectStatus.ReferenceToHead.Status = docs.CommitRelationSame
					} else {
						projectStatus.ReferenceToHead.Status = docs.CommitRelationUnknown
						legacyDifferent = true
					}
				}
			}

			// Determine status
			var status client.DocsStatus
			var serverHeadStr string
			relation := httpclient.CommitRelation{Status: docs.CommitRelationUnknown}
			var serverError error // Track server error to return at the end

			if err != nil {
				status = client.DocsStatusUnknown
				serverHeadStr = "(unavailable)"
				switch {
				case errors.Is(err, httpclient.ErrUnknownProject):
					cmd.PrintErrf("Warning: project '%s' is not registered on server\n", config.Project)
					serverError = fmt.Errorf("project '%s' is not registered on server", config.Project)
				case errors.Is(err, httpclient.ErrUnknownWorkspace):
					cmd.PrintErrf("Warning: workspace '%s' is not registered on server\n", config.WorkspaceID)
					serverError = fmt.Errorf("workspace '%s' is not registered on server", config.WorkspaceID)
				case errors.Is(err, httpclient.ErrWorkspaceProjectMismatch):
					cmd.PrintErrf("Warning: workspace '%s' is registered under another project\n", config.WorkspaceID)
					serverError = fmt.Errorf("workspace '%s' belongs to another project", config.WorkspaceID)
				case errors.Is(err, httpclient.ErrUnknownDocsCommit):
					cmd.PrintErrf("Warning: local docs commit '%s' is not available on server\n", localHash)
					serverError = fmt.Errorf("local docs commit '%s' is unknown", localHash)
				default:
					cmd.PrintErrf("Warning: failed to connect to server: %v\n", err)
					serverError = fmt.Errorf("failed to fetch project status: %w", err)
				}
			} else {
				serverHeadStr = projectStatus.DocsHead
				relation = projectStatus.ReferenceToHead
				if relation.Status == docs.CommitRelationSame {
					status = client.DocsStatusUpToDate
				} else if relation.Status == docs.CommitRelationUnknown && !legacyDifferent {
					status = client.DocsStatusUnknown
				} else {
					status = client.DocsStatusOutdated
				}
			}

			// Print status
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "kkachi status")
			fmt.Fprintf(out, "  project       : %s\n", config.Project)
			fmt.Fprintf(out, "  workspace     : %s\n", config.WorkspaceID)
			fmt.Fprintf(out, "  docs base     : %s\n", localHash)
			fmt.Fprintf(out, "  docs head     : %s\n", serverHeadStr)
			fmt.Fprintf(out, "  status        : %s\n", status)
			fmt.Fprintf(out, "  docs relation : %s\n", formatCommitRelation(relation))
			if hasPendingFix {
				fmt.Fprintln(out, "  pending_fix   : yes")
			} else {
				fmt.Fprintln(out, "  pending_fix   : no")
			}
			if serverError == nil {
				fmt.Fprintln(out)
				if comparisonAvailable {
					printProjectWorkspaces(cmd, projectStatus)
				} else {
					fmt.Fprintln(out, "project workspaces:")
					fmt.Fprintln(out, "  (server upgrade required for workspace comparisons)")
				}
			}

			fmt.Fprintln(out)

			// Additional messages based on status
			if serverError != nil {
				fmt.Fprintln(out, "kkachi: unable to determine sync status")
			} else if status == client.DocsStatusOutdated {
				fmt.Fprintln(out, "kkachi: docs base and server HEAD are different.")
				fmt.Fprintln(out, "kkachi: a merge may occur during pre-commit.")
			} else if status == client.DocsStatusUpToDate {
				fmt.Fprintln(out, "kkachi: docs are up to date.")
			}

			// Pending fix messages
			if hasPendingFix {
				if hasConflicts {
					fmt.Fprintln(out)
					fmt.Fprintln(out, "kkachi: pending fix detected with conflict markers in docs.")
					fmt.Fprintln(out, "kkachi: please resolve conflicts and run 'kkachi fix'.")
					fmt.Fprintf(out, "kkachi: files with conflicts: %v\n", conflictFiles)
				} else {
					fmt.Fprintln(out)
					fmt.Fprintln(out, "kkachi: pending fix detected but no conflict markers found.")
					fmt.Fprintln(out, "kkachi: if you have resolved all conflicts, run 'kkachi fix' to finalize.")
					fmt.Fprintf(out, "kkachi: pending fix was created at: %s\n", pendingFixState.CreatedAt.Format(time.RFC3339))
				}
			}

			// Return error if server was unreachable (roadmap: kkachi status should exit 1 on server error)
			return serverError
		},
	}
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

func printProjectWorkspaces(cmd *cobra.Command, status httpclient.ProjectStatusResponse) {
	rows := append([]httpclient.ProjectStatusWorkspace(nil), status.Workspaces...)
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

	out := cmd.OutOrStdout()
	fmt.Fprintln(out, "project workspaces:")
	if len(rows) == 0 {
		fmt.Fprintln(out, "  (none)")
		return
	}
	table := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(table, "  REPOSITORY\tWORKSPACE\tDOCS HASH\tVS CURRENT\tVS HEAD")
	for _, row := range rows {
		label := repositoryLabel(row.RepoURL, row.LocalPath)
		if row.WorkspaceID == status.ReferenceWorkspaceID {
			label += " (current)"
		}
		fmt.Fprintf(
			table,
			"  %s\t%s\t%s\t%s\t%s\n",
			label,
			row.WorkspaceID,
			shortHash(row.DocsHash),
			formatCommitRelation(row.RelativeToReference),
			formatCommitRelation(row.RelativeToHead),
		)
	}
	_ = table.Flush()
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
