package fs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMainPublicationStoreEnsureIsDurableAndIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sanho", "main-publication.json")
	store := NewMainPublicationStore(path)
	first := MainPublicationCommit{
		Commit:   "sync-1",
		Parent:   "base",
		DocsHash: "docs-1",
		Subject:  "[SANHO] Update docs",
	}
	if err := store.Ensure("origin-main", first); err != nil {
		t.Fatal(err)
	}
	if err := store.Ensure("ignored-base", first); err != nil {
		t.Fatal(err)
	}
	second := MainPublicationCommit{
		Commit:   "sync-2",
		Parent:   "user-main",
		DocsHash: "docs-2",
		Subject:  "[SANHO] Update docs",
	}
	if err := store.Ensure("ignored-base", second); err != nil {
		t.Fatal(err)
	}

	state, exists, err := store.Load()
	if err != nil || !exists {
		t.Fatalf("load state=%+v exists=%v err=%v", state, exists, err)
	}
	if state.BaseCommit != "origin-main" || len(state.Commits) != 2 {
		t.Fatalf("state=%+v", state)
	}
	if state.Commits[0] != first || state.Commits[1] != second {
		t.Fatalf("commits=%+v", state.Commits)
	}
	if state.CreatedAt.IsZero() || state.UpdatedAt.IsZero() {
		t.Fatalf("timestamps=%+v", state)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("mode=%#o want 0600", info.Mode().Perm())
	}

	if err := store.Remove(); err != nil {
		t.Fatal(err)
	}
	if _, exists, err := store.Load(); err != nil || exists {
		t.Fatalf("state remained: exists=%v err=%v", exists, err)
	}
}

func TestMainPublicationStoreRejectsCorruptState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"commits":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := NewMainPublicationStore(path).Load(); err == nil {
		t.Fatal("empty publication state was accepted")
	}
}
