package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestE2ECLI_PullAlreadyUpToDate tests pull when docs are already synced.
func TestE2ECLI_PullAlreadyUpToDate(t *testing.T) {
	cliBinary := getCliBinary(t)
	serverURL := getServerURL(t)
	ensureServerAvailable(t, serverURL)

	// Create origin docs repo
	originPath, initialHead := createOriginRepo(t, map[string]string{
		"docs/index.md": "# initial docs\n",
	})

	// Create workspace
	workspaceDir := t.TempDir()
	initGitRepo(t, workspaceDir)
	setGitUser(t, workspaceDir, "cli-pull@example.com")
	runCmd(t, workspaceDir, "git", "remote", "add", "origin", originPath)

	project := "cli-pull-uptodate-" + strings.ReplaceAll(filepath.Base(workspaceDir), string(filepath.Separator), "_")

	// Register project and workspace
	registerProjectViaCLI(t, cliBinary, serverURL, project, originPath, workspaceDir)
	wsID, currentHead := registerWorkspaceViaCLI(t, cliBinary, serverURL, project, workspaceDir)
	if currentHead == "" {
		currentHead = initialHead
	}

	// Prepare workspace config files
	writeConfig(t, workspaceDir, serverURL, project, wsID, "cli-pull@example.com")
	writeDocsHash(t, workspaceDir, currentHead)

	// Create docs directory (matching server content)
	docsDir := filepath.Join(workspaceDir, "docs", "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(docsDir, "index.md"), []byte("# initial docs\n"), 0644); err != nil {
		t.Fatalf("write docs: %v", err)
	}

	// Run pull - should say already up to date
	cmd := exec.Command(cliBinary, "pull")
	cmd.Dir = workspaceDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("pull failed: %v\nOutput:\n%s", err, string(out))
	}

	if !strings.Contains(string(out), "Already up to date") {
		t.Errorf("Expected 'Already up to date' message, got:\n%s", string(out))
	}

	// Cleanup
	deleteProjectViaCLI(t, cliBinary, serverURL, project, true)
}

// TestE2ECLI_PullOutdated tests pull when server has newer docs.
func TestE2ECLI_PullOutdated(t *testing.T) {
	cliBinary := getCliBinary(t)
	serverURL := getServerURL(t)
	ensureServerAvailable(t, serverURL)

	// Create origin docs repo
	originPath, initialHead := createOriginRepo(t, map[string]string{
		"docs/index.md": "# initial docs\n",
	})

	// Create workspace
	workspaceDir := t.TempDir()
	initGitRepo(t, workspaceDir)
	setGitUser(t, workspaceDir, "cli-pull@example.com")
	runCmd(t, workspaceDir, "git", "remote", "add", "origin", originPath)

	project := "cli-pull-outdated-" + strings.ReplaceAll(filepath.Base(workspaceDir), string(filepath.Separator), "_")

	// Register project and workspace
	registerProjectViaCLI(t, cliBinary, serverURL, project, originPath, workspaceDir)
	wsID, currentHead := registerWorkspaceViaCLI(t, cliBinary, serverURL, project, workspaceDir)
	if currentHead == "" {
		currentHead = initialHead
	}

	// Prepare workspace config files
	writeConfig(t, workspaceDir, serverURL, project, wsID, "cli-pull@example.com")
	writeDocsHash(t, workspaceDir, currentHead)

	// Create local docs directory
	docsDir := filepath.Join(workspaceDir, "docs", "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(docsDir, "index.md"), []byte("# initial docs\n"), 0644); err != nil {
		t.Fatalf("write docs: %v", err)
	}

	// Track initial docs so the workspace is considered clean before pulling.
	runCmd(t, workspaceDir, "git", "add", "docs/")
	runCmd(t, workspaceDir, "git", "commit", "-m", "seed docs")

	// Push updated docs from "another workspace" via HTTP
	newHead := pushDocsViaHTTP(t, serverURL, wsID, currentHead, map[string]string{
		"docs/index.md": "# updated by another workspace\n",
	}, "other@example.com")

	// Verify server has new head
	serverHead := fetchHeadViaHTTP(t, serverURL, project)
	if serverHead != newHead {
		t.Fatalf("server head %s != pushed head %s", serverHead, newHead)
	}

	// Run pull - should download new content
	cmd := exec.Command(cliBinary, "pull")
	cmd.Dir = workspaceDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("pull failed: %v\nOutput:\n%s", err, string(out))
	}

	if !strings.Contains(string(out), "Pull completed") {
		t.Errorf("Expected 'Pull completed' message, got:\n%s", string(out))
	}

	// Verify local docs hash updated
	newHashBytes, err := os.ReadFile(filepath.Join(workspaceDir, ".kkachi_docs_hash"))
	if err != nil {
		t.Fatalf("read docs hash: %v", err)
	}
	localHash := strings.TrimSpace(string(newHashBytes))
	if localHash != newHead {
		t.Errorf("local hash %s != server head %s", localHash, newHead)
	}

	// Verify docs content updated
	content, err := os.ReadFile(filepath.Join(docsDir, "index.md"))
	if err != nil {
		t.Fatalf("read docs content: %v", err)
	}
	if !strings.Contains(string(content), "updated by another workspace") {
		t.Errorf("docs content not updated, got:\n%s", string(content))
	}

	// Cleanup
	deleteProjectViaCLI(t, cliBinary, serverURL, project, true)
}

// TestE2ECLI_PullBlockedByPendingFix tests pull is blocked when pending fix exists.
func TestE2ECLI_PullBlockedByPendingFix(t *testing.T) {
	cliBinary := getCliBinary(t)
	serverURL := getServerURL(t)
	ensureServerAvailable(t, serverURL)

	// Create origin docs repo
	originPath, initialHead := createOriginRepo(t, map[string]string{
		"docs/index.md": "# initial\n",
	})

	// Create workspace
	workspaceDir := t.TempDir()
	initGitRepo(t, workspaceDir)
	setGitUser(t, workspaceDir, "cli-pull@example.com")
	runCmd(t, workspaceDir, "git", "remote", "add", "origin", originPath)

	project := "cli-pull-pending-" + strings.ReplaceAll(filepath.Base(workspaceDir), string(filepath.Separator), "_")

	// Register project and workspace
	registerProjectViaCLI(t, cliBinary, serverURL, project, originPath, workspaceDir)
	wsID, currentHead := registerWorkspaceViaCLI(t, cliBinary, serverURL, project, workspaceDir)
	if currentHead == "" {
		currentHead = initialHead
	}

	// Prepare workspace config files with pending fix
	writeConfig(t, workspaceDir, serverURL, project, wsID, "cli-pull@example.com")
	writeDocsHash(t, workspaceDir, currentHead)
	writePendingFix(t, workspaceDir, currentHead, currentHead)

	// Create docs directory
	docsDir := filepath.Join(workspaceDir, "docs", "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}

	// Run pull - should fail due to pending fix
	cmd := exec.Command(cliBinary, "pull")
	cmd.Dir = workspaceDir
	out, err := cmd.CombinedOutput()

	if err == nil {
		t.Errorf("Expected pull to fail with pending fix, but it succeeded")
	}

	if !strings.Contains(string(out), "pending fix") {
		t.Errorf("Expected message about pending fix, got:\n%s", string(out))
	}

	// Cleanup
	deleteProjectViaCLI(t, cliBinary, serverURL, project, true)
}

// TestE2ECLI_PullForce tests pull --force overwrites local changes.
func TestE2ECLI_PullForce(t *testing.T) {
	cliBinary := getCliBinary(t)
	serverURL := getServerURL(t)
	ensureServerAvailable(t, serverURL)

	// Create origin docs repo
	originPath, initialHead := createOriginRepo(t, map[string]string{
		"docs/index.md": "# initial\n",
	})

	// Create workspace
	workspaceDir := t.TempDir()
	initGitRepo(t, workspaceDir)
	setGitUser(t, workspaceDir, "cli-pull@example.com")
	runCmd(t, workspaceDir, "git", "remote", "add", "origin", originPath)

	project := "cli-pull-force-" + strings.ReplaceAll(filepath.Base(workspaceDir), string(filepath.Separator), "_")

	// Register project and workspace
	registerProjectViaCLI(t, cliBinary, serverURL, project, originPath, workspaceDir)
	wsID, currentHead := registerWorkspaceViaCLI(t, cliBinary, serverURL, project, workspaceDir)
	if currentHead == "" {
		currentHead = initialHead
	}

	// Prepare workspace config files
	writeConfig(t, workspaceDir, serverURL, project, wsID, "cli-pull@example.com")
	writeDocsHash(t, workspaceDir, currentHead)

	// Create local docs with modifications
	docsDir := filepath.Join(workspaceDir, "docs", "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(docsDir, "index.md"), []byte("# local modification\n"), 0644); err != nil {
		t.Fatalf("write docs: %v", err)
	}

	// Commit the initial docs to track them in git
	runCmd(t, workspaceDir, "git", "add", "docs/")
	runCmd(t, workspaceDir, "git", "commit", "-m", "add docs")

	// Make uncommitted local changes
	if err := os.WriteFile(filepath.Join(docsDir, "index.md"), []byte("# uncommitted local changes\n"), 0644); err != nil {
		t.Fatalf("write local changes: %v", err)
	}
	// Add an untracked file to ensure pull blocks when untracked docs exist
	if err := os.WriteFile(filepath.Join(docsDir, "untracked.md"), []byte("# untracked file\n"), 0644); err != nil {
		t.Fatalf("write untracked file: %v", err)
	}

	// Push updated docs from "another workspace"
	newHead := pushDocsViaHTTP(t, serverURL, wsID, currentHead, map[string]string{
		"docs/index.md": "# from server\n",
	}, "other@example.com")

	// Run pull without --force - MUST fail due to local changes
	cmd := exec.Command(cliBinary, "pull")
	cmd.Dir = workspaceDir
	out, err := cmd.CombinedOutput()

	if err == nil {
		t.Fatalf("Expected pull without --force to fail with local changes, but it succeeded.\nOutput:\n%s", string(out))
	}

	if !strings.Contains(string(out), "local") && !strings.Contains(string(out), "changes") {
		t.Errorf("Expected message about local changes, got:\n%s", string(out))
	}

	// Run pull with --force - should succeed
	cmd = exec.Command(cliBinary, "pull", "--force")
	cmd.Dir = workspaceDir
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("pull --force failed: %v\nOutput:\n%s", err, string(out))
	}

	// Verify docs content from server
	content, err := os.ReadFile(filepath.Join(docsDir, "index.md"))
	if err != nil {
		t.Fatalf("read docs: %v", err)
	}
	if !strings.Contains(string(content), "from server") {
		t.Errorf("docs not updated from server, got:\n%s", string(content))
	}

	// Verify hash updated
	hashBytes, _ := os.ReadFile(filepath.Join(workspaceDir, ".kkachi_docs_hash"))
	if strings.TrimSpace(string(hashBytes)) != newHead {
		t.Errorf("hash not updated to %s", newHead)
	}

	// Cleanup
	deleteProjectViaCLI(t, cliBinary, serverURL, project, true)
}

// TestE2ECLI_PullBlockedByUntrackedFiles tests pull is blocked when untracked files exist in docs.
func TestE2ECLI_PullBlockedByUntrackedFiles(t *testing.T) {
	cliBinary := getCliBinary(t)
	serverURL := getServerURL(t)
	ensureServerAvailable(t, serverURL)

	// Create origin docs repo
	originPath, initialHead := createOriginRepo(t, map[string]string{
		"docs/index.md": "# initial\n",
	})

	// Create workspace
	workspaceDir := t.TempDir()
	initGitRepo(t, workspaceDir)
	setGitUser(t, workspaceDir, "cli-pull@example.com")
	runCmd(t, workspaceDir, "git", "remote", "add", "origin", originPath)

	project := "cli-pull-untracked-" + strings.ReplaceAll(filepath.Base(workspaceDir), string(filepath.Separator), "_")

	// Register project and workspace
	registerProjectViaCLI(t, cliBinary, serverURL, project, originPath, workspaceDir)
	wsID, currentHead := registerWorkspaceViaCLI(t, cliBinary, serverURL, project, workspaceDir)
	if currentHead == "" {
		currentHead = initialHead
	}

	// Prepare workspace config files
	writeConfig(t, workspaceDir, serverURL, project, wsID, "cli-pull@example.com")
	writeDocsHash(t, workspaceDir, currentHead)

	// Create docs directory with tracked file
	docsDir := filepath.Join(workspaceDir, "docs", "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(docsDir, "index.md"), []byte("# initial\n"), 0644); err != nil {
		t.Fatalf("write docs: %v", err)
	}

	// Commit the docs to track them
	runCmd(t, workspaceDir, "git", "add", "docs/")
	runCmd(t, workspaceDir, "git", "commit", "-m", "add docs")

	// Create an UNTRACKED file in docs (not added to git)
	if err := os.WriteFile(filepath.Join(docsDir, "new_untracked.md"), []byte("# untracked file\n"), 0644); err != nil {
		t.Fatalf("write untracked file: %v", err)
	}

	// Push updated docs from "another workspace"
	newHead := pushDocsViaHTTP(t, serverURL, wsID, currentHead, map[string]string{
		"docs/index.md": "# from server\n",
	}, "other@example.com")
	_ = newHead // suppress unused warning

	// Run pull without --force - MUST fail due to untracked file
	cmd := exec.Command(cliBinary, "pull")
	cmd.Dir = workspaceDir
	out, err := cmd.CombinedOutput()

	if err == nil {
		t.Fatalf("Expected pull to fail with untracked files, but it succeeded.\nOutput:\n%s", string(out))
	}

	if !strings.Contains(string(out), "local") && !strings.Contains(string(out), "changes") {
		t.Errorf("Expected message about local changes, got:\n%s", string(out))
	}

	// Verify untracked file still exists (was not deleted)
	if _, err := os.Stat(filepath.Join(docsDir, "new_untracked.md")); os.IsNotExist(err) {
		t.Errorf("Untracked file was deleted! Data loss occurred.")
	}

	// Run pull with --force - should succeed
	cmd = exec.Command(cliBinary, "pull", "--force")
	cmd.Dir = workspaceDir
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("pull --force failed: %v\nOutput:\n%s", err, string(out))
	}

	// Cleanup
	deleteProjectViaCLI(t, cliBinary, serverURL, project, true)
}
