package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	DefaultHomeDirName   = ".sanho"
	DefaultStateFileName = "state.json"
	DefaultSocketName    = "sanhod.sock"
	DocsReposDirName     = "docs_repos"
)

// RuntimePaths contains all daemon-owned filesystem paths.
type RuntimePaths struct {
	HomeDir      string
	StatePath    string
	DocsReposDir string
	SocketPath   string
}

// ResolveRuntimePaths resolves absolute daemon paths without creating them.
func ResolveRuntimePaths(homeOverride, socketOverride string) (RuntimePaths, error) {
	homeDir := homeOverride
	if homeDir == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return RuntimePaths{}, fmt.Errorf("resolve user home: %w", err)
		}
		homeDir = filepath.Join(userHome, DefaultHomeDirName)
	}
	if !filepath.IsAbs(homeDir) {
		return RuntimePaths{}, errors.New("sanho home must be an absolute path")
	}
	homeDir = filepath.Clean(homeDir)

	socketPath := socketOverride
	if socketPath == "" {
		socketPath = filepath.Join(homeDir, DefaultSocketName)
	}
	if !filepath.IsAbs(socketPath) {
		return RuntimePaths{}, errors.New("sanho socket path must be an absolute path")
	}
	socketPath = filepath.Clean(socketPath)

	return RuntimePaths{
		HomeDir:      homeDir,
		StatePath:    filepath.Join(homeDir, DefaultStateFileName),
		DocsReposDir: filepath.Join(homeDir, DocsReposDirName),
		SocketPath:   socketPath,
	}, nil
}

// PrepareRuntime creates daemon-owned directories with private permissions.
func PrepareRuntime(paths RuntimePaths) error {
	for _, dir := range []string{paths.HomeDir, paths.DocsReposDir, filepath.Dir(paths.SocketPath)} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return fmt.Errorf("create runtime directory %s: %w", dir, err)
		}
	}
	if err := os.Chmod(paths.HomeDir, 0700); err != nil {
		return fmt.Errorf("secure runtime home: %w", err)
	}
	if err := os.Chmod(paths.DocsReposDir, 0700); err != nil {
		return fmt.Errorf("secure docs repository directory: %w", err)
	}
	return nil
}
