package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/irootkernel/sanho/internal/infra/fs"
)

func runPostCommitHook(cmd *cobra.Command) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	workDir, err := os.Getwd()
	if err != nil {
		cmd.PrintErrf("sanho hook post-commit: %v\n", err)
		return nil
	}
	config, err := fs.NewFileConfigLoader().Load(workDir)
	if err != nil {
		return nil
	}
	httpClient, err := newDaemonClient(config.SocketPath)
	if err != nil {
		cmd.PrintErrf("sanho hook post-commit: invalid socket path: %v\n", err)
		return nil
	}
	engine := newPullCommitEngine(httpClient)
	if err := engine.clearAfterCommit(ctx, workDir); err != nil {
		cmd.PrintErrf("sanho hook post-commit: failed to clear pull-commit state: %v\n", err)
	}
	hash, err := fs.NewFileDocsHashStore().Read(filepath.Join(workDir, config.DocsHashFile))
	if err != nil {
		cmd.PrintErrf("sanho hook post-commit: failed to read docs hash: %v\n", err)
		return nil
	}
	if err := reportWorkspaceDocsHash(ctx, workDir, config, hash); err != nil {
		cmd.PrintErrf("sanho hook post-commit: %v\n", fmt.Errorf("failed to report docs hash: %w", err))
	}
	return nil
}
