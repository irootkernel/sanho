package wsstate_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/irootkernel/sanho/internal/domain/client"
	"github.com/irootkernel/sanho/internal/domain/docs"
	"github.com/irootkernel/sanho/internal/domain/provenance"
	"github.com/irootkernel/sanho/internal/domain/workspace"
	"github.com/irootkernel/sanho/internal/infra/wsstate"
)

// --- Config: ApplyDefaults ---------------------------------------------

func TestConfigApplyDefaults_SetsDocsDirWhenEmpty(t *testing.T) {
	c := wsstate.Config{}
	c.ApplyDefaults()
	if c.DocsDir != "docs" {
		t.Fatalf("DocsDir = %q, want %q", c.DocsDir, "docs")
	}
}

func TestConfigApplyDefaults_PreservesExplicitDocsDir(t *testing.T) {
	c := wsstate.Config{DocsDir: "documentation"}
	c.ApplyDefaults()
	if c.DocsDir != "documentation" {
		t.Fatalf("DocsDir = %q, want unchanged %q", c.DocsDir, "documentation")
	}
}

// --- Config: v1/v2 detection and round-trip ------------------------------

func TestLoadConfig_DetectsV1FromRealLegacyShape(t *testing.T) {
	dir := t.TempDir()

	legacy := client.WorkspaceConfig{
		SocketPath:  filepath.Join(dir, "sanhod.sock"),
		WorkspaceID: workspace.WorkspaceID("ws-legacy"),
		Project:     docs.ProjectName("legacy-project"),
		ActorEmail:  "legacy@example.com",
		DocsDir:     "docs",
	}
	legacy.ApplyDefaults()
	writeJSON(t, filepath.Join(dir, wsstate.ConfigFileName), legacy)

	got, err := wsstate.LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, want no error (must route to migrate guidance, not fail)", err)
	}
	if got.SchemaVersion != 1 {
		t.Fatalf("SchemaVersion = %d, want 1", got.SchemaVersion)
	}
	if got.WorkspaceID != "ws-legacy" || got.Project != "legacy-project" || got.ActorEmail != "legacy@example.com" || got.DocsDir != "docs" {
		t.Fatalf("mapped fields = %+v, want workspace_id/project/actor_email/docs_dir carried over", got)
	}
	if got.DocsRepoURL != "" {
		t.Fatalf("DocsRepoURL = %q, want empty (v1 never had one)", got.DocsRepoURL)
	}
}

func TestLoadConfig_DetectsV1WhenSchemaVersionAbsentEvenWithoutSocketPath(t *testing.T) {
	dir := t.TempDir()
	// A minimal/hypothetical file with no schema_version and no
	// socket_path must still be treated as v1 — "lacks schema_version"
	// is sufficient on its own per the detection rule.
	raw := map[string]string{"workspace_id": "ws", "project": "proj"}
	writeJSON(t, filepath.Join(dir, wsstate.ConfigFileName), raw)

	got, err := wsstate.LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, want no error", err)
	}
	if got.SchemaVersion != 1 {
		t.Fatalf("SchemaVersion = %d, want 1", got.SchemaVersion)
	}
}

func TestLoadConfig_V2RoundTripViaSaveConfig(t *testing.T) {
	dir := t.TempDir()
	want := wsstate.Config{
		SchemaVersion: 2,
		WorkspaceID:   "ws-1",
		Project:       "proj-1",
		DocsRepoURL:   "https://example.com/docs.git",
		ActorEmail:    "actor@example.com",
		DocsDir:       "docs",
	}

	if err := wsstate.SaveConfig(dir, want); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	got, err := wsstate.LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if got != want {
		t.Fatalf("round-tripped config = %+v, want %+v", got, want)
	}
}

func TestSaveConfig_WritesAtomicFileWithPrivateEnoughPermissionsAndForcesV2(t *testing.T) {
	dir := t.TempDir()
	// SchemaVersion deliberately wrong (as if a v1-detected Config were
	// mistakenly round-tripped straight into SaveConfig): SaveConfig
	// must still always persist schema_version 2, since it is documented
	// as writing "the v2 config" unconditionally.
	if err := wsstate.SaveConfig(dir, wsstate.Config{SchemaVersion: 1, Project: "p"}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	path := filepath.Join(dir, wsstate.ConfigFileName)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0644 {
		t.Fatalf("perm = %o, want 0644", perm)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var onDisk struct {
		SchemaVersion int `json:"schema_version"`
	}
	if err := json.Unmarshal(data, &onDisk); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if onDisk.SchemaVersion != 2 {
		t.Fatalf("on-disk schema_version = %d, want 2", onDisk.SchemaVersion)
	}

	assertNoLeftoverTemp(t, dir)
}

func TestLoadConfig_MissingFileReturnsNotExistError(t *testing.T) {
	dir := t.TempDir()
	_, err := wsstate.LoadConfig(dir)
	if err == nil {
		t.Fatal("LoadConfig() error = nil, want error for missing config")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("LoadConfig() error = %v, want it to wrap os.ErrNotExist", err)
	}
}

// --- Base file: v2 round-trip, legacy tolerance, fail-closed errors -----

func TestLoadBase_V2RoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := provenance.Base{
		Commit: "67c4bbfeada37f5dda8fb79aa43216ef062cd8df",
		Tree:   "2f41ab90c3d2e1f4a5b6c7d8e9f0a1b2c3d4e5f6",
	}

	if err := wsstate.SaveBase(dir, want); err != nil {
		t.Fatalf("SaveBase() error = %v", err)
	}

	got, ok, err := wsstate.LoadBase(dir)
	if err != nil {
		t.Fatalf("LoadBase() error = %v", err)
	}
	if !ok {
		t.Fatal("LoadBase() ok = false, want true")
	}
	if got != want {
		t.Fatalf("LoadBase() = %+v, want %+v", got, want)
	}

	// Confirm the on-disk schema matches §5.7 exactly.
	path := filepath.Join(dir, wsstate.BaseFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var onDisk struct {
		Version int    `json:"version"`
		Commit  string `json:"commit"`
		Tree    string `json:"tree"`
	}
	if err := json.Unmarshal(data, &onDisk); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if onDisk.Version != 2 || onDisk.Commit != want.Commit || onDisk.Tree != want.Tree {
		t.Fatalf("on-disk base file = %+v, want version=2 commit=%q tree=%q", onDisk, want.Commit, want.Tree)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0644 {
		t.Fatalf("perm = %o, want 0644", perm)
	}
}

func TestLoadBase_V2WithEmptyTreeIsValid(t *testing.T) {
	dir := t.TempDir()
	// Empty Tree is a legitimate state: adopted from a legacy
	// docs-version trailer before the tree is resolved
	// (sanho-v0.2.md §5.10; provenance.Base's own documented allowance).
	want := provenance.Base{Commit: "67c4bbfeada37f5dda8fb79aa43216ef062cd8df", Tree: ""}
	if err := wsstate.SaveBase(dir, want); err != nil {
		t.Fatalf("SaveBase() error = %v", err)
	}

	got, ok, err := wsstate.LoadBase(dir)
	if err != nil {
		t.Fatalf("LoadBase() error = %v", err)
	}
	if !ok || got != want {
		t.Fatalf("LoadBase() = %+v, %v, want %+v, true", got, ok, want)
	}
}

func TestLoadBase_LegacySingleLineTolerated(t *testing.T) {
	dir := t.TempDir()
	oid := "67c4bbfeada37f5dda8fb79aa43216ef062cd8df"
	writeFile(t, filepath.Join(dir, wsstate.LegacyHashFileName), oid+"\n")

	got, ok, err := wsstate.LoadBase(dir)
	if err != nil {
		t.Fatalf("LoadBase() error = %v", err)
	}
	if !ok {
		t.Fatal("LoadBase() ok = false, want true")
	}
	want := provenance.Base{Commit: oid, Tree: ""}
	if got != want {
		t.Fatalf("LoadBase() = %+v, want %+v", got, want)
	}
}

func TestLoadBase_LegacyEmptyFileErrors(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, wsstate.LegacyHashFileName), "")

	_, ok, err := wsstate.LoadBase(dir)
	if ok {
		t.Fatal("LoadBase() ok = true, want false on empty legacy file")
	}
	if !errors.Is(err, wsstate.ErrLegacyBaseEmpty) {
		t.Fatalf("LoadBase() error = %v, want ErrLegacyBaseEmpty", err)
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Fatalf("error %q does not mention the file is empty", err.Error())
	}
}

func TestLoadBase_LegacyWhitespaceOnlyFileErrors(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, wsstate.LegacyHashFileName), "   \n\t \n")

	_, ok, err := wsstate.LoadBase(dir)
	if ok {
		t.Fatal("LoadBase() ok = true, want false on whitespace-only legacy file")
	}
	if !errors.Is(err, wsstate.ErrLegacyBaseEmpty) {
		t.Fatalf("LoadBase() error = %v, want ErrLegacyBaseEmpty", err)
	}
}

func TestLoadBase_LegacyGarbageContentErrors(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, wsstate.LegacyHashFileName), "not-a-commit-oid\n")

	_, ok, err := wsstate.LoadBase(dir)
	if ok {
		t.Fatal("LoadBase() ok = true, want false on non-OID legacy content")
	}
	if err == nil {
		t.Fatal("LoadBase() error = nil, want error")
	}
}

func TestLoadBase_CorruptJSONErrors(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, wsstate.BaseFileName), "{not valid json")

	_, ok, err := wsstate.LoadBase(dir)
	if ok {
		t.Fatal("LoadBase() ok = true, want false on corrupt JSON")
	}
	if !errors.Is(err, wsstate.ErrBaseCorrupt) {
		t.Fatalf("LoadBase() error = %v, want ErrBaseCorrupt", err)
	}
}

func TestLoadBase_InvalidCommitOIDErrors(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, wsstate.BaseFileName), `{"version":2,"commit":"not-an-oid","tree":"2f41ab90c3d2e1f4a5b6c7d8e9f0a1b2c3d4e5f6"}`)

	_, ok, err := wsstate.LoadBase(dir)
	if ok {
		t.Fatal("LoadBase() ok = true, want false on invalid commit OID")
	}
	if !errors.Is(err, wsstate.ErrBaseCorrupt) {
		t.Fatalf("LoadBase() error = %v, want ErrBaseCorrupt", err)
	}
}

func TestLoadBase_InvalidTreeOIDErrors(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, wsstate.BaseFileName), `{"version":2,"commit":"67c4bbfeada37f5dda8fb79aa43216ef062cd8df","tree":"short"}`)

	_, ok, err := wsstate.LoadBase(dir)
	if ok {
		t.Fatal("LoadBase() ok = true, want false on invalid tree OID")
	}
	if !errors.Is(err, wsstate.ErrBaseCorrupt) {
		t.Fatalf("LoadBase() error = %v, want ErrBaseCorrupt", err)
	}
}

func TestLoadBase_EmptyCommitOIDErrors(t *testing.T) {
	dir := t.TempDir()
	// An empty Commit is never valid, even though an empty Tree is
	// (provenance.Base's documented rule, enforced locally here).
	writeFile(t, filepath.Join(dir, wsstate.BaseFileName), `{"version":2,"commit":"","tree":""}`)

	_, ok, err := wsstate.LoadBase(dir)
	if ok {
		t.Fatal("LoadBase() ok = true, want false on empty commit OID")
	}
	if !errors.Is(err, wsstate.ErrBaseCorrupt) {
		t.Fatalf("LoadBase() error = %v, want ErrBaseCorrupt", err)
	}
}

func TestLoadBase_NoFilesPresentReturnsOkFalseNilError(t *testing.T) {
	dir := t.TempDir()
	got, ok, err := wsstate.LoadBase(dir)
	if err != nil {
		t.Fatalf("LoadBase() error = %v, want nil", err)
	}
	if ok {
		t.Fatal("LoadBase() ok = true, want false")
	}
	if got != (provenance.Base{}) {
		t.Fatalf("LoadBase() = %+v, want zero value", got)
	}
}

func TestLoadBase_PrefersV2FileOverLegacyWhenBothPresent(t *testing.T) {
	dir := t.TempDir()
	legacyOID := "1111111111111111111111111111111111111a"
	writeFile(t, filepath.Join(dir, wsstate.LegacyHashFileName), legacyOID+"\n")

	v2 := provenance.Base{Commit: "67c4bbfeada37f5dda8fb79aa43216ef062cd8df", Tree: "2f41ab90c3d2e1f4a5b6c7d8e9f0a1b2c3d4e5f6"}
	if err := wsstate.SaveBase(dir, v2); err != nil {
		t.Fatalf("SaveBase() error = %v", err)
	}

	got, ok, err := wsstate.LoadBase(dir)
	if err != nil {
		t.Fatalf("LoadBase() error = %v", err)
	}
	if !ok || got != v2 {
		t.Fatalf("LoadBase() = %+v, %v, want the v2 file's %+v (legacy must be ignored when present)", got, ok, v2)
	}
}

// --- Sync note lifecycle --------------------------------------------------

func TestSyncNoteLifecycle(t *testing.T) {
	gitDir := t.TempDir()

	_, ok, err := wsstate.LoadSyncNote(gitDir)
	if err != nil {
		t.Fatalf("LoadSyncNote() error = %v, want nil when absent", err)
	}
	if ok {
		t.Fatal("LoadSyncNote() ok = true, want false when absent")
	}

	note := wsstate.SyncNote{
		PrevBase:  provenance.Base{Commit: "1111111111111111111111111111111111111a", Tree: "2222222222222222222222222222222222222b"},
		Target:    provenance.Base{Commit: "3333333333333333333333333333333333333c", Tree: "4444444444444444444444444444444444444d"},
		StartedAt: time.Now().UTC(),
	}
	if err := wsstate.SaveSyncNote(gitDir, note); err != nil {
		t.Fatalf("SaveSyncNote() error = %v", err)
	}

	got, ok, err := wsstate.LoadSyncNote(gitDir)
	if err != nil {
		t.Fatalf("LoadSyncNote() error = %v", err)
	}
	if !ok {
		t.Fatal("LoadSyncNote() ok = false, want true after Save")
	}
	if got.PrevBase != note.PrevBase {
		t.Fatalf("PrevBase = %+v, want %+v", got.PrevBase, note.PrevBase)
	}
	if got.Target != note.Target {
		t.Fatalf("Target = %+v, want %+v", got.Target, note.Target)
	}
	if !got.StartedAt.Equal(note.StartedAt) {
		t.Fatalf("StartedAt = %v, want %v", got.StartedAt, note.StartedAt)
	}

	// Parent directory must be created privately.
	info, err := os.Stat(filepath.Join(gitDir, "sanho"))
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if !info.IsDir() {
		t.Fatal("sanho parent path is not a directory")
	}
	if perm := info.Mode().Perm(); perm != 0700 {
		t.Fatalf("sanho directory perm = %o, want 0700", perm)
	}

	if err := wsstate.ClearSyncNote(gitDir); err != nil {
		t.Fatalf("ClearSyncNote() error = %v", err)
	}

	_, ok, err = wsstate.LoadSyncNote(gitDir)
	if err != nil {
		t.Fatalf("LoadSyncNote() error = %v after Clear", err)
	}
	if ok {
		t.Fatal("LoadSyncNote() ok = true, want false after Clear")
	}
}

func TestClearSyncNote_AbsentIsNoop(t *testing.T) {
	gitDir := t.TempDir()
	if err := wsstate.ClearSyncNote(gitDir); err != nil {
		t.Fatalf("ClearSyncNote() error = %v, want nil no-op when note never existed", err)
	}
}

func TestLoadSyncNote_CorruptJSONErrors(t *testing.T) {
	gitDir := t.TempDir()
	path := filepath.Join(gitDir, "sanho", "sync.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	writeFile(t, path, "{not valid json")

	_, ok, err := wsstate.LoadSyncNote(gitDir)
	if ok {
		t.Fatal("LoadSyncNote() ok = true, want false on corrupt JSON")
	}
	if err == nil {
		t.Fatal("LoadSyncNote() error = nil, want error on corrupt JSON")
	}
}

// --- test helpers ----------------------------------------------------------

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent() error = %v", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
}

func assertNoLeftoverTemp(t *testing.T, dir string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, ".*.tmp-*"))
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("leftover temp files: %v", matches)
	}
}
