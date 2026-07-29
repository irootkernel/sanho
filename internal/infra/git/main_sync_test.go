package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/irootkernel/sanho/internal/infra/fs"
)

func TestCreateMainBasedDocsSyncCommitUpdatesMainAndPreservesDirtyLayers(t *testing.T) {
	ctx := context.Background()
	repo, remote := setupMainSyncRepository(t)
	advanceMainSyncRemote(t, remote, "remote.txt", "remote\n")

	writeMainSyncFile(t, repo, "staged.txt", "staged\n")
	runMainSyncGit(t, repo, "add", "staged.txt")
	writeMainSyncFile(t, repo, "base.txt", "unstaged\n")
	writeMainSyncFile(t, repo, "untracked.txt", "untracked\n")

	syncer := NewWorkspaceSync(fs.NewSnapshotBuilder(), fs.NewSnapshotApplier())
	result, err := syncer.CreateMainBasedDocsSyncCommit(
		ctx,
		repo,
		"docs",
		mainSyncSnapshot(t, "remote docs\n"),
		"[KKACHI] Update docs",
		"docs-hash-2",
	)
	if err != nil {
		t.Fatalf("create main-based sync: %v", err)
	}
	if result.Branch != "main" || result.PreparedHead != result.SyncCommit {
		t.Fatalf("unexpected result: %+v", result)
	}
	if got := strings.TrimSpace(runMainSyncGit(t, repo, "rev-parse", "HEAD^")); got != result.MainBase {
		t.Fatalf("sync parent=%s want %s", got, result.MainBase)
	}
	assertMainSyncFile(t, repo, "remote.txt", "remote\n")
	assertMainSyncFile(t, repo, "staged.txt", "staged\n")
	assertMainSyncFile(t, repo, "base.txt", "unstaged\n")
	assertMainSyncFile(t, repo, "untracked.txt", "untracked\n")
	if got := strings.TrimSpace(runMainSyncGit(t, repo, "diff", "--cached", "--name-only")); got != "staged.txt" {
		t.Fatalf("staged paths=%q", got)
	}
	if got := strings.TrimSpace(runMainSyncGit(t, repo, "diff", "--name-only")); got != "base.txt" {
		t.Fatalf("unstaged paths=%q", got)
	}
	if got := strings.TrimSpace(runMainSyncGit(t, repo, "ls-files", "--others", "--exclude-standard")); got != "untracked.txt" {
		t.Fatalf("untracked paths=%q", got)
	}
}

func TestCreateMainBasedDocsSyncCommitRebasesUnpublishedFeature(t *testing.T) {
	ctx := context.Background()
	repo, remote := setupMainSyncRepository(t)
	runMainSyncGit(t, repo, "switch", "-c", "feature")
	writeMainSyncFile(t, repo, "feature.txt", "feature\n")
	runMainSyncGit(t, repo, "add", "feature.txt")
	runMainSyncGit(t, repo, "commit", "-m", "feature")
	oldFeature := strings.TrimSpace(runMainSyncGit(t, repo, "rev-parse", "HEAD"))
	advanceMainSyncRemote(t, remote, "remote.txt", "remote\n")
	writeMainSyncFile(t, repo, "dirty.txt", "dirty staged\n")
	runMainSyncGit(t, repo, "add", "dirty.txt")
	writeMainSyncFile(t, repo, "work.txt", "dirty work\n")
	hookSentinel := filepath.Join(filepath.Dir(repo), "hook-ran")
	for _, hookName := range []string{"post-checkout", "post-rewrite"} {
		hookPath := filepath.Join(repo, ".git", "hooks", hookName)
		writeMainSyncFile(
			t,
			repo,
			filepath.Join(".git", "hooks", hookName),
			"#!/bin/sh\nprintf '"+hookName+" pwd=%s args=%s' \"$PWD\" \"$*\" > "+hookSentinel+"\n",
		)
		if err := os.Chmod(hookPath, 0755); err != nil {
			t.Fatal(err)
		}
	}

	syncer := NewWorkspaceSync(fs.NewSnapshotBuilder(), fs.NewSnapshotApplier())
	result, err := syncer.CreateMainBasedDocsSyncCommit(
		ctx,
		repo,
		"docs",
		mainSyncSnapshot(t, "remote docs\n"),
		"[KKACHI] Update docs",
		"docs-hash-2",
	)
	if err != nil {
		t.Fatalf("create main-based feature sync: %v", err)
	}
	if result.Branch != "feature" || result.PreparedHead == oldFeature {
		t.Fatalf("feature was not rebased: %+v", result)
	}
	if got := strings.TrimSpace(runMainSyncGit(t, repo, "rev-parse", "refs/heads/main")); got != result.SyncCommit {
		t.Fatalf("main=%s want %s", got, result.SyncCommit)
	}
	if got := strings.TrimSpace(runMainSyncGit(t, repo, "merge-base", "--is-ancestor", result.SyncCommit, "HEAD")); got != "" {
		t.Fatalf("unexpected merge-base output %q", got)
	}
	assertMainSyncFile(t, repo, "feature.txt", "feature\n")
	assertMainSyncFile(t, repo, "dirty.txt", "dirty staged\n")
	assertMainSyncFile(t, repo, "work.txt", "dirty work\n")
	if _, err := os.Stat(hookSentinel); !os.IsNotExist(err) {
		data, _ := os.ReadFile(hookSentinel)
		t.Fatalf("internal rebase executed repository hook %q: %v", data, err)
	}
}

func TestCreateMainBasedDocsSyncCommitRejectsPublishedFeatureUnchanged(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupMainSyncRepository(t)
	runMainSyncGit(t, repo, "switch", "-c", "feature")
	writeMainSyncFile(t, repo, "feature.txt", "feature\n")
	runMainSyncGit(t, repo, "add", "feature.txt")
	runMainSyncGit(t, repo, "commit", "-m", "feature")
	runMainSyncGit(t, repo, "push", "-u", "origin", "feature")
	before := strings.TrimSpace(runMainSyncGit(t, repo, "rev-parse", "HEAD"))

	syncer := NewWorkspaceSync(fs.NewSnapshotBuilder(), fs.NewSnapshotApplier())
	_, err := syncer.CreateMainBasedDocsSyncCommit(
		ctx,
		repo,
		"docs",
		mainSyncSnapshot(t, "remote docs\n"),
		"[KKACHI] Update docs",
		"docs-hash-2",
	)
	if err == nil || !strings.Contains(err.Error(), ErrPublishedFeature.Error()) {
		t.Fatalf("error=%v want published feature rejection", err)
	}
	if got := strings.TrimSpace(runMainSyncGit(t, repo, "rev-parse", "HEAD")); got != before {
		t.Fatalf("HEAD changed: %s -> %s", before, got)
	}
}

func TestCreateMainBasedDocsSyncCommitRebaseConflictLeavesRefsAndDirtyStateUnchanged(t *testing.T) {
	ctx := context.Background()
	repo, remote := setupMainSyncRepository(t)
	runMainSyncGit(t, repo, "switch", "-c", "feature")
	writeMainSyncFile(t, repo, "feature.txt", "feature\n")
	runMainSyncGit(t, repo, "add", "feature.txt")
	runMainSyncGit(t, repo, "commit", "-m", "feature")
	advanceMainSyncRemote(t, remote, "base.txt", "remote\n")
	writeMainSyncFile(t, repo, "base.txt", "local dirty\n")
	beforeFeature := strings.TrimSpace(runMainSyncGit(t, repo, "rev-parse", "HEAD"))
	beforeMain := strings.TrimSpace(runMainSyncGit(t, repo, "rev-parse", "refs/heads/main"))
	beforeStatus := runMainSyncGit(t, repo, "status", "--porcelain=v1")

	syncer := NewWorkspaceSync(fs.NewSnapshotBuilder(), fs.NewSnapshotApplier())
	_, err := syncer.CreateMainBasedDocsSyncCommit(
		ctx,
		repo,
		"docs",
		mainSyncSnapshot(t, "remote docs\n"),
		"[KKACHI] Update docs",
		"docs-hash-2",
	)
	if err == nil || !strings.Contains(err.Error(), ErrMainSyncRebaseConflict.Error()) {
		t.Fatalf("error=%v want rebase conflict", err)
	}
	if got := strings.TrimSpace(runMainSyncGit(t, repo, "rev-parse", "HEAD")); got != beforeFeature {
		t.Fatalf("feature ref changed: %s -> %s", beforeFeature, got)
	}
	if got := strings.TrimSpace(runMainSyncGit(t, repo, "rev-parse", "refs/heads/main")); got != beforeMain {
		t.Fatalf("main ref changed: %s -> %s", beforeMain, got)
	}
	if got := runMainSyncGit(t, repo, "status", "--porcelain=v1"); got != beforeStatus {
		t.Fatalf("status changed:\n%s\nwant:\n%s", got, beforeStatus)
	}
	assertMainSyncFile(t, repo, "base.txt", "local dirty\n")
}

func TestCreateMainBasedDocsSyncCommitRejectsDivergedMainUnchanged(t *testing.T) {
	ctx := context.Background()
	repo, remote := setupMainSyncRepository(t)
	writeMainSyncFile(t, repo, "local.txt", "local\n")
	runMainSyncGit(t, repo, "add", "local.txt")
	runMainSyncGit(t, repo, "commit", "-m", "local main")
	advanceMainSyncRemote(t, remote, "remote.txt", "remote\n")
	before := strings.TrimSpace(runMainSyncGit(t, repo, "rev-parse", "HEAD"))

	syncer := NewWorkspaceSync(fs.NewSnapshotBuilder(), fs.NewSnapshotApplier())
	_, err := syncer.CreateMainBasedDocsSyncCommit(
		ctx,
		repo,
		"docs",
		mainSyncSnapshot(t, "remote docs\n"),
		"[KKACHI] Update docs",
		"docs-hash-2",
	)
	if err == nil || !strings.Contains(err.Error(), ErrMainDiverged.Error()) {
		t.Fatalf("error=%v want diverged main rejection", err)
	}
	if got := strings.TrimSpace(runMainSyncGit(t, repo, "rev-parse", "HEAD")); got != before {
		t.Fatalf("main changed: %s -> %s", before, got)
	}
}

func setupMainSyncRepository(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	repo := filepath.Join(root, "repo")
	runMainSyncGit(t, root, "init", "--bare", "--initial-branch=main", remote)
	runMainSyncGit(t, root, "init", "--initial-branch=main", repo)
	runMainSyncGit(t, repo, "config", "user.email", "test@example.com")
	runMainSyncGit(t, repo, "config", "user.name", "Test User")
	runMainSyncGit(t, repo, "config", "commit.gpgsign", "false")
	writeMainSyncFile(t, repo, "docs/readme.md", "base docs\n")
	writeMainSyncFile(t, repo, "base.txt", "base\n")
	runMainSyncGit(t, repo, "add", ".")
	runMainSyncGit(t, repo, "commit", "-m", "base")
	runMainSyncGit(t, repo, "remote", "add", "origin", remote)
	runMainSyncGit(t, repo, "push", "-u", "origin", "main")
	return repo, remote
}

func advanceMainSyncRemote(t *testing.T, remote, path, content string) {
	t.Helper()
	clone := t.TempDir()
	runMainSyncGit(t, filepath.Dir(clone), "clone", remote, clone)
	runMainSyncGit(t, clone, "config", "user.email", "remote@example.com")
	runMainSyncGit(t, clone, "config", "user.name", "Remote User")
	writeMainSyncFile(t, clone, path, content)
	runMainSyncGit(t, clone, "add", path)
	runMainSyncGit(t, clone, "commit", "-m", "remote update")
	runMainSyncGit(t, clone, "push", "origin", "main")
}

func mainSyncSnapshot(t *testing.T, content string) []byte {
	t.Helper()
	dir := t.TempDir()
	writeMainSyncFile(t, dir, "readme.md", content)
	snapshot, err := fs.NewSnapshotBuilder().Build(dir)
	if err != nil {
		t.Fatalf("build snapshot: %v", err)
	}
	return snapshot
}

func writeMainSyncFile(t *testing.T, root, path, content string) {
	t.Helper()
	fullPath := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func assertMainSyncFile(t *testing.T, root, path, want string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(data) != want {
		t.Fatalf("%s=%q want %q", path, data, want)
	}
}

func runMainSyncGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}
