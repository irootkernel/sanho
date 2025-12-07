package cli

import (
	"path/filepath"
	"testing"
)

func TestResolveDocsPath_AllowsRelativeWithinWorkspace(t *testing.T) {
	cwd := t.TempDir()

	got, err := resolveDocsPath(cwd, "docs")
	if err != nil {
		t.Fatalf("resolveDocsPath returned error: %v", err)
	}

	want := filepath.Join(cwd, "docs")
	if got != want {
		t.Errorf("resolveDocsPath() = %q, want %q", got, want)
	}
}

func TestResolveDocsPath_RejectsAbsoluteDocsDir(t *testing.T) {
	cwd := t.TempDir()
	absDocs := t.TempDir() // guaranteed absolute path

	if _, err := resolveDocsPath(cwd, absDocs); err == nil {
		t.Fatalf("resolveDocsPath() with absolute docsDir %q expected error, got nil", absDocs)
	}
}

func TestResolveDocsPath_RejectsEscapingDocsDir(t *testing.T) {
	cwd := t.TempDir()

	if _, err := resolveDocsPath(cwd, "../shared"); err == nil {
		t.Fatalf("resolveDocsPath() with escaping docsDir '../shared' expected error, got nil")
	}
}
