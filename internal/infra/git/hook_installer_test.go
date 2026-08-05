package git

import (
	"context"
	"os"
	"os/exec"
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
	if !strings.Contains(string(data), "sanho hook post-commit") {
		t.Fatalf("post-commit hook content = %q", data)
	}
}

func TestHookInstaller_InstallAllHooksMigratesPrePushArguments(t *testing.T) {
	tempDir := t.TempDir()
	hooksDir := filepath.Join(tempDir, ".git", "hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		t.Fatal(err)
	}
	hookPath := filepath.Join(hooksDir, "pre-push")
	legacy := "#!/bin/sh\necho custom-before\nsanho hook pre-push\necho custom-after\n"
	if err := os.WriteFile(hookPath, []byte(legacy), 0740); err != nil {
		t.Fatal(err)
	}
	if err := NewHookInstaller().InstallAllHooks(context.Background(), tempDir); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Count(text, "sanho hook pre-push") != 1 || !strings.Contains(text, `sanho hook pre-push "$@"`) {
		t.Fatalf("pre-push hook was not migrated:\n%s", text)
	}
	if !strings.Contains(text, "echo custom-before") || !strings.Contains(text, "echo custom-after") {
		t.Fatalf("custom hook content was not preserved:\n%s", text)
	}
	info, err := os.Stat(hookPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0740 {
		t.Fatalf("pre-push permissions=%o want 740", info.Mode().Perm())
	}
}

func TestHookInstaller_InstallHookMakesExistingHookExecutable(t *testing.T) {
	tempDir := t.TempDir()
	hooksDir := filepath.Join(tempDir, ".git", "hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		t.Fatal(err)
	}
	hookPath := filepath.Join(hooksDir, "pre-commit")
	content := "#!/bin/sh\necho custom\nsanho hook pre-commit\n"
	if err := os.WriteFile(hookPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	if err := NewHookInstaller().InstallHook(context.Background(), tempDir, "pre-commit", "sanho hook pre-commit"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != content {
		t.Fatalf("custom hook content changed:\n%s", data)
	}
	info, err := os.Stat(hookPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0744 {
		t.Fatalf("hook permissions=%o want 744", got)
	}
}

func TestHookInstaller_InstallHookUsesCommonHooksDirForLinkedWorktree(t *testing.T) {
	root := t.TempDir()
	mainRepo := filepath.Join(root, "main")
	linkedRepo := filepath.Join(root, "linked")
	runHookInstallerGit(t, root, "init", "--initial-branch=main", mainRepo)
	runHookInstallerGit(t, mainRepo, "config", "user.name", "Hook Test")
	runHookInstallerGit(t, mainRepo, "config", "user.email", "hook@example.com")
	if err := os.WriteFile(filepath.Join(mainRepo, "tracked"), []byte("tracked\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runHookInstallerGit(t, mainRepo, "add", "tracked")
	runHookInstallerGit(t, mainRepo, "commit", "-m", "initial")
	runHookInstallerGit(t, mainRepo, "worktree", "add", "-b", "linked", linkedRepo)

	if err := NewHookInstaller().InstallHook(context.Background(), linkedRepo, "pre-commit", "sanho hook pre-commit"); err != nil {
		t.Fatal(err)
	}
	commonHooks := runHookInstallerGit(t, linkedRepo, "rev-parse", "--git-path", "hooks")
	if !filepath.IsAbs(commonHooks) {
		commonHooks = filepath.Join(linkedRepo, commonHooks)
	}
	if _, err := os.Stat(filepath.Join(commonHooks, "pre-commit")); err != nil {
		t.Fatalf("common hooks directory did not receive hook: %v", err)
	}
	privateGitDir := runHookInstallerGit(t, linkedRepo, "rev-parse", "--git-dir")
	if !filepath.IsAbs(privateGitDir) {
		privateGitDir = filepath.Join(linkedRepo, privateGitDir)
	}
	if _, err := os.Stat(filepath.Join(privateGitDir, "hooks", "pre-commit")); !os.IsNotExist(err) {
		t.Fatalf("hook was written to linked-worktree private gitdir: %v", err)
	}
}

func runHookInstallerGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
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
		"sanho hook pre-commit",
		"echo also-keep",
		"",
	}, "\n")
	if err := os.WriteFile(hookPath, []byte(content), 0755); err != nil {
		t.Fatalf("failed to seed hook file: %v", err)
	}

	installer := NewHookInstaller()
	if err := installer.RemoveHookLine(context.Background(), tempDir, "pre-commit", "sanho hook pre-commit"); err != nil {
		t.Fatalf("RemoveHookLine returned error: %v", err)
	}

	data, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("failed to read hook file: %v", err)
	}
	text := string(data)
	if strings.Contains(text, "sanho hook pre-commit") {
		t.Fatalf("expected sanho line to be removed, content:\n%s", text)
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
	content := "sanho hook pre-commit\n"
	if err := os.WriteFile(hookPath, []byte(content), 0755); err != nil {
		t.Fatalf("failed to seed hook file: %v", err)
	}

	installer := NewHookInstaller()
	if err := installer.RemoveHookLine(context.Background(), tempDir, "pre-commit", "sanho hook pre-commit"); err != nil {
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
	if err := installer.RemoveHookLine(context.Background(), tempDir, "pre-commit", "sanho hook pre-commit"); err != nil {
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
	content := "sanho hook pre-commit\necho keep\n"
	if err := os.WriteFile(hookPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to seed hook file: %v", err)
	}

	installer := NewHookInstaller()
	if err := installer.RemoveHookLine(context.Background(), tempDir, "pre-commit", "sanho hook pre-commit"); err != nil {
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
	if strings.Contains(string(data), "sanho hook pre-commit") {
		t.Fatalf("expected sanho line removed, got:\n%s", string(data))
	}
	if !strings.Contains(string(data), "echo keep") {
		t.Fatalf("expected other content preserved, got:\n%s", string(data))
	}
}

// TestHookInstaller_InstallHook_WithExitAtEnd verifies that when an existing hook file
// has an exit command at the end, the sanho command is inserted before the exit.
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
	if err := installer.InstallHook(context.Background(), tempDir, "pre-commit", "sanho hook pre-commit"); err != nil {
		t.Fatalf("InstallHook returned error: %v", err)
	}

	data, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("failed to read hook file: %v", err)
	}
	text := string(data)

	// Verify sanho line is present
	if !strings.Contains(text, "sanho hook pre-commit") {
		t.Fatalf("expected sanho line to be present, content:\n%s", text)
	}

	// Verify sanho line comes BEFORE exit 0
	sanhoIdx := strings.Index(text, "sanho hook pre-commit")
	exitIdx := strings.Index(text, "exit 0")
	if sanhoIdx > exitIdx {
		t.Fatalf("expected sanho line before exit 0, content:\n%s", text)
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
	if err := installer.InstallHook(context.Background(), tempDir, "pre-commit", "sanho hook pre-commit"); err != nil {
		t.Fatalf("InstallHook returned error: %v", err)
	}

	data, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("failed to read hook file: %v", err)
	}
	text := string(data)

	sanhoIdx := strings.Index(text, "sanho hook pre-commit")
	exitIdx := strings.Index(text, "exit;")
	if sanhoIdx < 0 || exitIdx < 0 {
		t.Fatalf("expected both sanho and exit; lines, content:\n%s", text)
	}
	if sanhoIdx > exitIdx {
		t.Fatalf("expected sanho line before exit;, content:\n%s", text)
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
	if err := installer.InstallHook(context.Background(), tempDir, "pre-commit", "sanho hook pre-commit"); err != nil {
		t.Fatalf("InstallHook returned error: %v", err)
	}

	data, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("failed to read hook file: %v", err)
	}
	text := string(data)

	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	lastLine := lines[len(lines)-1]
	if lastLine != "sanho hook pre-commit" {
		t.Fatalf("expected sanho line appended at end, got last line: %q, full content:\n%s", lastLine, text)
	}
}

// TestHookInstaller_InstallHook_WithoutExit verifies that when an existing hook file
// has no exit command, the sanho command is appended at the end.
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
	if err := installer.InstallHook(context.Background(), tempDir, "pre-commit", "sanho hook pre-commit"); err != nil {
		t.Fatalf("InstallHook returned error: %v", err)
	}

	data, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("failed to read hook file: %v", err)
	}
	text := string(data)

	// Verify sanho line is present
	if !strings.Contains(text, "sanho hook pre-commit") {
		t.Fatalf("expected sanho line to be present, content:\n%s", text)
	}

	// Verify sanho line is at the end (after existing content)
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	lastLine := lines[len(lines)-1]
	if lastLine != "sanho hook pre-commit" {
		t.Fatalf("expected sanho line at end, got last line: %q, full content:\n%s", lastLine, text)
	}
}

// TestHookInstaller_InstallHook_NewFile verifies that when no hook file exists,
// a new file is created with shebang and the sanho command.
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
	if err := installer.InstallHook(context.Background(), tempDir, "pre-commit", "sanho hook pre-commit"); err != nil {
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

	// Verify sanho line is present
	if !strings.Contains(text, "sanho hook pre-commit") {
		t.Fatalf("expected sanho line to be present, content:\n%s", text)
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
