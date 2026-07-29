package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestPrePushConflictMarkersBlock verifies pre-push blocks when conflict markers exist.
func TestPrePushConflictMarkersBlock(t *testing.T) {
	cliBinary := getCliBinary(t)

	// Setup temp workspace with config
	tempDir := t.TempDir()
	setupKkachiConfig(t, tempDir, "http://localhost:5789")

	// Create docs with conflict markers
	docsDir := filepath.Join(tempDir, "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatalf("Failed to create docs directory: %v", err)
	}

	conflictContent := `# Readme
<<<<<<< HEAD
Local changes
=======
Remote changes
>>>>>>> remote
End of file`
	if err := os.WriteFile(filepath.Join(docsDir, "readme.md"), []byte(conflictContent), 0644); err != nil {
		t.Fatalf("Failed to write conflicted file: %v", err)
	}

	// Run pre-push hook
	cmd := exec.Command(cliBinary, "hook", "pre-push")
	cmd.Dir = tempDir
	output, err := cmd.CombinedOutput()

	// Should fail (exit 1)
	if err == nil {
		t.Error("Expected pre-push to fail with conflict markers, but it succeeded")
	}

	// Should mention conflict
	if !strings.Contains(strings.ToLower(string(output)), "conflict") {
		t.Errorf("Expected output to mention 'conflict', got: %s", output)
	}
}

// TestPrePushPendingFixBlock verifies pre-push blocks when pending fix exists.
func TestPrePushPendingFixBlock(t *testing.T) {
	cliBinary := getCliBinary(t)

	// Setup temp workspace with config
	tempDir := t.TempDir()
	setupKkachiConfig(t, tempDir, "http://localhost:5789")

	// Create docs directory (no conflicts)
	docsDir := filepath.Join(tempDir, "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatalf("Failed to create docs directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(docsDir, "readme.md"), []byte("# Hello"), 0644); err != nil {
		t.Fatalf("Failed to write docs file: %v", err)
	}

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

	// Run pre-push hook
	cmd := exec.Command(cliBinary, "hook", "pre-push")
	cmd.Dir = tempDir
	output, err := cmd.CombinedOutput()

	// Should fail (exit 1)
	if err == nil {
		t.Error("Expected pre-push to fail with pending fix, but it succeeded")
	}

	// Should mention pending fix
	if !strings.Contains(strings.ToLower(string(output)), "pending") {
		t.Errorf("Expected output to mention 'pending', got: %s", output)
	}
}

// TestPrePushSuccess verifies pre-push succeeds when no issues.
func TestPrePushSuccess(t *testing.T) {
	cliBinary := getCliBinary(t)

	// Setup temp workspace with config
	tempDir := t.TempDir()
	setupKkachiConfig(t, tempDir, "http://localhost:5789")

	// Create clean docs directory
	docsDir := filepath.Join(tempDir, "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatalf("Failed to create docs directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(docsDir, "readme.md"), []byte("# Hello"), 0644); err != nil {
		t.Fatalf("Failed to write docs file: %v", err)
	}

	// Run pre-push hook
	cmd := exec.Command(cliBinary, "hook", "pre-push")
	cmd.Dir = tempDir
	output, err := cmd.CombinedOutput()

	// Should succeed (exit 0)
	if err != nil {
		t.Errorf("Expected pre-push to succeed, got error: %v\nOutput: %s", err, output)
	}

	// Should mention passing
	if !strings.Contains(strings.ToLower(string(output)), "pass") {
		t.Errorf("Expected output to mention 'pass', got: %s", output)
	}
}

// TestFixNoPendingFix verifies fix fails when no pending fix exists.
func TestFixNoPendingFix(t *testing.T) {
	cliBinary := getCliBinary(t)

	// Setup temp workspace with config (no pending fix)
	tempDir := t.TempDir()
	setupKkachiConfig(t, tempDir, "http://localhost:5789")

	// Create clean docs directory
	docsDir := filepath.Join(tempDir, "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatalf("Failed to create docs directory: %v", err)
	}

	// Run fix
	cmd := exec.Command(cliBinary, "fix")
	cmd.Dir = tempDir
	output, err := cmd.CombinedOutput()

	// Should fail (exit 1)
	if err == nil {
		t.Error("Expected fix to fail without pending fix, but it succeeded")
	}

	// Should mention no pending fix
	if !strings.Contains(strings.ToLower(string(output)), "pending") {
		t.Errorf("Expected output to mention 'pending', got: %s", output)
	}
}

// TestFixWithConflictMarkers verifies fix fails when conflicts exist.
func TestFixWithConflictMarkers(t *testing.T) {
	cliBinary := getCliBinary(t)

	// Setup fake server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Should not reach server when conflicts exist
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Setup temp workspace with config and pending fix
	tempDir := t.TempDir()
	setupKkachiConfig(t, tempDir, server.URL)

	// Create docs with conflict markers
	docsDir := filepath.Join(tempDir, "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatalf("Failed to create docs directory: %v", err)
	}

	conflictContent := `<<<<<<< HEAD
Local
=======
Remote
>>>>>>> remote`
	if err := os.WriteFile(filepath.Join(docsDir, "readme.md"), []byte(conflictContent), 0644); err != nil {
		t.Fatalf("Failed to write conflicted file: %v", err)
	}

	// Create pending fix
	pendingFix := PendingFixState{
		BaseHash:   "old-hash",
		RemoteHash: "new-hash",
		CreatedAt:  "2025-01-01T00:00:00Z",
	}
	pendingFixData, _ := json.MarshalIndent(pendingFix, "", "  ")
	if err := os.WriteFile(filepath.Join(tempDir, ".kkachi_pending_fix"), pendingFixData, 0644); err != nil {
		t.Fatalf("Failed to write .kkachi_pending_fix: %v", err)
	}

	// Run fix
	cmd := exec.Command(cliBinary, "fix")
	cmd.Dir = tempDir
	output, err := cmd.CombinedOutput()

	// Should fail (exit 1)
	if err == nil {
		t.Error("Expected fix to fail with conflict markers, but it succeeded")
	}

	// Should mention conflict
	if !strings.Contains(strings.ToLower(string(output)), "conflict") {
		t.Errorf("Expected output to mention 'conflict', got: %s", output)
	}
}

// TestStateNoConfig verifies state shows friendly error without config.
func TestStateNoConfig(t *testing.T) {
	cliBinary := getCliBinary(t)

	// Run from temp dir without config
	tempDir := t.TempDir()

	cmd := exec.Command(cliBinary, "state")
	cmd.Dir = tempDir
	output, err := cmd.CombinedOutput()

	// Should fail (exit 1)
	if err == nil {
		t.Error("Expected state to fail without config, but it succeeded")
	}

	// Should mention not a workspace
	if !strings.Contains(strings.ToLower(string(output)), "workspace") {
		t.Errorf("Expected output to mention 'workspace', got: %s", output)
	}
}

// TestStateWithFakeServer verifies state shows project info with fake server.
func TestStateWithFakeServer(t *testing.T) {
	cliBinary := getCliBinary(t)

	// Setup fake server that returns state
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/state" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"docs_heads": map[string]string{
					"test-project": "abc123def456",
				},
				"workspaces": []map[string]interface{}{
					{
						"workspace_id":     "test-project:/tmp/test",
						"project":          "test-project",
						"docs_hash":        "abc123def456",
						"local_path":       "/tmp/test",
						"repo_url":         "git@github.com:test/repo.git",
						"last_actor_email": "test@example.com",
					},
				},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	// Setup temp workspace with config pointing to fake server
	tempDir := t.TempDir()
	setupKkachiConfig(t, tempDir, server.URL)

	// Create docs directory
	docsDir := filepath.Join(tempDir, "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatalf("Failed to create docs directory: %v", err)
	}

	// Run state
	cmd := exec.Command(cliBinary, "state")
	cmd.Dir = tempDir
	output, err := cmd.CombinedOutput()

	// Should succeed (exit 0)
	if err != nil {
		t.Errorf("Expected state to succeed, got error: %v\nOutput: %s", err, output)
	}

	// Should show project info
	outputStr := string(output)
	if !strings.Contains(outputStr, "test-project") {
		t.Errorf("Expected output to contain 'test-project', got: %s", outputStr)
	}
	if !strings.Contains(outputStr, "abc123def456") {
		t.Errorf("Expected output to contain docs_head hash, got: %s", outputStr)
	}
}

// TestStateAllWithFakeServer verifies state --all shows all projects.
func TestStateAllWithFakeServer(t *testing.T) {
	cliBinary := getCliBinary(t)

	// Setup fake server that returns multiple projects
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/state" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"docs_heads": map[string]string{
					"project-a": "hash-a",
					"project-b": "hash-b",
				},
				"workspaces": []map[string]interface{}{
					{
						"workspace_id":     "project-a:/tmp/a",
						"project":          "project-a",
						"docs_hash":        "hash-a",
						"local_path":       "/tmp/a",
						"repo_url":         "git@github.com:test/a.git",
						"last_actor_email": "a@example.com",
					},
					{
						"workspace_id":     "project-b:/tmp/b",
						"project":          "project-b",
						"docs_hash":        "hash-b",
						"local_path":       "/tmp/b",
						"repo_url":         "git@github.com:test/b.git",
						"last_actor_email": "b@example.com",
					},
				},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	// Setup temp workspace with config pointing to fake server
	tempDir := t.TempDir()
	setupKkachiConfig(t, tempDir, server.URL)

	// Create docs directory
	docsDir := filepath.Join(tempDir, "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatalf("Failed to create docs directory: %v", err)
	}

	// Run state --all
	cmd := exec.Command(cliBinary, "state", "--all")
	cmd.Dir = tempDir
	output, err := cmd.CombinedOutput()

	// Should succeed (exit 0)
	if err != nil {
		t.Errorf("Expected state --all to succeed, got error: %v\nOutput: %s", err, output)
	}

	// Should show both projects
	outputStr := string(output)
	if !strings.Contains(outputStr, "project-a") {
		t.Errorf("Expected output to contain 'project-a', got: %s", outputStr)
	}
	if !strings.Contains(outputStr, "project-b") {
		t.Errorf("Expected output to contain 'project-b', got: %s", outputStr)
	}
}

// setupKkachiConfig creates .kkachi.json and .kkachi_docs_hash in the temp directory.
func setupKkachiConfig(t *testing.T, tempDir, serverURL string) {
	t.Helper()

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
	if err := os.WriteFile(filepath.Join(tempDir, ".kkachi_docs_hash"), []byte("initial-hash-123"), 0644); err != nil {
		t.Fatalf("Failed to write .kkachi_docs_hash: %v", err)
	}
}

func TestStatusShowsProjectWorkspaceComparisons(t *testing.T) {
	cliBinary := getCliBinary(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/projects/test-project/status" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("workspace_id") != "test-workspace-123" {
			t.Errorf("workspace_id = %q", r.URL.Query().Get("workspace_id"))
		}
		if r.URL.Query().Get("docs_hash") != "initial-hash-123" {
			t.Errorf("docs_hash = %q", r.URL.Query().Get("docs_hash"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"project":                "test-project",
			"reference_workspace_id": "test-workspace-123",
			"reference_docs_hash":    "initial-hash-123",
			"docs_head":              "head-hash-456",
			"reference_to_head": map[string]interface{}{
				"status": "behind",
				"ahead":  0,
				"behind": 2,
			},
			"workspaces": []map[string]interface{}{
				{
					"workspace_id": "peer-ahead",
					"repo_url":     "git@github.com:org/ahead.git",
					"docs_hash":    "ahead-hash-123456",
					"relative_to_reference": map[string]interface{}{
						"status": "ahead",
						"ahead":  2,
						"behind": 0,
					},
					"relative_to_head": map[string]interface{}{
						"status": "same",
						"ahead":  0,
						"behind": 0,
					},
				},
				{
					"workspace_id": "test-workspace-123",
					"repo_url":     "https://github.com/org/current.git",
					"docs_hash":    "initial-hash-123",
					"relative_to_reference": map[string]interface{}{
						"status": "same",
						"ahead":  0,
						"behind": 0,
					},
					"relative_to_head": map[string]interface{}{
						"status": "behind",
						"ahead":  0,
						"behind": 2,
					},
				},
			},
		})
	}))
	defer server.Close()

	tempDir := t.TempDir()
	setupKkachiConfig(t, tempDir, server.URL)
	cmd := exec.Command(cliBinary, "status")
	cmd.Dir = tempDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("status failed: %v\n%s", err, output)
	}
	text := string(output)
	for _, expected := range []string{
		"project       : test-project",
		"docs head     : head-hash-456",
		"status        : outdated",
		"docs relation : behind 2",
		"ahead 2",
		"current (current)",
	} {
		if !strings.Contains(text, expected) {
			t.Errorf("output missing %q:\n%s", expected, text)
		}
	}
	if strings.Index(text, "ahead") > strings.Index(text, "current (current)") {
		t.Errorf("ahead workspace was not listed first:\n%s", text)
	}
}

func TestStatusFallsBackToDocsHeadForOlderServer(t *testing.T) {
	cliBinary := getCliBinary(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/projects/test-project/status":
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "not_found"})
		case "/docs/head":
			_ = json.NewEncoder(w).Encode(map[string]string{"head": "initial-hash-123"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	tempDir := t.TempDir()
	setupKkachiConfig(t, tempDir, server.URL)
	cmd := exec.Command(cliBinary, "status")
	cmd.Dir = tempDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("status failed: %v\n%s", err, output)
	}
	text := string(output)
	for _, expected := range []string{
		"status        : up_to_date",
		"docs relation : same",
		"server upgrade required for workspace comparisons",
	} {
		if !strings.Contains(text, expected) {
			t.Errorf("output missing %q:\n%s", expected, text)
		}
	}
}

// TestStateAllWithServerURLOutsideWorkspace verifies state --all --server-url works outside a workspace.
func TestStateAllWithServerURLOutsideWorkspace(t *testing.T) {
	cliBinary := getCliBinary(t)

	// Setup fake server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/state" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"docs_heads": map[string]string{
					"project-x": "hash-x",
				},
				"workspaces": []map[string]interface{}{
					{
						"workspace_id":     "project-x:/tmp/x",
						"project":          "project-x",
						"docs_hash":        "hash-x",
						"local_path":       "/tmp/x",
						"repo_url":         "git@github.com:test/x.git",
						"last_actor_email": "x@example.com",
					},
				},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	// Use temp dir WITHOUT any sanho config (not a workspace)
	tempDir := t.TempDir()
	// Don't call setupKkachiConfig - intentionally no .kkachi.json

	// Run state --all --server-url
	cmd := exec.Command(cliBinary, "state", "--all", "--server-url", server.URL)
	cmd.Dir = tempDir
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	// Should succeed (exit 0) even without .kkachi.json
	if err != nil {
		t.Errorf("Expected state --all --server-url to succeed without workspace, got error: %v\nOutput: %s", err, outputStr)
	}

	// Should show project info from server
	if !strings.Contains(outputStr, "project-x") {
		t.Errorf("Expected output to contain 'project-x', got: %s", outputStr)
	}
	if !strings.Contains(outputStr, "hash-x") {
		t.Errorf("Expected output to contain 'hash-x', got: %s", outputStr)
	}
}

// TestStateAllWithoutServerURLOutsideWorkspace verifies state --all without --server-url fails outside workspace.
func TestStateAllWithoutServerURLOutsideWorkspace(t *testing.T) {
	cliBinary := getCliBinary(t)

	// Use temp dir WITHOUT any sanho config (not a workspace)
	tempDir := t.TempDir()

	// Run state --all without --server-url
	cmd := exec.Command(cliBinary, "state", "--all")
	cmd.Dir = tempDir
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	// Should fail (exit 1)
	if err == nil {
		t.Error("Expected state --all to fail without workspace and without --server-url")
	}

	// Should provide helpful message about using --server-url
	if !strings.Contains(outputStr, "--server-url") {
		t.Errorf("Expected output to suggest '--server-url', got: %s", outputStr)
	}
}
