package fs

import (
	"fmt"
	"os"

	"github.com/SeventeenthEarth/kkachi/internal/config"
	"gopkg.in/yaml.v3"
)

// SecurityLoader defines the interface for loading security configuration.
type SecurityLoader interface {
	Load() (*config.SecurityConfig, error)
}

// FileSecurityLoader implements SecurityLoader using the file system.
type FileSecurityLoader struct {
	path string
}

// NewFileSecurityLoader creates a new FileSecurityLoader with the given path.
func NewFileSecurityLoader(path string) *FileSecurityLoader {
	return &FileSecurityLoader{
		path: path,
	}
}

// Load reads and parses security_rules.yaml from the configured path.
func (l *FileSecurityLoader) Load() (*config.SecurityConfig, error) {
	data, err := os.ReadFile(l.path)
	if err != nil {
		return nil, fmt.Errorf("failed to read security rules file: %w", err)
	}

	var cfg config.SecurityConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse security rules file: %w", err)
	}

	return &cfg, nil
}
