package fs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/irootkernel/sanho/internal/domain/client"
	"github.com/irootkernel/sanho/internal/domain/docs"
)

func TestFilePendingFixStore_ReadWriteRemove(t *testing.T) {
	tmpDir := t.TempDir()
	fixFile := filepath.Join(tmpDir, ".sanho_pending_fix")
	store := NewFilePendingFixStore()

	// Test write
	expectedState := client.PendingFixState{
		BaseHash:   docs.CommitHash("base123"),
		RemoteHash: docs.CommitHash("remote456"),
		CreatedAt:  time.Now().Truncate(time.Second), // Truncate for comparison
	}
	if err := store.Write(fixFile, expectedState); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Test read
	state, exists, err := store.Read(fixFile)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if !exists {
		t.Error("Read returned exists=false, expected true")
	}

	if state.BaseHash != expectedState.BaseHash {
		t.Errorf("BaseHash = %q, want %q", state.BaseHash, expectedState.BaseHash)
	}
	if state.RemoteHash != expectedState.RemoteHash {
		t.Errorf("RemoteHash = %q, want %q", state.RemoteHash, expectedState.RemoteHash)
	}
	// Compare times with some tolerance for JSON serialization
	if state.CreatedAt.Sub(expectedState.CreatedAt).Abs() > time.Second {
		t.Errorf("CreatedAt = %v, want %v", state.CreatedAt, expectedState.CreatedAt)
	}

	// Test remove
	if err := store.Remove(fixFile); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	// Verify file is gone
	_, exists, err = store.Read(fixFile)
	if err != nil {
		t.Fatalf("Read after remove failed: %v", err)
	}
	if exists {
		t.Error("file still exists after remove")
	}
}

func TestFilePendingFixStore_Read_FileNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	fixFile := filepath.Join(tmpDir, "nonexistent")
	store := NewFilePendingFixStore()

	_, exists, err := store.Read(fixFile)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if exists {
		t.Error("exists should be false for nonexistent file")
	}
}

func TestFilePendingFixStore_Read_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	fixFile := filepath.Join(tmpDir, ".sanho_pending_fix")
	store := NewFilePendingFixStore()

	// Create file with invalid JSON
	if err := os.WriteFile(fixFile, []byte("{invalid json"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	_, exists, err := store.Read(fixFile)
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
		return
	}
	if exists {
		t.Error("exists should be false for invalid file")
	}
	if !errors.Is(err, ErrPendingFixParse) {
		t.Errorf("expected ErrPendingFixParse, got %v", err)
	}
}

func TestFilePendingFixStore_Remove_NonexistentFile(t *testing.T) {
	tmpDir := t.TempDir()
	fixFile := filepath.Join(tmpDir, "nonexistent")
	store := NewFilePendingFixStore()

	// Remove should not error for nonexistent file
	if err := store.Remove(fixFile); err != nil {
		t.Errorf("Remove should not error for nonexistent file: %v", err)
	}
}
