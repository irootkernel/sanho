package e2e

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestE2ECLI_StatusDaemonUnavailable checks UX when daemon is unreachable.
func TestE2ECLI_StatusDaemonUnavailable(t *testing.T) {
	cliBinary := getCliBinary(t)

	workspaceDir := t.TempDir()
	// Intentionally unavailable daemon socket.
	badSocketPath := filepath.Join(workspaceDir, "missing-sanhod.sock")
	// Minimal config/hash to allow status to run
	writeConfig(t, workspaceDir, badSocketPath, "unreachable-project", "ws-"+filepath.Base(workspaceDir), "offline@example.com")
	writeDocsHash(t, workspaceDir, "deadbeef")

	cmd := exec.Command(cliBinary, "status")
	cmd.Dir = workspaceDir
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected status to fail when daemon unreachable")
	}
	outStr := strings.ToLower(string(out))
	if !strings.Contains(outStr, "failed to connect") && !strings.Contains(outStr, "unreachable") {
		t.Fatalf("expected message about daemon unreachable, got:\n%s", string(out))
	}
}
