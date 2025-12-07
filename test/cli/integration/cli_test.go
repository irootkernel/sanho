package integration

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestCLIHelp verifies that the CLI help command works correctly.
func TestCLIHelp(t *testing.T) {
	cliBinary := getCliBinary(t)

	cmd := exec.Command(cliBinary, "--help")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI help command failed: %v\nOutput: %s", err, output)
	}

	// Verify expected content in help output
	outputStr := string(output)
	expectedStrings := []string{
		"kkachi",
		"Available Commands",
		"init",
		"status",
		"fix",
		"hook",
		"project",
		"workspace",
		"version",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(outputStr, expected) {
			t.Errorf("Expected help output to contain %q, got:\n%s", expected, outputStr)
		}
	}
}

// TestCLIVersion verifies that the version command outputs expected format.
func TestCLIVersion(t *testing.T) {
	cliBinary := getCliBinary(t)

	cmd := exec.Command(cliBinary, "version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI version command failed: %v\nOutput: %s", err, output)
	}

	outputStr := string(output)
	if !strings.HasPrefix(outputStr, "kkachi version") {
		t.Errorf("Expected version output to start with 'kkachi version', got: %s", outputStr)
	}
	if !strings.Contains(outputStr, "commit:") {
		t.Errorf("Expected version output to contain 'commit:', got: %s", outputStr)
	}
	if !strings.Contains(outputStr, "built:") {
		t.Errorf("Expected version output to contain 'built:', got: %s", outputStr)
	}
}

// TestCLIVerboseFlag verifies that the --verbose flag is recognized.
func TestCLIVerboseFlag(t *testing.T) {
	cliBinary := getCliBinary(t)

	cmd := exec.Command(cliBinary, "--verbose", "version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI with --verbose flag failed: %v\nOutput: %s", err, output)
	}

	// Just verify the command runs successfully with verbose flag
	if !strings.Contains(string(output), "kkachi version") {
		t.Errorf("Expected version output with verbose flag, got: %s", output)
	}
}

// TestCLISubcommandSkeletons verifies that skeleton commands output expected messages.
func TestCLISubcommandSkeletons(t *testing.T) {
	cliBinary := getCliBinary(t)

	// Only test commands that are still skeleton/not implemented
	tests := []struct {
		args     []string
		expected string
	}{
		{[]string{"fix"}, "not implemented yet"},
		{[]string{"state"}, "not implemented yet"},
		{[]string{"hook", "pre-commit"}, "not implemented yet"},
		{[]string{"hook", "post-checkout"}, "not implemented yet"},
	}

	for _, tt := range tests {
		t.Run(strings.Join(tt.args, "_"), func(t *testing.T) {
			cmd := exec.Command(cliBinary, tt.args...)
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("CLI command %v failed: %v\nOutput: %s", tt.args, err, output)
			}

			if !strings.Contains(string(output), tt.expected) {
				t.Errorf("Expected output to contain %q, got: %s", tt.expected, output)
			}
		})
	}
}

// TestCLIImplementedCommandsRequireFlags verifies implemented commands require flags.
func TestCLIImplementedCommandsRequireFlags(t *testing.T) {
	cliBinary := getCliBinary(t)

	tests := []struct {
		args          []string
		expectedError string
		expectNonZero bool
	}{
		// init prompts for interactive input; without stdin it fails fast on read
		{[]string{"init"}, "failed to read input", true},
		{[]string{"project", "add"}, "--server-url is required", true},
		{[]string{"project", "delete"}, "--server-url is required", true},
		{[]string{"workspace", "register"}, "--server-url is required", true},
		{[]string{"workspace", "unregister"}, "--server-url is required", true},
	}

	for _, tt := range tests {
		t.Run(strings.Join(tt.args, "_"), func(t *testing.T) {
			cmd := exec.Command(cliBinary, tt.args...)
			output, err := cmd.CombinedOutput()

			if tt.expectNonZero && err == nil {
				t.Errorf("Expected non-zero exit code for %v", tt.args)
			}

			if !strings.Contains(string(output), tt.expectedError) {
				t.Errorf("Expected output to contain %q, got: %s", tt.expectedError, output)
			}
		})
	}
}

// TestCLIStatusRequiresWorkspace verifies status command requires kkachi workspace.
func TestCLIStatusRequiresWorkspace(t *testing.T) {
	cliBinary := getCliBinary(t)

	// Run status from temp dir (not a kkachi workspace)
	tempDir := t.TempDir()
	cmd := exec.Command(cliBinary, "status")
	cmd.Dir = tempDir
	output, err := cmd.CombinedOutput()

	if err == nil {
		t.Error("Expected non-zero exit code for status without workspace")
	}

	if !strings.Contains(string(output), "not a kkachi workspace") {
		t.Errorf("Expected 'not a kkachi workspace' message, got: %s", output)
	}
}

// TestCLIUnknownCommand verifies that unknown commands return an error.
func TestCLIUnknownCommand(t *testing.T) {
	cliBinary := getCliBinary(t)

	cmd := exec.Command(cliBinary, "unknown-command")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()

	if err == nil {
		t.Error("Expected error for unknown command, but got none")
	}
}

// getCliBinary returns the path to the kkachi CLI binary.
// It first checks for KKACHI_CLI_BINARY environment variable,
// then falls back to the default bin/kkachi location.
func getCliBinary(t *testing.T) string {
	t.Helper()

	// Check environment variable first
	if binary := os.Getenv("KKACHI_CLI_BINARY"); binary != "" {
		if _, err := os.Stat(binary); err == nil {
			return binary
		}
		t.Logf("KKACHI_CLI_BINARY set but file not found: %s", binary)
	}

	// Try to find binary relative to project root
	// When running tests, we're typically in the test directory
	possiblePaths := []string{
		"../../bin/kkachi",    // from test/cli/integration
		"../../../bin/kkachi", // alternative
		"bin/kkachi",          // from project root
		"./bin/kkachi",        // explicit current dir
	}

	for _, path := range possiblePaths {
		absPath, err := filepath.Abs(path)
		if err != nil {
			continue
		}
		if _, err := os.Stat(absPath); err == nil {
			return absPath
		}
	}

	t.Skip("CLI binary not found. Run 'make cli-build' first.")
	return ""
}
