package fs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// GitignoreManager ensures .gitignore contains required sanho entries.
type GitignoreManager struct{}

// NewGitignoreManager creates a new GitignoreManager.
func NewGitignoreManager() *GitignoreManager {
	return &GitignoreManager{}
}

// EnsureEntries adds the given entries to .gitignore in dir if they are missing.
// A header line (e.g., "# Sanho") is added before the entries when provided
// and not already present. Existing contents are preserved, and duplicate lines
// are avoided.
func (g *GitignoreManager) EnsureEntries(dir string, header string, entries []string) error {
	if len(entries) == 0 {
		return nil
	}

	gitignorePath := filepath.Join(dir, ".gitignore")

	lines, lineSet, err := readGitignore(gitignorePath)
	if err != nil {
		return err
	}

	entrySet := make(map[string]struct{}, len(entries))
	var missing []string
	for _, entry := range entries {
		entrySet[entry] = struct{}{}
		if !lineSet[entry] {
			missing = append(missing, entry)
		}
	}

	if len(missing) == 0 {
		return nil
	}

	// Separate from existing content unless the last line is already part of
	// the sanho block.
	if len(lines) > 0 {
		last := lines[len(lines)-1]
		if last != "" {
			if _, isEntry := entrySet[last]; !isEntry && last != header {
				lines = append(lines, "")
			}
		}
	}

	// Add header if provided and not already present anywhere.
	if header != "" && !lineSet[header] {
		lines = append(lines, header)
	}

	// Append missing entries in the order provided.
	lines = append(lines, missing...)

	content := strings.Join(lines, "\n")
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}

	if err := os.WriteFile(gitignorePath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write .gitignore: %w", err)
	}

	return nil
}

// readGitignore returns existing .gitignore lines and a set for fast lookup.
func readGitignore(path string) ([]string, map[string]bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []string{}, map[string]bool{}, nil
		}
		return nil, nil, fmt.Errorf("failed to read .gitignore: %w", err)
	}

	content := strings.ReplaceAll(string(data), "\r\n", "\n")
	content = strings.TrimSuffix(content, "\n")
	if content == "" {
		return []string{}, map[string]bool{}, nil
	}

	lines := strings.Split(content, "\n")
	lineSet := make(map[string]bool, len(lines))
	for _, line := range lines {
		lineSet[line] = true
	}

	return lines, lineSet, nil
}
