package pty

import (
	"os"
	"testing"
)

func TestLoadConfigFromEnv_Defaults(t *testing.T) {
	// Clear any existing env vars
	os.Unsetenv("PTY_ALLOWED_SHELLS")
	os.Unsetenv("PTY_DEFAULT_SHELL")

	cfg := LoadConfigFromEnv()

	// Check defaults
	expectedShells := []string{"/bin/sh", "/bin/bash", "/bin/zsh"}
	if len(cfg.AllowedShells) != len(expectedShells) {
		t.Errorf("Expected %d allowed shells, got %d", len(expectedShells), len(cfg.AllowedShells))
	}
	for i, shell := range expectedShells {
		if cfg.AllowedShells[i] != shell {
			t.Errorf("Expected shell %s at index %d, got %s", shell, i, cfg.AllowedShells[i])
		}
	}

	if cfg.DefaultShell != "/bin/sh" {
		t.Errorf("Expected default shell /bin/sh, got %s", cfg.DefaultShell)
	}

	if cfg.DefaultCols != 80 {
		t.Errorf("Expected default cols 80, got %d", cfg.DefaultCols)
	}

	if cfg.DefaultRows != 24 {
		t.Errorf("Expected default rows 24, got %d", cfg.DefaultRows)
	}

	if cfg.DisconnectPolicy != DisconnectPolicyTerminate {
		t.Errorf("Expected default disconnect policy %s, got %s", DisconnectPolicyTerminate, cfg.DisconnectPolicy)
	}
}

func TestLoadConfigFromEnv_DisconnectPolicy(t *testing.T) {
	os.Setenv("PTY_DISCONNECT_POLICY", "stay")
	defer os.Unsetenv("PTY_DISCONNECT_POLICY")

	cfg := LoadConfigFromEnv()

	if cfg.DisconnectPolicy != DisconnectPolicyStay {
		t.Errorf("Expected disconnect policy stay, got %s", cfg.DisconnectPolicy)
	}
}

func TestLoadConfigFromEnv_MaxSessions(t *testing.T) {
	os.Setenv("PTY_MAX_SESSIONS", "50")
	defer os.Unsetenv("PTY_MAX_SESSIONS")

	cfg := LoadConfigFromEnv()

	if cfg.MaxSessions != 50 {
		t.Errorf("Expected max sessions 50, got %d", cfg.MaxSessions)
	}
}

func TestLoadConfigFromEnv_AllowedShells(t *testing.T) {
	os.Setenv("PTY_ALLOWED_SHELLS", "/bin/bash,/usr/bin/zsh")
	defer os.Unsetenv("PTY_ALLOWED_SHELLS")

	cfg := LoadConfigFromEnv()

	expected := []string{"/bin/bash", "/usr/bin/zsh"}
	if len(cfg.AllowedShells) != len(expected) {
		t.Errorf("Expected %d allowed shells, got %d", len(expected), len(cfg.AllowedShells))
	}
	for i, shell := range expected {
		if cfg.AllowedShells[i] != shell {
			t.Errorf("Expected shell %s at index %d, got %s", shell, i, cfg.AllowedShells[i])
		}
	}
}

func TestLoadConfigFromEnv_DefaultShell(t *testing.T) {
	os.Setenv("PTY_DEFAULT_SHELL", "/bin/zsh")
	defer os.Unsetenv("PTY_DEFAULT_SHELL")

	cfg := LoadConfigFromEnv()

	if cfg.DefaultShell != "/bin/zsh" {
		t.Errorf("Expected default shell /bin/zsh, got %s", cfg.DefaultShell)
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.DefaultShell != "/bin/sh" {
		t.Errorf("DefaultConfig should have /bin/sh as default, got %s", cfg.DefaultShell)
	}

	if cfg.DefaultCols != 80 || cfg.DefaultRows != 24 {
		t.Errorf("DefaultConfig should have 80x24 terminal size, got %dx%d",
			cfg.DefaultCols, cfg.DefaultRows)
	}
}
