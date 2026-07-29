package e2e

import (
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestE2ESetup verifies the E2E test environment is properly configured.
// This is a placeholder test that will be expanded as CLI features are implemented.
func TestE2ESetup(t *testing.T) {
	// Verify CLI binary exists
	cliBinary := getCliBinary(t)
	if cliBinary == "" {
		t.Skip("CLI binary not found")
	}

	// Verify TestMain configured either an isolated server or an explicit override.
	serverURL := os.Getenv("SANHO_E2E_BASE_URL")
	if serverURL == "" {
		t.Fatal("SANHO_E2E_BASE_URL was not configured by the E2E test harness")
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

	if !strings.Contains(string(output), "sanho version") {
		t.Errorf("Expected 'sanho version' in output, got: %s", output)
	}
}

// TODO: Add E2E tests for full workflows as features are implemented:
// - TestE2EInitWorkflow: sanho init with real server
// - TestE2EStatusWorkflow: sanho status with real server
// - TestE2EPreCommitOutdated: pre-commit hook with outdated docs
// - TestE2EFixWorkflow: sanho fix after conflict resolution
// - TestE2EPrePushBlocking: pre-push hook blocking push with pending fix

// getCliBinary returns the path to the sanho CLI binary.
func getCliBinary(t *testing.T) string {
	t.Helper()

	// Check environment variable first
	if binary := os.Getenv("SANHO_CLI_BINARY"); binary != "" {
		if _, err := os.Stat(binary); err == nil {
			return binary
		}
		t.Logf("SANHO_CLI_BINARY set but file not found: %s", binary)
	}

	// Try to find binary relative to project root
	possiblePaths := []string{
		"../../bin/sanho",    // from test/cli/e2e
		"../../../bin/sanho", // alternative
		"bin/sanho",          // from project root
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

// getServerURL returns the sanhod URL for E2E tests.
func getServerURL(t *testing.T) string {
	t.Helper()

	url := os.Getenv("SANHO_E2E_BASE_URL")
	if url == "" {
		t.Fatal("SANHO_E2E_BASE_URL was not configured by the E2E test harness")
	}
	return url
}

// ensureServerAvailable checks TCP connectivity to the given server URL and skips the test if unreachable.
func ensureServerAvailable(t *testing.T, rawURL string) {
	t.Helper()

	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" {
		t.Skipf("Skipping E2E: invalid server URL %q", rawURL)
	}

	hostPort := parsed.Host
	if !strings.Contains(hostPort, ":") {
		if parsed.Scheme == "https" {
			hostPort = hostPort + ":443"
		} else {
			hostPort = hostPort + ":80"
		}
	}

	conn, err := net.DialTimeout("tcp", hostPort, 2*time.Second)
	if err != nil {
		t.Skipf("Skipping E2E: sanhod not reachable at %s (%v)", rawURL, err)
	}
	_ = conn.Close()
}
