// Package merge provides utilities for detecting and handling merge conflicts in docs.
package merge

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Conflict marker constants.
const (
	MarkerStart  = "<<<<<<<"
	MarkerMiddle = "======="
	MarkerEnd    = ">>>>>>>"
)

// ConflictDetector defines the interface for detecting conflict markers in files.
type ConflictDetector interface {
	// DetectConflicts scans the given directory for files containing conflict markers.
	// Returns a list of relative paths to files with conflicts.
	DetectConflicts(docsDir string) ([]string, error)
}

// FileConflictDetector implements ConflictDetector by scanning file contents.
type FileConflictDetector struct{}

// NewFileConflictDetector creates a new FileConflictDetector.
func NewFileConflictDetector() *FileConflictDetector {
	return &FileConflictDetector{}
}

// DetectConflicts implements ConflictDetector.DetectConflicts.
// It walks through the docs directory and checks each file for conflict markers.
// A file is considered to have conflicts if it contains all three markers:
// "<<<<<<<", "=======", and ">>>>>>>".
func (d *FileConflictDetector) DetectConflicts(docsDir string) ([]string, error) {
	var conflictFiles []string

	err := filepath.WalkDir(docsDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if entry.IsDir() {
			return nil
		}

		// Check for conflicts
		hasConflict, err := d.fileHasConflict(path)
		if err != nil {
			// Log warning but continue scanning other files
			return nil
		}

		if hasConflict {
			relPath, err := filepath.Rel(docsDir, path)
			if err != nil {
				relPath = path
			}
			conflictFiles = append(conflictFiles, relPath)
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to scan docs directory: %w", err)
	}

	return conflictFiles, nil
}

// fileHasConflict checks if a single file contains all three conflict markers.
func (d *FileConflictDetector) fileHasConflict(filePath string) (bool, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return false, err
	}
	defer func() {
		_ = file.Close()
	}()

	var hasStart, hasMiddle, hasEnd bool

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()

		if strings.Contains(line, MarkerStart) {
			hasStart = true
		}
		if strings.Contains(line, MarkerMiddle) {
			hasMiddle = true
		}
		if strings.Contains(line, MarkerEnd) {
			hasEnd = true
		}

		// Early exit if all markers found
		if hasStart && hasMiddle && hasEnd {
			return true, nil
		}
	}

	if err := scanner.Err(); err != nil {
		return false, err
	}

	return false, nil
}

// HasConflictMarkers checks if the given text contains conflict markers.
// This is a utility function for testing text content directly.
func HasConflictMarkers(text string) bool {
	return strings.Contains(text, MarkerStart) &&
		strings.Contains(text, MarkerMiddle) &&
		strings.Contains(text, MarkerEnd)
}
