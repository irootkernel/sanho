// Package registry is the daemonless cross-workspace registry of sanho
// v0.2 (sanho-v0.2.md §5.7, D4): ~/.sanho/state.json guarded by an
// exclusive flock, updated directly by each CLI invocation, with .bak
// recovery. It is observational state — publication correctness never
// depends on it.
package registry

import (
	"context"
	"time"
)

// Default locations under the sanho home (~/.sanho unless SANHO_HOME).
const (
	StateFileName = "state.json"
	BackupSuffix  = ".bak"
	LockFileName  = "state.lock"
)

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

// Open resolves the registry under homeDir (creating the directory with
// 0700 when absent).
func Open(homeDir string) (*File, error) {
	panic("unimplemented (sanho v0.2 P1)")
}

// Read returns the current state under a shared view of the lock.
// Missing file yields an empty v2 state; corrupt primary recovers from
// .bak; both corrupt errors (never silently start empty — v0.1 daemon
// semantics carried over).
func (f *File) Read(ctx context.Context) (State, error) {
	panic("unimplemented (sanho v0.2 P1)")
}

// Update applies fn to the state under the exclusive flock and persists
// atomically (state + .bak). fn returning an error aborts without
// writing.
func (f *File) Update(ctx context.Context, fn func(*State) error) error {
	panic("unimplemented (sanho v0.2 P1)")
}
