package fs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const MainPublicationStateVersion = 1

// MainPublicationCommit identifies a system docs commit that must become
// reachable from origin/main before its publication record can be cleared.
type MainPublicationCommit struct {
	Commit   string `json:"commit"`
	Parent   string `json:"parent"`
	DocsHash string `json:"docs_hash"`
	Subject  string `json:"subject"`
}

// MainPublicationState records system docs commits that have not yet been
// confirmed reachable from origin/main.
type MainPublicationState struct {
	Version       int                     `json:"version"`
	BaseCommit    string                  `json:"base_commit,omitempty"`
	Commits       []MainPublicationCommit `json:"commits"`
	LastError     string                  `json:"last_error,omitempty"`
	LastAttemptAt *time.Time              `json:"last_attempt_at,omitempty"`
	CreatedAt     time.Time               `json:"created_at"`
	UpdatedAt     time.Time               `json:"updated_at"`
}

type MainPublicationStore struct {
	path string
}

func NewMainPublicationStore(path string) *MainPublicationStore {
	return &MainPublicationStore{path: path}
}

func (s *MainPublicationStore) Load() (MainPublicationState, bool, error) {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return MainPublicationState{}, false, nil
	}
	if err != nil {
		return MainPublicationState{}, false, fmt.Errorf("read main publication state: %w", err)
	}
	var state MainPublicationState
	if err := json.Unmarshal(data, &state); err != nil {
		return MainPublicationState{}, false, fmt.Errorf("parse main publication state: %w", err)
	}
	if state.Version != MainPublicationStateVersion {
		return MainPublicationState{}, false, fmt.Errorf("unsupported main publication state version: %d", state.Version)
	}
	if len(state.Commits) == 0 {
		return MainPublicationState{}, false, fmt.Errorf("main publication state has no commits")
	}
	return state, true, nil
}

func (s *MainPublicationStore) Ensure(baseCommit string, commit MainPublicationCommit) error {
	state, exists, err := s.Load()
	if err != nil {
		return err
	}
	for _, recorded := range state.Commits {
		if recorded.Commit == commit.Commit {
			return nil
		}
	}
	now := time.Now().UTC()
	if !exists {
		state = MainPublicationState{
			Version:    MainPublicationStateVersion,
			BaseCommit: baseCommit,
			Commits:    make([]MainPublicationCommit, 0, 1),
			CreatedAt:  now,
		}
	}
	state.Commits = append(state.Commits, commit)
	state.UpdatedAt = now
	return s.Save(state)
}

func (s *MainPublicationStore) Save(state MainPublicationState) error {
	if state.Version != MainPublicationStateVersion {
		return fmt.Errorf("unsupported main publication state version: %d", state.Version)
	}
	if len(state.Commits) == 0 {
		return fmt.Errorf("main publication state has no commits")
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal main publication state: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return fmt.Errorf("create main publication state directory: %w", err)
	}
	if err := writeFileAtomic(s.path, data, 0600); err != nil {
		return fmt.Errorf("write main publication state: %w", err)
	}
	return nil
}

func (s *MainPublicationStore) RecordFailure(message string) error {
	state, exists, err := s.Load()
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("cannot record main publication failure without pending state")
	}
	now := time.Now().UTC()
	state.LastError = message
	state.LastAttemptAt = &now
	state.UpdatedAt = now
	return s.Save(state)
}

func (s *MainPublicationStore) Remove() error {
	if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove main publication state: %w", err)
	}
	return nil
}
