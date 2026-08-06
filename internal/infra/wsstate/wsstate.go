// Package wsstate owns the workspace-local state files of sanho v0.2
// (sanho-v0.2.md §5.7): the base file, the v2 workspace config, and the
// sync-in-progress note. All writes go through fsx.WriteFileAtomic.
package wsstate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/irootkernel/sanho/internal/domain/provenance"
	"github.com/irootkernel/sanho/internal/infra/fsx"
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

// currentBaseFileVersion is the "version" field written to and expected
// in BaseFileName (§5.7). LoadBase does not reject other values outright
// (forward compatibility for a field the schema itself may grow); the
// OIDs are what is validated.
const currentBaseFileVersion = 2

// currentConfigSchemaVersion is the schema_version SaveConfig writes.
const currentConfigSchemaVersion = 2

// defaultDocsDir is ApplyDefaults' fallback for Config.DocsDir.
const defaultDocsDir = "docs"

// Sentinel errors. All are wrapped with the offending path via %w so
// callers and tests can both errors.Is() and read a precise message
// (fail-closed reads never guess a base — sanho-v0.2.md §5.7).
var (
	// ErrBaseCorrupt covers a present BaseFileName that fails to parse as
	// JSON, or a legacy/v2 base whose OIDs do not match
	// provenance.OIDPattern.
	ErrBaseCorrupt = errors.New("base file is corrupt")
	// ErrLegacyBaseEmpty is returned distinctly (rather than folded into
	// ErrBaseCorrupt) so the message says plainly that the file is empty
	// instead of a generic "invalid" — the messaging lesson carried over
	// from audit M5/A2.4.
	ErrLegacyBaseEmpty = errors.New("legacy base file is empty")
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
	if c.DocsDir == "" {
		c.DocsDir = defaultDocsDir
	}
}

// v1ConfigFields is the superset of field names LoadConfig inspects to
// tell a v0.1 config (has socket_path, no schema_version) from a v2 one,
// without depending on the retired client.WorkspaceConfig type.
type v1ConfigFields struct {
	SchemaVersion int    `json:"schema_version"`
	SocketPath    string `json:"socket_path"`
	WorkspaceID   string `json:"workspace_id"`
	Project       string `json:"project"`
	DocsRepoURL   string `json:"docs_repo_url"`
	ActorEmail    string `json:"actor_email"`
	DocsDir       string `json:"docs_dir"`
}

// LoadConfig reads ConfigFileName in workDir. A v0.1 config (no
// schema_version / has socket_path) is returned with SchemaVersion 1 so
// callers can route to the migrate guidance (§8 degradation).
func LoadConfig(workDir string) (Config, error) {
	path := filepath.Join(workDir, ConfigFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read workspace config %s: %w", path, err)
	}

	var raw v1ConfigFields
	if err := json.Unmarshal(data, &raw); err != nil {
		return Config{}, fmt.Errorf("parse workspace config %s: %w", path, err)
	}

	// v0.1 configs never carry schema_version and always carry
	// socket_path; either symptom alone is enough to route to migrate
	// guidance rather than error (§8 pre-migration degradation).
	if raw.SchemaVersion == 0 || raw.SocketPath != "" {
		return Config{
			SchemaVersion: 1,
			WorkspaceID:   raw.WorkspaceID,
			Project:       raw.Project,
			ActorEmail:    raw.ActorEmail,
			DocsDir:       raw.DocsDir,
		}, nil
	}

	return Config{
		SchemaVersion: raw.SchemaVersion,
		WorkspaceID:   raw.WorkspaceID,
		Project:       raw.Project,
		DocsRepoURL:   raw.DocsRepoURL,
		ActorEmail:    raw.ActorEmail,
		DocsDir:       raw.DocsDir,
	}, nil
}

// SaveConfig writes the v2 config atomically. SchemaVersion is always
// stamped as the current version (2): this function's sole purpose is
// persisting v2 configs, so a caller-supplied value (e.g. one echoed
// back from a v1 LoadConfig) never leaks into a written file.
func SaveConfig(workDir string, c Config) error {
	c.SchemaVersion = currentConfigSchemaVersion

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal workspace config: %w", err)
	}

	path := filepath.Join(workDir, ConfigFileName)
	if err := fsx.WriteFileAtomic(path, data, 0644); err != nil {
		return fmt.Errorf("write workspace config %s: %w", path, err)
	}
	return nil
}

// baseFileV2 is the on-disk shape of BaseFileName (§5.7).
type baseFileV2 struct {
	Version int    `json:"version"`
	Commit  string `json:"commit"`
	Tree    string `json:"tree"`
}

// LoadBase reads the base pointer: BaseFileName if present, else the
// legacy hash file (as {Commit, ""}), else ok=false. Corrupt content
// errors (fail closed; audit M5/A2.4 messaging lesson).
func LoadBase(workDir string) (b provenance.Base, ok bool, err error) {
	primaryPath := filepath.Join(workDir, BaseFileName)
	data, readErr := os.ReadFile(primaryPath)
	switch {
	case readErr == nil:
		return loadBaseV2(primaryPath, data)
	case os.IsNotExist(readErr):
		// Fall through to the legacy file.
	default:
		return provenance.Base{}, false, fmt.Errorf("read base file %s: %w", primaryPath, readErr)
	}

	legacyPath := filepath.Join(workDir, LegacyHashFileName)
	data, readErr = os.ReadFile(legacyPath)
	switch {
	case readErr == nil:
		return loadLegacyBase(legacyPath, data)
	case os.IsNotExist(readErr):
		return provenance.Base{}, false, nil
	default:
		return provenance.Base{}, false, fmt.Errorf("read legacy base file %s: %w", legacyPath, readErr)
	}
}

func loadBaseV2(path string, data []byte) (provenance.Base, bool, error) {
	var v baseFileV2
	if err := json.Unmarshal(data, &v); err != nil {
		return provenance.Base{}, false, fmt.Errorf("%w: %s: %v", ErrBaseCorrupt, path, err)
	}

	b := provenance.Base{Commit: v.Commit, Tree: v.Tree}
	if !isValidCommitOID(b.Commit) || !isValidOptionalTreeOID(b.Tree) {
		return provenance.Base{}, false, fmt.Errorf("%w: %s: commit=%q tree=%q is not a well-formed OID pair", ErrBaseCorrupt, path, b.Commit, b.Tree)
	}
	return b, true, nil
}

func loadLegacyBase(path string, data []byte) (provenance.Base, bool, error) {
	line := strings.TrimSpace(string(data))
	if line == "" {
		return provenance.Base{}, false, fmt.Errorf("%w: %s", ErrLegacyBaseEmpty, path)
	}
	if !isValidCommitOID(line) {
		return provenance.Base{}, false, fmt.Errorf("%w: %s does not contain a valid commit OID", ErrBaseCorrupt, path)
	}
	return provenance.Base{Commit: line, Tree: ""}, true, nil
}

// isValidCommitOID reports whether oid is a well-formed, present commit
// OID. Mirrors provenance.Base.Valid()'s rule for Commit (empty is never
// valid) using the shared pattern directly, since Base.Valid() itself is
// implemented elsewhere in parallel.
func isValidCommitOID(oid string) bool {
	return oid != "" && provenance.OIDPattern.MatchString(oid)
}

// isValidOptionalTreeOID reports whether oid is acceptable as a Base
// Tree: empty (unresolved/legacy adoption, per provenance.Base's own
// doc) or a well-formed OID.
func isValidOptionalTreeOID(oid string) bool {
	return oid == "" || provenance.OIDPattern.MatchString(oid)
}

// SaveBase writes BaseFileName atomically.
func SaveBase(workDir string, b provenance.Base) error {
	v := baseFileV2{Version: currentBaseFileVersion, Commit: b.Commit, Tree: b.Tree}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal base file: %w", err)
	}

	path := filepath.Join(workDir, BaseFileName)
	if err := fsx.WriteFileAtomic(path, data, 0644); err != nil {
		return fmt.Errorf("write base file %s: %w", path, err)
	}
	return nil
}

// ClearBase removes the base file, restoring the "no base recorded"
// state. An absent file is not an error: what matters is the
// post-condition, and `sanho sync --abort` must stay re-runnable after
// an interruption (§5.5 step 7, guidance closure).
//
// It exists because a zero provenance.Base is not a writable value —
// the schema has no representation for an empty commit OID and LoadBase
// rejects one as corrupt — so "forget the base" cannot be expressed as a
// SaveBase call.
//
// Only BaseFileName is removed. LegacyHashFileName is a read-only v0.1
// compatibility input (§8) that sanho never writes; deleting it would
// destroy state a rollback still needs.
func ClearBase(workDir string) error {
	path := filepath.Join(workDir, BaseFileName)
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("remove base file %s: %w", path, err)
	}
	return nil
}

// SyncNote records an in-progress conflicted sync (§5.5).
type SyncNote struct {
	PrevBase  provenance.Base `json:"prev_base"`
	Target    provenance.Base `json:"target"`
	StartedAt time.Time       `json:"started_at"`
}

func syncNotePath(gitDir string) string {
	return filepath.Join(gitDir, filepath.FromSlash(SyncNoteRelPath))
}

// LoadSyncNote returns the note and ok=false when absent. gitDir is the
// app repo's resolved git dir (worktree-private).
func LoadSyncNote(gitDir string) (SyncNote, bool, error) {
	path := syncNotePath(gitDir)
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		var n SyncNote
		if jsonErr := json.Unmarshal(data, &n); jsonErr != nil {
			return SyncNote{}, false, fmt.Errorf("parse sync note %s: %w", path, jsonErr)
		}
		return n, true, nil
	case os.IsNotExist(err):
		return SyncNote{}, false, nil
	default:
		return SyncNote{}, false, fmt.Errorf("read sync note %s: %w", path, err)
	}
}

// SaveSyncNote / ClearSyncNote manage the note atomically.
func SaveSyncNote(gitDir string, n SyncNote) error {
	path := syncNotePath(gitDir)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create sync note directory for %s: %w", path, err)
	}

	data, err := json.MarshalIndent(n, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal sync note: %w", err)
	}
	if err := fsx.WriteFileAtomic(path, data, 0644); err != nil {
		return fmt.Errorf("write sync note %s: %w", path, err)
	}
	return nil
}

func ClearSyncNote(gitDir string) error {
	path := syncNotePath(gitDir)
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("remove sync note %s: %w", path, err)
	}
	return nil
}
