package fs

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/irootkernel/sanho/internal/domain/docs"
	"github.com/irootkernel/sanho/internal/domain/workspace"
)

const WorkspaceReportFallbackFile = ".kkachi_workspace_report"

// WorkspaceReportState records a daemon update that must succeed before
// another guarded Git operation may continue.
type WorkspaceReportState struct {
	Version     int                   `json:"version"`
	WorkspaceID workspace.WorkspaceID `json:"workspace_id"`
	DocsHash    docs.CommitHash       `json:"docs_hash"`
	ActorEmail  string                `json:"actor_email"`
	CreatedAt   time.Time             `json:"created_at"`
}

type WorkspaceReportStore struct {
	path string
}

func NewWorkspaceReportStore(path string) *WorkspaceReportStore {
	return &WorkspaceReportStore{path: path}
}

func (s *WorkspaceReportStore) Load() (WorkspaceReportState, bool, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return WorkspaceReportState{}, false, nil
	}
	if err != nil {
		return WorkspaceReportState{}, false, err
	}
	var state WorkspaceReportState
	if err := json.Unmarshal(data, &state); err != nil {
		return WorkspaceReportState{}, false, err
	}
	return state, true, nil
}

func (s *WorkspaceReportStore) Save(state WorkspaceReportState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}
	return writeFileAtomic(s.path, data, 0600)
}

func (s *WorkspaceReportStore) Remove() error {
	err := os.Remove(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
