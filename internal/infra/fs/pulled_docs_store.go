package fs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/SeventeenthEarth/kkachi/internal/domain/docs"
)

const (
	PulledDocsOriginalIndexSnapshot = "original-index.tar.gz"
	PulledDocsAdoptedSnapshot       = "adopted.tar.gz"
)

// PulledDocsState records a docs snapshot adopted by pull but not yet
// materialized in the application repository history.
type PulledDocsState struct {
	Version      int             `json:"version"`
	OriginalHead string          `json:"original_head"`
	PreviousHash docs.CommitHash `json:"previous_hash"`
	AdoptedHash  docs.CommitHash `json:"adopted_hash"`
	CreatedAt    time.Time       `json:"created_at"`
}

type PulledDocsStore struct {
	dir string
}

func NewPulledDocsStore(dir string) *PulledDocsStore {
	return &PulledDocsStore{dir: dir}
}

func (s *PulledDocsStore) Load() (PulledDocsState, bool, error) {
	data, err := os.ReadFile(filepath.Join(s.dir, "state.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return PulledDocsState{}, false, nil
		}
		return PulledDocsState{}, false, fmt.Errorf("read pulled docs state: %w", err)
	}
	var state PulledDocsState
	if err := json.Unmarshal(data, &state); err != nil {
		return PulledDocsState{}, false, fmt.Errorf("parse pulled docs state: %w", err)
	}
	if state.Version != 1 {
		return PulledDocsState{}, false, fmt.Errorf("unsupported pulled docs state version: %d", state.Version)
	}
	return state, true, nil
}

func (s *PulledDocsStore) Save(state PulledDocsState) error {
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return fmt.Errorf("create pulled docs state directory: %w", err)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal pulled docs state: %w", err)
	}
	return writeFileAtomic(filepath.Join(s.dir, "state.json"), data, 0600)
}

func (s *PulledDocsStore) WriteArtifact(name string, data []byte) error {
	if filepath.Base(name) != name {
		return fmt.Errorf("invalid pulled docs artifact name %q", name)
	}
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return fmt.Errorf("create pulled docs state directory: %w", err)
	}
	return writeFileAtomic(filepath.Join(s.dir, name), data, 0600)
}

func (s *PulledDocsStore) ReadArtifact(name string) ([]byte, error) {
	if filepath.Base(name) != name {
		return nil, fmt.Errorf("invalid pulled docs artifact name %q", name)
	}
	data, err := os.ReadFile(filepath.Join(s.dir, name))
	if err != nil {
		return nil, fmt.Errorf("read pulled docs artifact %s: %w", name, err)
	}
	return data, nil
}

func (s *PulledDocsStore) Remove() error {
	if err := os.RemoveAll(s.dir); err != nil {
		return fmt.Errorf("remove pulled docs state: %w", err)
	}
	return nil
}
