package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/irootkernel/sanho/internal/infra/appgit"
	"github.com/irootkernel/sanho/internal/infra/gitx"
	"github.com/irootkernel/sanho/internal/infra/wsstate"
	"github.com/irootkernel/sanho/internal/usecase/docsync"
)

type inspectionWorkspace struct {
	repo   docsync.CompletionReader
	gitDir string
}

// Inspection establishes its Git policy before discovery, including discovery
// of a linked worktree's configuration. It needs no home, registry or clone.
func openInspectionWorkspace(ctx context.Context) (*inspectionWorkspace, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("resolve the current directory: %w", err)
	}
	root, err := canonicalFilesystemPath(cwd)
	if err != nil {
		return nil, err
	}
	run, err := gitx.NewInspection(ctx, root)
	if err != nil {
		return nil, err
	}
	configRoot, err := inspectionConfigRoot(ctx, root, run)
	if err != nil {
		return nil, err
	}
	cfg, err := wsstate.LoadConfig(configRoot)
	if err != nil {
		return nil, err
	}
	cfg.ApplyDefaults()
	if cfg.SchemaVersion == 1 {
		return nil, errV1Workspace
	}
	res, err := run.Run(ctx, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return nil, fmt.Errorf("resolve the worktree Git directory: %w", err)
	}
	// A path may end in whitespace; only Git's final record delimiter is removed.
	gitDir, err := canonicalFilesystemPath(strings.TrimSuffix(string(res.Stdout), "\n"))
	if err != nil {
		return nil, err
	}
	repo, err := appgit.NewInspection(ctx, root, cfg.DocsDir)
	if err != nil {
		return nil, err
	}
	return &inspectionWorkspace{gitDir: gitDir, repo: repo}, nil
}

func inspectionConfigRoot(ctx context.Context, root string, run *gitx.Runner) (string, error) {
	found, err := hasConfig(root)
	if err != nil || found {
		return root, err
	}
	// Only checkout roots can borrow the main worktree's config. Linked
	// worktrees have a .git file; an absent entry is a normal workspace refusal.
	if _, err := os.Lstat(filepath.Join(root, ".git")); os.IsNotExist(err) {
		return "", errNotWorkspace
	} else if err != nil {
		return "", fmt.Errorf("read checkout Git entry: %w", err)
	}
	res, err := run.Run(ctx, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return "", fmt.Errorf("locate workspace configuration: %w", err)
	}
	var roots []string
	for _, field := range strings.Split(string(res.Stdout), "\x00") {
		if name, ok := strings.CutPrefix(field, "worktree "); ok {
			roots = append(roots, filepath.Clean(name))
		}
	}
	if len(roots) == 0 || !containsPath(roots, root) || sameFilesystemPath(roots[0], root) {
		return "", errNotWorkspace
	}
	main, err := canonicalFilesystemPath(roots[0])
	if err != nil {
		return "", err
	}
	found, err = hasConfig(main)
	if err != nil {
		return "", err
	}
	if !found {
		return "", errNotWorkspace
	}
	return main, nil
}
