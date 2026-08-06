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
	})
}

// loadLocked reads state.json, recovering from state.json.bak when the
// primary exists but is corrupt. Callers must hold the registry's flock.
func (f *File) loadLocked() (State, error) {
	primaryPath := f.statePath()
	data, err := os.ReadFile(primaryPath)
	switch {
	case err == nil:
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
