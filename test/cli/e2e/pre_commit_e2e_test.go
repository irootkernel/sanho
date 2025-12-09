package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestE2ECLI_PreCommitOutdatedCreatesPendingFix triggers outdated flow and pending fix creation.
func TestE2ECLI_PreCommitOutdatedCreatesPendingFix(t *testing.T) {
	cliBinary := getCliBinary(t)
	serverURL := getServerURL(t)
	ensureServerAvailable(t, serverURL)

	originPath, initialHead := createOriginRepo(t, map[string]string{
		"docs/index.md": "# base\n",
	})

	workspaceDir := t.TempDir()
	initGitRepo(t, workspaceDir)
	setGitUser(t, workspaceDir, "cli-precommit@example.com")
	runCmd(t, workspaceDir, "git", "remote", "add", "origin", originPath)

	project := "cli-precommit-" + strings.ReplaceAll(filepath.Base(workspaceDir), string(filepath.Separator), "_")

	// Register project/workspace
	registerProjectViaCLI(t, cliBinary, serverURL, project, originPath, workspaceDir)
	wsID, currentHead := registerWorkspaceViaCLI(t, cliBinary, serverURL, project, workspaceDir)
	if currentHead == "" {
		currentHead = initialHead
	}

	// Prepare local workspace state files
	writeConfig(t, workspaceDir, serverURL, project, wsID, "cli-precommit@example.com")
	writeDocsHash(t, workspaceDir, currentHead)

	// Local change: modify docs/index.md and stage it
	docsDir := filepath.Join(workspaceDir, "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(docsDir, "index.md"), []byte("# local change\n"), 0644); err != nil {
		t.Fatalf("write local docs: %v", err)
	}
	runCmd(t, workspaceDir, "git", "add", "docs/index.md")

	// Advance server HEAD by pushing new snapshot via HTTP (simulating another workspace)
	remoteHead := pushDocsViaHTTP(t, serverURL, wsID, currentHead, map[string]string{
		"docs/server.md": "# server update\n",
	}, "remote@example.com")

	// Run pre-commit (should detect outdated and create pending fix)
	cmd := exec.Command(cliBinary, "hook", "pre-commit")
	cmd.Dir = workspaceDir
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected pre-commit to return error due to outdated")
	}
	if !strings.Contains(strings.ToLower(string(out)), "pending fix") && !strings.Contains(strings.ToLower(string(out)), "outdated") {
		t.Fatalf("expected pre-commit output to mention pending/outdated, got:\n%s", string(out))
	}

	// Pending fix file should exist
	if _, err := os.Stat(filepath.Join(workspaceDir, ".kkachi_pending_fix")); err != nil {
		t.Fatalf("pending fix file not created: %v", err)
	}

	// Docs hash should be updated to remote head
	hashBytes, err := os.ReadFile(filepath.Join(workspaceDir, ".kkachi_docs_hash"))
	if err != nil {
		t.Fatalf("read docs hash: %v", err)
	}
	if strings.TrimSpace(string(hashBytes)) != remoteHead {
		t.Fatalf("docs hash not updated to remote head; got %s want %s", strings.TrimSpace(string(hashBytes)), remoteHead)
	}

	// Clean up project
	deleteProjectViaCLI(t, cliBinary, serverURL, project, true)
}
