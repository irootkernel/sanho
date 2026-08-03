package integration

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIPostRewriteReportsSuccessfulRebaseToDaemon(t *testing.T) {
	for _, backend := range []string{"merge", "apply"} {
		t.Run(backend, func(t *testing.T) {
			testCLIPostRewriteReportsSuccessfulRebaseToDaemon(t, backend)
		})
	}
}

func testCLIPostRewriteReportsSuccessfulRebaseToDaemon(t *testing.T, backend string) {
	t.Helper()
	cliBinary := getCliBinary(t)
	reported := make(chan string, 1)
	daemon := newUnixTestDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/workspaces/workspace-rewrite-daemon/docs-hash":
			var body struct {
				DocsHash string `json:"docs_hash"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			reported <- body.DocsHash
			_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
		case r.Method == http.MethodGet && r.URL.Path == "/docs/head":
			_ = json.NewEncoder(w).Encode(map[string]string{"head": "docs-v2"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(daemon.Close)

	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	runRecoveryGit(t, root, "init", "--initial-branch=main", repo)
	runRecoveryGit(t, repo, "config", "user.email", "test@example.com")
	runRecoveryGit(t, repo, "config", "user.name", "Test User")
	writeRecoveryFile(t, repo, ".gitignore", ".sanho.json\n.sanho_docs_hash\n")
	writeRecoveryFile(t, repo, "docs/readme.md", "docs v1\n")
	runRecoveryGit(t, repo, "add", ".gitignore", "docs/readme.md")
	runRecoveryGit(t, repo, "commit", "-m", "docs v1", "-m", "docs-version: docs-v1")
	runRecoveryGit(t, repo, "switch", "-c", "feature")
	writeRecoveryFile(t, repo, "feature.txt", "feature\n")
	runRecoveryGit(t, repo, "add", "feature.txt")
	runRecoveryGit(t, repo, "commit", "-m", "feature")
	runRecoveryGit(t, repo, "switch", "main")
	writeRecoveryFile(t, repo, "docs/readme.md", "docs v2\n")
	runRecoveryGit(t, repo, "add", "docs/readme.md")
	runRecoveryGit(t, repo, "commit", "-m", "docs v2", "-m", "docs-version: docs-v2")
	runRecoveryGit(t, repo, "switch", "feature")

	config := WorkspaceConfig{
		SocketPath:  daemon.SocketPath,
		WorkspaceID: "workspace-rewrite-daemon",
		Project:     "project-rewrite",
		ActorEmail:  "test@example.com",
		DocsDir:     "docs",
	}
	configData, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".sanho.json"), configData, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".sanho_docs_hash"), []byte("docs-v1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	hooksDir := runRecoveryGit(t, repo, "rev-parse", "--git-path", "hooks")
	if !filepath.IsAbs(hooksDir) {
		hooksDir = filepath.Join(repo, hooksDir)
	}
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		t.Fatal(err)
	}
	hook := fmt.Sprintf("#!/bin/sh\nexec %q hook post-rewrite \"$@\"\n", cliBinary)
	if err := os.WriteFile(filepath.Join(hooksDir, "post-rewrite"), []byte(hook), 0755); err != nil {
		t.Fatal(err)
	}

	rebaseArgs := []string{"-C", repo, "rebase"}
	if backend == "apply" {
		rebaseArgs = append(rebaseArgs, "--apply")
	}
	rebaseArgs = append(rebaseArgs, "main")
	rebase := exec.Command("git", rebaseArgs...)
	if output, err := rebase.CombinedOutput(); err != nil {
		t.Fatalf("rebase failed: %v\n%s", err, output)
	}
	if got := strings.TrimSpace(string(readGuardFile(t, filepath.Join(repo, ".sanho_docs_hash")))); got != "docs-v2" {
		t.Fatalf("docs hash=%q want docs-v2", got)
	}
	select {
	case got := <-reported:
		if got != "docs-v2" {
			t.Fatalf("reported hash=%q want docs-v2", got)
		}
	default:
		t.Fatal("daemon did not receive rewritten docs hash")
	}
	reportPath := runRecoveryGit(t, repo, "rev-parse", "--git-path", "sanho/workspace-report.json")
	if !filepath.IsAbs(reportPath) {
		reportPath = filepath.Join(repo, reportPath)
	}
	if _, err := os.Stat(reportPath); !os.IsNotExist(err) {
		t.Fatalf("completed workspace report remained: %v", err)
	}
}

func TestCLIPostRewriteReconcilesLargeSuccessfulRebase(t *testing.T) {
	cliBinary := getCliBinary(t)
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	runRecoveryGit(t, root, "init", "--initial-branch=main", repo)
	runRecoveryGit(t, repo, "config", "user.email", "test@example.com")
	runRecoveryGit(t, repo, "config", "user.name", "Test User")
	writeRecoveryFile(t, repo, ".gitignore", ".sanho.json\n.sanho_docs_hash\n")
	writeRecoveryFile(t, repo, "docs/readme.md", "docs v1\n")
	runRecoveryGit(t, repo, "add", ".gitignore", "docs/readme.md")
	runRecoveryGit(t, repo, "commit", "-m", "docs v1", "-m", "docs-version: docs-v1")
	syncCommit := runRecoveryGit(t, repo, "rev-parse", "HEAD")
	runRecoveryGit(t, repo, "switch", "-c", "feature")
	writeRecoveryFile(t, repo, "feature.txt", "feature\n")
	runRecoveryGit(t, repo, "add", "feature.txt")
	runRecoveryGit(t, repo, "commit", "-m", "feature")
	for i := 1; i < 1000; i++ {
		runRecoveryGit(t, repo, "commit", "--allow-empty", "-m", fmt.Sprintf("feature %04d", i))
	}
	preparedHead := runRecoveryGit(t, repo, "rev-parse", "HEAD")
	preparedTree := runRecoveryGit(t, repo, "rev-parse", "HEAD^{tree}")
	runRecoveryGit(t, repo, "switch", "main")
	writeRecoveryFile(t, repo, "docs/readme.md", "docs v2\n")
	runRecoveryGit(t, repo, "add", "docs/readme.md")
	runRecoveryGit(t, repo, "commit", "-m", "docs v2", "-m", "docs-version: docs-v2")
	runRecoveryGit(t, repo, "switch", "feature")

	config := WorkspaceConfig{
		SocketPath:  filepath.Join(root, "missing.sock"),
		WorkspaceID: "workspace-rewrite",
		Project:     "project-rewrite",
		ActorEmail:  "test@example.com",
		DocsDir:     "docs",
	}
	configData, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".sanho.json"), configData, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".sanho_docs_hash"), []byte("docs-v1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	hooksDir := runRecoveryGit(t, repo, "rev-parse", "--git-path", "hooks")
	if !filepath.IsAbs(hooksDir) {
		hooksDir = filepath.Join(repo, hooksDir)
	}
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		t.Fatal(err)
	}
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	tracePath := filepath.Join(root, "post-rewrite-git.trace")
	wrapperDir := filepath.Join(root, "git-wrapper")
	if err := os.MkdirAll(wrapperDir, 0755); err != nil {
		t.Fatal(err)
	}
	wrapper := fmt.Sprintf(
		"#!/bin/sh\nprintf '%%s\\n' \"$*\" >> %q\n"+
			"case \"$*\" in *\"rev-parse --verify\"*'^{tree}'*) sleep 0.04 ;; esac\n"+
			"exec %q \"$@\"\n",
		tracePath,
		realGit,
	)
	if err := os.WriteFile(filepath.Join(wrapperDir, "git"), []byte(wrapper), 0755); err != nil {
		t.Fatal(err)
	}
	hook := fmt.Sprintf(
		"#!/bin/sh\nPATH=%q:\"$PATH\" exec %q hook post-rewrite \"$@\"\n",
		wrapperDir,
		cliBinary,
	)
	if err := os.WriteFile(filepath.Join(hooksDir, "post-rewrite"), []byte(hook), 0755); err != nil {
		t.Fatal(err)
	}

	transactionDir := runRecoveryGit(t, repo, "rev-parse", "--git-path", "sanho/pull-commit")
	if !filepath.IsAbs(transactionDir) {
		transactionDir = filepath.Join(repo, transactionDir)
	}
	if err := os.MkdirAll(transactionDir, 0700); err != nil {
		t.Fatal(err)
	}
	transaction := map[string]any{
		"version":        3,
		"phase":          "prepared",
		"transaction_id": "verified-rebase",
		"branch_ref":     "refs/heads/feature",
		"original_head":  preparedHead,
		"sync_commit":    syncCommit,
		"prepared_head":  preparedHead,
		"prepared_tree":  preparedTree,
		"base_hash":      "docs-v1",
		"remote_hash":    "docs-v2",
		"reported":       true,
		"created_at":     "2026-08-03T14:00:00+09:00",
	}
	transactionData, err := json.Marshal(transaction)
	if err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(transactionDir, "state.json")
	if err := os.WriteFile(statePath, transactionData, 0600); err != nil {
		t.Fatal(err)
	}

	rebase := exec.Command("git", "-C", repo, "rebase", "main")
	output, err := rebase.CombinedOutput()
	if err != nil {
		t.Fatalf("rebase failed: %v\n%s", err, output)
	}
	for _, unexpected := range []string{"Sanho mutation was skipped", "signal: killed", "context deadline exceeded"} {
		if strings.Contains(string(output), unexpected) {
			t.Fatalf("successful rebase output contains %q:\n%s", unexpected, output)
		}
	}
	newHead := runRecoveryGit(t, repo, "rev-parse", "HEAD")
	if newHead == preparedHead {
		t.Fatal("rebase did not rewrite the prepared commit")
	}
	if got := strings.TrimSpace(string(readGuardFile(t, filepath.Join(repo, ".sanho_docs_hash")))); got != "docs-v2" {
		t.Fatalf("docs hash=%q want docs-v2\n%s", got, output)
	}

	state := readGuardJSON(t, statePath)
	if state["prepared_head"] != newHead || state["phase"] != "prepared" {
		t.Fatalf("transaction state=%#v", state)
	}
	rewrites := state["rewrites"].([]any)
	if len(rewrites) != 1000 {
		t.Fatalf("rewrite count=%d want 1000", len(rewrites))
	}
	preparedRewriteFound := false
	for _, value := range rewrites {
		rewrite := value.(map[string]any)
		if rewrite["command"] == "rebase" && rewrite["old"] == preparedHead && rewrite["new"] == newHead {
			preparedRewriteFound = true
			break
		}
	}
	if !preparedRewriteFound {
		t.Fatalf("prepared rewrite %s -> %s was not recorded", preparedHead, newHead)
	}
	trace := string(readGuardFile(t, tracePath))
	if got := strings.Count(trace, "cat-file --batch-check="); got != 1 {
		t.Fatalf("batch object validation count=%d want 1\n%s", got, trace)
	}
	if got := strings.Count(trace, "rev-list --no-walk=unsorted --stdin"); got != 1 {
		t.Fatalf("batch reachability validation count=%d want 1\n%s", got, trace)
	}
	for _, line := range strings.Split(trace, "\n") {
		if strings.Contains(line, "rev-parse --verify") && strings.Contains(line, "^{tree}") {
			t.Fatalf("mapping reconciliation performed a per-commit tree lookup: %s", line)
		}
	}

	reportPath := runRecoveryGit(t, repo, "rev-parse", "--git-path", "sanho/workspace-report.json")
	if !filepath.IsAbs(reportPath) {
		reportPath = filepath.Join(repo, reportPath)
	}
	report := readGuardJSON(t, reportPath)
	if report["docs_hash"] != "docs-v2" {
		t.Fatalf("workspace report=%#v", report)
	}
	if status := runRecoveryGit(t, repo, "status"); strings.Contains(status, "rebase in progress") {
		t.Fatalf("rebase metadata remained:\n%s", status)
	}
	if got := runRecoveryGit(t, repo, "status", "--porcelain=v2", "--untracked-files=all"); got != "" {
		t.Fatalf("workspace changed after rebase: %s", got)
	}

	repeat := exec.Command(cliBinary, "hook", "post-rewrite", "rebase")
	repeat.Dir = repo
	repeat.Stdin = strings.NewReader(preparedHead + " " + newHead + "\n")
	if repeatOutput, err := repeat.CombinedOutput(); err != nil {
		t.Fatalf("repeat post-rewrite failed: %v\n%s", err, repeatOutput)
	}
	state = readGuardJSON(t, statePath)
	if got := len(state["rewrites"].([]any)); got != 1000 {
		t.Fatalf("repeated rewrite count=%d want 1000", got)
	}
}

func TestCLIMutationsFailClosedForCleanStaleRebase(t *testing.T) {
	cliBinary := getCliBinary(t)
	repo := setupCleanStaleRebaseWorkspace(t)
	before := captureCLIGitOperationState(t, repo)
	if before.head != before.originMain || before.porcelain != "" {
		t.Fatalf("fixture is not clean and aligned: %+v", before)
	}
	if status := runRecoveryGit(t, repo, "status"); !strings.Contains(status, "rebase in progress") {
		t.Fatalf("fixture does not report rebase in progress:\n%s", status)
	}

	tests := []struct {
		name  string
		args  []string
		stdin string
	}{
		{name: "init", args: []string{"init", "--project", "test", "--docs-repo-url", "git@example.com:test/docs.git"}},
		{name: "pull", args: []string{"pull"}},
		{name: "pull-commit", args: []string{"pull-commit"}},
		{name: "pull-commit continue", args: []string{"pull-commit", "--continue"}},
		{name: "pull-commit abort", args: []string{"pull-commit", "--abort"}},
		{name: "pull-commit recover", args: []string{"pull-commit", "--recover"}},
		{name: "fix", args: []string{"fix"}},
		{name: "clean", args: []string{"clean", "--yes", "--offline"}},
		{
			name:  "pre-push",
			args:  []string{"hook", "pre-push", "origin", "unused"},
			stdin: "refs/heads/main " + before.head + " refs/heads/main " + before.originMain + "\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cmd := exec.Command(cliBinary, test.args...)
			cmd.Dir = repo
			cmd.Stdin = strings.NewReader(test.stdin)
			output, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("command succeeded:\n%s", output)
			}
			text := string(output)
			for _, expected := range []string{
				"Git rebase operation metadata is present",
				"git status",
				"git rebase --abort",
				"git rebase --quit",
			} {
				if !strings.Contains(text, expected) {
					t.Fatalf("output missing %q:\n%s", expected, text)
				}
			}
			if after := captureCLIGitOperationState(t, repo); after != before {
				t.Fatalf("repository changed\nbefore: %+v\nafter:  %+v", before, after)
			}
		})
	}
}

func TestCLILifecycleHooksSkipMutationDuringGitOperation(t *testing.T) {
	cliBinary := getCliBinary(t)
	repo := setupCleanStaleRebaseWorkspace(t)
	messagePath := filepath.Join(repo, ".git", "COMMIT_EDITMSG")
	if err := os.WriteFile(messagePath, []byte("subject\n"), 0644); err != nil {
		t.Fatal(err)
	}
	before := captureCLIGitOperationState(t, repo)
	tests := []struct {
		name  string
		args  []string
		stdin string
	}{
		{name: "pre-commit", args: []string{"hook", "pre-commit"}},
		{name: "commit-msg", args: []string{"hook", "commit-msg", messagePath}},
		{name: "post-commit", args: []string{"hook", "post-commit"}},
		{name: "post-rewrite", args: []string{"hook", "post-rewrite", "rebase"}},
		{
			name:  "post-rewrite forged reachable mapping",
			args:  []string{"hook", "post-rewrite", "rebase"},
			stdin: before.head + " " + before.head + " forged extra info\n",
		},
		{name: "post-checkout", args: []string{"hook", "post-checkout"}},
		{name: "post-merge", args: []string{"hook", "post-merge", "0"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cmd := exec.Command(cliBinary, test.args...)
			cmd.Dir = repo
			cmd.Stdin = strings.NewReader(test.stdin)
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("hook failed: %v\n%s", err, output)
			}
			if !strings.Contains(string(output), "Sanho mutation was skipped") {
				t.Fatalf("hook did not explain skip:\n%s", output)
			}
			if test.name == "post-rewrite forged reachable mapping" &&
				!strings.Contains(string(output), "inspect rewrite input offset") {
				t.Fatalf("hook did not reject forged input provenance:\n%s", output)
			}
			if after := captureCLIGitOperationState(t, repo); after != before {
				t.Fatalf("repository changed\nbefore: %+v\nafter:  %+v", before, after)
			}
		})
	}

	dryRun := exec.Command(cliBinary, "clean", "--dry-run", "--offline")
	dryRun.Dir = repo
	if output, err := dryRun.CombinedOutput(); err != nil {
		t.Fatalf("clean --dry-run failed: %v\n%s", err, output)
	}
	if after := captureCLIGitOperationState(t, repo); after != before {
		t.Fatalf("dry-run changed repository\nbefore: %+v\nafter:  %+v", before, after)
	}
	message, err := os.ReadFile(messagePath)
	if err != nil || string(message) != "subject\n" {
		t.Fatalf("commit message changed: %q err=%v", message, err)
	}
}

func TestCLIPostRewriteAcceptsOptionalExtraInfoFromGitOwnedSource(t *testing.T) {
	cliBinary := getCliBinary(t)
	repo := setupCleanStaleRebaseWorkspace(t)
	head := runRecoveryGit(t, repo, "rev-parse", "HEAD")
	rewritePath := runRecoveryGit(t, repo, "rev-parse", "--git-path", "rebase-merge/rewritten-list")
	if !filepath.IsAbs(rewritePath) {
		rewritePath = filepath.Join(repo, rewritePath)
	}
	if err := os.WriteFile(
		rewritePath,
		[]byte(head+" "+head+" future metadata remains opaque\n"),
		0644,
	); err != nil {
		t.Fatal(err)
	}
	source, err := os.Open(rewritePath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := source.Close(); err != nil {
			t.Errorf("close rewrite source: %v", err)
		}
	}()
	before := captureCLIGitOperationState(t, repo)

	cmd := exec.Command(cliBinary, "hook", "post-rewrite", "rebase")
	cmd.Dir = repo
	cmd.Stdin = source
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("post-rewrite failed: %v\n%s", err, output)
	}
	for _, unexpected := range []string{"failed to read rewrite mappings", "evidence could not be validated"} {
		if strings.Contains(string(output), unexpected) {
			t.Fatalf("post-rewrite output contains %q:\n%s", unexpected, output)
		}
	}
	if after := captureCLIGitOperationState(t, repo); after != before {
		t.Fatalf("repository changed\nbefore: %+v\nafter:  %+v", before, after)
	}
}

func setupCleanStaleRebaseWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	repo := filepath.Join(root, "repo")
	runRecoveryGit(t, root, "init", "--bare", "--initial-branch=main", remote)
	runRecoveryGit(t, root, "init", "--initial-branch=main", repo)
	runRecoveryGit(t, repo, "config", "user.email", "test@example.com")
	runRecoveryGit(t, repo, "config", "user.name", "Test User")
	writeRecoveryFile(t, repo, ".gitignore", ".sanho.json\n.sanho_docs_hash\n.sanho_pending_fix\n")
	writeRecoveryFile(t, repo, "docs/readme.md", "base\n")
	runRecoveryGit(t, repo, "add", ".gitignore", "docs/readme.md")
	runRecoveryGit(t, repo, "commit", "-m", "base")
	writeRecoveryFile(t, repo, "app.txt", "second\n")
	runRecoveryGit(t, repo, "add", "app.txt")
	runRecoveryGit(t, repo, "commit", "-m", "second")
	runRecoveryGit(t, repo, "remote", "add", "origin", remote)
	runRecoveryGit(t, repo, "push", "-u", "origin", "main")

	config := WorkspaceConfig{
		SocketPath:  filepath.Join(root, "missing.sock"),
		WorkspaceID: "workspace-operation-guard",
		Project:     "project-operation-guard",
		ActorEmail:  "test@example.com",
		DocsDir:     "docs",
	}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".sanho.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".sanho_docs_hash"), []byte("docs-base\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("git", "-C", repo, "rebase", "--exec", "false", "HEAD~1")
	if output, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("rebase unexpectedly completed:\n%s", output)
	}
	runRecoveryGit(t, repo, "reset", "--hard", "refs/heads/main")
	return repo
}

type cliGitOperationState struct {
	head         string
	originMain   string
	index        string
	porcelain    string
	refs         string
	metadataHash string
	config       string
	docsHash     string
}

func captureCLIGitOperationState(t *testing.T, repo string) cliGitOperationState {
	t.Helper()
	return cliGitOperationState{
		head:         runRecoveryGit(t, repo, "rev-parse", "HEAD"),
		originMain:   runRecoveryGit(t, repo, "rev-parse", "refs/remotes/origin/main"),
		index:        runRecoveryGit(t, repo, "write-tree"),
		porcelain:    runRecoveryGit(t, repo, "status", "--porcelain=v2", "--untracked-files=all"),
		refs:         runRecoveryGit(t, repo, "for-each-ref", "--format=%(refname) %(objectname)"),
		metadataHash: hashGitOperationMetadata(t, repo, "rebase-merge"),
		config:       string(readGuardFile(t, filepath.Join(repo, ".sanho.json"))),
		docsHash:     string(readGuardFile(t, filepath.Join(repo, ".sanho_docs_hash"))),
	}
}

func hashGitOperationMetadata(t *testing.T, repo, name string) string {
	t.Helper()
	path := runRecoveryGit(t, repo, "rev-parse", "--git-path", name)
	if !filepath.IsAbs(path) {
		path = filepath.Join(repo, path)
	}
	hash := sha256.New()
	if err := filepath.WalkDir(path, func(current string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(path, current)
		if err != nil {
			return err
		}
		_, _ = hash.Write([]byte(relative))
		info, err := entry.Info()
		if err != nil {
			return err
		}
		_, _ = hash.Write([]byte(info.Mode().String()))
		if entry.Type().IsRegular() {
			data, err := os.ReadFile(current)
			if err != nil {
				return err
			}
			_, _ = hash.Write(data)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func readGuardFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func readGuardJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal(readGuardFile(t, path), &value); err != nil {
		t.Fatal(err)
	}
	return value
}
