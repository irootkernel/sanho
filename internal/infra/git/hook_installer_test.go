package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHookInstaller_RemoveHookLine(t *testing.T) {
	tempDir := t.TempDir()
	gitDir := filepath.Join(tempDir, ".git")
	hooksDir := filepath.Join(gitDir, "hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		t.Fatal(err)
	}

	hookPath := filepath.Join(hooksDir, "pre-commit")
	content := strings.Join([]string{
		"#!/bin/sh",
		"echo keep-me",
		"kkachi hook pre-commit",
		"echo also-keep",
		"",
	}, "\n")
	if err := os.WriteFile(hookPath, []byte(content), 0755); err != nil {
		t.Fatalf("failed to seed hook file: %v", err)
	}

	installer := NewHookInstaller()
	if err := installer.RemoveHookLine(context.Background(), tempDir, "pre-commit", "kkachi hook pre-commit"); err != nil {
		t.Fatalf("RemoveHookLine returned error: %v", err)
	}

	data, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("failed to read hook file: %v", err)
	}
	text := string(data)
	if strings.Contains(text, "kkachi hook pre-commit") {
		t.Fatalf("expected kkachi line to be removed, content:\n%s", text)
	}
	if !strings.Contains(text, "echo keep-me") || !strings.Contains(text, "echo also-keep") {
		t.Fatalf("expected other lines to remain, content:\n%s", text)
	}
}

func TestHookInstaller_RemoveHookLine_FileBecomesEmpty(t *testing.T) {
	tempDir := t.TempDir()
	gitDir := filepath.Join(tempDir, ".git")
	hooksDir := filepath.Join(gitDir, "hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		t.Fatal(err)
	}

	hookPath := filepath.Join(hooksDir, "pre-commit")
	content := "kkachi hook pre-commit\n"
	if err := os.WriteFile(hookPath, []byte(content), 0755); err != nil {
		t.Fatalf("failed to seed hook file: %v", err)
	}

	installer := NewHookInstaller()
	if err := installer.RemoveHookLine(context.Background(), tempDir, "pre-commit", "kkachi hook pre-commit"); err != nil {
		t.Fatalf("RemoveHookLine returned error: %v", err)
	}

	if _, err := os.Stat(hookPath); !os.IsNotExist(err) {
		t.Fatalf("expected hook file to be deleted, got err=%v", err)
	}
}

func TestHookInstaller_RemoveHookLine_NoFile(t *testing.T) {
	tempDir := t.TempDir()
	gitDir := filepath.Join(tempDir, ".git")
	if err := os.MkdirAll(filepath.Join(gitDir, "hooks"), 0755); err != nil {
		t.Fatal(err)
	}
	installer := NewHookInstaller()
	if err := installer.RemoveHookLine(context.Background(), tempDir, "pre-commit", "kkachi hook pre-commit"); err != nil {
		t.Fatalf("expected no error when hook file is missing, got %v", err)
	}
}

func TestHookInstaller_RemoveHookLine_PreservesPermissions(t *testing.T) {
	tempDir := t.TempDir()
	gitDir := filepath.Join(tempDir, ".git")
	hooksDir := filepath.Join(gitDir, "hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		t.Fatal(err)
	}

	hookPath := filepath.Join(hooksDir, "pre-commit")
	content := "kkachi hook pre-commit\necho keep\n"
	if err := os.WriteFile(hookPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to seed hook file: %v", err)
	}

	installer := NewHookInstaller()
	if err := installer.RemoveHookLine(context.Background(), tempDir, "pre-commit", "kkachi hook pre-commit"); err != nil {
		t.Fatalf("RemoveHookLine returned error: %v", err)
	}

	info, err := os.Stat(hookPath)
	if err != nil {
		t.Fatalf("failed to stat hook: %v", err)
	}
	if info.Mode().Perm() != 0644 {
		t.Fatalf("expected permissions 0644, got %v", info.Mode().Perm())
	}
	data, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("failed to read hook: %v", err)
	}
	if strings.Contains(string(data), "kkachi hook pre-commit") {
		t.Fatalf("expected kkachi line removed, got:\n%s", string(data))
	}
	if !strings.Contains(string(data), "echo keep") {
		t.Fatalf("expected other content preserved, got:\n%s", string(data))
	}
}
