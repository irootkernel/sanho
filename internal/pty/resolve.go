package pty

import (
	"path/filepath"
	"strings"
)

// ResolveCWD resolves a relative CWD path against a workspace local path.
// It prevents path traversal attacks by ensuring the result stays within the workspace.
//
// Parameters:
//   - localPath: The workspace's local filesystem path (must be absolute)
//   - cwdRel: The relative path within the workspace (can be empty for workspace root)
//
// Returns the resolved absolute path or an error if:
//   - cwdRel is an absolute path
//   - cwdRel attempts to escape the workspace via ".."
func ResolveCWD(localPath, cwdRel string) (string, error) {
	// Reject absolute paths in cwdRel
	if filepath.IsAbs(cwdRel) {
		return "", ErrAbsolutePathNotAllowed
	}

	// Handle Windows-style drive letters (e.g., "C:...")
	if len(cwdRel) >= 2 && cwdRel[1] == ':' {
		return "", ErrAbsolutePathNotAllowed
	}

	// Default to "." if cwdRel is empty
	if cwdRel == "" {
		cwdRel = "."
	}

	// Clean both paths
	cleanLocal := filepath.Clean(localPath)
	resolved := filepath.Clean(filepath.Join(cleanLocal, cwdRel))

	// Ensure resolved path is within localPath (prevent traversal via "..")
	// Add trailing separator for proper prefix matching
	if !strings.HasPrefix(resolved, cleanLocal+string(filepath.Separator)) && resolved != cleanLocal {
		return "", ErrCWDTraversal
	}

	// Resolve symlinks and ensure resolved path stays within workspace root.
	evalLocal, err := filepath.EvalSymlinks(cleanLocal)
	if err != nil {
		return "", err
	}
	evalResolved, err := filepath.EvalSymlinks(resolved)
	if err != nil {
		return "", err
	}

	if !strings.HasPrefix(evalResolved, evalLocal+string(filepath.Separator)) && evalResolved != evalLocal {
		return "", ErrCWDTraversal
	}

	return evalResolved, nil
}

// ValidateShell checks if the requested shell is in the allowed shells list.
// Returns nil if allowed, ErrShellNotAllowed otherwise.
func ValidateShell(shell string, allowedShells []string) error {
	if len(allowedShells) == 0 {
		// No restriction if allowlist is empty
		return nil
	}

	for _, allowed := range allowedShells {
		if shell == allowed {
			return nil
		}
	}

	return ErrShellNotAllowed
}
