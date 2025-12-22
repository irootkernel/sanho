package pty

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestResolveCWD(t *testing.T) {
	baseDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(baseDir, "src", "components"), 0755); err != nil {
		t.Fatalf("Failed to create test directories: %v", err)
	}

	tests := []struct {
		name      string
		localPath string
		cwdRel    string
		want      string
		wantErr   error
	}{
		{
			name:      "Empty cwdRel defaults to workspace root",
			localPath: baseDir,
			cwdRel:    "",
			want:      baseDir,
			wantErr:   nil,
		},
		{
			name:      "Simple relative path",
			localPath: baseDir,
			cwdRel:    "src",
			want:      filepath.Join(baseDir, "src"),
			wantErr:   nil,
		},
		{
			name:      "Nested relative path",
			localPath: baseDir,
			cwdRel:    "src/components",
			want:      filepath.Join(baseDir, "src", "components"),
			wantErr:   nil,
		},
		{
			name:      "Dot path resolves to root",
			localPath: baseDir,
			cwdRel:    ".",
			want:      baseDir,
			wantErr:   nil,
		},
		{
			name:      "Traversal attempt blocked - simple",
			localPath: baseDir,
			cwdRel:    "..",
			want:      "",
			wantErr:   ErrCWDTraversal,
		},
		{
			name:      "Traversal attempt blocked - double",
			localPath: baseDir,
			cwdRel:    "../..",
			want:      "",
			wantErr:   ErrCWDTraversal,
		},
		{
			name:      "Traversal attempt blocked - mixed",
			localPath: baseDir,
			cwdRel:    "src/../../outside",
			want:      "",
			wantErr:   ErrCWDTraversal,
		},
		{
			name:      "Absolute path rejected - Unix",
			localPath: baseDir,
			cwdRel:    "/etc/passwd",
			want:      "",
			wantErr:   ErrAbsolutePathNotAllowed,
		},
	}

	// Add Windows-specific test
	if runtime.GOOS == "windows" {
		tests = append(tests, struct {
			name      string
			localPath string
			cwdRel    string
			want      string
			wantErr   error
		}{
			name:      "Absolute path rejected - Windows",
			localPath: baseDir,
			cwdRel:    "C:\\Windows\\System32",
			want:      "",
			wantErr:   ErrAbsolutePathNotAllowed,
		})
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveCWD(tt.localPath, tt.cwdRel)

			if err != tt.wantErr {
				t.Errorf("ResolveCWD() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr == nil {
				want := filepath.Clean(tt.want)
				want, err = filepath.EvalSymlinks(want)
				if err != nil {
					t.Fatalf("Failed to eval symlinks for want path: %v", err)
				}
				if got != want {
					t.Errorf("ResolveCWD() = %v, want %v", got, want)
				}
			}
		})
	}
}

func TestResolveCWD_SymlinkEscapeBlocked(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Symlink tests are unreliable on Windows without elevated privileges")
	}

	baseDir := t.TempDir()
	workspaceDir := filepath.Join(baseDir, "workspace")
	outsideDir := filepath.Join(baseDir, "outside")
	if err := os.MkdirAll(workspaceDir, 0755); err != nil {
		t.Fatalf("Failed to create workspace dir: %v", err)
	}
	if err := os.MkdirAll(outsideDir, 0755); err != nil {
		t.Fatalf("Failed to create outside dir: %v", err)
	}

	linkPath := filepath.Join(workspaceDir, "link")
	if err := os.Symlink(outsideDir, linkPath); err != nil {
		t.Skipf("Symlink not available: %v", err)
	}

	_, err := ResolveCWD(workspaceDir, "link")
	if !errors.Is(err, ErrCWDTraversal) {
		t.Fatalf("Expected ErrCWDTraversal for symlink escape, got %v", err)
	}
}

func TestValidateShell(t *testing.T) {
	tests := []struct {
		name          string
		shell         string
		allowedShells []string
		wantErr       error
	}{
		{
			name:          "Allowed - in list",
			shell:         "/bin/bash",
			allowedShells: []string{"/bin/sh", "/bin/bash", "/bin/zsh"},
			wantErr:       nil,
		},
		{
			name:          "Allowed - empty allowlist (no restriction)",
			shell:         "/usr/local/bin/fish",
			allowedShells: []string{},
			wantErr:       nil,
		},
		{
			name:          "Not allowed - not in list",
			shell:         "/bin/fish",
			allowedShells: []string{"/bin/sh", "/bin/bash"},
			wantErr:       ErrShellNotAllowed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateShell(tt.shell, tt.allowedShells)
			if err != tt.wantErr {
				t.Errorf("ValidateShell() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
