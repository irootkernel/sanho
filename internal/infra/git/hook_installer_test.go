package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHookInstaller_InstallAllHooksIncludesPostCommit(t *testing.T) {
	tempDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tempDir, ".git", "hooks"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := NewHookInstaller().InstallAllHooks(context.Background(), tempDir); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(tempDir, ".git", "hooks", "post-commit"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "kkachi-cli hook post-commit") {
		t.Fatalf("post-commit hook content = %q", data)
	}
}

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
		"kkachi-cli hook pre-commit",
		"echo also-keep",
		"",
	}, "\n")
	if err := os.WriteFile(hookPath, []byte(content), 0755); err != nil {
		t.Fatalf("failed to seed hook file: %v", err)
	}

	installer := NewHookInstaller()
	if err := installer.RemoveHookLine(context.Background(), tempDir, "pre-commit", "kkachi-cli hook pre-commit"); err != nil {
		t.Fatalf("RemoveHookLine returned error: %v", err)
	}

	data, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("failed to read hook file: %v", err)
	}
	text := string(data)
	if strings.Contains(text, "kkachi-cli hook pre-commit") {
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
	content := "kkachi-cli hook pre-commit\n"
	if err := os.WriteFile(hookPath, []byte(content), 0755); err != nil {
		t.Fatalf("failed to seed hook file: %v", err)
	}

	installer := NewHookInstaller()
	if err := installer.RemoveHookLine(context.Background(), tempDir, "pre-commit", "kkachi-cli hook pre-commit"); err != nil {
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
	if err := installer.RemoveHookLine(context.Background(), tempDir, "pre-commit", "kkachi-cli hook pre-commit"); err != nil {
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
	content := "kkachi-cli hook pre-commit\necho keep\n"
	if err := os.WriteFile(hookPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to seed hook file: %v", err)
	}

	installer := NewHookInstaller()
	if err := installer.RemoveHookLine(context.Background(), tempDir, "pre-commit", "kkachi-cli hook pre-commit"); err != nil {
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
	if strings.Contains(string(data), "kkachi-cli hook pre-commit") {
		t.Fatalf("expected kkachi line removed, got:\n%s", string(data))
	}
	if !strings.Contains(string(data), "echo keep") {
		t.Fatalf("expected other content preserved, got:\n%s", string(data))
	}
}

// TestHookInstaller_InstallHook_WithExitAtEnd verifies that when an existing hook file
// has an exit command at the end, the kkachi command is inserted before the exit.
func TestHookInstaller_InstallHook_WithExitAtEnd(t *testing.T) {
	tempDir := t.TempDir()
	gitDir := filepath.Join(tempDir, ".git")
	hooksDir := filepath.Join(gitDir, "hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		t.Fatal(err)
	}

	hookPath := filepath.Join(hooksDir, "pre-commit")
	content := strings.Join([]string{
		"#!/bin/sh",
		"echo \"existing\"",
		"exit 0",
		"",
	}, "\n")
	if err := os.WriteFile(hookPath, []byte(content), 0755); err != nil {
		t.Fatalf("failed to seed hook file: %v", err)
	}

	installer := NewHookInstaller()
	if err := installer.InstallHook(context.Background(), tempDir, "pre-commit", "kkachi-cli hook pre-commit"); err != nil {
		t.Fatalf("InstallHook returned error: %v", err)
	}

	data, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("failed to read hook file: %v", err)
	}
	text := string(data)

	// Verify kkachi line is present
	if !strings.Contains(text, "kkachi-cli hook pre-commit") {
		t.Fatalf("expected kkachi line to be present, content:\n%s", text)
	}

	// Verify kkachi line comes BEFORE exit 0
	kkachiIdx := strings.Index(text, "kkachi-cli hook pre-commit")
	exitIdx := strings.Index(text, "exit 0")
	if kkachiIdx > exitIdx {
		t.Fatalf("expected kkachi line before exit 0, content:\n%s", text)
	}
}

func TestHookInstaller_InstallHook_WithExitSemicolonAtEnd(t *testing.T) {
	tempDir := t.TempDir()
	gitDir := filepath.Join(tempDir, ".git")
	hooksDir := filepath.Join(gitDir, "hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		t.Fatal(err)
	}

	hookPath := filepath.Join(hooksDir, "pre-commit")
	content := strings.Join([]string{
		"#!/bin/sh",
		"echo \"existing\"",
		"exit;",
		"",
	}, "\n")
	if err := os.WriteFile(hookPath, []byte(content), 0755); err != nil {
		t.Fatalf("failed to seed hook file: %v", err)
	}

	installer := NewHookInstaller()
	if err := installer.InstallHook(context.Background(), tempDir, "pre-commit", "kkachi-cli hook pre-commit"); err != nil {
		t.Fatalf("InstallHook returned error: %v", err)
	}

	data, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("failed to read hook file: %v", err)
	}
	text := string(data)

	kkachiIdx := strings.Index(text, "kkachi-cli hook pre-commit")
	exitIdx := strings.Index(text, "exit;")
	if kkachiIdx < 0 || exitIdx < 0 {
		t.Fatalf("expected both kkachi and exit; lines, content:\n%s", text)
	}
	if kkachiIdx > exitIdx {
		t.Fatalf("expected kkachi line before exit;, content:\n%s", text)
	}
}

func TestHookInstaller_InstallHook_DoesNotTreatExitPrefixAsExitCommand(t *testing.T) {
	tempDir := t.TempDir()
	gitDir := filepath.Join(tempDir, ".git")
	hooksDir := filepath.Join(gitDir, "hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		t.Fatal(err)
	}

	hookPath := filepath.Join(hooksDir, "pre-commit")
	content := strings.Join([]string{
		"#!/bin/sh",
		"echo \"existing\"",
		"exitStatus=0",
		"",
	}, "\n")
	if err := os.WriteFile(hookPath, []byte(content), 0755); err != nil {
		t.Fatalf("failed to seed hook file: %v", err)
	}

	installer := NewHookInstaller()
	if err := installer.InstallHook(context.Background(), tempDir, "pre-commit", "kkachi-cli hook pre-commit"); err != nil {
		t.Fatalf("InstallHook returned error: %v", err)
	}

	data, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("failed to read hook file: %v", err)
	}
	text := string(data)

	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	lastLine := lines[len(lines)-1]
	if lastLine != "kkachi-cli hook pre-commit" {
		t.Fatalf("expected kkachi line appended at end, got last line: %q, full content:\n%s", lastLine, text)
	}
}

// TestHookInstaller_InstallHook_WithoutExit verifies that when an existing hook file
// has no exit command, the kkachi command is appended at the end.
func TestHookInstaller_InstallHook_WithoutExit(t *testing.T) {
	tempDir := t.TempDir()
	gitDir := filepath.Join(tempDir, ".git")
	hooksDir := filepath.Join(gitDir, "hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		t.Fatal(err)
	}

	hookPath := filepath.Join(hooksDir, "pre-commit")
	content := strings.Join([]string{
		"#!/bin/sh",
		"echo \"existing\"",
		"",
	}, "\n")
	if err := os.WriteFile(hookPath, []byte(content), 0755); err != nil {
		t.Fatalf("failed to seed hook file: %v", err)
	}

	installer := NewHookInstaller()
	if err := installer.InstallHook(context.Background(), tempDir, "pre-commit", "kkachi-cli hook pre-commit"); err != nil {
		t.Fatalf("InstallHook returned error: %v", err)
	}

	data, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("failed to read hook file: %v", err)
	}
	text := string(data)

	// Verify kkachi line is present
	if !strings.Contains(text, "kkachi-cli hook pre-commit") {
		t.Fatalf("expected kkachi line to be present, content:\n%s", text)
	}

	// Verify kkachi line is at the end (after existing content)
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	lastLine := lines[len(lines)-1]
	if lastLine != "kkachi-cli hook pre-commit" {
		t.Fatalf("expected kkachi line at end, got last line: %q, full content:\n%s", lastLine, text)
	}
}

// TestHookInstaller_InstallHook_NewFile verifies that when no hook file exists,
// a new file is created with shebang and the kkachi command.
func TestHookInstaller_InstallHook_NewFile(t *testing.T) {
	tempDir := t.TempDir()
	gitDir := filepath.Join(tempDir, ".git")
	hooksDir := filepath.Join(gitDir, "hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		t.Fatal(err)
	}

	hookPath := filepath.Join(hooksDir, "pre-commit")
	// No file exists initially

	installer := NewHookInstaller()
	if err := installer.InstallHook(context.Background(), tempDir, "pre-commit", "kkachi-cli hook pre-commit"); err != nil {
		t.Fatalf("InstallHook returned error: %v", err)
	}

	data, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("failed to read hook file: %v", err)
	}
	text := string(data)

	// Verify shebang is present
	if !strings.HasPrefix(text, "#!/bin/sh\n") {
		t.Fatalf("expected shebang at start, content:\n%s", text)
	}

	// Verify kkachi line is present
	if !strings.Contains(text, "kkachi-cli hook pre-commit") {
		t.Fatalf("expected kkachi line to be present, content:\n%s", text)
	}

	// Verify file is executable
	info, err := os.Stat(hookPath)
	if err != nil {
		t.Fatalf("failed to stat hook file: %v", err)
	}
	if info.Mode().Perm()&0111 == 0 {
		t.Fatalf("expected hook file to be executable, mode: %v", info.Mode())
	}
}
