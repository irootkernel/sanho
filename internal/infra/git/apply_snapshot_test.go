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
				"docs/a..b.md":     "content",
				"docs/file...name": "content",
			},
			wantErr: false,
		},
		{
			name: "Path traversal with .. rejected",
			files: map[string]string{
				"docs/../evil.txt": "malicious",
			},
			wantErr:   true,
			errSubstr: "path traversal not allowed",
		},
		{
			name: "Nested path traversal rejected",
			files: map[string]string{
				"docs/subdir/../../evil.txt": "malicious",
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
				"docs/subdir/file.md":   "content",
				"docs/a/b/c/deep.md":    "content",
				"docs/readme..final.md": "content",
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
