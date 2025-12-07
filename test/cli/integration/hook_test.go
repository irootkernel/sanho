package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// WorkspaceConfig mirrors the client.WorkspaceConfig for test setup.
type WorkspaceConfig struct {
	ServerURL      string `json:"server_url"`
	WorkspaceID    string `json:"workspace_id"`
	Project        string `json:"project"`
	ActorEmail     string `json:"actor_email"`
	DocsDir        string `json:"docs_dir"`
	DocsHashFile   string `json:"docs_hash_file,omitempty"`
	PendingFixFile string `json:"pending_fix_file,omitempty"`
}

// PendingFixState mirrors the fs.PendingFixState for test setup.
type PendingFixState struct {
	BaseHash   string `json:"base_hash"`
	RemoteHash string `json:"remote_hash"`
	CreatedAt  string `json:"created_at"`
}

// setupTempWorkspace creates a temporary workspace with .kkachi.json and .kkachi_docs_hash.
func setupTempWorkspace(t *testing.T, serverURL, localHash string) string {
	t.Helper()

	tempDir := t.TempDir()

	// Create .kkachi.json
	config := WorkspaceConfig{
		ServerURL:   serverURL,
		WorkspaceID: "test-workspace-123",
		Project:     "test-project",
		ActorEmail:  "test@example.com",
		DocsDir:     "docs",
	}
	configData, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, ".kkachi.json"), configData, 0644); err != nil {
		t.Fatalf("Failed to write .kkachi.json: %v", err)
	}

	// Create .kkachi_docs_hash
	if err := os.WriteFile(filepath.Join(tempDir, ".kkachi_docs_hash"), []byte(localHash), 0644); err != nil {
		t.Fatalf("Failed to write .kkachi_docs_hash: %v", err)
	}

	// Create docs directory
	if err := os.MkdirAll(filepath.Join(tempDir, "docs"), 0755); err != nil {
		t.Fatalf("Failed to create docs directory: %v", err)
	}

	return tempDir
}

// setupFakeServer creates a fake kkachi-server that responds to /docs/head.
func setupFakeServer(t *testing.T, headHash string, statusCode int, errorCode string) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/docs/head" {
			if statusCode != http.StatusOK {
				w.WriteHeader(statusCode)
				if errorCode != "" {
					json.NewEncoder(w).Encode(map[string]string{"error": errorCode})
				}
				return
			}
			w.Header().Set("Content-Type", "application/json")
			// Note: API uses "head" not "docs_head"
			json.NewEncoder(w).Encode(map[string]string{"head": headHash})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))

	t.Cleanup(server.Close)
	return server
}

// TestHookPostCheckoutWithWorkspace verifies post-checkout hook in a real workspace.
func TestHookPostCheckoutWithWorkspace(t *testing.T) {
	cliBinary := getCliBinary(t)

	testCases := []struct {
		name           string
		localHash      string
		serverHash     string
		serverStatus   int
		errorCode      string
		expectInOutput []string
		expectExitZero bool
	}{
		{
			name:           "up to date",
			localHash:      "abc123def456",
			serverHash:     "abc123def456",
			serverStatus:   http.StatusOK,
			expectInOutput: []string{"up_to_date", "abc123def456"},
			expectExitZero: true,
		},
		{
			name:           "outdated",
			localHash:      "old-hash-111",
			serverHash:     "new-hash-222",
			serverStatus:   http.StatusOK,
			expectInOutput: []string{"outdated", "old-hash-111", "new-hash-222"},
			expectExitZero: true,
		},
		{
			name:           "server error - still exit 0",
			localHash:      "abc123",
			serverHash:     "",
			serverStatus:   http.StatusInternalServerError,
			expectInOutput: []string{"warning", "unknown"},
			expectExitZero: true,
		},
		{
			name:           "unknown project - still exit 0",
			localHash:      "abc123",
			serverHash:     "",
			serverStatus:   http.StatusBadRequest,
			errorCode:      "unknown_project",
			expectInOutput: []string{"warning", "not registered"},
			expectExitZero: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Setup fake server
			server := setupFakeServer(t, tc.serverHash, tc.serverStatus, tc.errorCode)

			// Setup temp workspace
			tempDir := setupTempWorkspace(t, server.URL, tc.localHash)

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

	// Setup fake server (up to date)
	server := setupFakeServer(t, "abc123", http.StatusOK, "")

	// Setup temp workspace
	tempDir := setupTempWorkspace(t, server.URL, "abc123")

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

// TestHookPostRewriteRebaseOnly verifies post-rewrite only runs status for rebase.
func TestHookPostRewriteRebaseOnly(t *testing.T) {
	cliBinary := getCliBinary(t)

	// Setup fake server
	server := setupFakeServer(t, "abc123", http.StatusOK, "")

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
			name:         "amend is silent",
			args:         []string{"hook", "post-rewrite", "amend"},
			expectOutput: false,
		},
		{
			name:         "amend with mapping file is silent",
			args:         []string{"hook", "post-rewrite", "amend", "/tmp/mapping"},
			expectOutput: false,
		},
		{
			name:         "no args is silent",
			args:         []string{"hook", "post-rewrite"},
			expectOutput: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Setup temp workspace
			tempDir := setupTempWorkspace(t, server.URL, "abc123")

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

// TestHookWithPendingFix verifies hooks show pending fix warning.
func TestHookWithPendingFix(t *testing.T) {
	cliBinary := getCliBinary(t)

	// Setup fake server
	server := setupFakeServer(t, "abc123", http.StatusOK, "")

	// Setup temp workspace
	tempDir := setupTempWorkspace(t, server.URL, "abc123")

	// Create .kkachi_pending_fix
	pendingFix := PendingFixState{
		BaseHash:   "old-hash",
		RemoteHash: "new-hash",
		CreatedAt:  "2025-01-01T00:00:00Z",
	}
	pendingFixData, _ := json.MarshalIndent(pendingFix, "", "  ")
	if err := os.WriteFile(filepath.Join(tempDir, ".kkachi_pending_fix"), pendingFixData, 0644); err != nil {
		t.Fatalf("Failed to write .kkachi_pending_fix: %v", err)
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

	// Setup fake server
	server := setupFakeServer(t, "abc123", http.StatusOK, "")

	// Setup temp workspace
	tempDir := setupTempWorkspace(t, server.URL, "abc123")

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
