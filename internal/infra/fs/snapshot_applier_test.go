package fs

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

// buildSnapshot is a small helper to create an in-memory tar.gz snapshot
// with the given file paths.
func buildSnapshot(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	for name, content := range files {
		hdr := &tar.Header{
			Name: name,
			Mode: 0644,
			Size: int64(len(content)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("failed to write tar header: %v", err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("failed to write tar content: %v", err)
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("failed to close tar writer: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("failed to close gzip writer: %v", err)
	}

	return buf.Bytes()
}

func TestSnapshotApplier_Apply_IncludesDotfilesAndDirs(t *testing.T) {
	snapshot := buildSnapshot(t, map[string]string{
		"visible.md":            "visible",
		".gitignore":            "ignored",
		".github/workflows.yml": "ignored",
		"dir/nested.md":         "nested",
		"dir/.hidden.md":        "hidden",
		"docs/inner.md":         "inner",
	})

	tmpDir, err := os.MkdirTemp("", "snapshot-applier-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	applier := NewSnapshotApplier()
	if err := applier.Apply(snapshot, tmpDir, "docs"); err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}

	// Visible files should exist under the target docs directory.
	if _, err := os.Stat(filepath.Join(tmpDir, "docs", "visible.md")); err != nil {
		t.Errorf("expected visible.md to be extracted, got error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "docs", "dir", "nested.md")); err != nil {
		t.Errorf("expected dir/nested.md to be extracted, got error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "docs", "docs", "inner.md")); err != nil {
		t.Errorf("expected docs/inner.md to be extracted under docs/docs/, got error: %v", err)
	}

	// Dotfiles and dot-directories (except .git) should be extracted.
	if _, err := os.Stat(filepath.Join(tmpDir, "docs", ".gitignore")); err != nil {
		t.Errorf("expected .gitignore to be extracted, got err: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "docs", ".github", "workflows.yml")); err != nil {
		t.Errorf("expected .github/workflows.yml to be extracted, got err: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "docs", "dir", ".hidden.md")); err != nil {
		t.Errorf("expected dir/.hidden.md to be extracted, got err: %v", err)
	}
}

func TestSnapshotApplier_Apply_SkipsGitDirEntries(t *testing.T) {
	snapshot := buildSnapshot(t, map[string]string{
		".git/config": "malicious",
		"file.md":     "content",
	})

	tmpDir, err := os.MkdirTemp("", "snapshot-applier-git-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	applier := NewSnapshotApplier()
	if err := applier.Apply(snapshot, tmpDir, "docs"); err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}

	// .git directory should not be created under docsDir.
	if _, err := os.Stat(filepath.Join(tmpDir, "docs", ".git")); !os.IsNotExist(err) {
		t.Errorf("expected .git directory to be skipped, got err: %v", err)
	}
	// Normal file should still be extracted.
	if _, err := os.Stat(filepath.Join(tmpDir, "docs", "file.md")); err != nil {
		t.Errorf("expected file.md to be extracted, got err: %v", err)
	}
}
