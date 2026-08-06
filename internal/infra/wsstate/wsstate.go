// Package wsstate owns the workspace-local state files of sanho v0.2
// (sanho-v0.2.md §5.7): the base file, the v2 workspace config, and the
// sync-in-progress note. All writes go through fsx.WriteFileAtomic.
package wsstate

import (
	"time"

	"github.com/irootkernel/sanho/internal/domain/provenance"
)

// File names (workspace root unless noted).
const (
	// BaseFileName replaces v0.1's .sanho_docs_hash. Reads tolerate the
	// legacy single-line format (§8): a bare OID loads as
	// {Commit: oid, Tree: ""}.
	BaseFileName = ".sanho_base.json"
	// LegacyHashFileName is read-only v0.1 compatibility input.
	LegacyHashFileName = ".sanho_docs_hash"
	// ConfigFileName is the workspace config (v2 adds docs_repo_url and
	// schema_version, drops socket_path).
	ConfigFileName = ".sanho.json"
	// SyncNoteRelPath is relative to the app repo's git dir; exists only
	// between a conflicted sync and its resolution/abort.
	SyncNoteRelPath = "sanho/sync.json"
)

// Config is the v2 workspace configuration.
type Config struct {
	SchemaVersion int    `json:"schema_version"`
	WorkspaceID   string `json:"workspace_id"`
	Project       string `json:"project"`
	DocsRepoURL   string `json:"docs_repo_url"`
	ActorEmail    string `json:"actor_email"`
	DocsDir       string `json:"docs_dir"`
}

// ApplyDefaults fills zero-value optional fields (DocsDir "docs").
func (c *Config) ApplyDefaults() {
	panic("unimplemented (sanho v0.2 P1)")
}

// LoadConfig reads ConfigFileName in workDir. A v0.1 config (no
// schema_version / has socket_path) is returned with SchemaVersion 1 so
// callers can route to the migrate guidance (§8 degradation).
func LoadConfig(workDir string) (Config, error) {
	panic("unimplemented (sanho v0.2 P1)")
}

// SaveConfig writes the v2 config atomically.
func SaveConfig(workDir string, c Config) error {
	panic("unimplemented (sanho v0.2 P1)")
}

// LoadBase reads the base pointer: BaseFileName if present, else the
// legacy hash file (as {Commit, ""}), else ok=false. Corrupt content
// errors (fail closed; audit M5/A2.4 messaging lesson).
func LoadBase(workDir string) (b provenance.Base, ok bool, err error) {
	panic("unimplemented (sanho v0.2 P1)")
}

// SaveBase writes BaseFileName atomically.
func SaveBase(workDir string, b provenance.Base) error {
	panic("unimplemented (sanho v0.2 P1)")
}

// SyncNote records an in-progress conflicted sync (§5.5).
type SyncNote struct {
	PrevBase  provenance.Base `json:"prev_base"`
	Target    provenance.Base `json:"target"`
	StartedAt time.Time       `json:"started_at"`
}

// LoadSyncNote returns the note and ok=false when absent. gitDir is the
// app repo's resolved git dir (worktree-private).
func LoadSyncNote(gitDir string) (SyncNote, bool, error) {
	panic("unimplemented (sanho v0.2 P1)")
}

// SaveSyncNote / ClearSyncNote manage the note atomically.
func SaveSyncNote(gitDir string, n SyncNote) error {
	panic("unimplemented (sanho v0.2 P1)")
}

func ClearSyncNote(gitDir string) error {
	panic("unimplemented (sanho v0.2 P1)")
}
