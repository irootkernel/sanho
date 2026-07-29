package cli

import (
	"context"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/irootkernel/sanho/internal/infra/fs"
	infraGit "github.com/irootkernel/sanho/internal/infra/git"
	"github.com/irootkernel/sanho/internal/usecase/hook"
)

// commitMsgTimeout is the timeout for commit-msg operations.
const commitMsgTimeout = 10 * time.Second

// runCommitMsgHook executes the commit-msg hook logic.
func runCommitMsgHook(cmd *cobra.Command, args []string) error {
	// Git passes the message file path as the first argument
	if len(args) == 0 {
		cmd.PrintErrln("sanho hook commit-msg: message file path not provided")
		return nil // Don't block commit
	}
	msgFilePath := args[0]

	ctx, cancel := context.WithTimeout(context.Background(), commitMsgTimeout)
	defer cancel()

	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		cmd.PrintErrf("sanho hook commit-msg: failed to get current directory: %v\n", err)
		return nil // Don't block commit
	}

	// Create dependencies
	configLoader := fs.NewFileConfigLoader()
	docsHashStore := fs.NewFileDocsHashStore()
	gitClient := infraGit.NewClient()
	output := newCLICommitMsgOutput(cmd)

	// Create usecase
	usecase := hook.NewCommitMsgUseCase(
		configLoader,
		docsHashStore,
		newCommitMsgGitClientAdapter(gitClient),
		output,
	)

	// Execute - commit-msg hook should never block commit
	// Note: CommitMsgUseCase.Execute always returns nil (handles errors internally with warnings)
	_ = usecase.Execute(ctx, cwd, msgFilePath)

	return nil // Always return nil to not block commit
}

// cliCommitMsgOutput implements hook.CommitMsgOutput for CLI.
type cliCommitMsgOutput struct {
	cmd *cobra.Command
}

func newCLICommitMsgOutput(cmd *cobra.Command) *cliCommitMsgOutput {
	return &cliCommitMsgOutput{cmd: cmd}
}

func (o *cliCommitMsgOutput) Info(msg string) {
	o.cmd.Printf("sanho: %s\n", msg)
}

func (o *cliCommitMsgOutput) Warning(msg string) {
	// Print warnings to stderr but don't block
	o.cmd.PrintErrf("sanho: %s\n", msg)
}

// commitMsgGitClientAdapter adapts git.Client to hook.CommitMsgGitClient interface.
type commitMsgGitClientAdapter struct {
	client *infraGit.Client
}

func newCommitMsgGitClientAdapter(client *infraGit.Client) *commitMsgGitClientAdapter {
	return &commitMsgGitClientAdapter{client: client}
}

func (a *commitMsgGitClientAdapter) HasDocsChangeStaged(ctx context.Context, repoPath, docsDir string) (bool, error) {
	return a.client.HasDocsChangeStaged(ctx, repoPath, docsDir)
}
