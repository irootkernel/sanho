package pty

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// Config holds configuration for PTY operations.
type Config struct {
	AllowedShells    []string // Allowed shell executables
	DefaultShell     string   // Default shell if not specified
	DefaultCols      uint16   // Default terminal columns
	DefaultRows      uint16   // Default terminal rows
	WSAllowedOrigins []string // Normalized WebSocket allowed origins
	MaxSessions      int      // Maximum number of concurrent sessions (0 = unlimited)
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
		AllowedShells: allowed,
		DefaultShell:  defaultShell,
		DefaultCols:   80,
		DefaultRows:   24,
		MaxSessions:   100,
	}
}

// LoadConfigFromEnv loads PTY configuration from environment variables.
// Environment variables:
//   - PTY_ALLOWED_SHELLS: Comma-separated list of allowed shells
//   - PTY_DEFAULT_SHELL: Default shell to use
//   - PTY_WS_ALLOWED_ORIGINS: Comma-separated list of allowed WS origins (http(s)://host[:port])
//   - PTY_DISCONNECT_POLICY: Deprecated and ignored (terminate-only)
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

	// Load WS allowed origins
	if origins := os.Getenv("PTY_WS_ALLOWED_ORIGINS"); origins != "" {
		cfg.WSAllowedOrigins = parseAllowedOrigins(origins)
	}

	// PTY_DISCONNECT_POLICY is deprecated and ignored.
	if policy := os.Getenv("PTY_DISCONNECT_POLICY"); policy != "" {
		slog.Warn("pty_disconnect_policy_ignored", "value", policy)
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

// NormalizeOrigin normalizes an origin string to scheme://host[:port].
func NormalizeOrigin(origin string) (string, error) {
	trimmed := strings.TrimSpace(origin)
	if trimmed == "" {
		return "", fmt.Errorf("origin is empty")
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("origin parse failed: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("origin must include scheme and host")
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("origin scheme must be http or https")
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return "", fmt.Errorf("origin must not include path")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("origin must not include query or fragment")
	}

	return scheme + "://" + strings.ToLower(parsed.Host), nil
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

func parseAllowedOrigins(origins string) []string {
	entries := splitAndTrim(origins, ",")
	allowed := make([]string, 0, len(entries))
	for _, entry := range entries {
		normalized, err := NormalizeOrigin(entry)
		if err != nil {
			slog.Warn("pty_ws_allowed_origin_invalid", "value", entry)
			continue
		}
		allowed = append(allowed, normalized)
	}
	return allowed
}
