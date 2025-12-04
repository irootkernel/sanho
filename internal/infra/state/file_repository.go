package state

import (
	"encoding/json"
	"os"
	"sync"

	"github.com/SeventeenthEarth/kkachi/internal/config"
)

type WorkspaceState struct {
	ID string `json:"id"`
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
	s := &State{
		DocsRepos:         make(map[string]config.DocsRepoConfig),
		ProjectToDocsRepo: make(map[string]string),
		Workspaces:        make(map[string]WorkspaceState),
	}

	data, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(data, s); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	// Ensure maps are not nil
	if s.DocsRepos == nil {
		s.DocsRepos = make(map[string]config.DocsRepoConfig)
	}
	if s.ProjectToDocsRepo == nil {
		s.ProjectToDocsRepo = make(map[string]string)
	}
	if s.Workspaces == nil {
		s.Workspaces = make(map[string]WorkspaceState)
	}

	return &FileStateRepository{path: path, state: s}, nil
}

func (r *FileStateRepository) Save() error {
	data, err := json.MarshalIndent(r.state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(r.path, data, 0644)
}

func (r *FileStateRepository) DeleteProject(project string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.state.ProjectToDocsRepo, project)
	return r.Save()
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
	r.mu.Lock()
	defer r.mu.Unlock()
	r.state.ProjectToDocsRepo[project] = repoID
	return r.Save()
}

// AddDocsRepo for testing/setup
func (r *FileStateRepository) AddDocsRepo(repo config.DocsRepoConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.state.DocsRepos[repo.ID] = repo
	return r.Save()
}

func (r *FileStateRepository) DeleteDocsRepo(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.state.DocsRepos, id)
	return r.Save()
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
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.state.Workspaces[id]; !ok {
		return os.ErrNotExist // Or custom error
	}
	delete(r.state.Workspaces, id)
	return r.Save()
}

func (r *FileStateRepository) AddWorkspace(ws WorkspaceState) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.state.Workspaces[ws.ID] = ws
	return r.Save()
}

func (r *FileStateRepository) GetWorkspace(id string) (WorkspaceState, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ws, ok := r.state.Workspaces[id]
	return ws, ok
}
