// Package registry is the daemonless cross-workspace registry of sanho
// v0.2 (sanho-v0.2.md §5.7, D4): ~/.sanho/state.json guarded by an
// exclusive flock, updated directly by each CLI invocation, with .bak
// recovery. It is observational state — publication correctness never
// depends on it.
package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/irootkernel/sanho/internal/infra/fsx"
)

// Default locations under the sanho home (~/.sanho unless SANHO_HOME).
const (
	StateFileName = "state.json"
	BackupSuffix  = ".bak"
	LockFileName  = "state.lock"
)

// registryFileMode is applied to both state.json and its .bak sibling
// (§5.7): the registry lives under the private sanho home and, like the
// v0.1 daemon's state file, is not group/world readable.
const registryFileMode = 0600

// currentStateVersion is the schema version stamped into a freshly
// created State and expected on disk.
const currentStateVersion = 2

// ErrRegistryUnreadable is returned when neither state.json nor its
// .bak backup can be read back into a valid State. Per §5.7 and the
// v0.1 daemon semantics this carries over, the registry never silently
// starts empty in this case — the caller must be told and given a
// chance to intervene.
var ErrRegistryUnreadable = errors.New("registry state is unreadable")

// ErrLegacyState reports a state.json still in the v0.1 daemon schema.
//
// It is a refusal, not a recovery. The v0.1 file records every project's
// docs repository under `docs_repos`/`project_to_docs_repo`, keys the v2
// struct does not have — so unmarshalling it succeeds while producing an
// EMPTY v2 state, and the first ordinary command that wrote the registry
// would have replaced the whole file with that emptiness (F-H8a). Only
// `sanho migrate` converts it; everything else stops here.
var ErrLegacyState = errors.New("registry state uses the v0.1 daemon schema")

// LegacyState is the v0.1 daemon's ~/.sanho/state.json, as much of it as
// v0.2 has a place for. It exists so `sanho migrate` can convert the
// WHOLE file in one pass rather than lifting out one project's URL and
// letting the next write destroy the rest.
type LegacyState struct {
	DocsRepos         map[string]LegacyDocsRepo  `json:"docs_repos"`
	ProjectToDocsRepo map[string]string          `json:"project_to_docs_repo"`
	Workspaces        map[string]LegacyWorkspace `json:"workspaces"`
}

// LegacyDocsRepo is one v0.1 docs-repository registration. The field
// names are the daemon's exported Go names, which is how they were
// marshalled (no json tags in v0.1).
type LegacyDocsRepo struct {
	ID      string
	Path    string
	RepoURL string
}

// LegacyWorkspace is one v0.1 workspace row. Only the fields that have a
// v2 counterpart are read; v0.1's transport bookkeeping has none.
type LegacyWorkspace struct {
	Project        string    `json:"project"`
	LocalPath      string    `json:"local_path"`
	DocsHash       string    `json:"docs_hash"`
	LastActorEmail string    `json:"last_actor_email"`
	OwnerEmail     string    `json:"owner_email"`
	LastReportedAt time.Time `json:"last_reported_at"`
	LastUpdatedAt  time.Time `json:"last_updated_at"`
}

// State is the v2 registry schema.
type State struct {
	Version    int                  `json:"version"`
	Projects   map[string]Project   `json:"projects"`
	Workspaces map[string]Workspace `json:"workspaces"`
}

// Project maps a project name to its canonical docs repository.
type Project struct {
	DocsRepoURL string `json:"docs_repo_url"`
}

// Workspace is one registered checkout's last-reported observational
// state. Keyed by "<project>:<absolute path>".
type Workspace struct {
	Project       string    `json:"project"`
	LocalPath     string    `json:"local_path"`
	BaseCommit    string    `json:"base_commit"`
	BaseTree      string    `json:"base_tree"`
	ActorEmail    string    `json:"actor_email"`
	LastUpdatedAt time.Time `json:"last_updated_at"`
}

// File is a handle on the registry rooted at homeDir.
type File struct {
	homeDir string
}

// HomeDir returns the sanho home this registry lives under (used by
// doctor and error messages naming the lock path).
func (f *File) HomeDir() string { return f.homeDir }

func (f *File) statePath() string  { return filepath.Join(f.homeDir, StateFileName) }
func (f *File) backupPath() string { return filepath.Join(f.homeDir, StateFileName+BackupSuffix) }
func (f *File) lockPath() string   { return filepath.Join(f.homeDir, LockFileName) }

// Open resolves the registry under homeDir (creating the directory with
// 0700 when absent).
func Open(homeDir string) (*File, error) {
	if err := os.MkdirAll(homeDir, 0700); err != nil {
		return nil, fmt.Errorf("create sanho home %s: %w", homeDir, err)
	}
	// MkdirAll only applies the requested mode to directories it
	// creates; chmod explicitly so a pre-existing, more permissive home
	// directory is still tightened.
	if err := os.Chmod(homeDir, 0700); err != nil {
		return nil, fmt.Errorf("secure sanho home %s: %w", homeDir, err)
	}
	return &File{homeDir: homeDir}, nil
}

func emptyState() State {
	return State{
		Version:    currentStateVersion,
		Projects:   make(map[string]Project),
		Workspaces: make(map[string]Workspace),
	}
}

func ensureMaps(s *State) {
	if s.Projects == nil {
		s.Projects = make(map[string]Project)
	}
	if s.Workspaces == nil {
		s.Workspaces = make(map[string]Workspace)
	}
}

// Read returns the current state under a shared view of the lock.
// Missing file yields an empty v2 state; corrupt primary recovers from
// .bak; both corrupt errors (never silently start empty — v0.1 daemon
// semantics carried over).
//
// Read and Update take the same exclusive flock rather than a
// reader/writer lock: the registry is updated by short-lived CLI
// invocations (never held open across a long session), so a plain
// exclusive lock costs nothing observable in practice while keeping the
// locking code — and its failure modes — in one place. Documented here
// per the skeleton's explicit allowance.
func (f *File) Read(ctx context.Context) (State, error) {
	var result State
	err := fsx.WithFlock(ctx, f.lockPath(), func() error {
		st, loadErr := f.loadLocked()
		if loadErr != nil {
			return loadErr
		}
		result = st
		return nil
	})
	if err != nil {
		return State{}, err
	}
	return result, nil
}

// Update applies fn to the state under the exclusive flock and persists
// atomically (state + .bak). fn returning an error aborts without
// writing.
func (f *File) Update(ctx context.Context, fn func(*State) error) error {
	return fsx.WithFlock(ctx, f.lockPath(), func() error {
		st, loadErr := f.loadLocked()
		if loadErr != nil {
			return loadErr
		}

		if err := fn(&st); err != nil {
			return err
		}
		return f.persistLocked(st)
	})
}

// persistLocked writes state and its backup. Callers hold the flock.
//
// Version is stamped rather than trusted (R1-G5, F-H8f): a State that
// reached here through a callback, a legacy conversion, or a test always
// leaves as version 2, so the on-disk schema marker can never disagree
// with the bytes beside it.
func (f *File) persistLocked(st State) error {
	st.Version = currentStateVersion
	ensureMaps(&st)

	data, err := json.MarshalIndent(&st, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal registry state: %w", err)
	}
	// Primary first, then refresh the backup from the same bytes
	// (§5.7) — both through the shared atomic writer.
	if err := fsx.WriteFileAtomic(f.statePath(), data, registryFileMode); err != nil {
		return fmt.Errorf("write registry state %s: %w", f.statePath(), err)
	}
	if err := fsx.WriteFileAtomic(f.backupPath(), data, registryFileMode); err != nil {
		return fmt.Errorf("write registry backup %s: %w", f.backupPath(), err)
	}
	return nil
}

// loadLocked reads state.json, recovering from state.json.bak when the
// primary exists but is corrupt. Callers must hold the registry's flock.
//
// A v0.1 file is neither valid nor corrupt: it parses cleanly into an
// empty v2 State, because none of its keys are v2 keys. Detecting that
// explicitly is what keeps an ordinary `sanho status` from rewriting the
// daemon's whole project map as `{}` (F-H8a).
func (f *File) loadLocked() (State, error) {
	primaryPath := f.statePath()
	data, err := os.ReadFile(primaryPath)
	switch {
	case err == nil:
		if legacy, legacyErr := isLegacySchema(data); legacyErr == nil && legacy {
			return State{}, fmt.Errorf("%w: %s", ErrLegacyState, primaryPath)
		}
		var st State
		jsonErr := json.Unmarshal(data, &st)
		if jsonErr != nil {
			return f.recoverFromBackupLocked(primaryPath, jsonErr)
		}
		ensureMaps(&st)
		return st, nil
	case os.IsNotExist(err):
		return emptyState(), nil
	default:
		return State{}, fmt.Errorf("read registry state %s: %w", primaryPath, err)
	}
}

// isLegacySchema recognizes the v0.1 daemon's file: no version field
// (v0.1 had none, v2 always writes 2) plus at least one v0.1-only key.
// Requiring both keeps a hand-written or truncated v2 file — which has
// no version either — from being misread as legacy.
func isLegacySchema(data []byte) (bool, error) {
	var probe struct {
		Version           int             `json:"version"`
		DocsRepos         json.RawMessage `json:"docs_repos"`
		ProjectToDocsRepo json.RawMessage `json:"project_to_docs_repo"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return false, err
	}
	return probe.Version == 0 && (len(probe.DocsRepos) > 0 || len(probe.ProjectToDocsRepo) > 0), nil
}

// ReadLegacy returns the v0.1 daemon state, or ok=false when state.json
// is absent or already v2. Only `sanho migrate` calls it.
func (f *File) ReadLegacy() (LegacyState, bool, error) {
	data, err := os.ReadFile(f.statePath())
	if err != nil {
		if os.IsNotExist(err) {
			return LegacyState{}, false, nil
		}
		return LegacyState{}, false, fmt.Errorf("read registry state %s: %w", f.statePath(), err)
	}
	legacy, err := isLegacySchema(data)
	if err != nil || !legacy {
		return LegacyState{}, false, nil //nolint:nilerr // an unparsable file is not a legacy file
	}

	var state LegacyState
	if err := json.Unmarshal(data, &state); err != nil {
		return LegacyState{}, false, fmt.Errorf("parse v0.1 registry state %s: %w", f.statePath(), err)
	}
	return state, true, nil
}

// ConvertLegacy replaces a v0.1 state.json with its v2 equivalent, under
// the lock. It is migrate's only writer of the registry file and it
// converts the WHOLE file: every project mapping and every workspace
// row, not just the one being migrated.
//
// That completeness is the point. Migrating workspace A used to write a
// v2 file holding A alone, silently erasing project B's docs-repository
// URL — so migrating B afterwards demanded --docs-repo-url for a value
// the machine had already recorded (F-H8a, R1's lab3).
//
// A file that is already v2 is left exactly as it is, so a second
// migrate cannot undo the first.
func (f *File) ConvertLegacy(ctx context.Context) (converted bool, err error) {
	err = fsx.WithFlock(ctx, f.lockPath(), func() error {
		legacy, ok, readErr := f.ReadLegacy()
		if readErr != nil || !ok {
			return readErr
		}

		st := emptyState()
		for project, repoID := range legacy.ProjectToDocsRepo {
			repo, known := legacy.DocsRepos[repoID]
			if !known || repo.RepoURL == "" {
				continue
			}
			st.Projects[project] = Project{DocsRepoURL: repo.RepoURL}
		}
		for key, ws := range legacy.Workspaces {
			st.Workspaces[key] = Workspace{
				Project:       ws.Project,
				LocalPath:     ws.LocalPath,
				BaseCommit:    ws.DocsHash,
				ActorEmail:    firstNonEmpty(ws.LastActorEmail, ws.OwnerEmail),
				LastUpdatedAt: laterOf(ws.LastReportedAt, ws.LastUpdatedAt),
			}
		}

		converted = true
		return f.persistLocked(st)
	})
	return converted, err
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func laterOf(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

// recoverFromBackupLocked is entered once the primary has been read but
// failed to parse. It never silently starts empty: if the backup is
// also unreadable or unparsable, it returns ErrRegistryUnreadable naming
// both files.
func (f *File) recoverFromBackupLocked(primaryPath string, primaryErr error) (State, error) {
	backupPath := f.backupPath()
	data, err := os.ReadFile(backupPath)
	if err != nil {
		return State{}, fmt.Errorf("%w: primary %s is corrupt (%v) and backup %s is not readable (%v)",
			ErrRegistryUnreadable, primaryPath, primaryErr, backupPath, err)
	}

	var st State
	if jsonErr := json.Unmarshal(data, &st); jsonErr != nil {
		return State{}, fmt.Errorf("%w: primary %s is corrupt (%v) and backup %s is also corrupt (%v)",
			ErrRegistryUnreadable, primaryPath, primaryErr, backupPath, jsonErr)
	}
	ensureMaps(&st)

	// Restore the primary from the good backup so the workspace is
	// healed for the next read, not just this one.
	restored, marshalErr := json.MarshalIndent(&st, "", "  ")
	if marshalErr != nil {
		return State{}, fmt.Errorf("marshal recovered registry state: %w", marshalErr)
	}
	if writeErr := fsx.WriteFileAtomic(primaryPath, restored, registryFileMode); writeErr != nil {
		return State{}, fmt.Errorf("restore registry state from backup: %w", writeErr)
	}
	return st, nil
}
