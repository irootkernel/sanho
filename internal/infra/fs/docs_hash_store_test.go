package fs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/irootkernel/sanho/internal/domain/docs"
)

func TestFileDocsHashStore_ReadWrite(t *testing.T) {
	tmpDir := t.TempDir()
	hashFile := filepath.Join(tmpDir, ".sanho_docs_hash")
	store := NewFileDocsHashStore()

	// Test write
	expectedHash := docs.CommitHash("abc123def456")
	if err := store.Write(hashFile, expectedHash); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Test read
	hash, err := store.Read(hashFile)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	if hash != expectedHash {
		t.Errorf("Read returned %q, want %q", hash, expectedHash)
	}
}

func TestFileDocsHashStore_Read_FileNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	hashFile := filepath.Join(tmpDir, "nonexistent")
	store := NewFileDocsHashStore()

	_, err := store.Read(hashFile)
	if err == nil {
		t.Error("expected error for nonexistent file, got nil")
		return
	}

	if !errors.Is(err, ErrHashFileNotFound) {
		t.Errorf("expected ErrHashFileNotFound, got %v", err)
	}
}

func TestFileDocsHashStore_Read_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	hashFile := filepath.Join(tmpDir, ".sanho_docs_hash")
	store := NewFileDocsHashStore()

	// Create empty file
	if err := os.WriteFile(hashFile, []byte(""), 0644); err != nil {
		t.Fatalf("failed to create empty file: %v", err)
	}

	_, err := store.Read(hashFile)
	if err == nil {
		t.Error("expected error for empty file, got nil")
		return
	}

	if !errors.Is(err, ErrHashFileEmpty) {
		t.Errorf("expected ErrHashFileEmpty, got %v", err)
	}
}

func TestFileDocsHashStore_Read_WhitespaceOnly(t *testing.T) {
	tmpDir := t.TempDir()
	hashFile := filepath.Join(tmpDir, ".sanho_docs_hash")
	store := NewFileDocsHashStore()

	// Create file with only whitespace
	if err := os.WriteFile(hashFile, []byte("  \n\t  \n"), 0644); err != nil {
		t.Fatalf("failed to create whitespace file: %v", err)
	}

	_, err := store.Read(hashFile)
	if err == nil {
		t.Error("expected error for whitespace-only file, got nil")
		return
	}

	if !errors.Is(err, ErrHashFileEmpty) {
		t.Errorf("expected ErrHashFileEmpty, got %v", err)
	}
}

func TestFileDocsHashStore_Read_TrimsWhitespace(t *testing.T) {
	tmpDir := t.TempDir()
	hashFile := filepath.Join(tmpDir, ".sanho_docs_hash")
	store := NewFileDocsHashStore()

	// Create file with whitespace around hash
	if err := os.WriteFile(hashFile, []byte("  abc123  \n"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	hash, err := store.Read(hashFile)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	if hash != "abc123" {
		t.Errorf("expected trimmed hash 'abc123', got %q", hash)
	}
}
