package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/SeventeenthEarth/kkachi/internal/infra/fs"
	"github.com/SeventeenthEarth/kkachi/internal/infra/httpclient"
)

func runPostCommitHook(cmd *cobra.Command) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	workDir, err := os.Getwd()
	if err != nil {
		cmd.PrintErrf("kkachi-cli hook post-commit: %v\n", err)
		return nil
	}
	config, err := fs.NewFileConfigLoader().Load(workDir)
	if err != nil {
		return nil
	}
	engine := newPullCommitEngine(httpclient.NewHTTPClient(config.ServerURL))
	if err := engine.clearAfterCommit(ctx, workDir); err != nil {
		cmd.PrintErrf("kkachi-cli hook post-commit: failed to clear pull-commit state: %v\n", err)
	}
	hash, err := fs.NewFileDocsHashStore().Read(filepath.Join(workDir, config.DocsHashFile))
	if err != nil {
		cmd.PrintErrf("kkachi-cli hook post-commit: failed to read docs hash: %v\n", err)
		return nil
	}
	if err := reportWorkspaceDocsHash(ctx, workDir, config, hash); err != nil {
		cmd.PrintErrf("kkachi-cli hook post-commit: %v\n", fmt.Errorf("failed to report docs hash: %w", err))
	}
	return nil
}
