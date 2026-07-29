package config

import (
	"os"
	"path/filepath"
)

const DefaultStatePath = "data/sanho_state.json"

// ResolveStatePath returns a usable path for the state file, creating parent directories if needed.
func ResolveStatePath(envPath string) (string, error) {
	statePath := envPath
	if statePath == "" {
		statePath = DefaultStatePath
	}
	if dir := filepath.Dir(statePath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return "", err
		}
	}
	return statePath, nil
}
