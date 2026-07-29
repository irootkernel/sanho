package fs

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/irootkernel/sanho/internal/domain/docs"
)

// Error types for docs hash operations.
var (
	// ErrHashFileNotFound is returned when the hash file does not exist.
	ErrHashFileNotFound = errors.New("hash file not found")
	// ErrHashFileEmpty is returned when the hash file is empty.
	ErrHashFileEmpty = errors.New("hash file is empty")
)

// DocsHashStore defines the interface for reading and writing docs hash files.
type DocsHashStore interface {
	// Read reads the docs hash from the specified file path.
	Read(path string) (docs.CommitHash, error)
	// Write writes the docs hash to the specified file path.
	Write(path string, hash docs.CommitHash) error
}

// FileDocsHashStore implements DocsHashStore using the file system.
type FileDocsHashStore struct{}

// NewFileDocsHashStore creates a new FileDocsHashStore.
func NewFileDocsHashStore() *FileDocsHashStore {
	return &FileDocsHashStore{}
}

// Read reads the docs hash from the specified file path.
// The file should contain a single line with the commit hash.
// Returns ErrHashFileNotFound if the file doesn't exist,
// and ErrHashFileEmpty if the file is empty.
func (s *FileDocsHashStore) Read(path string) (docs.CommitHash, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("%w: %s", ErrHashFileNotFound, path)
		}
		return "", fmt.Errorf("failed to read hash file: %w", err)
	}

	hash := strings.TrimSpace(string(data))
	if hash == "" {
		return "", fmt.Errorf("%w: %s", ErrHashFileEmpty, path)
	}

	return docs.CommitHash(hash), nil
}

// Write writes the docs hash to the specified file path.
// The hash is written as a single line with a trailing newline.
func (s *FileDocsHashStore) Write(path string, hash docs.CommitHash) error {
	content := string(hash) + "\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write hash file: %w", err)
	}
	return nil
}
