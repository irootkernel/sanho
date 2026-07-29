package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveRuntimePaths(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")

	paths, err := ResolveRuntimePaths(home, "")
	if err != nil {
		t.Fatalf("ResolveRuntimePaths() error = %v", err)
	}
	if paths.HomeDir != home {
		t.Fatalf("HomeDir = %q, want %q", paths.HomeDir, home)
	}
	if paths.StatePath != filepath.Join(home, "state.json") {
		t.Fatalf("StatePath = %q", paths.StatePath)
	}
	if paths.DocsReposDir != filepath.Join(home, "docs_repos") {
		t.Fatalf("DocsReposDir = %q", paths.DocsReposDir)
	}
	if paths.SocketPath != filepath.Join(home, "sanhod.sock") {
		t.Fatalf("SocketPath = %q", paths.SocketPath)
	}
}

func TestResolveRuntimePathsRejectsRelativeOverrides(t *testing.T) {
	if _, err := ResolveRuntimePaths("relative", ""); err == nil {
		t.Fatal("expected relative home to fail")
	}

	home := t.TempDir()
	if _, err := ResolveRuntimePaths(home, "relative.sock"); err == nil {
		t.Fatal("expected relative socket path to fail")
	}
}

func TestPrepareRuntimeCreatesPrivateDirectories(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	socketDir := filepath.Join(t.TempDir(), "socket")
	paths, err := ResolveRuntimePaths(home, filepath.Join(socketDir, "sanhod.sock"))
	if err != nil {
		t.Fatalf("ResolveRuntimePaths() error = %v", err)
	}
	if err := PrepareRuntime(paths); err != nil {
		t.Fatalf("PrepareRuntime() error = %v", err)
	}

	for _, dir := range []string{paths.HomeDir, paths.DocsReposDir, socketDir} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("stat %s: %v", dir, err)
		}
		if !info.IsDir() {
			t.Fatalf("%s is not a directory", dir)
		}
	}
	for _, dir := range []string{paths.HomeDir, paths.DocsReposDir, socketDir} {
		info, _ := os.Stat(dir)
		if got := info.Mode().Perm(); got != 0700 {
			t.Fatalf("%s mode = %o, want 700", dir, got)
		}
	}
}
