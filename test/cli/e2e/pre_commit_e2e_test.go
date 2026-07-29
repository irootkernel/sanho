package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestE2ECLI_PreCommitOutdatedCreatesDocsBaseCommit verifies the automatic
// two-attempt commit flow while preserving the user's staged docs.
func TestE2ECLI_PreCommitOutdatedCreatesDocsBaseCommit(t *testing.T) {
	cliBinary := getCliBinary(t)
	socketPath := getSocketPath(t)
	ensureServerAvailable(t, socketPath)

	originPath, initialHead := createOriginRepo(t, map[string]string{
		"docs/index.md": "# base\n",
	})

	workspaceDir := t.TempDir()
	initGitRepo(t, workspaceDir)
	setGitUser(t, workspaceDir, "cli-precommit@example.com")
	if err := os.WriteFile(filepath.Join(workspaceDir, "app.txt"), []byte("base\n"), 0644); err != nil {
		t.Fatalf("write app base: %v", err)
	}
	runCmd(t, workspaceDir, "git", "add", "app.txt")
	runCmd(t, workspaceDir, "git", "commit", "-m", "app base")
	appOrigin := filepath.Join(t.TempDir(), "app-origin.git")
	runCmd(t, "", "git", "init", "--bare", "--initial-branch=main", appOrigin)
	runCmd(t, workspaceDir, "git", "remote", "add", "origin", appOrigin)
	runCmd(t, workspaceDir, "git", "push", "-u", "origin", "main")

	project := "cli-precommit-" + strings.ReplaceAll(filepath.Base(workspaceDir), string(filepath.Separator), "_")

	// Register project/workspace
	registerProjectViaCLI(t, cliBinary, socketPath, project, originPath, workspaceDir)
	wsID, currentHead := registerWorkspaceViaCLI(t, cliBinary, socketPath, project, workspaceDir)
	if currentHead == "" {
		currentHead = initialHead
	}

	// Prepare local workspace state files
	writeConfig(t, workspaceDir, socketPath, project, wsID, "cli-precommit@example.com")
	writeDocsHash(t, workspaceDir, currentHead)
	hooksDir := filepath.Join(workspaceDir, ".git", "hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		t.Fatalf("mkdir hooks: %v", err)
	}
	for name, command := range map[string]string{
		"pre-commit":  "hook pre-commit",
		"post-commit": "hook post-commit",
	} {
		script := fmt.Sprintf("#!/bin/sh\nexec %q %s\n", cliBinary, command)
		if err := os.WriteFile(filepath.Join(hooksDir, name), []byte(script), 0755); err != nil {
			t.Fatalf("write %s hook: %v", name, err)
		}
	}

	// Local change: modify docs/index.md and stage it
	docsDir := filepath.Join(workspaceDir, "docs", "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(docsDir, "index.md"), []byte("# local change\n"), 0644); err != nil {
		t.Fatalf("write local docs: %v", err)
	}
	runCmd(t, workspaceDir, "git", "add", "docs/docs/index.md")

	// Advance server HEAD by pushing new snapshot via HTTP (simulating another workspace)
	remoteHead := pushDocsViaHTTP(t, socketPath, wsID, currentHead, map[string]string{
		"docs/index.md":  "# base\n",
		"docs/server.md": "# server update\n",
	}, "remote@example.com")

	// First pre-commit attempt creates only the remote docs base commit and stops.
	cmd := exec.Command("git", "commit", "-m", "local docs change")
	cmd.Dir = workspaceDir
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected pre-commit to return error due to outdated")
	}
	if !strings.Contains(string(out), "created docs base commit") ||
		!strings.Contains(string(out), "Run the same git commit command again") {
		t.Fatalf("expected pre-commit output to explain the retry, got:\n%s", string(out))
	}

	// The compatibility pending-fix path is not used by pull-commit.
	if _, err := os.Stat(filepath.Join(workspaceDir, ".sanho_pending_fix")); !os.IsNotExist(err) {
		t.Fatalf("legacy pending fix file should not exist: %v", err)
	}

	// Docs hash should be updated to remote head
	hashBytes, err := os.ReadFile(filepath.Join(workspaceDir, ".sanho_docs_hash"))
	if err != nil {
		t.Fatalf("read docs hash: %v", err)
	}
	if strings.TrimSpace(string(hashBytes)) != remoteHead {
		t.Fatalf("docs hash not updated to remote head; got %s want %s", strings.TrimSpace(string(hashBytes)), remoteHead)
	}
	subject := strings.TrimSpace(string(runCmd(t, workspaceDir, "git", "show", "-s", "--format=%s", "HEAD")))
	if subject != "[SANHO] Update docs" {
		t.Fatalf("system commit subject = %q", subject)
	}
	remoteContent := strings.TrimSpace(string(runCmd(t, workspaceDir, "git", "show", "HEAD:docs/docs/server.md")))
	if remoteContent != "# server update" {
		t.Fatalf("system commit remote content = %q", remoteContent)
	}

	// The second attempt restores the merged staged docs into Git's prepared
	// commit index, performs the normal docs push, and finishes the user commit.
	cmd = exec.Command("git", "commit", "-m", "local docs change")
	cmd.Dir = workspaceDir
	if secondOut, secondErr := cmd.CombinedOutput(); secondErr != nil {
		t.Fatalf("second git commit failed: %v\n%s", secondErr, secondOut)
	}
	committedContent := strings.TrimSpace(string(runCmd(t, workspaceDir, "git", "show", "HEAD:docs/docs/index.md")))
	if committedContent != "# local change" {
		t.Fatalf("local docs were not committed after retry: %q", committedContent)
	}
	transactionDir := strings.TrimSpace(string(runCmd(t, workspaceDir, "git", "rev-parse", "--git-path", "sanho/pull-commit")))
	if !filepath.IsAbs(transactionDir) {
		transactionDir = filepath.Join(workspaceDir, transactionDir)
	}
	if _, err := os.Stat(filepath.Join(transactionDir, "state.json")); !os.IsNotExist(err) {
		t.Fatalf("pull-commit transaction was not cleared: %v", err)
	}

	// Clean up project
	deleteProjectViaCLI(t, cliBinary, socketPath, project, true)
}
