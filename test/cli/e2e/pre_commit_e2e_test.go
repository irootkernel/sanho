package e2e

import (
	"encoding/json"
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
	ensureDaemonAvailable(t, socketPath)

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

	// Advance daemon HEAD by pushing new snapshot via HTTP (simulating another workspace)
	remoteHead := pushDocsViaHTTP(t, socketPath, wsID, currentHead, map[string]string{
		"docs/index.md":  "# base\n",
		"docs/daemon.md": "# daemon update\n",
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
	remoteContent := strings.TrimSpace(string(runCmd(t, workspaceDir, "git", "show", "HEAD:docs/docs/daemon.md")))
	if remoteContent != "# daemon update" {
		t.Fatalf("system commit remote content = %q", remoteContent)
	}

	// A later hook failure must leave the prepared transaction retryable.
	commitMsgHook := filepath.Join(hooksDir, "commit-msg")
	rejectingCommitMsg := fmt.Sprintf("#!/bin/sh\n%q hook commit-msg \"$1\" || exit $?\nexit 1\n", cliBinary)
	if err := os.WriteFile(commitMsgHook, []byte(rejectingCommitMsg), 0755); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("git", "commit", "-m", "local docs change")
	cmd.Dir = workspaceDir
	if secondOut, secondErr := cmd.CombinedOutput(); secondErr == nil {
		t.Fatalf("commit-msg failure did not stop commit:\n%s", secondOut)
	}
	transactionDir := strings.TrimSpace(string(runCmd(t, workspaceDir, "git", "rev-parse", "--git-path", "sanho/pull-commit")))
	if !filepath.IsAbs(transactionDir) {
		transactionDir = filepath.Join(workspaceDir, transactionDir)
	}
	if _, err := os.Stat(filepath.Join(transactionDir, "state.json")); err != nil {
		t.Fatalf("failed commit lost transaction state: %v", err)
	}

	// Retrying the same commit command consumes the preserved index and clears
	// the transaction.
	normalCommitMsg := fmt.Sprintf("#!/bin/sh\nexec %q hook commit-msg \"$1\"\n", cliBinary)
	if err := os.WriteFile(commitMsgHook, []byte(normalCommitMsg), 0755); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("git", "commit", "-m", "local docs change")
	cmd.Dir = workspaceDir
	if thirdOut, thirdErr := cmd.CombinedOutput(); thirdErr != nil {
		t.Fatalf("retried git commit failed: %v\n%s", thirdErr, thirdOut)
	}
	committedContent := strings.TrimSpace(string(runCmd(t, workspaceDir, "git", "show", "HEAD:docs/docs/index.md")))
	if committedContent != "# local change" {
		t.Fatalf("local docs were not committed after retry: %q", committedContent)
	}
	if _, err := os.Stat(filepath.Join(transactionDir, "state.json")); !os.IsNotExist(err) {
		t.Fatalf("pull-commit transaction was not cleared: %v", err)
	}

	// Clean up project
	deleteProjectViaCLI(t, cliBinary, socketPath, project, true)
}

func TestE2ECLI_PreCommitOutdatedSupportsCommitAmend(t *testing.T) {
	cliBinary := getCliBinary(t)
	socketPath := getSocketPath(t)
	ensureDaemonAvailable(t, socketPath)

	for _, tc := range []struct {
		name              string
		amendDocsAndApp   bool
		consecutiveAmends bool
	}{
		{name: "single amend"},
		{name: "docs and non-doc amend", amendDocsAndApp: true},
		{name: "consecutive amend", consecutiveAmends: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			docsOrigin, initialDocsHead := createOriginRepo(t, map[string]string{
				"docs/index.md": "# base\n",
			})
			workspaceDir := t.TempDir()
			initGitRepo(t, workspaceDir)
			setGitUser(t, workspaceDir, "cli-amend@example.com")
			if err := os.MkdirAll(filepath.Join(workspaceDir, "docs", "docs"), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(workspaceDir, "docs", "docs", "index.md"), []byte("# base\n"), 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(workspaceDir, "app.txt"), []byte("base\n"), 0644); err != nil {
				t.Fatal(err)
			}
			runCmd(t, workspaceDir, "git", "add", "docs", "app.txt")
			runCmd(t, workspaceDir, "git", "commit", "-m", "app base")
			appOrigin := filepath.Join(t.TempDir(), "app-origin.git")
			runCmd(t, "", "git", "init", "--bare", "--initial-branch=main", appOrigin)
			runCmd(t, workspaceDir, "git", "remote", "add", "origin", appOrigin)
			runCmd(t, workspaceDir, "git", "push", "-u", "origin", "main")
			runCmd(t, workspaceDir, "git", "switch", "-c", "feature/amend")
			if err := os.WriteFile(filepath.Join(workspaceDir, "feature.txt"), []byte("prepared\n"), 0644); err != nil {
				t.Fatal(err)
			}
			runCmd(t, workspaceDir, "git", "add", "feature.txt")
			runCmd(t, workspaceDir, "git", "commit", "-m", "prepared feature")

			project := "cli-amend-" + strings.ReplaceAll(filepath.Base(workspaceDir), string(filepath.Separator), "_")
			registerProjectViaCLI(t, cliBinary, socketPath, project, docsOrigin, workspaceDir)
			t.Cleanup(func() { deleteProjectViaCLI(t, cliBinary, socketPath, project, true) })
			wsID, currentHead := registerWorkspaceViaCLI(t, cliBinary, socketPath, project, workspaceDir)
			if currentHead == "" {
				currentHead = initialDocsHead
			}
			writeConfig(t, workspaceDir, socketPath, project, wsID, "cli-amend@example.com")
			writeDocsHash(t, workspaceDir, currentHead)
			installPullCommitLifecycleHooks(t, workspaceDir, cliBinary)

			if err := os.WriteFile(filepath.Join(workspaceDir, "feature.txt"), []byte("amended\n"), 0644); err != nil {
				t.Fatal(err)
			}
			runCmd(t, workspaceDir, "git", "add", "feature.txt")
			if tc.amendDocsAndApp {
				if err := os.WriteFile(filepath.Join(workspaceDir, "docs", "docs", "index.md"), []byte("# local amend\n"), 0644); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(workspaceDir, "app.txt"), []byte("amended app\n"), 0644); err != nil {
					t.Fatal(err)
				}
				runCmd(t, workspaceDir, "git", "add", "docs/docs/index.md", "app.txt")
			}
			remoteHead := pushDocsViaHTTP(t, socketPath, wsID, currentHead, map[string]string{
				"docs/index.md":  "# base\n",
				"docs/remote.md": "# remote\n",
			}, "remote@example.com")

			first := exec.Command("git", "commit", "--amend", "-m", "amended feature")
			first.Dir = workspaceDir
			firstOutput, firstErr := first.CombinedOutput()
			if firstErr == nil || !strings.Contains(string(firstOutput), "Run the same git commit command again") {
				t.Fatalf("first amend did not prepare retry: err=%v\n%s", firstErr, firstOutput)
			}
			preparedHead := strings.TrimSpace(string(runCmd(t, workspaceDir, "git", "rev-parse", "HEAD")))

			second := exec.Command("git", "commit", "--amend", "-m", "amended feature")
			second.Dir = workspaceDir
			secondOutput, secondErr := second.CombinedOutput()
			if secondErr != nil {
				t.Fatalf("second amend failed: %v\n%s", secondErr, secondOutput)
			}
			amendedHead := strings.TrimSpace(string(runCmd(t, workspaceDir, "git", "rev-parse", "HEAD")))
			if amendedHead == preparedHead {
				t.Fatal("amend did not rewrite the prepared commit")
			}
			if got := strings.TrimSpace(string(runCmd(t, workspaceDir, "git", "show", "HEAD:feature.txt"))); got != "amended" {
				t.Fatalf("feature content=%q", got)
			}
			if got := strings.TrimSpace(string(runCmd(t, workspaceDir, "git", "show", "HEAD^:docs/docs/remote.md"))); got != "# remote" {
				t.Fatalf("docs sync base content=%q", got)
			}
			if got := strings.TrimSpace(string(runCmd(t, workspaceDir, "git", "show", "HEAD^", "-s", "--format=%s"))); got != "[SANHO] Update docs" {
				t.Fatalf("docs sync parent subject=%q", got)
			}
			if tc.amendDocsAndApp {
				if got := strings.TrimSpace(string(runCmd(t, workspaceDir, "git", "show", "HEAD:docs/docs/index.md"))); got != "# local amend" {
					t.Fatalf("amended docs=%q", got)
				}
				if got := strings.TrimSpace(string(runCmd(t, workspaceDir, "git", "show", "HEAD:app.txt"))); got != "amended app" {
					t.Fatalf("amended app=%q", got)
				}
			}
			assertNoPullCommitTransaction(t, workspaceDir)
			if tc.name == "single amend" {
				writeCompletedPullCommitState(t, workspaceDir, amendedHead, remoteHead)
				prePush := exec.Command(cliBinary, "hook", "pre-push")
				prePush.Dir = workspaceDir
				if output, err := prePush.CombinedOutput(); err != nil {
					t.Fatalf("pre-push rejected logically completed stale transaction: %v\n%s", err, output)
				}
				assertNoPullCommitTransaction(t, workspaceDir)
			}

			if tc.consecutiveAmends {
				if err := os.WriteFile(filepath.Join(workspaceDir, "feature.txt"), []byte("amended twice\n"), 0644); err != nil {
					t.Fatal(err)
				}
				runCmd(t, workspaceDir, "git", "add", "feature.txt")
				runCmd(t, workspaceDir, "git", "commit", "--amend", "-m", "amended feature twice")
				assertNoPullCommitTransaction(t, workspaceDir)
			}
			if got := strings.TrimSpace(string(runCmd(t, workspaceDir, "git", "show", "HEAD^", "-s", "--format=%B"))); !strings.Contains(got, "docs-version: "+remoteHead) {
				t.Fatalf("docs sync commit is missing expected docs-version trailer:\n%s", got)
			}
		})
	}
}

func installPullCommitLifecycleHooks(t *testing.T, workspaceDir, cliBinary string) {
	t.Helper()
	hooksDir := filepath.Join(workspaceDir, ".git", "hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		t.Fatal(err)
	}
	scripts := map[string]string{
		"pre-commit":   fmt.Sprintf("#!/bin/sh\nexec %q hook pre-commit\n", cliBinary),
		"commit-msg":   fmt.Sprintf("#!/bin/sh\nexec %q hook commit-msg \"$1\"\n", cliBinary),
		"post-commit":  fmt.Sprintf("#!/bin/sh\nexec %q hook post-commit\n", cliBinary),
		"post-rewrite": fmt.Sprintf("#!/bin/sh\nexec %q hook post-rewrite \"$@\"\n", cliBinary),
	}
	for name, script := range scripts {
		if err := os.WriteFile(filepath.Join(hooksDir, name), []byte(script), 0755); err != nil {
			t.Fatal(err)
		}
	}
}

func assertNoPullCommitTransaction(t *testing.T, workspaceDir string) {
	t.Helper()
	transactionDir := strings.TrimSpace(string(runCmd(t, workspaceDir, "git", "rev-parse", "--git-path", "sanho/pull-commit")))
	if !filepath.IsAbs(transactionDir) {
		transactionDir = filepath.Join(workspaceDir, transactionDir)
	}
	if _, err := os.Stat(filepath.Join(transactionDir, "state.json")); !os.IsNotExist(err) {
		t.Fatalf("pull-commit transaction remained: %v", err)
	}
}

func writeCompletedPullCommitState(t *testing.T, workspaceDir, head, remoteHash string) {
	t.Helper()
	preparedHead := strings.TrimSpace(string(runCmd(t, workspaceDir, "git", "rev-parse", head+"^")))
	state := map[string]any{
		"version":        3,
		"phase":          "prepared",
		"transaction_id": "stale-completed-e2e",
		"original_head":  preparedHead,
		"sync_commit":    preparedHead,
		"prepared_head":  preparedHead,
		"base_hash":      remoteHash,
		"remote_hash":    remoteHash,
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	transactionDir := strings.TrimSpace(string(runCmd(t, workspaceDir, "git", "rev-parse", "--git-path", "sanho/pull-commit")))
	if !filepath.IsAbs(transactionDir) {
		transactionDir = filepath.Join(workspaceDir, transactionDir)
	}
	if err := os.MkdirAll(transactionDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(transactionDir, "state.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
}
