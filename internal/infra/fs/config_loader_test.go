package fs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/irootkernel/sanho/internal/domain/client"
)

func TestFileConfigLoader_Load(t *testing.T) {
	tests := []struct {
		name       string
		configJSON string
		wantErr    error
		wantConfig *client.WorkspaceConfig
	}{
		{
			name: "valid config with all fields",
			configJSON: `{
				"socket_path": "/tmp/sanhod.sock",
				"workspace_id": "ws-123",
				"project": "sudal",
				"actor_email": "user@example.com",
				"docs_dir": "my_docs",
				"docs_hash_file": ".my_hash",
				"pending_fix_file": ".my_fix",
				"docs_sync_commit_message": "[DOCS] Sync base"
			}`,
			wantErr: nil,
			wantConfig: &client.WorkspaceConfig{
				SocketPath:            "/tmp/sanhod.sock",
				WorkspaceID:           "ws-123",
				Project:               "sudal",
				ActorEmail:            "user@example.com",
				DocsDir:               "my_docs",
				DocsHashFile:          ".my_hash",
				PendingFixFile:        ".my_fix",
				DocsSyncCommitMessage: "[DOCS] Sync base",
			},
		},
		{
			name: "valid config with defaults applied",
			configJSON: `{
				"socket_path": "/tmp/sanhod.sock",
				"workspace_id": "ws-456",
				"project": "dolgorae"
			}`,
			wantErr: nil,
			wantConfig: &client.WorkspaceConfig{
				SocketPath:            "/tmp/sanhod.sock",
				WorkspaceID:           "ws-456",
				Project:               "dolgorae",
				ActorEmail:            "",
				DocsDir:               client.DefaultDocsDir,
				DocsHashFile:          client.DefaultDocsHashFile,
				PendingFixFile:        client.DefaultPendingFixFile,
				DocsSyncCommitMessage: client.DefaultDocsSyncCommitMessage,
			},
		},
		{
			name:       "missing socket_path",
			configJSON: `{"workspace_id": "ws-123", "project": "sudal"}`,
			wantErr:    ErrConfigMissingField,
			wantConfig: nil,
		},
		{
			name:       "missing workspace_id",
			configJSON: `{"socket_path": "/tmp/sanhod.sock", "project": "sudal"}`,
			wantErr:    ErrConfigMissingField,
			wantConfig: nil,
		},
		{
			name:       "missing project",
			configJSON: `{"socket_path": "/tmp/sanhod.sock", "workspace_id": "ws-123"}`,
			wantErr:    ErrConfigMissingField,
			wantConfig: nil,
		},
		{
			name:       "invalid JSON",
			configJSON: `{invalid json`,
			wantErr:    ErrConfigParse,
			wantConfig: nil,
		},
		{
			name: "invalid multiline docs sync commit message",
			configJSON: `{
				"socket_path": "/tmp/sanhod.sock",
				"workspace_id": "ws-123",
				"project": "sudal",
				"docs_sync_commit_message": "line one\nline two"
			}`,
			wantErr:    ErrConfigMissingField,
			wantConfig: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp directory
			tmpDir := t.TempDir()

			// Write config file
			configPath := filepath.Join(tmpDir, ConfigFileName)
			if err := os.WriteFile(configPath, []byte(tt.configJSON), 0644); err != nil {
				t.Fatalf("failed to write test config: %v", err)
			}

			// Load config
			loader := NewFileConfigLoader()
			config, err := loader.Load(tmpDir)

			// Check error
			if tt.wantErr != nil {
				if err == nil {
					t.Errorf("expected error %v, got nil", tt.wantErr)
					return
				}
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("expected error %v, got %v", tt.wantErr, err)
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			// Check config values
			if config.SocketPath != tt.wantConfig.SocketPath {
				t.Errorf("SocketPath = %v, want %v", config.SocketPath, tt.wantConfig.SocketPath)
			}
			if config.WorkspaceID != tt.wantConfig.WorkspaceID {
				t.Errorf("WorkspaceID = %v, want %v", config.WorkspaceID, tt.wantConfig.WorkspaceID)
			}
			if config.Project != tt.wantConfig.Project {
				t.Errorf("Project = %v, want %v", config.Project, tt.wantConfig.Project)
			}
			if config.ActorEmail != tt.wantConfig.ActorEmail {
				t.Errorf("ActorEmail = %v, want %v", config.ActorEmail, tt.wantConfig.ActorEmail)
			}
			if config.DocsDir != tt.wantConfig.DocsDir {
				t.Errorf("DocsDir = %v, want %v", config.DocsDir, tt.wantConfig.DocsDir)
			}
			if config.DocsHashFile != tt.wantConfig.DocsHashFile {
				t.Errorf("DocsHashFile = %v, want %v", config.DocsHashFile, tt.wantConfig.DocsHashFile)
			}
			if config.PendingFixFile != tt.wantConfig.PendingFixFile {
				t.Errorf("PendingFixFile = %v, want %v", config.PendingFixFile, tt.wantConfig.PendingFixFile)
			}
			if config.DocsSyncCommitMessage != tt.wantConfig.DocsSyncCommitMessage {
				t.Errorf("DocsSyncCommitMessage = %v, want %v", config.DocsSyncCommitMessage, tt.wantConfig.DocsSyncCommitMessage)
			}
		})
	}
}

func TestFileConfigLoader_Load_FileNotFound(t *testing.T) {
	tmpDir := t.TempDir()

	loader := NewFileConfigLoader()
	_, err := loader.Load(tmpDir)

	if err == nil {
		t.Error("expected error for missing config file, got nil")
		return
	}

	if !errors.Is(err, ErrConfigNotFound) {
		t.Errorf("expected ErrConfigNotFound, got %v", err)
	}
}
