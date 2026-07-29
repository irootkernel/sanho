package cli

import (
	"path/filepath"
	"testing"
)

func TestResolveDocsPath_AllowsRelativeWithinWorkspace(t *testing.T) {
	cwd := t.TempDir()

	got, err := resolveDocsPath(cwd, "docs")
	if err != nil {
		t.Fatalf("resolveDocsPath returned error: %v", err)
	}

	want := filepath.Join(cwd, "docs")
	if got != want {
		t.Errorf("resolveDocsPath() = %q, want %q", got, want)
	}
}

func TestResolveDocsPath_RejectsAbsoluteDocsDir(t *testing.T) {
	cwd := t.TempDir()
	absDocs := t.TempDir() // guaranteed absolute path

	if _, err := resolveDocsPath(cwd, absDocs); err == nil {
		t.Fatalf("resolveDocsPath() with absolute docsDir %q expected error, got nil", absDocs)
	}
}

func TestResolveDocsPath_RejectsEscapingDocsDir(t *testing.T) {
	cwd := t.TempDir()

	if _, err := resolveDocsPath(cwd, "../shared"); err == nil {
		t.Fatalf("resolveDocsPath() with escaping docsDir '../shared' expected error, got nil")
	}
}

func TestResolveSocketPathPrecedence(t *testing.T) {
	previousFlag := socketPathFlag
	t.Cleanup(func() { socketPathFlag = previousFlag })
	t.Setenv("SANHO_HOME", filepath.Join(t.TempDir(), "home"))
	t.Setenv("SANHO_SOCKET", filepath.Join(t.TempDir(), "environment.sock"))

	configured := filepath.Join(t.TempDir(), "workspace.sock")
	got, err := resolveSocketPath(configured)
	if err != nil {
		t.Fatalf("resolveSocketPath() error = %v", err)
	}
	if got != configured {
		t.Fatalf("configured path = %q, want %q", got, configured)
	}

	override := filepath.Join(t.TempDir(), "flag.sock")
	socketPathFlag = override
	got, err = resolveSocketPath(configured)
	if err != nil {
		t.Fatalf("resolveSocketPath() with flag error = %v", err)
	}
	if got != override {
		t.Fatalf("flag path = %q, want %q", got, override)
	}
}

func TestResolveSocketPathUsesEnvironmentDefaults(t *testing.T) {
	previousFlag := socketPathFlag
	socketPathFlag = ""
	t.Cleanup(func() { socketPathFlag = previousFlag })

	home := filepath.Join(t.TempDir(), "home")
	t.Setenv("SANHO_HOME", home)
	t.Setenv("SANHO_SOCKET", "")
	got, err := resolveSocketPath("")
	if err != nil {
		t.Fatalf("resolveSocketPath() error = %v", err)
	}
	if want := filepath.Join(home, "sanhod.sock"); got != want {
		t.Fatalf("default path = %q, want %q", got, want)
	}
}

func TestResolveSocketPathRejectsRelativePath(t *testing.T) {
	previousFlag := socketPathFlag
	socketPathFlag = ""
	t.Cleanup(func() { socketPathFlag = previousFlag })

	if _, err := resolveSocketPath("relative.sock"); err == nil {
		t.Fatal("resolveSocketPath() error = nil, want relative path rejection")
	}
}
