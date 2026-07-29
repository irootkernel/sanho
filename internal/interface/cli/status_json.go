package cli

import (
	"sort"
	"time"

	"github.com/irootkernel/sanho/internal/domain/client"
	"github.com/irootkernel/sanho/internal/infra/fs"
	"github.com/irootkernel/sanho/internal/infra/httpclient"
)

type statusJSONOutput struct {
	Project                       string                    `json:"project"`
	WorkspaceID                   string                    `json:"workspace_id"`
	DocsBase                      string                    `json:"docs_base"`
	DocsHead                      string                    `json:"docs_head"`
	Status                        client.DocsStatus         `json:"status"`
	DocsRelation                  httpclient.CommitRelation `json:"docs_relation"`
	PendingFix                    statusJSONPendingFix      `json:"pending_fix"`
	Conflicts                     statusJSONConflicts       `json:"conflicts"`
	WorkspaceComparisonsAvailable bool                      `json:"workspace_comparisons_available"`
	Workspaces                    []statusJSONWorkspace     `json:"workspaces"`
}

type statusJSONPendingFix struct {
	Exists    bool    `json:"exists"`
	CreatedAt *string `json:"created_at"`
}

type statusJSONConflicts struct {
	ScanStatus string   `json:"scan_status"`
	Files      []string `json:"files"`
}

type statusJSONWorkspace struct {
	Repository        string                    `json:"repository"`
	WorkspaceID       string                    `json:"workspace_id"`
	DocsHash          string                    `json:"docs_hash"`
	RelativeToCurrent httpclient.CommitRelation `json:"relative_to_current"`
	RelativeToHead    httpclient.CommitRelation `json:"relative_to_head"`
}

func buildStatusJSONOutput(
	config *client.WorkspaceConfig,
	docsBase string,
	status client.DocsStatus,
	projectStatus httpclient.ProjectStatusResponse,
	pendingFixState fs.PendingFixState,
	hasPendingFix bool,
	conflictScanStatus string,
	conflictFiles []string,
	comparisonAvailable bool,
) statusJSONOutput {
	var pendingFixCreatedAt *string
	if hasPendingFix {
		createdAt := pendingFixState.CreatedAt.Format(time.RFC3339)
		pendingFixCreatedAt = &createdAt
	}

	files := append([]string(nil), conflictFiles...)
	sort.Strings(files)
	if files == nil {
		files = make([]string, 0)
	}

	output := statusJSONOutput{
		Project:      string(config.Project),
		WorkspaceID:  string(config.WorkspaceID),
		DocsBase:     docsBase,
		DocsHead:     projectStatus.DocsHead,
		Status:       status,
		DocsRelation: projectStatus.ReferenceToHead,
		PendingFix: statusJSONPendingFix{
			Exists:    hasPendingFix,
			CreatedAt: pendingFixCreatedAt,
		},
		Conflicts: statusJSONConflicts{
			ScanStatus: conflictScanStatus,
			Files:      files,
		},
		WorkspaceComparisonsAvailable: comparisonAvailable,
		Workspaces:                    make([]statusJSONWorkspace, 0, len(projectStatus.Workspaces)),
	}

	for _, row := range sortedProjectWorkspaces(projectStatus.Workspaces) {
		output.Workspaces = append(output.Workspaces, statusJSONWorkspace{
			Repository:        repositoryLabel(row.RepoURL, row.LocalPath),
			WorkspaceID:       row.WorkspaceID,
			DocsHash:          row.DocsHash,
			RelativeToCurrent: row.RelativeToReference,
			RelativeToHead:    row.RelativeToHead,
		})
	}
	return output
}
