package pty

import (
	"log/slog"
	"os"
	"strconv"
	"strings"
)

// DisconnectPolicy represents the action to take when a client disconnects.
type DisconnectPolicy string

const (
	// DisconnectPolicyTerminate kills the PTY session immediately on disconnect.
	DisconnectPolicyTerminate DisconnectPolicy = "terminate"
	// DisconnectPolicyStay keeps the PTY session running on disconnect.
	DisconnectPolicyStay DisconnectPolicy = "stay"
)

// Config holds configuration for PTY operations.
type Config struct {
	AllowedShells    []string         // Allowed shell executables
	DefaultShell     string           // Default shell if not specified
	DefaultCols      uint16           // Default terminal columns
	DefaultRows      uint16           // Default terminal rows
	DisconnectPolicy DisconnectPolicy // Action on client disconnect
	MaxSessions      int              // Maximum number of concurrent sessions (0 = unlimited)
}

const (
	MinCols = 20
	MaxCols = 400
	MinRows = 5
	MaxRows = 200
)

// DefaultConfig returns the default PTY configuration.
func DefaultConfig() Config {
	// 1. Detect default shell from environment or fallback list
	defaultShell := os.Getenv("SHELL")
	if defaultShell == "" {
		for _, s := range []string{"/bin/zsh", "/bin/bash", "/bin/sh"} {
			if _, err := os.Stat(s); err == nil {
				defaultShell = s
				break
			}
		}
	}
	if defaultShell == "" {
		defaultShell = "/bin/sh"
	}

	// 2. Setup allowed shells (common paths + detected default)
	allowed := []string{"/bin/sh", "/bin/bash", "/bin/zsh", "/usr/bin/bash", "/usr/bin/zsh"}
	isAllowed := false
	for _, a := range allowed {
		if a == defaultShell {
			isAllowed = true
			break
		}
	}
	if !isAllowed {
		allowed = append(allowed, defaultShell)
	}

	return Config{
		AllowedShells:    allowed,
		DefaultShell:     defaultShell,
		DefaultCols:      80,
		DefaultRows:      24,
		DisconnectPolicy: DisconnectPolicyTerminate,
		MaxSessions:      100,
	}
}

// LoadConfigFromEnv loads PTY configuration from environment variables.
// Environment variables:
//   - PTY_ALLOWED_SHELLS: Comma-separated list of allowed shells
//   - PTY_DEFAULT_SHELL: Default shell to use
//   - PTY_DISCONNECT_POLICY: Action on disconnect (terminate, stay)
//   - PTY_MAX_SESSIONS: Maximum concurrent sessions
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

	// Load disconnect policy
	if policy := os.Getenv("PTY_DISCONNECT_POLICY"); policy != "" {
		switch DisconnectPolicy(policy) {
		case DisconnectPolicyTerminate, DisconnectPolicyStay:
			cfg.DisconnectPolicy = DisconnectPolicy(policy)
		default:
			slog.Warn("invalid_disconnect_policy", "value", policy, "using_default", DisconnectPolicyTerminate)
			// Keep default
		}
	}

	// Load max sessions
	if maxSessions := os.Getenv("PTY_MAX_SESSIONS"); maxSessions != "" {
		if val, err := parseMaxSessions(maxSessions); err == nil {
			cfg.MaxSessions = val
		}
	}

	return cfg
}

func parseMaxSessions(s string) (int, error) {
	return strconv.Atoi(s)
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
