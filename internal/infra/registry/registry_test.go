package registry_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/irootkernel/sanho/internal/infra/registry"
)

func TestOpen_CreatesHomeDirWithPrivatePermissions(t *testing.T) {
	root := t.TempDir()
	homeDir := filepath.Join(root, "nested", ".sanho")

	f, err := registry.Open(homeDir)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if f.HomeDir() != homeDir {
		t.Fatalf("HomeDir() = %q, want %q", f.HomeDir(), homeDir)
	}

	info, err := os.Stat(homeDir)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if !info.IsDir() {
		t.Fatal("homeDir was not created as a directory")
	}
	if perm := info.Mode().Perm(); perm != 0700 {
		t.Fatalf("homeDir perm = %o, want 0700", perm)
	}
}

func TestOpen_TightensExistingLoosePermissions(t *testing.T) {
	homeDir := t.TempDir()
	if err := os.Chmod(homeDir, 0755); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}

	if _, err := registry.Open(homeDir); err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	info, err := os.Stat(homeDir)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0700 {
		t.Fatalf("homeDir perm = %o, want 0700 (Open must tighten a pre-existing directory)", perm)
	}
}

func TestRead_FreshHomeReturnsEmptyV2State(t *testing.T) {
	f := openT(t)

	st, err := f.Read(context.Background())
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if st.Version != 2 {
		t.Fatalf("Version = %d, want 2", st.Version)
	}
	if st.Projects == nil || len(st.Projects) != 0 {
		t.Fatalf("Projects = %#v, want non-nil empty map", st.Projects)
	}
	if st.Workspaces == nil || len(st.Workspaces) != 0 {
		t.Fatalf("Workspaces = %#v, want non-nil empty map", st.Workspaces)
	}

	// A fresh home must not have manifested state.json just from Read.
	if _, err := os.Stat(filepath.Join(f.HomeDir(), registry.StateFileName)); !os.IsNotExist(err) {
		t.Fatalf("Stat(state.json) error = %v, want os.IsNotExist (Read must not write on a missing file)", err)
	}
}

func TestReadCompatible_ProjectsLegacyStateWithoutWritingIt(t *testing.T) {
	f := openT(t)
	legacy := []byte(`{
  "docs_repos": {"docs": {"ID": "docs", "Path": "/cache/docs", "RepoURL": "ssh://example.test/docs.git"}},
  "project_to_docs_repo": {"product": "docs"},
  "workspaces": {
    "product:/app": {
      "project": "product",
      "local_path": "/app",
      "docs_hash": "abc123",
      "owner_email": "owner@example.test",
      "last_updated_at": "2026-08-08T00:00:00Z"
    }
  }
}`)
	path := filepath.Join(f.HomeDir(), registry.StateFileName)
	if err := os.WriteFile(path, legacy, 0600); err != nil {
		t.Fatal(err)
	}

	state, err := f.ReadCompatible(context.Background())
	if err != nil {
		t.Fatalf("ReadCompatible: %v", err)
	}
	if got := state.Projects["product"].DocsRepoURL; got != "ssh://example.test/docs.git" {
		t.Fatalf("project URL = %q", got)
	}
	workspace := state.Workspaces["product:/app"]
	if workspace.LocalPath != "/app" || workspace.BaseCommit != "abc123" || workspace.ActorEmail != "owner@example.test" {
		t.Fatalf("workspace = %+v", workspace)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(legacy) {
		t.Fatalf("ReadCompatible rewrote legacy state:\n%s", after)
	}
	if _, err := f.Read(context.Background()); !errors.Is(err, registry.ErrLegacyState) {
		t.Fatalf("ordinary Read = %v, want ErrLegacyState", err)
	}
}

func TestUpdate_RoundTrip(t *testing.T) {
	f := openT(t)
	ctx := context.Background()

	err := f.Update(ctx, func(s *registry.State) error {
		s.Projects["proj"] = registry.Project{DocsRepoURL: "https://example.com/docs.git"}
		s.Workspaces["proj:/repo"] = registry.Workspace{
			Project:       "proj",
			LocalPath:     "/repo",
			BaseCommit:    "67c4bbfeada37f5dda8fb79aa43216ef062cd8df",
			BaseTree:      "2f41ab90c3d2e1f4a5b6c7d8e9f0a1b2c3d4e5f6",
			ActorEmail:    "actor@example.com",
			LastUpdatedAt: time.Now().UTC(),
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	st, err := f.Read(ctx)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	proj, ok := st.Projects["proj"]
	if !ok || proj.DocsRepoURL != "https://example.com/docs.git" {
		t.Fatalf("Projects[proj] = %+v, %v, want the saved project", proj, ok)
	}
	ws, ok := st.Workspaces["proj:/repo"]
	if !ok || ws.BaseCommit != "67c4bbfeada37f5dda8fb79aa43216ef062cd8df" {
		t.Fatalf("Workspaces[proj:/repo] = %+v, %v, want the saved workspace", ws, ok)
	}

	// Primary and backup must both exist, mode 0600, with matching content.
	for _, name := range []string{registry.StateFileName, registry.StateFileName + registry.BackupSuffix} {
		path := filepath.Join(f.HomeDir(), name)
		info, statErr := os.Stat(path)
		if statErr != nil {
			t.Fatalf("Stat(%s) error = %v", name, statErr)
		}
		if perm := info.Mode().Perm(); perm != 0600 {
			t.Fatalf("%s perm = %o, want 0600", name, perm)
		}
	}
	primary, err := os.ReadFile(filepath.Join(f.HomeDir(), registry.StateFileName))
	if err != nil {
		t.Fatalf("ReadFile(primary) error = %v", err)
	}
	backup, err := os.ReadFile(filepath.Join(f.HomeDir(), registry.StateFileName+registry.BackupSuffix))
	if err != nil {
		t.Fatalf("ReadFile(backup) error = %v", err)
	}
	if string(primary) != string(backup) {
		t.Fatalf("primary and backup content differ:\nprimary: %s\nbackup:  %s", primary, backup)
	}
}

func TestUpdate_FnErrorAbortsWithoutWriting(t *testing.T) {
	f := openT(t)
	ctx := context.Background()

	sentinel := errors.New("boom")
	err := f.Update(ctx, func(s *registry.State) error {
		s.Projects["should-not-persist"] = registry.Project{DocsRepoURL: "https://example.com/x.git"}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Update() error = %v, want sentinel %v", err, sentinel)
	}

	if _, statErr := os.Stat(filepath.Join(f.HomeDir(), registry.StateFileName)); !os.IsNotExist(statErr) {
		t.Fatalf("state.json exists after a failed fn, want no write at all; stat error = %v", statErr)
	}

	st, err := f.Read(ctx)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if _, ok := st.Projects["should-not-persist"]; ok {
		t.Fatal("aborted mutation leaked into subsequent Read()")
	}
}

func TestUpdate_FnErrorAfterPriorSuccessLeavesPriorStateIntact(t *testing.T) {
	f := openT(t)
	ctx := context.Background()

	if err := f.Update(ctx, func(s *registry.State) error {
		s.Projects["keep"] = registry.Project{DocsRepoURL: "https://example.com/keep.git"}
		return nil
	}); err != nil {
		t.Fatalf("first Update() error = %v", err)
	}

	sentinel := errors.New("boom")
	err := f.Update(ctx, func(s *registry.State) error {
		s.Projects["drop"] = registry.Project{DocsRepoURL: "https://example.com/drop.git"}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("second Update() error = %v, want sentinel", err)
	}

	st, err := f.Read(ctx)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if _, ok := st.Projects["keep"]; !ok {
		t.Fatal("prior successful state was lost after a later failed Update")
	}
	if _, ok := st.Projects["drop"]; ok {
		t.Fatal("failed Update's mutation leaked despite returning an error")
	}
}

func TestRead_CorruptPrimaryRecoversFromBackupAndRestoresPrimary(t *testing.T) {
	f := openT(t)
	ctx := context.Background()

	if err := f.Update(ctx, func(s *registry.State) error {
		s.Projects["proj"] = registry.Project{DocsRepoURL: "https://example.com/docs.git"}
		return nil
	}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	primaryPath := filepath.Join(f.HomeDir(), registry.StateFileName)
	if err := os.WriteFile(primaryPath, []byte("{not valid json"), 0600); err != nil {
		t.Fatalf("corrupt primary: %v", err)
	}

	st, err := f.Read(ctx)
	if err != nil {
		t.Fatalf("Read() error = %v, want recovery from .bak", err)
	}
	if _, ok := st.Projects["proj"]; !ok {
		t.Fatal("recovered state is missing data that was only in the backup")
	}

	// The primary must have been healed with valid, matching JSON.
	data, err := os.ReadFile(primaryPath)
	if err != nil {
		t.Fatalf("ReadFile(primary) error = %v", err)
	}
	if !json.Valid(data) {
		t.Fatal("primary was not restored with valid JSON")
	}
	var restored registry.State
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal(restored primary) error = %v", err)
	}
	if _, ok := restored.Projects["proj"]; !ok {
		t.Fatal("restored primary on disk does not contain the recovered data")
	}
}

func TestRead_BothPrimaryAndBackupCorruptErrors(t *testing.T) {
	f := openT(t)
	ctx := context.Background()

	if err := f.Update(ctx, func(s *registry.State) error {
		s.Projects["proj"] = registry.Project{DocsRepoURL: "https://example.com/docs.git"}
		return nil
	}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	primaryPath := filepath.Join(f.HomeDir(), registry.StateFileName)
	backupPath := filepath.Join(f.HomeDir(), registry.StateFileName+registry.BackupSuffix)
	if err := os.WriteFile(primaryPath, []byte("{not valid json"), 0600); err != nil {
		t.Fatalf("corrupt primary: %v", err)
	}
	if err := os.WriteFile(backupPath, []byte("{also not valid"), 0600); err != nil {
		t.Fatalf("corrupt backup: %v", err)
	}

	st, err := f.Read(ctx)
	if err == nil {
		t.Fatal("Read() error = nil, want error when both primary and backup are corrupt")
	}
	if !errors.Is(err, registry.ErrRegistryUnreadable) {
		t.Fatalf("Read() error = %v, want ErrRegistryUnreadable", err)
	}
	if !reflect.DeepEqual(st, registry.State{}) {
		t.Fatalf("Read() state = %+v, want zero value on error (never silently start empty)", st)
	}
	if !containsAll(err.Error(), primaryPath, backupPath) {
		t.Fatalf("error %q does not name both %q and %q", err.Error(), primaryPath, backupPath)
	}
}

func TestUpdate_ConcurrentGoroutinesAllPersist(t *testing.T) {
	f := openT(t)
	ctx := context.Background()

	const n = 20
	var wg sync.WaitGroup
	errCh := make(chan error, n)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := fmt.Sprintf("proj:/repo-%02d", i)
			err := f.Update(ctx, func(s *registry.State) error {
				s.Workspaces[key] = registry.Workspace{
					Project:       "proj",
					LocalPath:     fmt.Sprintf("/repo-%02d", i),
					BaseCommit:    "67c4bbfeada37f5dda8fb79aa43216ef062cd8df",
					LastUpdatedAt: time.Now().UTC(),
				}
				return nil
			})
			if err != nil {
				errCh <- err
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("Update() error = %v", err)
	}

	st, err := f.Read(ctx)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if len(st.Workspaces) != n {
		t.Fatalf("len(Workspaces) = %d, want %d — a concurrent Update was lost", len(st.Workspaces), n)
	}
	for i := range n {
		key := fmt.Sprintf("proj:/repo-%02d", i)
		if _, ok := st.Workspaces[key]; !ok {
			t.Fatalf("Workspaces[%q] missing after concurrent Update", key)
		}
	}
}

// openT opens a registry rooted at a fresh temp directory, failing the
// test on error.
func openT(t *testing.T) *registry.File {
	t.Helper()
	f, err := registry.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	return f
}

func containsAll(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
