package cli

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/SeventeenthEarth/kkachi/internal/domain/docs"
	"github.com/SeventeenthEarth/kkachi/internal/infra/httpclient"
)

func TestBuildStateJSONOutputFiltersAndSortsProject(t *testing.T) {
	reportedAt := "2026-07-28T10:00:00Z"
	response := httpclient.StateResponse{
		DocsHeads: map[string]string{
			"alpha": "alpha-head",
			"beta":  "beta-head",
		},
		Workspaces: []httpclient.WorkspaceSummary{
			{WorkspaceID: "ws-z", Project: "alpha", DocsHash: "z", LastReportedAt: &reportedAt},
			{WorkspaceID: "ws-beta", Project: "beta", DocsHash: "beta"},
			{WorkspaceID: "ws-a", Project: "alpha", DocsHash: "a", LastActorEmail: "actor@example.com"},
		},
	}

	output := buildStateJSONOutput(false, docs.ProjectName("alpha"), response)

	if output.Scope != "project" || output.Project == nil || *output.Project != "alpha" {
		t.Fatalf("scope/project = %q/%v", output.Scope, output.Project)
	}
	if !reflect.DeepEqual(output.DocsHeads, map[string]string{"alpha": "alpha-head"}) {
		t.Fatalf("docs_heads = %#v", output.DocsHeads)
	}
	if len(output.Workspaces) != 2 ||
		output.Workspaces[0].WorkspaceID != "ws-a" ||
		output.Workspaces[1].WorkspaceID != "ws-z" {
		t.Fatalf("workspaces = %#v", output.Workspaces)
	}
	if output.Workspaces[0].LastActor == nil || *output.Workspaces[0].LastActor != "actor@example.com" {
		t.Fatalf("last_actor = %v", output.Workspaces[0].LastActor)
	}

	data, err := json.Marshal(output)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if _, ok := decoded["project"]; !ok {
		t.Fatal("project must always be present")
	}
}

func TestBuildStateJSONOutputUsesNullProjectForAll(t *testing.T) {
	output := buildStateJSONOutput(true, "", httpclient.StateResponse{})
	if output.Scope != "all" || output.Project != nil {
		t.Fatalf("scope/project = %q/%v", output.Scope, output.Project)
	}
	if output.DocsHeads == nil || output.Workspaces == nil {
		t.Fatalf("collections must not be nil: %#v", output)
	}
}
