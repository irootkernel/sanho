package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/irootkernel/sanho/internal/domain/client"
	"github.com/irootkernel/sanho/internal/domain/docs"
	"github.com/irootkernel/sanho/internal/infra/fs"
	infraGit "github.com/irootkernel/sanho/internal/infra/git"
	"github.com/irootkernel/sanho/internal/infra/httpclient"
	"github.com/spf13/cobra"
)

func TestFormatCommitRelation(t *testing.T) {
	tests := []struct {
		relation httpclient.CommitRelation
		want     string
	}{
		{httpclient.CommitRelation{Status: docs.CommitRelationSame}, "same"},
		{httpclient.CommitRelation{Status: docs.CommitRelationAhead, Ahead: 3}, "ahead 3"},
		{httpclient.CommitRelation{Status: docs.CommitRelationBehind, Behind: 2}, "behind 2"},
		{httpclient.CommitRelation{Status: docs.CommitRelationDiverged, Ahead: 1, Behind: 4}, "diverged +1/-4"},
		{httpclient.CommitRelation{Status: docs.CommitRelationUnknown}, "unknown"},
	}
	for _, test := range tests {
		if got := formatCommitRelation(test.relation); got != test.want {
			t.Errorf("formatCommitRelation(%#v) = %q, want %q", test.relation, got, test.want)
		}
	}
}

func TestEffectiveStatusRelationUsesValidHEADWithoutChangingReferenceRelation(t *testing.T) {
	reference := httpclient.CommitRelation{Status: docs.CommitRelationBehind, Behind: 1}
	for _, test := range []struct {
		classification headReconciliationClassification
		relation       httpclient.CommitRelation
		want           docs.CommitRelationStatus
	}{
		{classification: headReconciliationPending, relation: httpclient.CommitRelation{Status: docs.CommitRelationSame}, want: docs.CommitRelationSame},
		{classification: headReconciliationReconciled, relation: httpclient.CommitRelation{Status: docs.CommitRelationBehind}, want: docs.CommitRelationBehind},
		{classification: headReconciliationInvalid, relation: httpclient.CommitRelation{Status: docs.CommitRelationSame}, want: docs.CommitRelationBehind},
		{classification: headReconciliationUnknown, relation: httpclient.CommitRelation{Status: docs.CommitRelationSame}, want: docs.CommitRelationBehind},
	} {
		reconciliation := headReconciliationAssessment{
			Classification: test.classification,
			DocsRelation:   test.relation,
		}
		if got := effectiveStatusRelation(reference, reconciliation); got.Status != test.want {
			t.Fatalf("classification=%s effective relation=%+v want %s", test.classification, got, test.want)
		}
	}
	if reference.Status != docs.CommitRelationBehind || reference.Behind != 1 {
		t.Fatalf("reference relation was mutated: %+v", reference)
	}
}

func TestPrintProjectWorkspacesSortsAndMarksCurrent(t *testing.T) {
	status := httpclient.ProjectStatusResponse{
		ReferenceWorkspaceID: "ws-current",
		Workspaces: []httpclient.ProjectStatusWorkspace{
			{
				WorkspaceID: "ws-behind",
				RepoURL:     "git@github.com:org/behind.git",
				DocsHash:    "bbbbbbbbbbbbbbbb",
				RelativeToReference: httpclient.CommitRelation{
					Status: docs.CommitRelationBehind,
					Behind: 2,
				},
				RelativeToHead: httpclient.CommitRelation{Status: docs.CommitRelationBehind, Behind: 4},
			},
			{
				WorkspaceID: "ws-current",
				RepoURL:     "https://github.com/org/current.git",
				DocsHash:    "cccccccccccccccc",
				RelativeToReference: httpclient.CommitRelation{
					Status: docs.CommitRelationSame,
				},
				RelativeToHead: httpclient.CommitRelation{Status: docs.CommitRelationBehind, Behind: 2},
			},
			{
				WorkspaceID: "ws-ahead",
				RepoURL:     "git@github.com:org/ahead.git",
				DocsHash:    "aaaaaaaaaaaaaaaa",
				RelativeToReference: httpclient.CommitRelation{
					Status: docs.CommitRelationAhead,
					Ahead:  2,
				},
				RelativeToHead: httpclient.CommitRelation{Status: docs.CommitRelationSame},
			},
		},
	}
	command := &cobra.Command{}
	output := new(bytes.Buffer)
	command.SetOut(output)

	if err := printProjectWorkspaces(command, status); err != nil {
		t.Fatal(err)
	}

	text := output.String()
	aheadIndex := strings.Index(text, "ahead")
	currentIndex := strings.Index(text, "current (current)")
	behindIndex := strings.Index(text, "behind")
	if aheadIndex < 0 || currentIndex < 0 || behindIndex < 0 {
		t.Fatalf("missing rows in output:\n%s", text)
	}
	if aheadIndex >= currentIndex || currentIndex >= behindIndex {
		t.Fatalf("rows not sorted ahead, same, behind:\n%s", text)
	}
	if !strings.Contains(text, "aaaaaaaaaaaa") || strings.Contains(text, "aaaaaaaaaaaaaaaa") {
		t.Fatalf("docs hash was not shortened:\n%s", text)
	}
}

func TestRepositoryLabel(t *testing.T) {
	tests := []struct {
		repoURL   string
		localPath string
		want      string
	}{
		{"git@github.com:org/repo.git", "/tmp/fallback", "repo"},
		{"https://github.com/org/repo.git", "/tmp/fallback", "repo"},
		{"", "/tmp/local-repo", "local-repo"},
	}
	for _, test := range tests {
		if got := repositoryLabel(test.repoURL, test.localPath); got != test.want {
			t.Errorf("repositoryLabel(%q, %q) = %q, want %q", test.repoURL, test.localPath, got, test.want)
		}
	}
}

func TestBuildStatusJSONOutputUsesStableMachineFields(t *testing.T) {
	createdAt := time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC)
	config := &client.WorkspaceConfig{
		Project:     "alpha",
		WorkspaceID: "ws-current",
	}
	projectStatus := httpclient.ProjectStatusResponse{
		DocsHead:        "full-head-hash",
		ReferenceToHead: httpclient.CommitRelation{Status: docs.CommitRelationBehind, Behind: 2},
		Workspaces: []httpclient.ProjectStatusWorkspace{
			{
				WorkspaceID: "ws-current",
				RepoURL:     "git@github.com:org/current.git",
				LocalPath:   "/private/current",
				DocsHash:    "full-current-hash",
				RelativeToReference: httpclient.CommitRelation{
					Status: docs.CommitRelationSame,
				},
				RelativeToHead: httpclient.CommitRelation{
					Status: docs.CommitRelationBehind,
					Behind: 2,
				},
			},
		},
	}

	output := buildStatusJSONOutput(
		config,
		"full-current-hash",
		client.DocsStatusOutdated,
		projectStatus,
		client.PendingFixState{CreatedAt: createdAt},
		true,
		"complete",
		[]string{"z.md", "a.md"},
		true,
		pullCommitAssessment{},
		mainPublicationAssessment{},
		infraGit.GitOperation{
			Type:           infraGit.OperationNone,
			Classification: infraGit.OperationClear,
			NextCommands:   make([]string, 0),
		},
		headReconciliationAssessment{
			Pending:        true,
			Classification: headReconciliationPending,
			AppCommit:      "application-commit",
			DocsHash:       "full-head-hash",
			Reason:         "valid HEAD awaits local reconciliation",
		},
	)

	data, err := json.Marshal(output)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["docs_base"] != "full-current-hash" || decoded["docs_head"] != "full-head-hash" {
		t.Fatalf("hash fields = %#v", decoded)
	}
	if _, ok := decoded["local_path"]; ok {
		t.Fatalf("status JSON exposed local_path: %#v", decoded)
	}
	pullCommit := decoded["pull_commit"].(map[string]any)
	if pullCommit["exists"] != false {
		t.Fatalf("pull_commit = %#v", pullCommit)
	}
	mainPublication := decoded["main_publication"].(map[string]any)
	if mainPublication["pending"] != false || len(mainPublication["sync_commits"].([]any)) != 0 {
		t.Fatalf("main_publication = %#v", mainPublication)
	}
	gitOperation := decoded["git_operation"].(map[string]any)
	if gitOperation["active"] != false || gitOperation["type"] != "none" ||
		gitOperation["classification"] != "clear" || len(gitOperation["next_commands"].([]any)) != 0 {
		t.Fatalf("git_operation = %#v", gitOperation)
	}
	headReconciliation := decoded["head_reconciliation"].(map[string]any)
	if headReconciliation["pending"] != true || headReconciliation["classification"] != "pending" ||
		headReconciliation["app_commit"] != "application-commit" || headReconciliation["docs_hash"] != "full-head-hash" {
		t.Fatalf("head_reconciliation = %#v", headReconciliation)
	}
	workspaces := decoded["workspaces"].([]any)
	workspace := workspaces[0].(map[string]any)
	if workspace["repository"] != "current" || workspace["docs_hash"] != "full-current-hash" {
		t.Fatalf("workspace = %#v", workspace)
	}
	conflicts := decoded["conflicts"].(map[string]any)
	files := conflicts["files"].([]any)
	if files[0] != "a.md" || files[1] != "z.md" {
		t.Fatalf("conflict files = %#v", files)
	}
}

func TestBuildStatusJSONGitOperationIncludesRecoveryChoices(t *testing.T) {
	output := buildStatusJSONGitOperation(infraGit.GitOperation{
		Active:         true,
		Type:           infraGit.OperationRebase,
		Classification: infraGit.OperationBlocked,
		Reason:         "Git rebase operation metadata is present",
		NextCommands: []string{
			"git status",
			"git rebase --continue",
			"git rebase --abort",
			"git rebase --quit",
		},
	})
	if !output.Active || output.Type != "rebase" || output.Classification != "blocked" ||
		len(output.NextCommands) != 4 || output.NextCommands[0] != "git status" {
		t.Fatalf("git_operation=%+v", output)
	}
}

func TestBuildStatusJSONGitOperationIncludesOrphanedMetadata(t *testing.T) {
	output := buildStatusJSONGitOperation(infraGit.GitOperation{
		Active:                 true,
		Type:                   infraGit.OperationRebase,
		Classification:         infraGit.OperationBlocked,
		Reason:                 "orphaned REBASE_HEAD metadata is present",
		Backend:                infraGit.OperationBackendNone,
		MetadataPaths:          []string{"/repo/.git/REBASE_HEAD"},
		Orphaned:               true,
		MetadataOID:            "955aa992c9418137ad65c17c17a3fa1a4cb972ea",
		RecoveryClassification: infraGit.OperationRecoveryConditionalRef,
		NextCommands: []string{
			"git status",
			"git rev-parse --verify 'REBASE_HEAD^{commit}'",
			"git update-ref -d REBASE_HEAD 955aa992c9418137ad65c17c17a3fa1a4cb972ea",
		},
	})
	if !output.Orphaned || output.Backend != "none" ||
		output.MetadataOID != "955aa992c9418137ad65c17c17a3fa1a4cb972ea" ||
		output.RecoveryClassification != "conditional_pseudo_ref_delete" ||
		len(output.MetadataPaths) != 1 {
		t.Fatalf("git_operation=%+v", output)
	}
}

func TestPrintStatusGitOperationExplainsRebaseRecovery(t *testing.T) {
	command := &cobra.Command{}
	output := new(bytes.Buffer)
	command.SetOut(output)
	printStatusGitOperation(command, infraGit.GitOperation{
		Active:         true,
		Type:           infraGit.OperationRebase,
		Classification: infraGit.OperationBlocked,
		Reason:         "Git rebase operation metadata is present",
		NextCommands:   []string{"git status", "git rebase --abort", "git rebase --quit"},
	})
	text := output.String()
	for _, value := range []string{
		"git_operation: rebase (blocked)",
		"git status",
		"git rebase --abort",
		"git rebase --quit",
		"--abort restores the pre-rebase state",
		"--quit keeps the current HEAD",
	} {
		if !strings.Contains(text, value) {
			t.Fatalf("status output missing %q:\n%s", value, text)
		}
	}
}

func TestPrintStatusGitOperationExplainsConditionalOrphanRecovery(t *testing.T) {
	command := &cobra.Command{}
	output := new(bytes.Buffer)
	command.SetOut(output)
	printStatusGitOperation(command, infraGit.GitOperation{
		Active:                 true,
		Type:                   infraGit.OperationRebase,
		Classification:         infraGit.OperationBlocked,
		Reason:                 "orphaned REBASE_HEAD metadata is present",
		Backend:                infraGit.OperationBackendNone,
		MetadataPaths:          []string{"/repo/.git/REBASE_HEAD"},
		Orphaned:               true,
		MetadataOID:            "955aa992c9418137ad65c17c17a3fa1a4cb972ea",
		RecoveryClassification: infraGit.OperationRecoveryConditionalRef,
		NextCommands: []string{
			"git status",
			"git rev-parse --verify 'REBASE_HEAD^{commit}'",
			"git update-ref -d REBASE_HEAD 955aa992c9418137ad65c17c17a3fa1a4cb972ea",
		},
	})
	text := output.String()
	for _, value := range []string{
		"git_backend  : none",
		"git_orphaned : true",
		"git_recovery_class: conditional_pseudo_ref_delete",
		"git_metadata : /repo/.git/REBASE_HEAD",
		"git_oid      : 955aa992c9418137ad65c17c17a3fa1a4cb972ea",
		"git update-ref -d REBASE_HEAD 955aa992c9418137ad65c17c17a3fa1a4cb972ea",
	} {
		if !strings.Contains(text, value) {
			t.Fatalf("status output missing %q:\n%s", value, text)
		}
	}
	for _, value := range []string{"git rebase --continue", "git rebase --abort", "git rebase --quit"} {
		if strings.Contains(text, value) {
			t.Fatalf("orphan recovery must not recommend %q:\n%s", value, text)
		}
	}
}

func TestBuildStatusJSONMainPublicationIncludesPendingCommits(t *testing.T) {
	output := buildStatusJSONMainPublication(mainPublicationAssessment{
		Exists:         true,
		Classification: mainPublicationBlocked,
		Reason:         "local main has diverged from origin/main",
		LocalMain:      "local-main",
		RemoteMain:     "remote-main",
		State: fs.MainPublicationState{
			BaseCommit: "base-main",
			Commits: []fs.MainPublicationCommit{
				{Commit: "sync-1"},
				{Commit: "sync-2"},
			},
		},
	})
	if !output.Pending || output.Classification != "blocked" || output.BaseCommit != "base-main" ||
		output.LocalMain != "local-main" || output.RemoteMain != "remote-main" ||
		len(output.SyncCommits) != 2 || output.SyncCommits[1] != "sync-2" {
		t.Fatalf("main_publication=%+v", output)
	}
}

func TestBuildStatusJSONPullCommitIncludesRecoveryAction(t *testing.T) {
	output := buildStatusJSONPullCommit(pullCommitAssessment{
		Exists:         true,
		Classification: pullCommitAmbiguous,
		Reason:         "history rewrite is not proven",
		NextCommand:    "sanho pull-commit --recover",
		Head:           "current-head",
		State: fs.PullCommitState{
			Phase:        fs.PullCommitPhasePrepared,
			PreparedHead: "prepared-head",
			Recovery: &fs.PullCommitRecovery{
				HeadRef: "refs/sanho/recovery/transaction/head",
			},
		},
	})
	if !output.Exists || output.Classification != "ambiguous" ||
		output.NextCommand != "sanho pull-commit --recover" ||
		output.BackupHeadRef != "refs/sanho/recovery/transaction/head" {
		t.Fatalf("pull_commit=%+v", output)
	}
}
