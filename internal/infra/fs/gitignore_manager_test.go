package fs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitignoreManager_CreatesFileWithEntries(t *testing.T) {
	dir := t.TempDir()
	manager := NewGitignoreManager()

	if err := manager.EnsureEntries(dir, "# Kkachi", []string{".kkachi_docs_hash", ".kkachi.json"}); err != nil {
		t.Fatalf("EnsureEntries returned error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("failed to read .gitignore: %v", err)
	}
	got := string(data)
	want := "# Kkachi\n.kkachi_docs_hash\n.kkachi.json\n"
	if got != want {
		t.Fatalf("unexpected .gitignore content\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestGitignoreManager_AppendsWithoutDuplicates(t *testing.T) {
	dir := t.TempDir()
	gitignorePath := filepath.Join(dir, ".gitignore")
	initial := "node_modules\n.kkachi_docs_hash\n"
	if err := os.WriteFile(gitignorePath, []byte(initial), 0644); err != nil {
		t.Fatalf("failed to seed .gitignore: %v", err)
	}

	manager := NewGitignoreManager()
	if err := manager.EnsureEntries(dir, "# Kkachi", []string{".kkachi_docs_hash", ".kkachi.json"}); err != nil {
		t.Fatalf("EnsureEntries returned error: %v", err)
	}

	data, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatalf("failed to read .gitignore: %v", err)
	}
	got := strings.ReplaceAll(string(data), "\r\n", "\n")
	want := "node_modules\n.kkachi_docs_hash\n# Kkachi\n.kkachi.json\n"
	if got != want {
		t.Fatalf("unexpected .gitignore content after append\nwant:\n%s\ngot:\n%s", want, got)
	}

	// Second call should be a no-op.
	if err := manager.EnsureEntries(dir, "# Kkachi", []string{".kkachi_docs_hash", ".kkachi.json"}); err != nil {
		t.Fatalf("EnsureEntries returned error on second call: %v", err)
	}
	data, _ = os.ReadFile(gitignorePath)
	got = strings.ReplaceAll(string(data), "\r\n", "\n")
	if got != want {
		t.Fatalf("EnsureEntries should be idempotent\nwant:\n%s\ngot:\n%s", want, got)
	}
}
