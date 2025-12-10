package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestE2ECLI_InitWorkflow verifies kkachi init happy path against real server.
func TestE2ECLI_InitWorkflow(t *testing.T) {
	cliBinary := getCliBinary(t)
	serverURL := getServerURL(t)
	ensureServerAvailable(t, serverURL)

	originPath, head := createOriginRepo(t, map[string]string{
		"docs/index.md": "# init workflow\n",
	})

	workspaceDir := t.TempDir()
	initGitRepo(t, workspaceDir)
	setGitUser(t, workspaceDir, "cli-init@example.com")
	runCmd(t, workspaceDir, "git", "remote", "add", "origin", originPath)

	project := "cli-init-" + strings.ReplaceAll(filepath.Base(workspaceDir), string(filepath.Separator), "_")

	// Ensure project is deleted even if the test fails midway.
	t.Cleanup(func() {
		deleteProjectViaCLI(t, cliBinary, serverURL, project, true)
	})

	cmd := exec.Command(cliBinary, "init",
		"--server-url", serverURL,
		"--project", project,
		"--docs-repo-url", originPath,
	)
	cmd.Dir = workspaceDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("kkachi init failed: %v\nOutput:\n%s", err, string(out))
	}

	// Config and hash files should exist
	if _, err := os.Stat(filepath.Join(workspaceDir, ".kkachi.json")); err != nil {
		t.Fatalf(".kkachi.json missing: %v", err)
	}
	hashBytes, err := os.ReadFile(filepath.Join(workspaceDir, ".kkachi_docs_hash"))
	if err != nil {
		t.Fatalf("read docs hash: %v", err)
	}
	if strings.TrimSpace(string(hashBytes)) != head {
		t.Fatalf("docs hash mismatch: got %s want %s", strings.TrimSpace(string(hashBytes)), head)
	}

	// Docs directory should have content from server snapshot
	body, err := os.ReadFile(filepath.Join(workspaceDir, "docs", "docs", "index.md"))
	if err != nil {
		t.Fatalf("read docs/index.md: %v", err)
	}
	if !strings.Contains(string(body), "# init workflow") {
		t.Fatalf("docs snapshot not applied: %s", string(body))
	}

	assertGitignoreHasEntries(t, workspaceDir, "# Kkachi", ".kkachi_docs_hash", ".kkachi.json")

	// Cleanup: delete project
	deleteProjectViaCLI(t, cliBinary, serverURL, project, true)
}

// TestE2ECLI_InitForce verifies init --force overwrites existing docs dir.
func TestE2ECLI_InitForce(t *testing.T) {
	cliBinary := getCliBinary(t)
	serverURL := getServerURL(t)
	ensureServerAvailable(t, serverURL)

	originPath, head := createOriginRepo(t, map[string]string{
		"docs/index.md": "# init force\n",
	})

	workspaceDir := t.TempDir()
	initGitRepo(t, workspaceDir)
	setGitUser(t, workspaceDir, "cli-init-force@example.com")
	runCmd(t, workspaceDir, "git", "remote", "add", "origin", originPath)

	// Pre-create docs dir with junk to ensure force clears it
	if err := os.MkdirAll(filepath.Join(workspaceDir, "docs"), 0755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspaceDir, "docs", "junk.txt"), []byte("junk"), 0644); err != nil {
		t.Fatalf("write junk: %v", err)
	}

	project := "cli-init-force-" + strings.ReplaceAll(filepath.Base(workspaceDir), string(filepath.Separator), "_")

	// Ensure project is deleted even if the test fails midway.
	t.Cleanup(func() {
		deleteProjectViaCLI(t, cliBinary, serverURL, project, true)
	})

	cmd := exec.Command(cliBinary, "init",
		"--server-url", serverURL,
		"--project", project,
		"--docs-repo-url", originPath,
		"--force",
	)
	cmd.Dir = workspaceDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("kkachi init --force failed: %v\nOutput:\n%s", err, string(out))
	}

	// Docs dir should be replaced with snapshot (no junk)
	if _, err := os.Stat(filepath.Join(workspaceDir, "docs", "junk.txt")); err == nil {
		t.Fatalf("junk file should have been removed by --force")
	}
	hashBytes, err := os.ReadFile(filepath.Join(workspaceDir, ".kkachi_docs_hash"))
	if err != nil {
		t.Fatalf("read docs hash: %v", err)
	}
	if strings.TrimSpace(string(hashBytes)) != head {
		t.Fatalf("docs hash mismatch: got %s want %s", strings.TrimSpace(string(hashBytes)), head)
	}

	assertGitignoreHasEntries(t, workspaceDir, "# Kkachi", ".kkachi_docs_hash", ".kkachi.json")

	// Cleanup
	deleteProjectViaCLI(t, cliBinary, serverURL, project, true)
}
