package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// WorkspaceConfig mirrors the client.WorkspaceConfig for test setup.
type WorkspaceConfig struct {
	SocketPath     string `json:"socket_path"`
	WorkspaceID    string `json:"workspace_id"`
	Project        string `json:"project"`
	ActorEmail     string `json:"actor_email"`
	DocsDir        string `json:"docs_dir"`
	DocsHashFile   string `json:"docs_hash_file,omitempty"`
	PendingFixFile string `json:"pending_fix_file,omitempty"`
}

// PendingFixState mirrors the client.PendingFixState for test setup.
type PendingFixState struct {
	BaseHash   string `json:"base_hash"`
	RemoteHash string `json:"remote_hash"`
	CreatedAt  string `json:"created_at"`
}

// setupTempWorkspace creates a temporary workspace with .sanho.json and .sanho_docs_hash.
func setupTempWorkspace(t *testing.T, socketPath, localHash string) string {
	t.Helper()

	tempDir := t.TempDir()

	// Create .sanho.json
	config := WorkspaceConfig{
		SocketPath:  socketPath,
		WorkspaceID: "test-workspace-123",
		Project:     "test-project",
		ActorEmail:  "test@example.com",
		DocsDir:     "docs",
	}
	configData, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, ".sanho.json"), configData, 0644); err != nil {
		t.Fatalf("Failed to write .sanho.json: %v", err)
	}

	// Create .sanho_docs_hash
	if err := os.WriteFile(filepath.Join(tempDir, ".sanho_docs_hash"), []byte(localHash), 0644); err != nil {
		t.Fatalf("Failed to write .sanho_docs_hash: %v", err)
	}

	// Create docs directory
	if err := os.MkdirAll(filepath.Join(tempDir, "docs"), 0755); err != nil {
		t.Fatalf("Failed to create docs directory: %v", err)
	}

	return tempDir
}

// setupFakeDaemon creates a fake sanhod that responds to /docs/head.
func setupFakeDaemon(t *testing.T, headHash string, statusCode int, errorCode string) *unixTestDaemon {
	t.Helper()

	daemon := newUnixTestDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/docs/head" {
			if statusCode != http.StatusOK {
				w.WriteHeader(statusCode)
				if errorCode != "" {
					if err := json.NewEncoder(w).Encode(map[string]string{"error": errorCode}); err != nil {
						t.Errorf("encode response: %v", err)
					}
				}
				return
			}
			w.Header().Set("Content-Type", "application/json")
			// Note: API uses "head" not "docs_head"
			if err := json.NewEncoder(w).Encode(map[string]string{"head": headHash}); err != nil {
				t.Errorf("encode response: %v", err)
			}
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))

	t.Cleanup(daemon.Close)
	return daemon
}

// TestHookPostCheckoutWithWorkspace verifies post-checkout hook in a real workspace.
func TestHookPostCheckoutWithWorkspace(t *testing.T) {
	cliBinary := getCliBinary(t)

	testCases := []struct {
		name           string
		localHash      string
		daemonHash     string
		daemonStatus   int
		errorCode      string
		expectInOutput []string
		expectExitZero bool
	}{
		{
			name:           "up to date",
			localHash:      "abc123def456",
			daemonHash:     "abc123def456",
			daemonStatus:   http.StatusOK,
			expectInOutput: []string{"up_to_date", "abc123def456"},
			expectExitZero: true,
		},
		{
			name:           "outdated",
			localHash:      "old-hash-111",
			daemonHash:     "new-hash-222",
			daemonStatus:   http.StatusOK,
			expectInOutput: []string{"outdated", "old-hash-111", "new-hash-222"},
			expectExitZero: true,
		},
		{
			name:           "daemon error - still exit 0",
			localHash:      "abc123",
			daemonHash:     "",
			daemonStatus:   http.StatusInternalServerError,
			expectInOutput: []string{"warning", "unknown"},
			expectExitZero: true,
		},
		{
			name:           "unknown project - still exit 0",
			localHash:      "abc123",
			daemonHash:     "",
			daemonStatus:   http.StatusBadRequest,
			errorCode:      "unknown_project",
			expectInOutput: []string{"warning", "not registered"},
			expectExitZero: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Setup fake daemon
			daemon := setupFakeDaemon(t, tc.daemonHash, tc.daemonStatus, tc.errorCode)

			// Setup temp workspace
			tempDir := setupTempWorkspace(t, daemon.SocketPath, tc.localHash)

			// Run hook
			cmd := exec.Command(cliBinary, "hook", "post-checkout")
			cmd.Dir = tempDir
			output, err := cmd.CombinedOutput()
			outputStr := string(output)

			// Check exit code
			if tc.expectExitZero && err != nil {
				t.Errorf("Expected exit 0, got error: %v\nOutput: %s", err, outputStr)
			}

			// Check output contains expected strings
			for _, expected := range tc.expectInOutput {
				if !strings.Contains(strings.ToLower(outputStr), strings.ToLower(expected)) {
					t.Errorf("Expected output to contain %q, got:\n%s", expected, outputStr)
				}
			}
		})
	}
}

// TestHookPostMergeWithWorkspace verifies post-merge hook in a real workspace.
func TestHookPostMergeWithWorkspace(t *testing.T) {
	cliBinary := getCliBinary(t)

	// Setup fake daemon (up to date)
	daemon := setupFakeDaemon(t, "abc123", http.StatusOK, "")

	// Setup temp workspace
	tempDir := setupTempWorkspace(t, daemon.SocketPath, "abc123")

	// Run hook
	cmd := exec.Command(cliBinary, "hook", "post-merge", "0") // squash flag
	cmd.Dir = tempDir
	output, err := cmd.CombinedOutput()

	if err != nil {
		t.Errorf("Expected exit 0, got error: %v\nOutput: %s", err, output)
	}

	if !strings.Contains(string(output), "up_to_date") {
		t.Errorf("Expected 'up_to_date' in output, got:\n%s", output)
	}
}

// TestHookPostRewriteReconcilesAllRewrites verifies every rewrite checks HEAD state.
func TestHookPostRewriteReconcilesAllRewrites(t *testing.T) {
	cliBinary := getCliBinary(t)

	// Setup fake daemon
	daemon := setupFakeDaemon(t, "abc123", http.StatusOK, "")

	testCases := []struct {
		name         string
		args         []string
		expectOutput bool // true = should show status, false = silent
	}{
		{
			name:         "rebase triggers status",
			args:         []string{"hook", "post-rewrite", "rebase"},
			expectOutput: true,
		},
		{
			name:         "rebase with mapping file triggers status",
			args:         []string{"hook", "post-rewrite", "rebase", "/tmp/mapping"},
			expectOutput: true,
		},
		{
			name:         "amend triggers status",
			args:         []string{"hook", "post-rewrite", "amend"},
			expectOutput: true,
		},
		{
			name:         "amend with mapping file triggers status",
			args:         []string{"hook", "post-rewrite", "amend", "/tmp/mapping"},
			expectOutput: true,
		},
		{
			name:         "no args triggers status",
			args:         []string{"hook", "post-rewrite"},
			expectOutput: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Setup temp workspace
			tempDir := setupTempWorkspace(t, daemon.SocketPath, "abc123")

			// Run hook
			cmd := exec.Command(cliBinary, tc.args...)
			cmd.Dir = tempDir
			output, err := cmd.CombinedOutput()
			outputStr := string(output)

			// Always exit 0
			if err != nil {
				t.Errorf("Expected exit 0, got error: %v\nOutput: %s", err, outputStr)
			}

			// Check output based on expectation
			hasStatusOutput := strings.Contains(outputStr, "post-rewrite") && strings.Contains(outputStr, "status")
			if tc.expectOutput && !hasStatusOutput {
				t.Errorf("Expected status output for %v, got:\n%s", tc.args, outputStr)
			}
			if !tc.expectOutput && len(strings.TrimSpace(outputStr)) > 0 {
				t.Errorf("Expected silent output for %v, got:\n%s", tc.args, outputStr)
			}
		})
	}
}

func TestHookPostMergeReconcilesHashAndReportsDaemon(t *testing.T) {
	cliBinary := getCliBinary(t)
	var reportMu sync.Mutex
	var reportedHash string
	daemon := newUnixTestDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/docs/head":
			_ = json.NewEncoder(w).Encode(map[string]string{"head": "docs-new"})
		case r.Method == http.MethodPut && r.URL.Path == "/workspaces/test-workspace-123/docs-hash":
			var body struct {
				DocsHash string `json:"docs_hash"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode workspace report: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			reportMu.Lock()
			reportedHash = body.DocsHash
			reportMu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(daemon.Close)

	tempDir := setupTempWorkspace(t, daemon.SocketPath, "docs-old")
	runGitCommand(t, tempDir, "init")
	runGitCommand(t, tempDir, "config", "user.email", "test@example.com")
	runGitCommand(t, tempDir, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(tempDir, "docs", "readme.md"), []byte("# New docs\n"), 0644); err != nil {
		t.Fatalf("write docs: %v", err)
	}
	runGitCommand(t, tempDir, "add", "docs/readme.md")
	runGitCommand(t, tempDir, "commit", "-m", "Sync docs", "-m", "docs-version: docs-new")

	cmd := exec.Command(cliBinary, "hook", "post-merge", "0")
	cmd.Dir = tempDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("post-merge hook failed: %v\nOutput: %s", err, output)
	}
	hash, err := os.ReadFile(filepath.Join(tempDir, ".sanho_docs_hash"))
	if err != nil {
		t.Fatalf("read reconciled hash: %v", err)
	}
	if got := strings.TrimSpace(string(hash)); got != "docs-new" {
		t.Fatalf("reconciled hash = %q, want docs-new", got)
	}
	reportMu.Lock()
	defer reportMu.Unlock()
	if reportedHash != "docs-new" {
		t.Fatalf("reported daemon hash = %q, want docs-new", reportedHash)
	}
}

func TestGitPullPostMergeHookReconcilesHashAndReportsDaemon(t *testing.T) {
	cliBinary := getCliBinary(t)
	var reportMu sync.Mutex
	var reportedHash string
	daemon := newUnixTestDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/docs/head":
			_ = json.NewEncoder(w).Encode(map[string]string{"head": "docs-new"})
		case r.Method == http.MethodPut && r.URL.Path == "/workspaces/test-workspace-123/docs-hash":
			var body struct {
				DocsHash string `json:"docs_hash"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			reportMu.Lock()
			reportedHash = body.DocsHash
			reportMu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(daemon.Close)

	workspaceDir := setupTempWorkspace(t, daemon.SocketPath, "docs-old")
	runGitCommand(t, workspaceDir, "init", "--initial-branch=main")
	runGitCommand(t, workspaceDir, "config", "user.email", "test@example.com")
	runGitCommand(t, workspaceDir, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(workspaceDir, "docs", "readme.md"), []byte("# Old docs\n"), 0644); err != nil {
		t.Fatalf("write initial docs: %v", err)
	}
	runGitCommand(t, workspaceDir, "add", "docs/readme.md")
	runGitCommand(t, workspaceDir, "commit", "-m", "Initial docs", "-m", "docs-version: docs-old")

	originDir := filepath.Join(t.TempDir(), "origin.git")
	runGitCommand(t, workspaceDir, "init", "--bare", "--initial-branch=main", originDir)
	runGitCommand(t, workspaceDir, "remote", "add", "origin", originDir)
	runGitCommand(t, workspaceDir, "push", "-u", "origin", "main")

	publisherDir := t.TempDir()
	runGitCommand(t, publisherDir, "clone", originDir, ".")
	runGitCommand(t, publisherDir, "config", "user.email", "publisher@example.com")
	runGitCommand(t, publisherDir, "config", "user.name", "Publisher")
	if err := os.WriteFile(filepath.Join(publisherDir, "docs", "readme.md"), []byte("# New docs\n"), 0644); err != nil {
		t.Fatalf("write remote docs: %v", err)
	}
	runGitCommand(t, publisherDir, "add", "docs/readme.md")
	runGitCommand(t, publisherDir, "commit", "-m", "Update docs", "-m", "docs-version: docs-new")
	runGitCommand(t, publisherDir, "push", "origin", "main")

	hookPath := filepath.Join(workspaceDir, ".git", "hooks", "post-merge")
	hook := fmt.Sprintf("#!/bin/sh\nexec %q hook post-merge \"$@\"\n", cliBinary)
	if err := os.WriteFile(hookPath, []byte(hook), 0755); err != nil {
		t.Fatalf("install post-merge hook: %v", err)
	}

	runGitCommand(t, workspaceDir, "pull", "--ff-only")

	hash, err := os.ReadFile(filepath.Join(workspaceDir, ".sanho_docs_hash"))
	if err != nil {
		t.Fatalf("read reconciled hash: %v", err)
	}
	if got := strings.TrimSpace(string(hash)); got != "docs-new" {
		t.Fatalf("reconciled hash after git pull = %q, want docs-new", got)
	}
	reportMu.Lock()
	defer reportMu.Unlock()
	if reportedHash != "docs-new" {
		t.Fatalf("reported daemon hash after git pull = %q, want docs-new", reportedHash)
	}
}

// TestHookWithPendingFix verifies hooks show pending fix warning.
func TestHookWithPendingFix(t *testing.T) {
	cliBinary := getCliBinary(t)

	// Setup fake daemon
	daemon := setupFakeDaemon(t, "abc123", http.StatusOK, "")

	// Setup temp workspace
	tempDir := setupTempWorkspace(t, daemon.SocketPath, "abc123")

	// Create .sanho_pending_fix
	pendingFix := PendingFixState{
		BaseHash:   "old-hash",
		RemoteHash: "new-hash",
		CreatedAt:  "2025-01-01T00:00:00Z",
	}
	pendingFixData, _ := json.MarshalIndent(pendingFix, "", "  ")
	if err := os.WriteFile(filepath.Join(tempDir, ".sanho_pending_fix"), pendingFixData, 0644); err != nil {
		t.Fatalf("Failed to write .sanho_pending_fix: %v", err)
	}

	// Run hook
	cmd := exec.Command(cliBinary, "hook", "post-checkout")
	cmd.Dir = tempDir
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	// Still exit 0
	if err != nil {
		t.Errorf("Expected exit 0, got error: %v\nOutput: %s", err, outputStr)
	}

	// Should mention pending fix
	if !strings.Contains(strings.ToLower(outputStr), "pending fix") {
		t.Errorf("Expected 'pending fix' warning in output, got:\n%s", outputStr)
	}
}

// TestHookWithConflictMarkers verifies hooks show conflict warning.
func TestHookWithConflictMarkers(t *testing.T) {
	cliBinary := getCliBinary(t)

	// Setup fake daemon
	daemon := setupFakeDaemon(t, "abc123", http.StatusOK, "")

	// Setup temp workspace
	tempDir := setupTempWorkspace(t, daemon.SocketPath, "abc123")

	// Create file with conflict markers in docs/
	conflictContent := `Some content
<<<<<<< HEAD
Local changes
=======
Remote changes
>>>>>>> remote
More content`
	if err := os.WriteFile(filepath.Join(tempDir, "docs", "conflicted.md"), []byte(conflictContent), 0644); err != nil {
		t.Fatalf("Failed to write conflicted file: %v", err)
	}

	// Run hook
	cmd := exec.Command(cliBinary, "hook", "post-checkout")
	cmd.Dir = tempDir
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	// Still exit 0
	if err != nil {
		t.Errorf("Expected exit 0, got error: %v\nOutput: %s", err, outputStr)
	}

	// Should mention conflict
	if !strings.Contains(strings.ToLower(outputStr), "conflict") {
		t.Errorf("Expected 'conflict' warning in output, got:\n%s", outputStr)
	}
}

// Prevent unused import error for fmt.
var _ = fmt.Sprint
