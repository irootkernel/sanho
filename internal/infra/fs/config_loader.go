package fs

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/irootkernel/sanho/internal/domain/client"
)

// Config file name constant.
const ConfigFileName = ".sanho.json"

// Error types for config loading.
var (
	// ErrConfigNotFound is returned when the config file does not exist.
	ErrConfigNotFound = errors.New("config file not found")
	// ErrConfigParse is returned when the config file cannot be parsed.
	ErrConfigParse = errors.New("failed to parse config file")
	// ErrConfigMissingField is returned when a required field is missing.
	ErrConfigMissingField = errors.New("missing required field in config")
)

// ConfigLoader defines the interface for loading workspace configuration.
type ConfigLoader interface {
	// Load reads and parses the config file from the given directory.
	Load(dir string) (*client.WorkspaceConfig, error)
}

// FileConfigLoader implements ConfigLoader using the file system.
type FileConfigLoader struct{}

// NewFileConfigLoader creates a new FileConfigLoader.
func NewFileConfigLoader() *FileConfigLoader {
	return &FileConfigLoader{}
}

// Load reads and parses .sanho.json from the specified directory.
// Returns ErrConfigNotFound if the file doesn't exist,
// ErrConfigParse if the file cannot be parsed,
// and ErrConfigMissingField if required fields are missing.
func (l *FileConfigLoader) Load(dir string) (*client.WorkspaceConfig, error) {
	configPath := filepath.Join(dir, ConfigFileName)

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrConfigNotFound, configPath)
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config client.WorkspaceConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrConfigParse, err)
	}

	// Validate required fields
	if config.ServerURL == "" {
		return nil, fmt.Errorf("%w: server_url", ErrConfigMissingField)
	}
	if config.WorkspaceID == "" {
		return nil, fmt.Errorf("%w: workspace_id", ErrConfigMissingField)
	}
	if config.Project == "" {
		return nil, fmt.Errorf("%w: project", ErrConfigMissingField)
	}

	// Apply defaults for optional fields
	config.ApplyDefaults()
	if strings.TrimSpace(config.DocsSyncCommitMessage) == "" ||
		strings.ContainsAny(config.DocsSyncCommitMessage, "\r\n") {
		return nil, fmt.Errorf("%w: docs_sync_commit_message", ErrConfigMissingField)
	}

	return &config, nil
}
