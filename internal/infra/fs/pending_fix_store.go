package fs

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/irootkernel/sanho/internal/domain/docs"
)

// Error types for pending fix operations.
var (
	// ErrPendingFixNotFound is returned when the pending fix file does not exist.
	ErrPendingFixNotFound = errors.New("pending fix file not found")
	// ErrPendingFixParse is returned when the pending fix file cannot be parsed.
	ErrPendingFixParse = errors.New("failed to parse pending fix file")
)

// PendingFixState represents the state of a pending fix operation.
type PendingFixState struct {
	// BaseHash is the docs hash that was used as the base for the merge.
	BaseHash docs.CommitHash `json:"base_hash"`
	// RemoteHash is the docs hash from the remote (server HEAD at merge time).
	RemoteHash docs.CommitHash `json:"remote_hash"`
	// CreatedAt is the time when the pending fix state was created.
	CreatedAt time.Time `json:"created_at"`
}

// PendingFixStore defines the interface for managing pending fix state.
type PendingFixStore interface {
	// Read reads the pending fix state from the specified file path.
	// Returns the state, a boolean indicating if the file exists, and any error.
	Read(path string) (PendingFixState, bool, error)
	// Write writes the pending fix state to the specified file path.
	Write(path string, state PendingFixState) error
	// Remove deletes the pending fix file.
	Remove(path string) error
}

// FilePendingFixStore implements PendingFixStore using the file system.
type FilePendingFixStore struct{}

// NewFilePendingFixStore creates a new FilePendingFixStore.
func NewFilePendingFixStore() *FilePendingFixStore {
	return &FilePendingFixStore{}
}

// Read reads the pending fix state from the specified file path.
// Returns (state, true, nil) if the file exists and is valid,
// (empty, false, nil) if the file doesn't exist,
// and (empty, false, error) if there's an error reading or parsing.
func (s *FilePendingFixStore) Read(path string) (PendingFixState, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return PendingFixState{}, false, nil
		}
		return PendingFixState{}, false, fmt.Errorf("failed to read pending fix file: %w", err)
	}

	var state PendingFixState
	if err := json.Unmarshal(data, &state); err != nil {
		return PendingFixState{}, false, fmt.Errorf("%w: %v", ErrPendingFixParse, err)
	}

	return state, true, nil
}

// Write writes the pending fix state to the specified file path.
func (s *FilePendingFixStore) Write(path string, state PendingFixState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal pending fix state: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write pending fix file: %w", err)
	}

	return nil
}

// Remove deletes the pending fix file.
// Returns nil if the file doesn't exist.
func (s *FilePendingFixStore) Remove(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove pending fix file: %w", err)
	}
	return nil
}
