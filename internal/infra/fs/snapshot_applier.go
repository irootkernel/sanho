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
// The snapshot paths are interpreted as relative to the docs repo root and
// mirrored under the local docsDir beneath targetDir.
func (s *SnapshotApplier) Apply(snapshot []byte, targetDir, docsDir string) error {
	if len(snapshot) == 0 {
		// Empty snapshot, nothing to do
		return nil
	}

	// Helper: returns true if the path contains a ".git" segment.
	// This is a defensive guard to avoid ever creating or modifying Git
	// metadata directories from snapshots, while allowing other dotfiles
	// (e.g., ".gitignore", ".github", ".vitepress") to be part of the docs.
	hasGitSegment := func(p string) bool {
		// Tar headers always use '/' as the path separator, regardless of
		// the host OS. We must split on '/' here instead of filepath.Separator
		// to correctly detect nested segments like "dir/.git/config"
		// on Windows.
		for _, segment := range strings.Split(p, "/") {
			if segment == "" {
				continue
			}
			if segment == ".git" {
				return true
			}
		}
		return false
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

		// Normalize header path and drop any redundant docs prefix so we don't
		// create nested docs/docs when snapshots already include it.
		relPath := strings.TrimPrefix(header.Name, "./")
		if relPath == "" || relPath == "." {
			continue
		}
		if docsDir != "" {
			if relPath == docsDir {
				// Skip the root docsDir entry.
				continue
			}
			if strings.HasPrefix(relPath, docsDir+"/") {
				relPath = strings.TrimPrefix(relPath, docsDir+"/")
				if relPath == "" {
					continue
				}
			}
		}

		// Skip any entries that would touch a ".git" directory to avoid
		// creating nested Git repositories or modifying Git metadata.
		if hasGitSegment(relPath) {
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
