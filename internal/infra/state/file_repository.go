package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/irootkernel/sanho/internal/config"
)

var ErrStateCorrupt = errors.New("state_corrupt")

type WorkspaceState struct {
	ID             string    `json:"id"`
	Project        string    `json:"project"`
	DocsRepoID     string    `json:"docs_repo_id"`
	LocalPath      string    `json:"local_path"`
	RepoURL        string    `json:"repo_url"`
	DocsHash       string    `json:"docs_hash"`
	LastReportedAt time.Time `json:"last_reported_at"`
	OwnerEmail     string    `json:"owner_email"`
	LastActorEmail string    `json:"last_actor_email"`
}

type State struct {
	DocsRepos         map[string]config.DocsRepoConfig `json:"docs_repos"`
	ProjectToDocsRepo map[string]string                `json:"project_to_docs_repo"`
	Workspaces        map[string]WorkspaceState        `json:"workspaces"`
}

type FileStateRepository struct {
	path  string
	mu    sync.RWMutex
	state *State
}

func NewFileStateRepository(path string) (*FileStateRepository, error) {
	s := newState()
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if decodeErr := json.Unmarshal(data, s); decodeErr != nil {
			recovered, recoveryErr := loadStateBackup(path)
			if recoveryErr != nil {
				return nil, fmt.Errorf("%w: primary: %v; backup: %v", ErrStateCorrupt, decodeErr, recoveryErr)
			}
			s = recovered
			if writeErr := writeAtomic(path, mustMarshalState(s)); writeErr != nil {
				return nil, fmt.Errorf("restore state backup: %w", writeErr)
			}
		}
	case os.IsNotExist(err):
		recovered, recoveryErr := loadStateBackup(path)
		if recoveryErr == nil {
			s = recovered
			if writeErr := writeAtomic(path, mustMarshalState(s)); writeErr != nil {
				return nil, fmt.Errorf("restore state backup: %w", writeErr)
			}
		} else if !os.IsNotExist(recoveryErr) {
			return nil, fmt.Errorf("%w: backup: %v", ErrStateCorrupt, recoveryErr)
		}
	default:
		return nil, err
	}

	ensureStateMaps(s)

	return &FileStateRepository{path: path, state: s}, nil
}

func (r *FileStateRepository) Save() error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.saveLocked()
}

func (r *FileStateRepository) saveLocked() error {
	data, err := json.MarshalIndent(r.state, "", "  ")
	if err != nil {
		return err
	}
	if err := writeAtomic(r.path+".bak", data); err != nil {
		return fmt.Errorf("write state backup: %w", err)
	}
	if err := writeAtomic(r.path, data); err != nil {
		return fmt.Errorf("write state: %w", err)
	}
	return nil
}

func (r *FileStateRepository) DeleteProject(project string) error {
	return r.update(func(next *State) (bool, error) {
		delete(next.ProjectToDocsRepo, project)
		return true, nil
	})
}

func (r *FileStateRepository) GetRepoUsage() map[string]int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	usage := make(map[string]int)
	for _, repoID := range r.state.ProjectToDocsRepo {
		usage[repoID]++
	}
	return usage
}

func (r *FileStateRepository) GetDocsRepo(id string) (config.DocsRepoConfig, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	repo, ok := r.state.DocsRepos[id]
	return repo, ok
}

// AddProject for testing/setup
func (r *FileStateRepository) AddProject(project, repoID string) error {
	return r.update(func(next *State) (bool, error) {
		next.ProjectToDocsRepo[project] = repoID
		return true, nil
	})
}

// AddDocsRepo for testing/setup
func (r *FileStateRepository) AddDocsRepo(repo config.DocsRepoConfig) error {
	return r.update(func(next *State) (bool, error) {
		next.DocsRepos[repo.ID] = repo
		return true, nil
	})
}

func (r *FileStateRepository) DeleteDocsRepo(id string) error {
	return r.update(func(next *State) (bool, error) {
		delete(next.DocsRepos, id)
		return true, nil
	})
}

func (r *FileStateRepository) GetDocsRepoID(project string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, ok := r.state.ProjectToDocsRepo[project]
	return id, ok
}

func (r *FileStateRepository) ListDocsRepos() []config.DocsRepoConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	repos := make([]config.DocsRepoConfig, 0, len(r.state.DocsRepos))
	for _, repo := range r.state.DocsRepos {
		repos = append(repos, repo)
	}
	return repos
}

func (r *FileStateRepository) DeleteWorkspace(id string) error {
	return r.update(func(next *State) (bool, error) {
		if _, ok := next.Workspaces[id]; !ok {
			return false, os.ErrNotExist
		}
		delete(next.Workspaces, id)
		return true, nil
	})
}

func (r *FileStateRepository) AddWorkspace(ws WorkspaceState) error {
	return r.update(func(next *State) (bool, error) {
		next.Workspaces[ws.ID] = ws
		return true, nil
	})
}

func (r *FileStateRepository) GetWorkspace(id string) (WorkspaceState, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ws, ok := r.state.Workspaces[id]
	return ws, ok
}

func (r *FileStateRepository) UpdateWorkspaceDocsHash(id, newHash, actorEmail string) error {
	return r.update(func(next *State) (bool, error) {
		ws, ok := next.Workspaces[id]
		if !ok {
			return false, os.ErrNotExist
		}
		ws.DocsHash = newHash
		ws.LastReportedAt = time.Now()
		ws.LastActorEmail = actorEmail
		next.Workspaces[id] = ws
		return true, nil
	})
}

// ListWorkspaces returns all registered workspaces.
func (r *FileStateRepository) ListWorkspaces() []WorkspaceState {
	r.mu.RLock()
	defer r.mu.RUnlock()
	workspaces := make([]WorkspaceState, 0, len(r.state.Workspaces))
	for _, ws := range r.state.Workspaces {
		workspaces = append(workspaces, ws)
	}
	return workspaces
}

// ListProjects returns all project names.
func (r *FileStateRepository) ListProjects() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	projects := make([]string, 0, len(r.state.ProjectToDocsRepo))
	for project := range r.state.ProjectToDocsRepo {
		projects = append(projects, project)
	}
	return projects
}

// HasWorkspacesForProject checks if there are any workspaces registered for the given project.
func (r *FileStateRepository) HasWorkspacesForProject(project string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, ws := range r.state.Workspaces {
		if ws.Project == project {
			return true
		}
	}
	return false
}

// DeleteWorkspacesByProject removes all workspaces registered to the given project.
func (r *FileStateRepository) DeleteWorkspacesByProject(project string) error {
	return r.update(func(next *State) (bool, error) {
		changed := false
		for id, ws := range next.Workspaces {
			if ws.Project == project {
				delete(next.Workspaces, id)
				changed = true
			}
		}
		return changed, nil
	})
}

func (r *FileStateRepository) update(mutate func(*State) (bool, error)) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	previous := r.state
	next := cloneState(previous)
	changed, err := mutate(next)
	if err != nil || !changed {
		return err
	}
	r.state = next
	if err := r.saveLocked(); err != nil {
		r.state = previous
		return err
	}
	return nil
}

func newState() *State {
	return &State{
		DocsRepos:         make(map[string]config.DocsRepoConfig),
		ProjectToDocsRepo: make(map[string]string),
		Workspaces:        make(map[string]WorkspaceState),
	}
}

func ensureStateMaps(s *State) {
	if s.DocsRepos == nil {
		s.DocsRepos = make(map[string]config.DocsRepoConfig)
	}
	if s.ProjectToDocsRepo == nil {
		s.ProjectToDocsRepo = make(map[string]string)
	}
	if s.Workspaces == nil {
		s.Workspaces = make(map[string]WorkspaceState)
	}
}

func cloneState(s *State) *State {
	clone := newState()
	for id, repo := range s.DocsRepos {
		clone.DocsRepos[id] = repo
	}
	for project, repoID := range s.ProjectToDocsRepo {
		clone.ProjectToDocsRepo[project] = repoID
	}
	for id, workspace := range s.Workspaces {
		clone.Workspaces[id] = workspace
	}
	return clone
}

func loadStateBackup(path string) (*State, error) {
	data, err := os.ReadFile(path + ".bak")
	if err != nil {
		return nil, err
	}
	recovered := newState()
	if err := json.Unmarshal(data, recovered); err != nil {
		return nil, err
	}
	ensureStateMaps(recovered)
	return recovered, nil
}

func mustMarshalState(s *State) []byte {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		panic(err)
	}
	return data
}

func writeAtomic(path string, data []byte) (returnErr error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer func() {
		if returnErr != nil {
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(0644); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	directory, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
