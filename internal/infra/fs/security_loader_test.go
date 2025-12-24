package fs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSecurityLoader_Load(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "security-loader-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "security_rules.yaml")
	yamlContent := `
blacklist:
  - pattern: "rm -rf .*"
    reason: "Blocked for safety"
`
	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	loader := NewFileSecurityLoader(configPath)
	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("failed to load security rules: %v", err)
	}

	if len(cfg.Blacklist) != 1 {
		t.Errorf("expected 1 blacklist rule, got %d", len(cfg.Blacklist))
	} else {
		if cfg.Blacklist[0].Pattern != "rm -rf .*" {
			t.Errorf("expected pattern 'rm -rf .*', got '%s'", cfg.Blacklist[0].Pattern)
		}
		if cfg.Blacklist[0].Reason != "Blocked for safety" {
			t.Errorf("expected reason 'Blocked for safety', got '%s'", cfg.Blacklist[0].Reason)
		}
	}
}

func TestSecurityLoader_Load_NotFound(t *testing.T) {
	loader := NewFileSecurityLoader("non-existent.yaml")
	_, err := loader.Load()
	if err == nil {
		t.Error("expected error for non-existent file, got nil")
	}
}
