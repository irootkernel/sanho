package merge

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHasConflictMarkers(t *testing.T) {
	tests := []struct {
		name string
		text string
		want bool
	}{
		{
			name: "all markers present",
			text: `some code
<<<<<<< HEAD
my changes
=======
their changes
>>>>>>> branch
more code`,
			want: true,
		},
		{
			name: "missing start marker",
			text: `some code
=======
their changes
>>>>>>> branch`,
			want: false,
		},
		{
			name: "missing middle marker",
			text: `some code
<<<<<<< HEAD
my changes
>>>>>>> branch`,
			want: false,
		},
		{
			name: "missing end marker",
			text: `some code
<<<<<<< HEAD
my changes
=======`,
			want: false,
		},
		{
			name: "no markers",
			text: "normal file content",
			want: false,
		},
		{
			name: "empty text",
			text: "",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HasConflictMarkers(tt.text)
			if got != tt.want {
				t.Errorf("HasConflictMarkers() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFileConflictDetector_DetectConflicts(t *testing.T) {
	// Create temp directory structure
	tmpDir := t.TempDir()

	// Create files with different conflict states
	files := map[string]string{
		"clean.md": "this is clean content",
		"conflict.md": `file with conflict
<<<<<<< HEAD
local change
=======
remote change
>>>>>>> origin/main
`,
		"subdir/nested.md":          "nested clean file",
		"subdir/nested_conflict.md": "<<<<<<< HEAD\nfoo\n=======\nbar\n>>>>>>>\n",
	}

	for path, content := range files {
		fullPath := filepath.Join(tmpDir, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatalf("failed to create dir: %v", err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write file: %v", err)
		}
	}

	detector := NewFileConflictDetector()
	conflictFiles, err := detector.DetectConflicts(tmpDir)

	if err != nil {
		t.Fatalf("DetectConflicts failed: %v", err)
	}

	// Should find 2 conflict files
	if len(conflictFiles) != 2 {
		t.Errorf("expected 2 conflict files, got %d: %v", len(conflictFiles), conflictFiles)
	}

	// Check expected files are in the result
	expectedConflicts := map[string]bool{
		"conflict.md":               false,
		"subdir/nested_conflict.md": false,
	}

	for _, f := range conflictFiles {
		if _, ok := expectedConflicts[f]; ok {
			expectedConflicts[f] = true
		}
	}

	for f, found := range expectedConflicts {
		if !found {
			t.Errorf("expected conflict file %q not found in results", f)
		}
	}
}

func TestFileConflictDetector_EmptyDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	detector := NewFileConflictDetector()
	conflictFiles, err := detector.DetectConflicts(tmpDir)

	if err != nil {
		t.Fatalf("DetectConflicts failed: %v", err)
	}

	if len(conflictFiles) != 0 {
		t.Errorf("expected 0 conflict files for empty dir, got %d", len(conflictFiles))
	}
}
