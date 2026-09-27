package wsstate_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/irootkernel/sanho/internal/infra/wsstate"
)

func TestSyncNoteObservationIdentityContentAndReadErrors(t *testing.T) {
	dir := t.TempDir()
	observe := func() wsstate.SyncNoteObservation {
		t.Helper()
		o, err := wsstate.ObserveSyncNote(dir)
		if err != nil {
			t.Fatal(err)
		}
		return o
	}
	absent := observe()
	if _, exists, err := absent.Load(); exists || err != nil || !absent.Same(observe()) {
		t.Fatalf("absent observation: exists=%t err=%v", exists, err)
	}
	if err := wsstate.SaveSyncNote(dir, wsstate.SyncNote{}); err != nil {
		t.Fatal(err)
	}
	first := observe()
	if absent.Same(first) || !first.Same(observe()) {
		t.Fatal("presence or stable identity comparison failed")
	}
	// The normal atomic writer replaces the inode even with identical bytes.
	if err := wsstate.SaveSyncNote(dir, wsstate.SyncNote{}); err != nil {
		t.Fatal(err)
	}
	if first.Same(observe()) {
		t.Fatal("same-content replacement was missed")
	}
	path := filepath.Join(dir, wsstate.SyncNoteRelPath)
	if err := os.WriteFile(path, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	corrupt := observe()
	if _, exists, err := corrupt.Load(); !exists || !errors.Is(err, wsstate.ErrSyncNoteCorrupt) {
		t.Fatalf("corrupt observation: exists=%t err=%v", exists, err)
	}
	if _, exists, err := first.Load(); !exists || err != nil {
		t.Fatal("captured bytes changed after the file was rewritten")
	}
	if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if corrupt.Same(observe()) {
		t.Fatal("in-place content change was missed")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if !absent.Same(observe()) || first.Same(observe()) {
		t.Fatal("removal was missed")
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := wsstate.ObserveSyncNote(dir); err == nil || errors.Is(err, wsstate.ErrSyncNoteCorrupt) {
		t.Fatalf("unreadable note must remain an operational error: %v", err)
	}
}
