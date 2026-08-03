package cli

import (
	"sort"
	"time"

	"github.com/irootkernel/sanho/internal/domain/client"
	infraGit "github.com/irootkernel/sanho/internal/infra/git"
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
	PullCommit                    statusJSONPullCommit      `json:"pull_commit"`
	MainPublication               statusJSONMainPublication `json:"main_publication"`
	GitOperation                  statusJSONGitOperation    `json:"git_operation"`
	Conflicts                     statusJSONConflicts       `json:"conflicts"`
	WorkspaceComparisonsAvailable bool                      `json:"workspace_comparisons_available"`
	Workspaces                    []statusJSONWorkspace     `json:"workspaces"`
}

type statusJSONGitOperation struct {
	Active         bool     `json:"active"`
	Type           string   `json:"type"`
	Classification string   `json:"classification"`
	Reason         string   `json:"reason"`
	NextCommands   []string `json:"next_commands"`
}

type statusJSONMainPublication struct {
	Pending        bool     `json:"pending"`
	Classification string   `json:"classification,omitempty"`
	Reason         string   `json:"reason,omitempty"`
	BaseCommit     string   `json:"base_commit,omitempty"`
	LocalMain      string   `json:"local_main,omitempty"`
	RemoteMain     string   `json:"remote_main,omitempty"`
	SyncCommits    []string `json:"sync_commits"`
}

type statusJSONPullCommit struct {
	Exists         bool   `json:"exists"`
	Phase          string `json:"phase,omitempty"`
	Classification string `json:"classification,omitempty"`
	Reason         string `json:"reason,omitempty"`
	CurrentHead    string `json:"current_head,omitempty"`
	PreparedHead   string `json:"prepared_head,omitempty"`
	NextCommand    string `json:"next_command,omitempty"`
	BackupHeadRef  string `json:"backup_head_ref,omitempty"`
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
	pendingFixState client.PendingFixState,
	hasPendingFix bool,
	conflictScanStatus string,
	conflictFiles []string,
	comparisonAvailable bool,
	pullCommit pullCommitAssessment,
	mainPublication mainPublicationAssessment,
	gitOperation infraGit.GitOperation,
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
		PullCommit:      buildStatusJSONPullCommit(pullCommit),
		MainPublication: buildStatusJSONMainPublication(mainPublication),
		GitOperation:    buildStatusJSONGitOperation(gitOperation),
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

func buildStatusJSONGitOperation(operation infraGit.GitOperation) statusJSONGitOperation {
	commands := append([]string(nil), operation.NextCommands...)
	if commands == nil {
		commands = make([]string, 0)
	}
	return statusJSONGitOperation{
		Active:         operation.Active,
		Type:           string(operation.Type),
		Classification: string(operation.Classification),
		Reason:         operation.Reason,
		NextCommands:   commands,
	}
}

func buildStatusJSONMainPublication(assessment mainPublicationAssessment) statusJSONMainPublication {
	output := statusJSONMainPublication{
		Pending:     assessment.Exists,
		SyncCommits: make([]string, 0),
	}
	if !assessment.Exists {
		return output
	}
	output.Classification = string(assessment.Classification)
	output.Reason = assessment.Reason
	output.BaseCommit = assessment.State.BaseCommit
	output.LocalMain = assessment.LocalMain
	output.RemoteMain = assessment.RemoteMain
	for _, commit := range assessment.State.Commits {
		output.SyncCommits = append(output.SyncCommits, commit.Commit)
	}
	return output
}

func buildStatusJSONPullCommit(assessment pullCommitAssessment) statusJSONPullCommit {
	output := statusJSONPullCommit{Exists: assessment.Exists}
	if !assessment.Exists {
		return output
	}
	output.Phase = assessment.State.Phase
	output.Classification = string(assessment.Classification)
	output.Reason = assessment.Reason
	output.CurrentHead = assessment.Head
	output.PreparedHead = pullCommitPreparedHead(assessment.State)
	output.NextCommand = assessment.NextCommand
	if assessment.State.Recovery != nil {
		output.BackupHeadRef = assessment.State.Recovery.HeadRef
	}
	return output
}
