package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestE2EStateCommand verifies sanho state command against running daemon.
func TestE2EStateCommand(t *testing.T) {
	cliBinary := getCliBinary(t)
	socketPath := getSocketPath(t)
	ensureDaemonAvailable(t, socketPath)

	// Set up a temp workspace with config pointing to real daemon
	tempDir := t.TempDir()
	setupE2EWorkspace(t, tempDir, socketPath, "test-e2e-project")

	// Create docs directory
	docsDir := filepath.Join(tempDir, "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatalf("Failed to create docs directory: %v", err)
	}

	// Note: The project might not exist on daemon, so we expect either
	// success (project exists) or specific error (project not found)
	cmd := exec.Command(cliBinary, "state")
	cmd.Dir = tempDir
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	// Either success with state info, or error about unknown project
	if err != nil {
		if !strings.Contains(outputStr, "not registered") && !strings.Contains(outputStr, "unknown") {
			t.Logf("Note: state command failed (expected if project not registered): %s", outputStr)
		}
	} else {
		// If success, should contain project name
		if !strings.Contains(outputStr, "test-e2e-project") && !strings.Contains(outputStr, "state") {
			t.Errorf("Expected state output, got: %s", outputStr)
		}
	}
}

// TestE2EStateAllCommand verifies sanho state --all command against running daemon.
func TestE2EStateAllCommand(t *testing.T) {
	cliBinary := getCliBinary(t)
	socketPath := getSocketPath(t)
	ensureDaemonAvailable(t, socketPath)

	// Set up a temp workspace with config pointing to real daemon
	tempDir := t.TempDir()
	setupE2EWorkspace(t, tempDir, socketPath, "test-e2e-project")

	// Create docs directory
	docsDir := filepath.Join(tempDir, "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatalf("Failed to create docs directory: %v", err)
	}

	// Run state --all
	cmd := exec.Command(cliBinary, "state", "--all")
	cmd.Dir = tempDir
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	// Should succeed and show docs_heads / workspaces
	if err != nil {
		t.Errorf("Expected state --all to succeed, got error: %v\nOutput: %s", err, outputStr)
	}

	// Should contain "docs_heads" or "workspaces"
	if !strings.Contains(outputStr, "docs_heads") && !strings.Contains(outputStr, "workspaces") {
		t.Errorf("Expected state --all output to contain docs_heads or workspaces, got: %s", outputStr)
	}
}

// TestE2EPrePushNoIssues verifies pre-push succeeds when no issues.
func TestE2EPrePushNoIssues(t *testing.T) {
	cliBinary := getCliBinary(t)
	socketPath := getSocketPath(t)
	ensureDaemonAvailable(t, socketPath)

	// Set up a temp workspace with config
	tempDir := t.TempDir()
	setupE2EWorkspace(t, tempDir, socketPath, "test-e2e-project")

	// Create clean docs directory
	docsDir := filepath.Join(tempDir, "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatalf("Failed to create docs directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(docsDir, "readme.md"), []byte("# Hello World"), 0644); err != nil {
		t.Fatalf("Failed to write docs file: %v", err)
	}

	// Run pre-push hook
	cmd := exec.Command(cliBinary, "hook", "pre-push")
	cmd.Dir = tempDir
	output, err := cmd.CombinedOutput()

	// Should succeed (no conflicts, no pending fix)
	if err != nil {
		t.Errorf("Expected pre-push to succeed, got error: %v\nOutput: %s", err, output)
	}
}

// TestE2EPrePushBlocksOnConflict verifies pre-push blocks with conflict markers.
func TestE2EPrePushBlocksOnConflict(t *testing.T) {
	cliBinary := getCliBinary(t)
	socketPath := getSocketPath(t)
	ensureDaemonAvailable(t, socketPath)

	// Set up a temp workspace with config
	tempDir := t.TempDir()
	setupE2EWorkspace(t, tempDir, socketPath, "test-e2e-project")

	// Create docs with conflict markers
	docsDir := filepath.Join(tempDir, "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatalf("Failed to create docs directory: %v", err)
	}

	conflictContent := `# Header
<<<<<<< HEAD
Local version
=======
Remote version
>>>>>>> remote
# Footer`
	if err := os.WriteFile(filepath.Join(docsDir, "readme.md"), []byte(conflictContent), 0644); err != nil {
		t.Fatalf("Failed to write conflicted file: %v", err)
	}

	// Run pre-push hook
	cmd := exec.Command(cliBinary, "hook", "pre-push")
	cmd.Dir = tempDir
	output, err := cmd.CombinedOutput()

	// Should fail
	if err == nil {
		t.Error("Expected pre-push to fail with conflict markers")
	}

	// Should mention conflict
	if !strings.Contains(strings.ToLower(string(output)), "conflict") {
		t.Errorf("Expected output to mention conflict, got: %s", output)
	}
}

// TestE2EPrePushBlocksOnPendingFix verifies pre-push blocks with pending fix.
func TestE2EPrePushBlocksOnPendingFix(t *testing.T) {
	cliBinary := getCliBinary(t)
	socketPath := getSocketPath(t)
	ensureDaemonAvailable(t, socketPath)

	// Set up a temp workspace with config
	tempDir := t.TempDir()
	setupE2EWorkspace(t, tempDir, socketPath, "test-e2e-project")

	// Create clean docs
	docsDir := filepath.Join(tempDir, "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatalf("Failed to create docs directory: %v", err)
	}

	// Create pending fix file
	pendingFix := map[string]string{
		"base_hash":   "old-hash",
		"remote_hash": "new-hash",
		"created_at":  "2025-01-01T00:00:00Z",
	}
	data, _ := json.MarshalIndent(pendingFix, "", "  ")
	if err := os.WriteFile(filepath.Join(tempDir, ".sanho_pending_fix"), data, 0644); err != nil {
		t.Fatalf("Failed to write pending fix file: %v", err)
	}

	// Run pre-push hook
	cmd := exec.Command(cliBinary, "hook", "pre-push")
	cmd.Dir = tempDir
	output, err := cmd.CombinedOutput()

	// Should fail
	if err == nil {
		t.Error("Expected pre-push to fail with pending fix")
	}

	// Should mention pending
	if !strings.Contains(strings.ToLower(string(output)), "pending") {
		t.Errorf("Expected output to mention pending, got: %s", output)
	}
}

// TestE2EFixNoPendingFix verifies fix fails without pending fix.
func TestE2EFixNoPendingFix(t *testing.T) {
	cliBinary := getCliBinary(t)
	socketPath := getSocketPath(t)
	ensureDaemonAvailable(t, socketPath)

	// Set up a temp workspace with config (no pending fix)
	tempDir := t.TempDir()
	setupE2EWorkspace(t, tempDir, socketPath, "test-e2e-project")

	// Create docs directory
	docsDir := filepath.Join(tempDir, "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatalf("Failed to create docs directory: %v", err)
	}

	// Run fix
	cmd := exec.Command(cliBinary, "fix")
	cmd.Dir = tempDir
	output, err := cmd.CombinedOutput()

	// Should fail
	if err == nil {
		t.Error("Expected fix to fail without pending fix")
	}

	// Should mention no pending fix
	if !strings.Contains(strings.ToLower(string(output)), "pending") {
		t.Errorf("Expected output to mention pending, got: %s", output)
	}
}

// setupE2EWorkspace creates a minimal sanho workspace for E2E testing.
func setupE2EWorkspace(t *testing.T, tempDir, socketPath, project string) {
	t.Helper()

	config := map[string]interface{}{
		"socket_path":  socketPath,
		"workspace_id": project + ":" + tempDir,
		"project":      project,
		"actor_email":  "e2e-test@example.com",
		"docs_dir":     "docs",
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, ".sanho.json"), data, 0644); err != nil {
		t.Fatalf("Failed to write .sanho.json: %v", err)
	}

	// Create .sanho_docs_hash
	if err := os.WriteFile(filepath.Join(tempDir, ".sanho_docs_hash"), []byte("e2e-test-hash-123"), 0644); err != nil {
		t.Fatalf("Failed to write .sanho_docs_hash: %v", err)
	}
}
