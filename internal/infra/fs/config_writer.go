package fs

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/irootkernel/sanho/internal/domain/client"
)

// ConfigWriter defines the interface for writing workspace configuration.
type ConfigWriter interface {
	// Write saves the config to the specified directory.
	Write(dir string, config *client.WorkspaceConfig) error
}

// FileConfigWriter implements ConfigWriter using the file system.
type FileConfigWriter struct{}

// NewFileConfigWriter creates a new FileConfigWriter.
func NewFileConfigWriter() *FileConfigWriter {
	return &FileConfigWriter{}
}

// Write saves .kkachi.json to the specified directory.
func (w *FileConfigWriter) Write(dir string, config *client.WorkspaceConfig) error {
	configPath := filepath.Join(dir, ConfigFileName)

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(configPath, data, 0644)
}

// Exists checks if .kkachi.json already exists in the directory.
func (w *FileConfigWriter) Exists(dir string) bool {
	configPath := filepath.Join(dir, ConfigFileName)
	_, err := os.Stat(configPath)
	return err == nil
}
