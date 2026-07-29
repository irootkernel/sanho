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

func TestCLIClean_RemovesFilesAndHooks(t *testing.T) {
	cliBinary := getCliBinary(t)

	deleteCalls := 0
	daemon := newUnixTestDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/workspaces/") {
			deleteCalls++
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]bool{"ok": true})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer daemon.Close()

	wsDir := t.TempDir()

	// Minimal git hooks structure
	hooksDir := filepath.Join(wsDir, ".git", "hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		t.Fatal(err)
	}
	hookPath := filepath.Join(hooksDir, "pre-commit")
	hookContent := "#!/bin/sh\necho keep-me\nsanho hook pre-commit\n"
	if err := os.WriteFile(hookPath, []byte(hookContent), 0755); err != nil {
		t.Fatalf("failed to write hook file: %v", err)
	}

	// Workspace files
	config := `{
  "socket_path": "` + daemon.SocketPath + `",
  "workspace_id": "proj:` + wsDir + `",
  "project": "proj",
  "docs_dir": "docs",
  "docs_hash_file": ".sanho_docs_hash",
  "pending_fix_file": ".sanho_pending_fix"
}`
	if err := os.WriteFile(filepath.Join(wsDir, ".sanho.json"), []byte(config), 0644); err != nil {
		t.Fatalf("failed to write .sanho.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wsDir, ".sanho_docs_hash"), []byte("hash123\n"), 0644); err != nil {
		t.Fatalf("failed to write docs hash: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wsDir, ".sanho_pending_fix"), []byte("{}"), 0644); err != nil {
		t.Fatalf("failed to write pending fix: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(wsDir, "docs"), 0755); err != nil {
		t.Fatalf("failed to create docs dir: %v", err)
	}

	cmd := exec.Command(cliBinary, "clean", "--yes")
	cmd.Dir = wsDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("sanho clean failed: %v\nOutput: %s", err, output)
	}

	if deleteCalls != 1 {
		t.Fatalf("expected 1 DELETE call, got %d", deleteCalls)
	}

	// Files removed
	for _, p := range []string{".sanho.json", ".sanho_docs_hash", ".sanho_pending_fix"} {
		if _, err := os.Stat(filepath.Join(wsDir, p)); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be removed, stat err=%v", p, err)
		}
	}

	// Docs directory preserved by default
	if _, err := os.Stat(filepath.Join(wsDir, "docs")); err != nil {
		t.Fatalf("expected docs dir to remain, err=%v", err)
	}

	// Hook cleaned but other lines kept
	data, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("failed to read hook file: %v", err)
	}
	text := string(data)
	if strings.Contains(text, "sanho hook pre-commit") {
		t.Fatalf("expected sanho hook line removed, content:\n%s", text)
	}
	if !strings.Contains(text, "echo keep-me") {
		t.Fatalf("expected other hook lines preserved, content:\n%s", text)
	}
}

func TestCLIClean_OfflineSkipsDaemon(t *testing.T) {
	cliBinary := getCliBinary(t)

	daemon := newUnixTestDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("daemon should not be called in offline mode")
	}))
	defer daemon.Close()

	wsDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(wsDir, ".git", "hooks"), 0755); err != nil {
		t.Fatal(err)
	}
	cfg := `{
  "socket_path": "` + daemon.SocketPath + `",
  "workspace_id": "proj:` + wsDir + `",
  "project": "proj",
  "docs_dir": "docs",
  "docs_hash_file": ".sanho_docs_hash",
  "pending_fix_file": ".sanho_pending_fix"
}`
	if err := os.WriteFile(filepath.Join(wsDir, ".sanho.json"), []byte(cfg), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	cmd := exec.Command(cliBinary, "clean", "--yes", "--offline")
	cmd.Dir = wsDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("sanho clean offline failed: %v\nOutput: %s", err, output)
	}
}
