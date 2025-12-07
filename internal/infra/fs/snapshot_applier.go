package fs

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// SnapshotApplier provides methods for applying docs snapshots.
type SnapshotApplier struct{}

// NewSnapshotApplier creates a new SnapshotApplier.
func NewSnapshotApplier() *SnapshotApplier {
	return &SnapshotApplier{}
}

// Apply extracts a tar.gz snapshot to the target docs directory.
// The snapshot's internal "docs/" path is remapped to the target docsDir.
func (s *SnapshotApplier) Apply(snapshot []byte, targetDir, docsDir string) error {
	if len(snapshot) == 0 {
		// Empty snapshot, nothing to do
		return nil
	}

	// Create gzip reader - use bytes.NewReader to handle binary data correctly
	gzReader, err := gzip.NewReader(bytes.NewReader(snapshot))
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gzReader.Close()

	// Create tar reader
	tarReader := tar.NewReader(gzReader)

	// Ensure target directory exists
	targetDocsPath := filepath.Join(targetDir, docsDir)
	if err := os.MkdirAll(targetDocsPath, 0755); err != nil {
		return fmt.Errorf("failed to create docs directory: %w", err)
	}

	// Extract files
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar entry: %w", err)
		}

		// Skip if header name is empty
		if header.Name == "" {
			continue
		}

		// Remap "docs/" prefix to the target docsDir
		relPath := strings.TrimPrefix(header.Name, "docs/")

		// Skip if this results in empty path (the docs/ directory entry itself)
		if relPath == "" || relPath == "." {
			continue
		}

		targetPath := filepath.Join(targetDocsPath, relPath)

		// Security check: ensure path is within target directory using filepath.Rel
		// This properly handles ".." traversal attacks that simple prefix check misses
		cleanTargetDocsPath := filepath.Clean(targetDocsPath)
		cleanTargetPath := filepath.Clean(targetPath)
		relToTarget, err := filepath.Rel(cleanTargetDocsPath, cleanTargetPath)
		if err != nil || strings.HasPrefix(relToTarget, "..") {
			return fmt.Errorf("path escape detected: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, os.FileMode(header.Mode)); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", targetPath, err)
			}
		case tar.TypeReg:
			// Ensure parent directory exists
			if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
				return fmt.Errorf("failed to create parent directory: %w", err)
			}

			// Create file
			file, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return fmt.Errorf("failed to create file %s: %w", targetPath, err)
			}

			if _, err := io.Copy(file, tarReader); err != nil {
				file.Close()
				return fmt.Errorf("failed to write file %s: %w", targetPath, err)
			}
			file.Close()
		default:
			// Skip other types (symlinks, etc.)
		}
	}

	return nil
}
