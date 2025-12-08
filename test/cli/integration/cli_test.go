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
	// Note: hook pre-commit, hook commit-msg are now implemented (Phase 4)
	tests := []struct {
		args     []string
		expected string
	}{
		{[]string{"fix"}, "not implemented yet"},
		{[]string{"state"}, "not implemented yet"},
		{[]string{"hook", "pre-push"}, "not implemented yet"},
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

// TestCLIReadOnlyHooksAlwaysExitZero verifies that read-only hooks always exit 0
// even when not in a kkachi workspace (per Phase 3 requirement).
func TestCLIReadOnlyHooksAlwaysExitZero(t *testing.T) {
	cliBinary := getCliBinary(t)

	// Run from temp dir (not a kkachi workspace)
	tempDir := t.TempDir()

	tests := []struct {
		name string
		args []string
	}{
		{"post-checkout", []string{"hook", "post-checkout"}},
		{"post-checkout with args", []string{"hook", "post-checkout", "abc123", "def456", "1"}},
		{"post-merge", []string{"hook", "post-merge"}},
		{"post-merge with squash flag", []string{"hook", "post-merge", "1"}},
		{"post-rewrite rebase", []string{"hook", "post-rewrite", "rebase"}},
		{"post-rewrite rebase with mapping file", []string{"hook", "post-rewrite", "rebase", "/tmp/git-rebase-todo"}},
		{"post-rewrite amend", []string{"hook", "post-rewrite", "amend"}},
		{"post-rewrite amend with mapping file", []string{"hook", "post-rewrite", "amend", "/tmp/git-amend-mapping"}},
		{"post-rewrite no args", []string{"hook", "post-rewrite"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(cliBinary, tt.args...)
			cmd.Dir = tempDir
			output, err := cmd.CombinedOutput()

			// All read-only hooks should exit 0
			if err != nil {
				t.Errorf("Expected exit code 0 for %v, got error: %v\nOutput: %s", tt.args, err, output)
			}
		})
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

// TestCLICommitMsgHookAddsDocsVersion tests that commit-msg hook adds docs-version tag.
func TestCLICommitMsgHookAddsDocsVersion(t *testing.T) {
	cliBinary := getCliBinary(t)

	// Create temp workspace
	tempDir := t.TempDir()

	// Initialize git repo
	runGitCommand(t, tempDir, "init")
	runGitCommand(t, tempDir, "config", "user.email", "test@example.com")
	runGitCommand(t, tempDir, "config", "user.name", "Test User")

	// Create docs directory and file
	docsDir := filepath.Join(tempDir, "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatalf("Failed to create docs directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(docsDir, "readme.md"), []byte("# Hello"), 0644); err != nil {
		t.Fatalf("Failed to create docs file: %v", err)
	}

	// Create .kkachi.json
	config := `{
		"server_url": "http://localhost:5789",
		"workspace_id": "test-workspace",
		"project": "test-project",
		"actor_email": "test@example.com",
		"docs_dir": "docs",
		"docs_hash_file": ".kkachi_docs_hash"
	}`
	if err := os.WriteFile(filepath.Join(tempDir, ".kkachi.json"), []byte(config), 0644); err != nil {
		t.Fatalf("Failed to create .kkachi.json: %v", err)
	}

	// Create .kkachi_docs_hash
	docsHash := "abc123def456789\n"
	if err := os.WriteFile(filepath.Join(tempDir, ".kkachi_docs_hash"), []byte(docsHash), 0644); err != nil {
		t.Fatalf("Failed to create .kkachi_docs_hash: %v", err)
	}

	// Stage docs
	runGitCommand(t, tempDir, "add", "docs/")

	// Create commit message file
	msgFile := filepath.Join(tempDir, "COMMIT_EDITMSG")
	originalMsg := "Add documentation\n\nThis adds initial docs."
	if err := os.WriteFile(msgFile, []byte(originalMsg), 0644); err != nil {
		t.Fatalf("Failed to create commit message file: %v", err)
	}

	// Run commit-msg hook
	cmd := exec.Command(cliBinary, "hook", "commit-msg", msgFile)
	cmd.Dir = tempDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("commit-msg hook failed: %v\nOutput: %s", err, output)
	}

	// Verify message was updated
	content, err := os.ReadFile(msgFile)
	if err != nil {
		t.Fatalf("Failed to read message file: %v", err)
	}

	if !strings.Contains(string(content), "docs-version: abc123def456789") {
		t.Errorf("Expected message to contain docs-version tag, got:\n%s", content)
	}
}

// TestCLICommitMsgHookNoConfigExitsZero tests that commit-msg exits 0 when no config.
func TestCLICommitMsgHookNoConfigExitsZero(t *testing.T) {
	cliBinary := getCliBinary(t)

	// Create temp dir without kkachi config
	tempDir := t.TempDir()

	// Create commit message file
	msgFile := filepath.Join(tempDir, "COMMIT_EDITMSG")
	originalMsg := "Some commit message"
	if err := os.WriteFile(msgFile, []byte(originalMsg), 0644); err != nil {
		t.Fatalf("Failed to create commit message file: %v", err)
	}

	// Run commit-msg hook
	cmd := exec.Command(cliBinary, "hook", "commit-msg", msgFile)
	cmd.Dir = tempDir
	output, err := cmd.CombinedOutput()

	// Should exit 0 (not block commit)
	if err != nil {
		t.Errorf("Expected exit 0 for commit-msg without config, got error: %v\nOutput: %s", err, output)
	}
}

// TestCLIPreCommitHookRequiresConfig tests that pre-commit exits 1 when config is missing.
func TestCLIPreCommitHookRequiresConfig(t *testing.T) {
	cliBinary := getCliBinary(t)

	// Run from temp dir without kkachi config
	tempDir := t.TempDir()

	cmd := exec.Command(cliBinary, "hook", "pre-commit")
	cmd.Dir = tempDir
	output, err := cmd.CombinedOutput()

	// Should exit non-zero (block commit)
	if err == nil {
		t.Error("Expected pre-commit to fail without config")
	}

	// Should mention config issue
	if !strings.Contains(string(output), "config") {
		t.Errorf("Expected output to mention config issue, got: %s", output)
	}
}

// runGitCommand runs a git command in the given directory.
func runGitCommand(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\nOutput: %s", args, err, output)
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
