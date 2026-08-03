package integration

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

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
