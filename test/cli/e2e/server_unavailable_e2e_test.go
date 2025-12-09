package e2e

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestE2ECLI_StatusServerUnavailable checks UX when server is unreachable.
func TestE2ECLI_StatusServerUnavailable(t *testing.T) {
	cliBinary := getCliBinary(t)

	// Intentionally unreachable server (closed port)
	badServerURL := "http://127.0.0.1:1"

	workspaceDir := t.TempDir()
	// Minimal config/hash to allow status to run
	writeConfig(t, workspaceDir, badServerURL, "unreachable-project", "ws-"+filepath.Base(workspaceDir), "offline@example.com")
	writeDocsHash(t, workspaceDir, "deadbeef")

	cmd := exec.Command(cliBinary, "status")
	cmd.Dir = workspaceDir
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected status to fail when server unreachable")
	}
	outStr := strings.ToLower(string(out))
	if !strings.Contains(outStr, "failed to connect") && !strings.Contains(outStr, "unreachable") {
		t.Fatalf("expected message about server unreachable, got:\n%s", string(out))
	}
}
