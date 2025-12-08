package git

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"

	"github.com/SeventeenthEarth/kkachi/internal/domain/docs"
)

func TestApplySnapshot_PathTraversal(t *testing.T) {
	repo := &GitDocsRepository{}

	tests := []struct {
		name      string
		files     map[string]string
		wantErr   bool
		errSubstr string
	}{
		{
			name: "Normal filename with dots allowed",
			files: map[string]string{
				"a..b.md":     "content",
				"file...name": "content",
			},
			wantErr: false,
		},
		{
			name: "Path traversal with .. rejected",
			files: map[string]string{
				"../evil.txt": "malicious",
			},
			wantErr:   true,
			errSubstr: "path traversal not allowed",
		},
		{
			name: "Nested path traversal rejected",
			files: map[string]string{
				"subdir/../../evil.txt": "malicious",
			},
			wantErr:   true,
			errSubstr: "path traversal not allowed",
		},
		{
			name: "Absolute path rejected",
			files: map[string]string{
				"/etc/passwd": "malicious",
			},
			wantErr:   true,
			errSubstr: "absolute path not allowed",
		},
		{
			name: "Normal nested paths allowed",
			files: map[string]string{
				"subdir/file.md":   "content",
				"a/b/c/deep.md":    "content",
				"readme..final.md": "content",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir, err := os.MkdirTemp("", "apply-snapshot-test-*")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(tempDir)

			docsDir := filepath.Join(tempDir, "docs")
			snapshot := createTestSnapshot(t, tt.files)

			err = repo.applySnapshot(docsDir, snapshot)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got nil")
					return
				}
				if tt.errSubstr != "" && !bytes.Contains([]byte(err.Error()), []byte(tt.errSubstr)) {
					t.Errorf("error %q should contain %q", err.Error(), tt.errSubstr)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func createTestSnapshot(t *testing.T, files map[string]string) docs.DocsSnapshot {
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
			t.Fatalf("Failed to write tar header: %v", err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("Failed to write tar content: %v", err)
		}
	}

	tw.Close()
	gw.Close()

	return docs.DocsSnapshot(buf.Bytes())
}

func TestApplySnapshot_SkipsGitDirEntries(t *testing.T) {
	repo := &GitDocsRepository{}

	tempDir, err := os.MkdirTemp("", "apply-snapshot-git-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	docsRoot := tempDir

	// Prepare an existing .git/config that must not be modified by snapshots.
	gitDir := filepath.Join(docsRoot, ".git")
	if err := os.MkdirAll(gitDir, 0755); err != nil {
		t.Fatalf("failed to create .git dir: %v", err)
	}
	gitConfigPath := filepath.Join(gitDir, "config")
	if err := os.WriteFile(gitConfigPath, []byte("original"), 0644); err != nil {
		t.Fatalf("failed to write initial git config: %v", err)
	}

	snapshot := createTestSnapshot(t, map[string]string{
		".git/config": "malicious",
		"file.md":     "content",
	})

	if err := repo.applySnapshot(docsRoot, snapshot); err != nil {
		t.Fatalf("applySnapshot returned error: %v", err)
	}

	// .git/config must remain unchanged.
	data, err := os.ReadFile(gitConfigPath)
	if err != nil {
		t.Fatalf("failed to read git config after snapshot: %v", err)
	}
	if string(data) != "original" {
		t.Errorf("expected .git/config to remain unchanged, got %q", string(data))
	}

	// Normal files should still be written.
	if _, err := os.Stat(filepath.Join(docsRoot, "file.md")); err != nil {
		t.Errorf("expected file.md to be created, got error: %v", err)
	}
}
