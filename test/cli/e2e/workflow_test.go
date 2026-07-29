package e2e

import (
	"context"
	"net"
	"net/http"
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
	socketPath := os.Getenv("SANHO_E2E_SOCKET")
	if socketPath == "" {
		t.Fatal("SANHO_E2E_SOCKET was not configured by the E2E test harness")
	}
	t.Logf("E2E test environment: CLI=%s, Server=%s", cliBinary, socketPath)
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

// getSocketPath returns the sanhod Unix socket for E2E tests.
func getSocketPath(t *testing.T) string {
	t.Helper()

	path := os.Getenv("SANHO_E2E_SOCKET")
	if path == "" {
		t.Fatal("SANHO_E2E_SOCKET was not configured by the E2E test harness")
	}
	if !filepath.IsAbs(path) {
		t.Fatalf("SANHO_E2E_SOCKET must be absolute: %q", path)
	}
	return path
}

// ensureServerAvailable checks connectivity to the daemon Unix socket.
func ensureServerAvailable(t *testing.T, socketPath string) {
	t.Helper()
	conn, err := net.DialTimeout("unix", socketPath, 2*time.Second)
	if err != nil {
		t.Fatalf("sanhod not reachable at %s: %v", socketPath, err)
	}
	_ = conn.Close()
}

func unixHTTPClient(socketPath string, timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var dialer net.Dialer
				return dialer.DialContext(ctx, "unix", socketPath)
			},
		},
	}
}
