package integration

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestCLIPullRequiresWorkspace verifies pull command requires sanho workspace.
func TestCLIPullRequiresWorkspace(t *testing.T) {
	cliBinary := getCliBinary(t)

	// Run pull from temp dir (not a sanho workspace)
	tempDir := t.TempDir()
	cmd := exec.Command(cliBinary, "pull")
	cmd.Dir = tempDir
	output, err := cmd.CombinedOutput()

	if err == nil {
		t.Error("Expected non-zero exit code for pull without workspace")
	}

	if !strings.Contains(string(output), "not a sanho workspace") {
		t.Errorf("Expected 'not a sanho workspace' message, got: %s", output)
	}
}

// TestCLIPullAlreadyUpToDate verifies pull when already up to date.
func TestCLIPullAlreadyUpToDate(t *testing.T) {
	cliBinary := getCliBinary(t)

	// Create a fake daemon
	daemon := newUnixTestDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/docs/head"):
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(map[string]string{"head": "abc123def456"}); err != nil {
				t.Errorf("encode response: %v", err)
			}
		case r.Method == http.MethodPut && r.URL.Path == "/workspaces/test-workspace/docs-hash":
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(map[string]bool{"ok": true}); err != nil {
				t.Errorf("encode response: %v", err)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer daemon.Close()

	// Create temp workspace
	tempDir := t.TempDir()

	// Initialize git repo
	runGitCommand(t, tempDir, "init")
	runGitCommand(t, tempDir, "config", "user.email", "test@example.com")
	runGitCommand(t, tempDir, "config", "user.name", "Test User")

	// Create docs directory
	docsDir := filepath.Join(tempDir, "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatalf("Failed to create docs directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(docsDir, "readme.md"), []byte("# Hello"), 0644); err != nil {
		t.Fatalf("Failed to create docs file: %v", err)
	}

	// Create .sanho.json
	config := map[string]interface{}{
		"socket_path":      daemon.SocketPath,
		"workspace_id":     "test-workspace",
		"project":          "test-project",
		"actor_email":      "test@example.com",
		"docs_dir":         "docs",
		"docs_hash_file":   ".sanho_docs_hash",
		"pending_fix_file": ".sanho_pending_fix",
	}
	configBytes, _ := json.Marshal(config)
	if err := os.WriteFile(filepath.Join(tempDir, ".sanho.json"), configBytes, 0644); err != nil {
		t.Fatalf("Failed to create .sanho.json: %v", err)
	}

	// Create .sanho_docs_hash with same hash as daemon
	if err := os.WriteFile(filepath.Join(tempDir, ".sanho_docs_hash"), []byte("abc123def456\n"), 0644); err != nil {
		t.Fatalf("Failed to create .sanho_docs_hash: %v", err)
	}

	// Run pull
	cmd := exec.Command(cliBinary, "pull")
	cmd.Dir = tempDir
	output, err := cmd.CombinedOutput()

	if err != nil {
		t.Fatalf("Pull command failed: %v\nOutput: %s", err, output)
	}

	if !strings.Contains(string(output), "Already up to date") {
		t.Errorf("Expected 'Already up to date' message, got: %s", output)
	}
}

// TestCLIPullPendingFixBlocks verifies pull is blocked when pending fix exists.
func TestCLIPullPendingFixBlocks(t *testing.T) {
	cliBinary := getCliBinary(t)

	// Create a fake daemon
	daemon := newUnixTestDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/docs/head"):
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(map[string]string{"head": "def456"}); err != nil {
				t.Errorf("encode response: %v", err)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer daemon.Close()

	// Create temp workspace
	tempDir := t.TempDir()

	// Initialize git repo
	runGitCommand(t, tempDir, "init")
	runGitCommand(t, tempDir, "config", "user.email", "test@example.com")
	runGitCommand(t, tempDir, "config", "user.name", "Test User")

	// Create docs directory
	docsDir := filepath.Join(tempDir, "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatalf("Failed to create docs directory: %v", err)
	}

	// Create .sanho.json
	config := map[string]interface{}{
		"socket_path":      daemon.SocketPath,
		"workspace_id":     "test-workspace",
		"project":          "test-project",
		"actor_email":      "test@example.com",
		"docs_dir":         "docs",
		"docs_hash_file":   ".sanho_docs_hash",
		"pending_fix_file": ".sanho_pending_fix",
	}
	configBytes, _ := json.Marshal(config)
	if err := os.WriteFile(filepath.Join(tempDir, ".sanho.json"), configBytes, 0644); err != nil {
		t.Fatalf("Failed to create .sanho.json: %v", err)
	}

	// Create .sanho_docs_hash
	if err := os.WriteFile(filepath.Join(tempDir, ".sanho_docs_hash"), []byte("abc123\n"), 0644); err != nil {
		t.Fatalf("Failed to create .sanho_docs_hash: %v", err)
	}

	// Create .sanho_pending_fix
	pendingFix := map[string]interface{}{
		"base_hash":   "abc123",
		"remote_hash": "def456",
		"created_at":  "2025-01-01T00:00:00Z",
	}
	pendingFixBytes, _ := json.Marshal(pendingFix)
	if err := os.WriteFile(filepath.Join(tempDir, ".sanho_pending_fix"), pendingFixBytes, 0644); err != nil {
		t.Fatalf("Failed to create .sanho_pending_fix: %v", err)
	}

	// Run pull
	cmd := exec.Command(cliBinary, "pull")
	cmd.Dir = tempDir
	output, err := cmd.CombinedOutput()

	if err == nil {
		t.Error("Expected non-zero exit code when pending fix exists")
	}

	if !strings.Contains(string(output), "pending fix") {
		t.Errorf("Expected message about pending fix, got: %s", output)
	}
}

// TestCLIPullUnknownProject shows a helpful message when project is not registered.
func TestCLIPullUnknownProject(t *testing.T) {
	cliBinary := getCliBinary(t)

	// Fake daemon returns unknown_project for docs/head
	daemon := newUnixTestDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/docs/head") {
			w.WriteHeader(http.StatusBadRequest)
			if err := json.NewEncoder(w).Encode(map[string]string{"error": "unknown_project"}); err != nil {
				t.Errorf("encode response: %v", err)
			}
			return
		}
		http.NotFound(w, r)
	}))
	defer daemon.Close()

	tempDir := t.TempDir()

	// Initialize git repo
	runGitCommand(t, tempDir, "init")
	runGitCommand(t, tempDir, "config", "user.email", "test@example.com")
	runGitCommand(t, tempDir, "config", "user.name", "Test User")

	// Create docs directory
	docsDir := filepath.Join(tempDir, "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatalf("Failed to create docs directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(docsDir, "readme.md"), []byte("# Hello"), 0644); err != nil {
		t.Fatalf("Failed to create docs file: %v", err)
	}

	// Create .sanho.json
	config := map[string]interface{}{
		"socket_path":      daemon.SocketPath,
		"workspace_id":     "test-workspace",
		"project":          "unknown-project",
		"actor_email":      "test@example.com",
		"docs_dir":         "docs",
		"docs_hash_file":   ".sanho_docs_hash",
		"pending_fix_file": ".sanho_pending_fix",
	}
	configBytes, _ := json.Marshal(config)
	if err := os.WriteFile(filepath.Join(tempDir, ".sanho.json"), configBytes, 0644); err != nil {
		t.Fatalf("Failed to create .sanho.json: %v", err)
	}

	// Create .sanho_docs_hash
	if err := os.WriteFile(filepath.Join(tempDir, ".sanho_docs_hash"), []byte("abc123\n"), 0644); err != nil {
		t.Fatalf("Failed to create .sanho_docs_hash: %v", err)
	}

	// Run pull
	cmd := exec.Command(cliBinary, "pull")
	cmd.Dir = tempDir
	output, err := cmd.CombinedOutput()

	if err == nil {
		t.Fatalf("Expected non-zero exit code for unknown project, got success. Output: %s", output)
	}

	if !strings.Contains(string(output), "project 'unknown-project'") {
		t.Fatalf("Expected unknown project message, got: %s", output)
	}
}

// TestCLIPullUnknownWorkspace shows a helpful message when workspace is not registered.
func TestCLIPullUnknownWorkspace(t *testing.T) {
	cliBinary := getCliBinary(t)

	// Fake daemon returns unknown_workspace for docs/head
	daemon := newUnixTestDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/docs/head") {
			w.WriteHeader(http.StatusBadRequest)
			if err := json.NewEncoder(w).Encode(map[string]string{"error": "unknown_workspace"}); err != nil {
				t.Errorf("encode response: %v", err)
			}
			return
		}
		http.NotFound(w, r)
	}))
	defer daemon.Close()

	tempDir := t.TempDir()

	// Initialize git repo
	runGitCommand(t, tempDir, "init")
	runGitCommand(t, tempDir, "config", "user.email", "test@example.com")
	runGitCommand(t, tempDir, "config", "user.name", "Test User")

	// Create docs directory
	docsDir := filepath.Join(tempDir, "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatalf("Failed to create docs directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(docsDir, "readme.md"), []byte("# Hello"), 0644); err != nil {
		t.Fatalf("Failed to create docs file: %v", err)
	}

	// Create .sanho.json
	config := map[string]interface{}{
		"socket_path":      daemon.SocketPath,
		"workspace_id":     "test-workspace",
		"project":          "test-project",
		"actor_email":      "test@example.com",
		"docs_dir":         "docs",
		"docs_hash_file":   ".sanho_docs_hash",
		"pending_fix_file": ".sanho_pending_fix",
	}
	configBytes, _ := json.Marshal(config)
	if err := os.WriteFile(filepath.Join(tempDir, ".sanho.json"), configBytes, 0644); err != nil {
		t.Fatalf("Failed to create .sanho.json: %v", err)
	}

	// Create .sanho_docs_hash
	if err := os.WriteFile(filepath.Join(tempDir, ".sanho_docs_hash"), []byte("abc123\n"), 0644); err != nil {
		t.Fatalf("Failed to create .sanho_docs_hash: %v", err)
	}

	// Run pull
	cmd := exec.Command(cliBinary, "pull")
	cmd.Dir = tempDir
	output, err := cmd.CombinedOutput()

	if err == nil {
		t.Fatalf("Expected non-zero exit code for unknown workspace, got success. Output: %s", output)
	}

	if !strings.Contains(string(output), "workspace 'test-workspace'") {
		t.Fatalf("Expected unknown workspace message, got: %s", output)
	}
}

// TestCLIPullForceFlag verifies --force flag is recognized.
func TestCLIPullForceFlag(t *testing.T) {
	cliBinary := getCliBinary(t)

	// Run help for pull
	cmd := exec.Command(cliBinary, "pull", "--help")
	output, err := cmd.CombinedOutput()

	if err != nil {
		t.Fatalf("Pull help command failed: %v\nOutput: %s", err, output)
	}

	if !strings.Contains(string(output), "--force") {
		t.Errorf("Expected --force flag in help output, got: %s", output)
	}
}

// TestCLIPullInHelp verifies pull command appears in main help.
func TestCLIPullInHelp(t *testing.T) {
	cliBinary := getCliBinary(t)

	cmd := exec.Command(cliBinary, "--help")
	output, err := cmd.CombinedOutput()

	if err != nil {
		t.Fatalf("Help command failed: %v\nOutput: %s", err, output)
	}

	if !strings.Contains(string(output), "pull") {
		t.Errorf("Expected 'pull' in help output, got: %s", output)
	}
}
