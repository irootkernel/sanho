package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestRootCommand(t *testing.T) {
	info := BuildInfo{
		Version:   "test-version",
		Commit:    "test-commit",
		BuildDate: "test-date",
	}
	cmd := NewRootCmd(info)

	// Test that root command exists and has expected properties
	if cmd.Use != "kkachi" {
		t.Errorf("Expected root command Use to be 'kkachi', got '%s'", cmd.Use)
	}

	// Test that --verbose flag is registered
	f := cmd.PersistentFlags().Lookup("verbose")
	if f == nil {
		t.Error("Expected --verbose flag to be registered")
	}
	if f.Shorthand != "v" {
		t.Errorf("Expected --verbose shorthand to be 'v', got '%s'", f.Shorthand)
	}

	// Test that subcommands are registered
	subcommands := []string{"version", "init", "status", "fix", "hook", "project", "workspace", "state", "pull"}
	for _, name := range subcommands {
		found := false
		for _, sub := range cmd.Commands() {
			if sub.Name() == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected subcommand '%s' to be registered", name)
		}
	}
}

func TestExitCodeConstants(t *testing.T) {
	// Verify exit code values match roadmap policy
	if ExitCodeSuccess != 0 {
		t.Errorf("Expected ExitCodeSuccess to be 0, got %d", ExitCodeSuccess)
	}
	if ExitCodeUserError != 1 {
		t.Errorf("Expected ExitCodeUserError to be 1, got %d", ExitCodeUserError)
	}
	if ExitCodeInternalError != 2 {
		t.Errorf("Expected ExitCodeInternalError to be 2, got %d", ExitCodeInternalError)
	}
}

func TestVersionCommand(t *testing.T) {
	info := BuildInfo{
		Version:   "1.2.3",
		Commit:    "abcdef",
		BuildDate: "2024-01-01",
	}
	cmd := NewRootCmd(info)

	// Capture output
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"version"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("version command failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "1.2.3") {
		t.Errorf("Expected version output to contain '1.2.3', got: %s", output)
	}
	if !strings.Contains(output, "abcdef") {
		t.Errorf("Expected version output to contain commit 'abcdef', got: %s", output)
	}
	if !strings.Contains(output, "2024-01-01") {
		t.Errorf("Expected version output to contain build date '2024-01-01', got: %s", output)
	}
}

func TestHelpOutput(t *testing.T) {
	info := BuildInfo{}
	cmd := NewRootCmd(info)

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("help command failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "kkachi") {
		t.Errorf("Expected help output to contain 'kkachi', got: %s", output)
	}
	if !strings.Contains(output, "Available Commands") {
		t.Errorf("Expected help output to contain 'Available Commands', got: %s", output)
	}
}

func TestHookSubcommands(t *testing.T) {
	info := BuildInfo{}
	cmd := NewRootCmd(info)

	// Find hook command
	var hookCmd *cobra.Command
	for _, sub := range cmd.Commands() {
		if sub.Name() == "hook" {
			hookCmd = sub
			break
		}
	}

	if hookCmd == nil {
		t.Fatal("hook command not found")
	}

	// Check hook subcommands
	hookSubcommands := []string{"pre-commit", "post-checkout", "post-merge", "post-rewrite", "pre-push", "commit-msg"}
	for _, name := range hookSubcommands {
		found := false
		for _, sub := range hookCmd.Commands() {
			if sub.Name() == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected hook subcommand '%s' to be registered", name)
		}
	}
}
