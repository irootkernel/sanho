package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestVersionJSONOutput(t *testing.T) {
	cmd := NewRootCmd(BuildInfo{
		Version:   "1.2.3",
		Commit:    "abcdef",
		BuildDate: "2024-01-01",
	})
	output := new(bytes.Buffer)
	cmd.SetOut(output)
	cmd.SetArgs([]string{"version", "--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("version --json failed: %v", err)
	}

	var got map[string]string
	if err := json.Unmarshal(output.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON output %q: %v", output.String(), err)
	}
	want := map[string]string{"name": "kkachi", "version": "1.2.3"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("version JSON = %#v, want %#v", got, want)
	}
}

func TestRenderCommandErrorAsJSON(t *testing.T) {
	cmd := newStatusCmd()
	stderr := new(bytes.Buffer)
	cmd.SetErr(stderr)
	if err := cmd.Flags().Set("json", "true"); err != nil {
		t.Fatal(err)
	}

	renderCommandError(cmd, withErrorCode("not_in_workspace", errors.New("missing workspace")))

	var got jsonErrorEnvelope
	if err := json.Unmarshal(stderr.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON error %q: %v", stderr.String(), err)
	}
	if got.Error.Code != "not_in_workspace" || got.Error.Message != "missing workspace" {
		t.Fatalf("JSON error = %#v", got)
	}
}

func TestCommandErrorCanPreserveHumanMessage(t *testing.T) {
	err := withErrorCodeMessage(
		"server_url_required",
		"--server-url is required with --all outside a kkachi workspace",
		errors.New("configuration file not found"),
	)
	if err.Error() != "configuration file not found" {
		t.Fatalf("human message = %q", err.Error())
	}
	if commandErrorMessage(err) != "--server-url is required with --all outside a kkachi workspace" {
		t.Fatalf("JSON message = %q", commandErrorMessage(err))
	}
}

func TestJSONFlagIsLimitedToQueryCommands(t *testing.T) {
	root := NewRootCmd(BuildInfo{})
	withJSON := map[string]bool{
		"version": true,
		"status":  true,
		"state":   true,
	}
	for _, command := range root.Commands() {
		hasJSON := command.Flags().Lookup("json") != nil
		if hasJSON != withJSON[command.Name()] {
			t.Errorf("%s JSON flag registered = %v, want %v", command.Name(), hasJSON, withJSON[command.Name()])
		}
	}
}
