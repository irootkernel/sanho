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
	socketPath := getSocketPath(t)
	ensureDaemonAvailable(t, socketPath)

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
	registerProjectViaCLI(t, cliBinary, socketPath, project, originPath, workspaceDir)
	wsID, currentHead := registerWorkspaceViaCLI(t, cliBinary, socketPath, project, workspaceDir)
	if currentHead == "" {
		currentHead = initialHead
	}

	// Prepare workspace config files
	writeConfig(t, workspaceDir, socketPath, project, wsID, "cli-pull@example.com")
	writeDocsHash(t, workspaceDir, currentHead)

	// Create docs directory (matching daemon content)
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
	deleteProjectViaCLI(t, cliBinary, socketPath, project, true)
}

// TestE2ECLI_PullOutdated tests pull when daemon has newer docs.
func TestE2ECLI_PullOutdated(t *testing.T) {
	cliBinary := getCliBinary(t)
	socketPath := getSocketPath(t)
	ensureDaemonAvailable(t, socketPath)

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
	registerProjectViaCLI(t, cliBinary, socketPath, project, originPath, workspaceDir)
	wsID, currentHead := registerWorkspaceViaCLI(t, cliBinary, socketPath, project, workspaceDir)
	if currentHead == "" {
		currentHead = initialHead
	}

	// Prepare workspace config files
	writeConfig(t, workspaceDir, socketPath, project, wsID, "cli-pull@example.com")
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
	newHead := pushDocsViaHTTP(t, socketPath, wsID, currentHead, map[string]string{
		"docs/index.md": "# updated by another workspace\n",
	}, "other@example.com")

	// Verify daemon has new head
	daemonHead := fetchHeadViaHTTP(t, socketPath, project)
	if daemonHead != newHead {
		t.Fatalf("daemon head %s != pushed head %s", daemonHead, newHead)
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
	newHashBytes, err := os.ReadFile(filepath.Join(workspaceDir, ".sanho_docs_hash"))
	if err != nil {
		t.Fatalf("read docs hash: %v", err)
	}
	localHash := strings.TrimSpace(string(newHashBytes))
	if localHash != newHead {
		t.Errorf("local hash %s != daemon head %s", localHash, newHead)
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
	deleteProjectViaCLI(t, cliBinary, socketPath, project, true)
}

func TestE2ECLI_PullThenPullCommitPreservesRemoteAddedFile(t *testing.T) {
	cliBinary := getCliBinary(t)
	socketPath := getSocketPath(t)
	ensureDaemonAvailable(t, socketPath)

	docsOrigin, initialDocsHead := createOriginRepo(t, map[string]string{
		"docs/index.md": "# initial docs\n",
	})
	appOrigin, _ := createOriginRepo(t, map[string]string{
		"docs/docs/index.md": "# initial docs\n",
		"app.txt":            "application\n",
	})
	appPublisher := filepath.Join(t.TempDir(), "publisher")
	runCmd(t, "", "git", "clone", appOrigin, appPublisher)
	setGitUser(t, appPublisher, "app-publisher@example.com")
	runCmd(
		t,
		appPublisher,
		"git",
		"commit",
		"--amend",
		"-m",
		"Initial application",
		"-m",
		"docs-version: "+initialDocsHead,
	)
	runCmd(t, appPublisher, "git", "push", "--force", "origin", "HEAD:main")

	workspaceDir := filepath.Join(t.TempDir(), "workspace")
	runCmd(t, "", "git", "clone", appOrigin, workspaceDir)
	setGitUser(t, workspaceDir, "cli-pull-baseline@example.com")

	project := "cli-pull-baseline-" + strings.ReplaceAll(filepath.Base(docsOrigin), string(filepath.Separator), "_")
	registerProjectViaCLI(t, cliBinary, socketPath, project, docsOrigin, workspaceDir)
	wsID, currentHead := registerWorkspaceViaCLI(t, cliBinary, socketPath, project, workspaceDir)
	if currentHead == "" {
		currentHead = initialDocsHead
	}
	writeConfig(t, workspaceDir, socketPath, project, wsID, "cli-pull-baseline@example.com")
	writeDocsHash(t, workspaceDir, currentHead)

	firstRemoteHead := pushDocsViaHTTP(t, socketPath, wsID, currentHead, map[string]string{
		"docs/index.md":     "# first remote update\n",
		"docs/preserved.md": "must survive\n",
	}, "first-remote@example.com")

	pull := exec.Command(cliBinary, "pull")
	pull.Dir = workspaceDir
	if output, err := pull.CombinedOutput(); err != nil {
		t.Fatalf("pull failed: %v\nOutput:\n%s", err, output)
	}

	secondRemoteHead := pushDocsViaHTTP(t, socketPath, wsID, firstRemoteHead, map[string]string{
		"docs/index.md":     "# second remote update\n",
		"docs/preserved.md": "must survive\n",
	}, "second-remote@example.com")

	pullCommit := exec.Command(cliBinary, "pull-commit")
	pullCommit.Dir = workspaceDir
	if output, err := pullCommit.CombinedOutput(); err != nil {
		t.Fatalf("pull-commit failed: %v\nOutput:\n%s", err, output)
	}

	if got := strings.TrimSpace(string(runCmd(
		t,
		workspaceDir,
		"git",
		"show",
		":docs/docs/preserved.md",
	))); got != "must survive" {
		t.Fatalf("preserved file in index = %q", got)
	}
	if staged := strings.TrimSpace(string(runCmd(
		t,
		workspaceDir,
		"git",
		"diff",
		"--cached",
		"--name-status",
	))); staged != "" {
		t.Fatalf("pull-commit left unexpected staged changes:\n%s", staged)
	}
	if got := strings.TrimSpace(string(runCmd(
		t,
		workspaceDir,
		"git",
		"log",
		"-1",
		"--format=%B",
	))); !strings.Contains(got, "docs-version: "+secondRemoteHead) {
		t.Fatalf("system commit does not record remote docs head:\n%s", got)
	}
}

// TestE2ECLI_PullBlockedByPendingFix tests pull is blocked when pending fix exists.
func TestE2ECLI_PullBlockedByPendingFix(t *testing.T) {
	cliBinary := getCliBinary(t)
	socketPath := getSocketPath(t)
	ensureDaemonAvailable(t, socketPath)

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
	registerProjectViaCLI(t, cliBinary, socketPath, project, originPath, workspaceDir)
	wsID, currentHead := registerWorkspaceViaCLI(t, cliBinary, socketPath, project, workspaceDir)
	if currentHead == "" {
		currentHead = initialHead
	}

	// Prepare workspace config files with pending fix
	writeConfig(t, workspaceDir, socketPath, project, wsID, "cli-pull@example.com")
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
	deleteProjectViaCLI(t, cliBinary, socketPath, project, true)
}

// TestE2ECLI_PullForce tests pull --force overwrites local changes.
func TestE2ECLI_PullForce(t *testing.T) {
	cliBinary := getCliBinary(t)
	socketPath := getSocketPath(t)
	ensureDaemonAvailable(t, socketPath)

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
	registerProjectViaCLI(t, cliBinary, socketPath, project, originPath, workspaceDir)
	wsID, currentHead := registerWorkspaceViaCLI(t, cliBinary, socketPath, project, workspaceDir)
	if currentHead == "" {
		currentHead = initialHead
	}

	// Prepare workspace config files
	writeConfig(t, workspaceDir, socketPath, project, wsID, "cli-pull@example.com")
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
	newHead := pushDocsViaHTTP(t, socketPath, wsID, currentHead, map[string]string{
		"docs/index.md": "# from daemon\n",
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

	// Verify docs content from daemon
	content, err := os.ReadFile(filepath.Join(docsDir, "index.md"))
	if err != nil {
		t.Fatalf("read docs: %v", err)
	}
	if !strings.Contains(string(content), "from daemon") {
		t.Errorf("docs not updated from daemon, got:\n%s", string(content))
	}

	// Verify hash updated
	hashBytes, _ := os.ReadFile(filepath.Join(workspaceDir, ".sanho_docs_hash"))
	if strings.TrimSpace(string(hashBytes)) != newHead {
		t.Errorf("hash not updated to %s", newHead)
	}

	// Cleanup
	deleteProjectViaCLI(t, cliBinary, socketPath, project, true)
}

// TestE2ECLI_PullBlockedByUntrackedFiles tests pull is blocked when untracked files exist in docs.
func TestE2ECLI_PullBlockedByUntrackedFiles(t *testing.T) {
	cliBinary := getCliBinary(t)
	socketPath := getSocketPath(t)
	ensureDaemonAvailable(t, socketPath)

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
	registerProjectViaCLI(t, cliBinary, socketPath, project, originPath, workspaceDir)
	wsID, currentHead := registerWorkspaceViaCLI(t, cliBinary, socketPath, project, workspaceDir)
	if currentHead == "" {
		currentHead = initialHead
	}

	// Prepare workspace config files
	writeConfig(t, workspaceDir, socketPath, project, wsID, "cli-pull@example.com")
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
	newHead := pushDocsViaHTTP(t, socketPath, wsID, currentHead, map[string]string{
		"docs/index.md": "# from daemon\n",
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
	deleteProjectViaCLI(t, cliBinary, socketPath, project, true)
}
