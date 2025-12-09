package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := exec.Command("git", "init", dir).Run(); err != nil {
		t.Skipf("git not available: %v", err)
	}
	if err := exec.Command("git", "-C", dir, "config", "user.email", "test@example.com").Run(); err != nil {
		t.Fatalf("failed to set user.email: %v", err)
	}
	if err := exec.Command("git", "-C", dir, "config", "user.name", "Test User").Run(); err != nil {
		t.Fatalf("failed to set user.name: %v", err)
	}
	return dir
}

func commitFile(t *testing.T, repo, path, content, message string) {
	t.Helper()
	full := filepath.Join(repo, path)
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := exec.Command("git", "-C", repo, "add", ".").Run(); err != nil {
		t.Fatalf("git add: %v", err)
	}
	if out, err := exec.Command("git", "-C", repo, "commit", "-m", message).CombinedOutput(); err != nil {
		t.Fatalf("git commit failed: %v\n%s", err, string(out))
	}
}

func TestHasDocsVersionCommits(t *testing.T) {
	ctx := context.Background()
	client := NewClient()
	repo := initTestRepo(t)

	// No commits yet
	has, err := client.HasDocsVersionCommits(ctx, repo)
	if err != nil {
		t.Fatalf("unexpected error on empty repo: %v", err)
	}
	if has {
		t.Fatalf("expected no docs-version commits on empty repo")
	}

	// Commit without docs-version tag
	commitFile(t, repo, "docs/a.md", "hello", "initial")
	has, err = client.HasDocsVersionCommits(ctx, repo)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if has {
		t.Fatalf("expected no docs-version commits")
	}

	// Commit with docs-version tag
	commitFile(t, repo, "docs/b.md", "hi", "add docs\n\ndocs-version: deadbeef")
	has, err = client.HasDocsVersionCommits(ctx, repo)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !has {
		t.Fatalf("expected docs-version commits to be detected")
	}
}

func TestGetLastDocsVersionHash(t *testing.T) {
	ctx := context.Background()
	client := NewClient()
	repo := initTestRepo(t)

	// No commit
	if hash, err := client.GetLastDocsVersionHash(ctx, repo); err == nil || err != ErrNoDocsVersionCommits {
		t.Fatalf("expected ErrNoDocsVersionCommits, got hash=%q err=%v", hash, err)
	}

	// Commit without docs-version
	commitFile(t, repo, "docs/a.md", "hello", "initial")
	if hash, err := client.GetLastDocsVersionHash(ctx, repo); err == nil || err != ErrNoDocsVersionCommits {
		t.Fatalf("expected ErrNoDocsVersionCommits after plain commit, got hash=%q err=%v", hash, err)
	}

	// Commit with docs-version
	commitFile(t, repo, "docs/b.md", "v1", "body\n\ndocs-version: abc123")
	hash, err := client.GetLastDocsVersionHash(ctx, repo)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hash != "abc123" {
		t.Fatalf("hash mismatch: got %s want abc123", hash)
	}

	// More recent docs-version should win
	commitFile(t, repo, "docs/c.md", "v2", "update\n\ndocs-version: ff00ff")
	hash, err = client.GetLastDocsVersionHash(ctx, repo)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hash != "ff00ff" {
		t.Fatalf("expected latest docs-version, got %s", hash)
	}
}

func TestIsPathClean(t *testing.T) {
	ctx := context.Background()
	client := NewClient()
	repo := initTestRepo(t)

	commitFile(t, repo, "docs/a.md", "hello", "add")

	clean, err := client.IsPathClean(ctx, repo, "docs")
	if err != nil {
		t.Fatalf("IsPathClean error: %v", err)
	}
	if !clean {
		t.Fatalf("expected clean docs after commit")
	}

	// Unstaged change
	appendToFile(t, filepath.Join(repo, "docs/a.md"), "\nchange")
	clean, err = client.IsPathClean(ctx, repo, "docs")
	if err != nil {
		t.Fatalf("IsPathClean error: %v", err)
	}
	if clean {
		t.Fatalf("expected dirty docs after modification")
	}

	// Stage change should still be dirty
	if err := exec.Command("git", "-C", repo, "add", ".").Run(); err != nil {
		t.Fatalf("git add: %v", err)
	}
	clean, err = client.IsPathClean(ctx, repo, "docs")
	if err != nil {
		t.Fatalf("IsPathClean error: %v", err)
	}
	if clean {
		t.Fatalf("expected dirty docs with staged changes")
	}

	// Commit makes clean again
	if out, err := exec.Command("git", "-C", repo, "commit", "-m", "cleanup").CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, string(out))
	}
	clean, err = client.IsPathClean(ctx, repo, "docs")
	if err != nil {
		t.Fatalf("IsPathClean error: %v", err)
	}
	if !clean {
		t.Fatalf("expected clean after commit")
	}

	// Untracked file should mark dirty
	if err := os.WriteFile(filepath.Join(repo, "docs", "new.md"), []byte("new"), 0644); err != nil {
		t.Fatalf("write untracked: %v", err)
	}
	clean, err = client.IsPathClean(ctx, repo, "docs")
	if err != nil {
		t.Fatalf("IsPathClean error: %v", err)
	}
	if clean {
		t.Fatalf("expected dirty due to untracked file")
	}
}

func appendToFile(t *testing.T, path, content string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("open file: %v", err)
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("append: %v", err)
	}
}
