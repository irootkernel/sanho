package fs

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/irootkernel/sanho/internal/domain/docs"
)

const (
	PullCommitPhaseReady         = "ready"
	PullCommitPhaseConflict      = "conflict"
	PullCommitPhaseSyncCommitted = "sync_committed"
	PullCommitPhasePrepared      = "prepared"
	PullCommitPhaseCompleted     = "completed"
)

const (
	PullCommitBaseSnapshot          = "base.tar.gz"
	PullCommitOriginalIndexSnapshot = "original-index.tar.gz"
	PullCommitOriginalWorkSnapshot  = "original-work.tar.gz"
	PullCommitMergedIndexSnapshot   = "merged-index.tar.gz"
	PullCommitMergedWorkSnapshot    = "merged-work.tar.gz"
	PullCommitRemoteSnapshot        = "remote.tar.gz"
)

// PullCommitState records a resumable local docs base synchronization.
type PullCommitState struct {
	Version          int                 `json:"version"`
	Phase            string              `json:"phase"`
	TransactionID    string              `json:"transaction_id,omitempty"`
	BranchRef        string              `json:"branch_ref,omitempty"`
	OriginalHead     string              `json:"original_head"`
	SyncCommit       string              `json:"sync_commit,omitempty"`
	PreparedHead     string              `json:"prepared_head,omitempty"`
	PreparedTree     string              `json:"prepared_tree,omitempty"`
	CompletionHead   string              `json:"completion_head,omitempty"`
	CompletionReason string              `json:"completion_reason,omitempty"`
	Rewrites         []PullCommitRewrite `json:"rewrites,omitempty"`
	Recovery         *PullCommitRecovery `json:"recovery,omitempty"`
	BaseHash         docs.CommitHash     `json:"base_hash"`
	RemoteHash       docs.CommitHash     `json:"remote_hash"`
	Reported         bool                `json:"reported,omitempty"`
	ConflictFiles    []string            `json:"conflict_files,omitempty"`
	CreatedAt        time.Time           `json:"created_at"`
}

// PullCommitRecovery records the refs that protect Git state during recovery.
type PullCommitRecovery struct {
	HeadRef     string    `json:"head_ref"`
	IndexRef    string    `json:"index_ref"`
	WorktreeRef string    `json:"worktree_ref"`
	CreatedAt   time.Time `json:"created_at"`
}

// PullCommitRewrite records a Git post-rewrite old-to-new commit mapping.
type PullCommitRewrite struct {
	Command string `json:"command"`
	Old     string `json:"old"`
	New     string `json:"new"`
}

// PullCommitStore persists transaction state beneath Git's private metadata.
type PullCommitStore struct {
	dir string
}

func NewPullCommitStore(dir string) *PullCommitStore {
	return &PullCommitStore{dir: dir}
}

func (s *PullCommitStore) Exists() (bool, error) {
	_, err := os.Stat(s.statePath())
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("stat pull-commit state: %w", err)
}

func (s *PullCommitStore) Load() (PullCommitState, bool, error) {
	data, err := os.ReadFile(s.statePath())
	if err != nil {
		if os.IsNotExist(err) {
			return PullCommitState{}, false, nil
		}
		return PullCommitState{}, false, fmt.Errorf("read pull-commit state: %w", err)
	}

	var state PullCommitState
	if err := json.Unmarshal(data, &state); err != nil {
		return PullCommitState{}, false, fmt.Errorf("parse pull-commit state: %w", err)
	}
	if state.Version != 1 && state.Version != 2 && state.Version != 3 {
		return PullCommitState{}, false, fmt.Errorf("unsupported pull-commit state version: %d", state.Version)
	}
	return state, true, nil
}

func (s *PullCommitStore) Save(state PullCommitState) error {
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return fmt.Errorf("create pull-commit state directory: %w", err)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal pull-commit state: %w", err)
	}
	return writeFileAtomic(s.statePath(), data, 0600)
}

func (s *PullCommitStore) WriteArtifact(name string, data []byte) error {
	if filepath.Base(name) != name {
		return errors.New("invalid pull-commit artifact name")
	}
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return fmt.Errorf("create pull-commit state directory: %w", err)
	}
	return writeFileAtomic(filepath.Join(s.dir, name), data, 0600)
}

func (s *PullCommitStore) ReadArtifact(name string) ([]byte, error) {
	if filepath.Base(name) != name {
		return nil, errors.New("invalid pull-commit artifact name")
	}
	data, err := os.ReadFile(filepath.Join(s.dir, name))
	if err != nil {
		return nil, fmt.Errorf("read pull-commit artifact %s: %w", name, err)
	}
	return data, nil
}

func (s *PullCommitStore) Remove() error {
	if err := os.RemoveAll(s.dir); err != nil {
		return fmt.Errorf("remove pull-commit state: %w", err)
	}
	return nil
}

func (s *PullCommitStore) statePath() string {
	return filepath.Join(s.dir, "state.json")
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	tempPath := path + ".tmp"
	if err := os.WriteFile(tempPath, data, mode); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	return nil
}
