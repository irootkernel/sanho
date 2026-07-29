package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/irootkernel/sanho/internal/domain/client"
	"github.com/irootkernel/sanho/internal/domain/docs"
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
