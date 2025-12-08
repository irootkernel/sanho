package fs

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// SnapshotBuilder provides methods for building docs snapshots.
type SnapshotBuilder struct{}

// NewSnapshotBuilder creates a new SnapshotBuilder.
func NewSnapshotBuilder() *SnapshotBuilder {
	return &SnapshotBuilder{}
}

// Build creates a tar.gz snapshot of the given docs directory.
// The snapshot paths are relative to the provided sourceDir.
// Returns the tar.gz bytes or an error.
func (b *SnapshotBuilder) Build(sourceDir string) ([]byte, error) {
	// Verify source directory exists
	info, err := os.Stat(sourceDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("source directory does not exist: %s", sourceDir)
		}
		return nil, fmt.Errorf("failed to stat source directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("source path is not a directory: %s", sourceDir)
	}

	var buf bytes.Buffer
	gzWriter := gzip.NewWriter(&buf)
	tarWriter := tar.NewWriter(gzWriter)

	// Walk the source directory and add files to tar
	err = filepath.WalkDir(sourceDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Get relative path from source directory
		relPath, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return fmt.Errorf("failed to get relative path: %w", err)
		}

		// Skip the root directory entry
		if relPath == "." {
			return nil
		}

		// Use path relative to sourceDir to keep archive layout consistent
		tarPath := filepath.ToSlash(relPath)

		// Get file info
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("failed to get file info: %w", err)
		}

		// Create tar header
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return fmt.Errorf("failed to create tar header: %w", err)
		}
		header.Name = tarPath

		// Write header
		if err := tarWriter.WriteHeader(header); err != nil {
			return fmt.Errorf("failed to write tar header: %w", err)
		}

		// Write file content (skip directories)
		if !entry.IsDir() {
			if err := func() error {
				file, err := os.Open(path)
				if err != nil {
					return fmt.Errorf("failed to open file: %w", err)
				}
				defer file.Close()

				if _, err := io.Copy(tarWriter, file); err != nil {
					return fmt.Errorf("failed to write file to tar: %w", err)
				}
				return nil
			}(); err != nil {
				return err
			}
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to build snapshot: %w", err)
	}

	// Close tar and gzip writers in order
	if err := tarWriter.Close(); err != nil {
		return nil, fmt.Errorf("failed to close tar writer: %w", err)
	}
	if err := gzWriter.Close(); err != nil {
		return nil, fmt.Errorf("failed to close gzip writer: %w", err)
	}

	return buf.Bytes(), nil
}
