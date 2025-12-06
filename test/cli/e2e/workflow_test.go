package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestE2ESetup verifies the E2E test environment is properly configured.
// This is a placeholder test that will be expanded as CLI features are implemented.
func TestE2ESetup(t *testing.T) {
	// Verify CLI binary exists
	cliBinary := getCliBinary(t)
	if cliBinary == "" {
		t.Skip("CLI binary not found")
	}

	// Verify server URL is configured (for future tests)
	serverURL := os.Getenv("KKACHI_E2E_BASE_URL")
	if serverURL == "" {
		serverURL = "http://127.0.0.1:5789" // default
	}
	t.Logf("E2E test environment: CLI=%s, Server=%s", cliBinary, serverURL)
}

// TestE2EVersionCommand is a simple E2E test that verifies the CLI runs correctly.
func TestE2EVersionCommand(t *testing.T) {
	cliBinary := getCliBinary(t)

	cmd := exec.Command(cliBinary, "version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI version command failed: %v\nOutput: %s", err, output)
	}

	if !strings.Contains(string(output), "kkachi version") {
		t.Errorf("Expected 'kkachi version' in output, got: %s", output)
	}
}

// TODO: Add E2E tests for full workflows as features are implemented:
// - TestE2EInitWorkflow: kkachi init with real server
// - TestE2EStatusWorkflow: kkachi status with real server
// - TestE2EPreCommitOutdated: pre-commit hook with outdated docs
// - TestE2EFixWorkflow: kkachi fix after conflict resolution
// - TestE2EPrePushBlocking: pre-push hook blocking push with pending fix

// getCliBinary returns the path to the kkachi CLI binary.
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
	possiblePaths := []string{
		"../../bin/kkachi",    // from test/cli/e2e
		"../../../bin/kkachi", // alternative
		"bin/kkachi",          // from project root
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

// getServerURL returns the kkachi-server URL for E2E tests.
func getServerURL(t *testing.T) string {
	t.Helper()

	url := os.Getenv("KKACHI_E2E_BASE_URL")
	if url == "" {
		url = "http://127.0.0.1:5789"
	}
	return url
}
