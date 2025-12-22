package pty

import (
	"os"
	"strings"
)

// Config holds configuration for PTY operations.
type Config struct {
	AllowedShells []string // Allowed shell executables
	DefaultShell  string   // Default shell if not specified
	DefaultCols   uint16   // Default terminal columns
	DefaultRows   uint16   // Default terminal rows
}

const (
	MinCols = 20
	MaxCols = 400
	MinRows = 5
	MaxRows = 200
)

// DefaultConfig returns the default PTY configuration.
func DefaultConfig() Config {
	return Config{
		AllowedShells: []string{"/bin/sh", "/bin/bash", "/bin/zsh"},
		DefaultShell:  "/bin/sh",
		DefaultCols:   80,
		DefaultRows:   24,
	}
}

// LoadConfigFromEnv loads PTY configuration from environment variables.
// Environment variables:
//   - PTY_ALLOWED_SHELLS: Comma-separated list of allowed shells
//   - PTY_DEFAULT_SHELL: Default shell to use
func LoadConfigFromEnv() Config {
	cfg := DefaultConfig()

	// Load allowed shells
	if shells := os.Getenv("PTY_ALLOWED_SHELLS"); shells != "" {
		cfg.AllowedShells = splitAndTrim(shells, ",")
	}

	// Load default shell
	if shell := os.Getenv("PTY_DEFAULT_SHELL"); shell != "" {
		cfg.DefaultShell = shell
	}

	return cfg
}

// splitAndTrim splits a string by separator and trims whitespace from each part.
func splitAndTrim(s, sep string) []string {
	parts := strings.Split(s, sep)
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
