package e2e_test

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/irootkernel/sanho/internal/interface/http/dto"
)

// E2E: sanho clean should remove daemon workspace and local artifacts.
func TestE2E_CliClean_RemovesWorkspaceAndLocalFiles(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	baseURL, client, socketPath := requireDaemon(t, ctx)

	originPath, head := createOriginRepo(t, map[string]string{
		"docs/index.md": "# Clean E2E\n",
	})

	projectName := uniqueName("clean-project")
	repoID := uniqueName("clean-repo")

	addProject(t, client, baseURL, projectName, repoID, originPath)
	t.Cleanup(func() {
		req, _ := http.NewRequest(http.MethodDelete, baseURL+"/projects/"+projectName+"?force=true", nil)
		if resp, err := client.Do(req); err == nil {
			resp.Body.Close()
		}
	})

	// Prepare workspace dir
	wsDir := t.TempDir()
	runCmdE2E(t, wsDir, nil, "git", "init")
	runCmdE2E(t, wsDir, nil, "git", "config", "user.email", "dev@example.com")
	runCmdE2E(t, wsDir, nil, "git", "config", "user.name", "Dev User")

	wsID, _ := registerWorkspace(t, client, baseURL, dto.RegisterWorkspaceRequest{
		Project:    projectName,
		LocalPath:  wsDir,
		RepoURL:    originPath,
		ActorEmail: "dev@example.com",
	})

	// Seed sanho files and hooks
	configJSON := `{
  "socket_path": "` + socketPath + `",
  "workspace_id": "` + wsID + `",
  "project": "` + projectName + `",
  "actor_email": "dev@example.com",
  "docs_dir": "docs",
  "docs_hash_file": ".sanho_docs_hash",
  "pending_fix_file": ".sanho_pending_fix"
}`
	if err := os.WriteFile(filepath.Join(wsDir, ".sanho.json"), []byte(configJSON), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wsDir, ".sanho_docs_hash"), []byte(head+"\n"), 0644); err != nil {
		t.Fatalf("failed to write docs hash: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wsDir, ".sanho_pending_fix"), []byte("{}"), 0644); err != nil {
		t.Fatalf("failed to write pending fix: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(wsDir, "docs"), 0755); err != nil {
		t.Fatalf("failed to create docs dir: %v", err)
	}
	hooksDir := filepath.Join(wsDir, ".git", "hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		t.Fatalf("failed to create hooks dir: %v", err)
	}
	hookPath := filepath.Join(hooksDir, "pre-commit")
	hookContent := "#!/bin/sh\necho keep\nsanho hook pre-commit\n"
	if err := os.WriteFile(hookPath, []byte(hookContent), 0755); err != nil {
		t.Fatalf("failed to write hook: %v", err)
	}

	// Run sanho clean
	cliBin := getCliBinaryE2E(t)
	runCmdE2E(t, wsDir, nil, cliBin, "clean", "--yes", "--remove-docs")

	// Daemon workspace should be gone
	req, _ := http.NewRequest(http.MethodDelete, baseURL+"/workspaces/"+url.PathEscape(wsID), nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("daemon delete check failed: %v", err)
	}
	if resp.StatusCode != http.StatusNotFound {
		resp.Body.Close()
		t.Fatalf("expected 404 unknown_workspace after clean, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Local files removed
	for _, p := range []string{".sanho.json", ".sanho_docs_hash", ".sanho_pending_fix"} {
		if _, err := os.Stat(filepath.Join(wsDir, p)); !os.IsNotExist(err) {
			t.Fatalf("expected %s removed, err=%v", p, err)
		}
	}
	// Docs dir removed
	if _, err := os.Stat(filepath.Join(wsDir, "docs")); !os.IsNotExist(err) {
		t.Fatalf("expected docs dir removed, err=%v", err)
	}
	// Hook cleaned (sanho line removed, other content preserved)
	data, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("failed to read hook after clean: %v", err)
	}
	text := string(data)
	if strings.Contains(text, "sanho hook pre-commit") {
		t.Fatalf("expected sanho hook line removed, content:\n%s", text)
	}
	if !strings.Contains(text, "echo keep") {
		t.Fatalf("expected other hook content preserved, content:\n%s", text)
	}
}
