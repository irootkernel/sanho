package hook

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SeventeenthEarth/kkachi/internal/domain/client"
	"github.com/SeventeenthEarth/kkachi/internal/domain/docs"
)

// --- Fake implementations for commit-msg testing ---

type fakeCommitMsgConfigLoader struct {
	config *client.WorkspaceConfig
	err    error
}

func (f *fakeCommitMsgConfigLoader) Load(workDir string) (*client.WorkspaceConfig, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.config, nil
}

type fakeCommitMsgDocsHashStore struct {
	hash docs.CommitHash
	err  error
}

func (f *fakeCommitMsgDocsHashStore) Read(path string) (docs.CommitHash, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.hash, nil
}

type fakeCommitMsgGitClient struct {
	hasChanges bool
	err        error
}

func (f *fakeCommitMsgGitClient) HasDocsChangeStaged(ctx context.Context, repoPath, docsDir string) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	return f.hasChanges, nil
}

type fakeCommitMsgOutput struct {
	infos    []string
	warnings []string
}

func (f *fakeCommitMsgOutput) Info(msg string)    { f.infos = append(f.infos, msg) }
func (f *fakeCommitMsgOutput) Warning(msg string) { f.warnings = append(f.warnings, msg) }

// --- Tests ---

func TestCommitMsgUseCase_NoConfig(t *testing.T) {
	output := &fakeCommitMsgOutput{}
	uc := NewCommitMsgUseCase(
		&fakeCommitMsgConfigLoader{err: os.ErrNotExist},
		&fakeCommitMsgDocsHashStore{},
		&fakeCommitMsgGitClient{},
		output,
	)

	err := uc.Execute(context.Background(), "/fake/dir", "/fake/msg")
	if err != nil {
		t.Errorf("Expected no error (should not block commit), got: %v", err)
	}
	if len(output.warnings) == 0 {
		t.Error("Expected warning about not being a kkachi workspace")
	}
}

func TestCommitMsgUseCase_NoDocsChanges(t *testing.T) {
	output := &fakeCommitMsgOutput{}
	uc := NewCommitMsgUseCase(
		&fakeCommitMsgConfigLoader{config: &client.WorkspaceConfig{
			ServerURL:   "http://localhost",
			WorkspaceID: "test",
			Project:     "test",
		}},
		&fakeCommitMsgDocsHashStore{hash: "abc123"},
		&fakeCommitMsgGitClient{hasChanges: false},
		output,
	)

	err := uc.Execute(context.Background(), "/fake/dir", "/fake/msg")
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	// Should exit silently without adding tag
	if len(output.infos) != 0 {
		t.Errorf("Expected no info messages, got: %v", output.infos)
	}
}

func TestCommitMsgUseCase_AddsDocsVersionTag(t *testing.T) {
	// Create temp file for commit message
	tempDir := t.TempDir()
	msgFile := filepath.Join(tempDir, "COMMIT_EDITMSG")
	originalMsg := "Initial commit\n\nThis adds the docs."
	if err := os.WriteFile(msgFile, []byte(originalMsg), 0644); err != nil {
		t.Fatalf("Failed to create message file: %v", err)
	}

	output := &fakeCommitMsgOutput{}
	uc := NewCommitMsgUseCase(
		&fakeCommitMsgConfigLoader{config: &client.WorkspaceConfig{
			ServerURL:    "http://localhost",
			WorkspaceID:  "test",
			Project:      "test",
			DocsHashFile: ".kkachi_docs_hash",
		}},
		&fakeCommitMsgDocsHashStore{hash: "abc123def456"},
		&fakeCommitMsgGitClient{hasChanges: true},
		output,
	)

	err := uc.Execute(context.Background(), tempDir, msgFile)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	// Verify message was updated
	content, err := os.ReadFile(msgFile)
	if err != nil {
		t.Fatalf("Failed to read message file: %v", err)
	}

	if !strings.Contains(string(content), "docs-version: abc123def456") {
		t.Errorf("Expected message to contain 'docs-version: abc123def456', got:\n%s", content)
	}
}

func TestCommitMsgUseCase_DocsVersionAlreadyExists(t *testing.T) {
	// Create temp file for commit message that already has docs-version
	tempDir := t.TempDir()
	msgFile := filepath.Join(tempDir, "COMMIT_EDITMSG")
	originalMsg := "Initial commit\n\ndocs-version: existing123\n"
	if err := os.WriteFile(msgFile, []byte(originalMsg), 0644); err != nil {
		t.Fatalf("Failed to create message file: %v", err)
	}

	output := &fakeCommitMsgOutput{}
	uc := NewCommitMsgUseCase(
		&fakeCommitMsgConfigLoader{config: &client.WorkspaceConfig{
			ServerURL:    "http://localhost",
			WorkspaceID:  "test",
			Project:      "test",
			DocsHashFile: ".kkachi_docs_hash",
		}},
		&fakeCommitMsgDocsHashStore{hash: "newHash456"},
		&fakeCommitMsgGitClient{hasChanges: true},
		output,
	)

	err := uc.Execute(context.Background(), tempDir, msgFile)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	// Verify message was NOT updated (still has original hash)
	content, err := os.ReadFile(msgFile)
	if err != nil {
		t.Fatalf("Failed to read message file: %v", err)
	}

	if strings.Contains(string(content), "newHash456") {
		t.Errorf("Expected message to NOT contain new hash, but it does:\n%s", content)
	}
	if !strings.Contains(string(content), "existing123") {
		t.Errorf("Expected message to still contain 'existing123', got:\n%s", content)
	}
}

// --- Unit tests for helper functions ---

func TestHasDocsVersionTag(t *testing.T) {
	tests := []struct {
		content  string
		expected bool
	}{
		{"Initial commit", false},
		{"docs-version: abc123", true},
		{"Initial commit\n\ndocs-version: abc123", true},
		{"  docs-version: abc123", true},
		{"docs-version without colon", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.content[:min(20, len(tt.content))], func(t *testing.T) {
			result := hasDocsVersionTag(tt.content)
			if result != tt.expected {
				t.Errorf("hasDocsVersionTag(%q) = %v, want %v", tt.content, result, tt.expected)
			}
		})
	}
}

func TestAppendDocsVersionTag(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		hash     string
		expected string
	}{
		{
			name:     "empty content",
			content:  "",
			hash:     "abc123",
			expected: "docs-version: abc123\n",
		},
		{
			name:     "single line",
			content:  "Initial commit",
			hash:     "abc123",
			expected: "Initial commit\n\ndocs-version: abc123\n",
		},
		{
			name:     "multi line",
			content:  "Initial commit\n\nThis is the body.",
			hash:     "abc123",
			expected: "Initial commit\n\nThis is the body.\n\ndocs-version: abc123\n",
		},
		{
			name:     "trailing newlines",
			content:  "Initial commit\n\n",
			hash:     "abc123",
			expected: "Initial commit\n\ndocs-version: abc123\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := appendDocsVersionTag(tt.content, tt.hash)
			if result != tt.expected {
				t.Errorf("appendDocsVersionTag() =\n%q\nwant:\n%q", result, tt.expected)
			}
		})
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
