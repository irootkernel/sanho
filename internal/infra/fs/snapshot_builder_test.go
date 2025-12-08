package fs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSnapshotBuilder_Build(t *testing.T) {
	// Create temp directory with test files
	tempDir := t.TempDir()
	docsDir := filepath.Join(tempDir, "my_docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatalf("Failed to create docs dir: %v", err)
	}

	// Create test files
	if err := os.WriteFile(filepath.Join(docsDir, "readme.md"), []byte("# Hello"), 0644); err != nil {
		t.Fatalf("Failed to create readme.md: %v", err)
	}

	subDir := filepath.Join(docsDir, "subdir")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("Failed to create subdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "file.txt"), []byte("content"), 0644); err != nil {
		t.Fatalf("Failed to create file.txt: %v", err)
	}

	// Build snapshot
	builder := NewSnapshotBuilder()
	snapshot, err := builder.Build(docsDir)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	if len(snapshot) == 0 {
		t.Error("Snapshot should not be empty")
	}

	// Apply snapshot to verify roundtrip
	outputDir := filepath.Join(tempDir, "output")
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		t.Fatalf("Failed to create output dir: %v", err)
	}

	applier := NewSnapshotApplier()
	if err := applier.Apply(snapshot, outputDir, "docs"); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	// Verify files exist
	readmeContent, err := os.ReadFile(filepath.Join(outputDir, "docs", "readme.md"))
	if err != nil {
		t.Fatalf("Failed to read readme.md: %v", err)
	}
	if string(readmeContent) != "# Hello" {
		t.Errorf("readme.md content mismatch: got %q, want %q", string(readmeContent), "# Hello")
	}

	fileContent, err := os.ReadFile(filepath.Join(outputDir, "docs", "subdir", "file.txt"))
	if err != nil {
		t.Fatalf("Failed to read file.txt: %v", err)
	}
	if string(fileContent) != "content" {
		t.Errorf("file.txt content mismatch: got %q, want %q", string(fileContent), "content")
	}
}

func TestSnapshotBuilder_Build_NonExistentDir(t *testing.T) {
	builder := NewSnapshotBuilder()
	_, err := builder.Build("/nonexistent/path")
	if err == nil {
		t.Error("Expected error for non-existent directory")
	}
}

func TestSnapshotBuilder_Build_NotADirectory(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "file.txt")
	if err := os.WriteFile(filePath, []byte("content"), 0644); err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	builder := NewSnapshotBuilder()
	_, err := builder.Build(filePath)
	if err == nil {
		t.Error("Expected error for file path instead of directory")
	}
}

func TestSnapshotBuilder_Build_EmptyDir(t *testing.T) {
	tempDir := t.TempDir()
	emptyDir := filepath.Join(tempDir, "empty")
	if err := os.MkdirAll(emptyDir, 0755); err != nil {
		t.Fatalf("Failed to create empty dir: %v", err)
	}

	builder := NewSnapshotBuilder()
	snapshot, err := builder.Build(emptyDir)
	if err != nil {
		t.Fatalf("Build failed for empty dir: %v", err)
	}

	// Empty directory should still produce a valid (though minimal) tar.gz
	if len(snapshot) == 0 {
		t.Error("Snapshot should not be empty even for empty directory")
	}
}
