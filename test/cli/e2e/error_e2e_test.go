package e2e

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestE2ECLI_StatusUnknownProject checks unknown_project handling.
func TestE2ECLI_StatusUnknownProject(t *testing.T) {
	cliBinary := getCliBinary(t)
	socketPath := getSocketPath(t)
	ensureServerAvailable(t, socketPath)

	workspaceDir := t.TempDir()
	// minimal git to satisfy status conflict scan if needed
	initGitRepo(t, workspaceDir)
	setGitUser(t, workspaceDir, "cli-status@example.com")

	unknownProject := "unknown-project-" + filepath.Base(workspaceDir)
	writeConfig(t, workspaceDir, socketPath, unknownProject, "ws-"+workspaceDir, "cli-status@example.com")
	writeDocsHash(t, workspaceDir, "deadbeef")

	cmd := exec.Command(cliBinary, "status")
	cmd.Dir = workspaceDir
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected status to fail for unknown project")
	}
	if !strings.Contains(strings.ToLower(string(out)), "not registered") {
		t.Fatalf("expected message about project not registered, got:\n%s", string(out))
	}
}

// TestE2ECLI_WorkspaceUnregisterUnknown checks unknown workspace message.
func TestE2ECLI_WorkspaceUnregisterUnknown(t *testing.T) {
	cliBinary := getCliBinary(t)
	socketPath := getSocketPath(t)
	ensureServerAvailable(t, socketPath)

	cmd := exec.Command(cliBinary, "workspace", "unregister",
		"--socket", socketPath,
		"--workspace-id", "non-existent-workspace-id",
		"--yes",
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected unregister to fail for unknown workspace")
	}
	if !strings.Contains(strings.ToLower(string(out)), "not registered") {
		t.Fatalf("expected unknown workspace message, got:\n%s", string(out))
	}
}

// TestE2ECLI_ProjectDeleteUnknown checks unknown project delete message.
func TestE2ECLI_ProjectDeleteUnknown(t *testing.T) {
	cliBinary := getCliBinary(t)
	socketPath := getSocketPath(t)
	ensureServerAvailable(t, socketPath)

	cmd := exec.Command(cliBinary, "project", "delete",
		"--socket", socketPath,
		"--project", "non-existent-project-id",
		"--yes",
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected project delete to fail for unknown project")
	}
	if !strings.Contains(strings.ToLower(string(out)), "does not exist") {
		t.Fatalf("expected unknown project message, got:\n%s", string(out))
	}
}
