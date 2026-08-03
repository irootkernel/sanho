package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	infraGit "github.com/irootkernel/sanho/internal/infra/git"
)

func requireWorkspaceMutationSafe(ctx context.Context, workDir string) error {
	detector := infraGit.NewDetector()
	if !detector.HasGitDir(workDir) {
		return nil
	}
	err := detector.RequireNoOperation(ctx, workDir)
	if err == nil {
		return nil
	}
	var blocked *infraGit.GitOperationBlockedError
	if !errors.As(err, &blocked) {
		return err
	}
	return &workspaceMutationBlockedError{
		cause:     err,
		operation: blocked.Operation,
	}
}

type workspaceMutationBlockedError struct {
	cause     error
	operation infraGit.GitOperation
}

func (e *workspaceMutationBlockedError) Error() string {
	var message strings.Builder
	message.WriteString(e.operation.Reason)
	message.WriteString(". Inspect 'git status' before choosing a recovery command.")
	for _, command := range e.operation.NextCommands {
		message.WriteString("\n  ")
		message.WriteString(command)
	}
	if e.operation.Type == infraGit.OperationRebase {
		message.WriteString("\n'git rebase --abort' restores the pre-rebase state; ")
		message.WriteString("'git rebase --quit' keeps the current HEAD, index, and working tree.")
	}
	return message.String()
}

func (e *workspaceMutationBlockedError) Unwrap() error {
	return e.cause
}

// skipMutationHookDuringGitOperation preserves user-owned operation state and
// lets Git continue. Pre-push deliberately uses requireWorkspaceMutationSafe.
func skipMutationHookDuringGitOperation(
	ctx context.Context,
	workDir, hookName string,
	cmd *cobra.Command,
) bool {
	detector := infraGit.NewDetector()
	if !detector.HasGitDir(workDir) {
		return false
	}
	operation, err := detector.DetectOperation(ctx, workDir)
	if err != nil {
		cmd.PrintErrf(
			"sanho hook %s: warning: Git operation state could not be inspected; Sanho mutation was skipped: %v\n",
			hookName,
			err,
		)
		return true
	}
	if !operation.Active {
		return false
	}
	cmd.PrintErrf(
		"sanho hook %s: warning: %s. Sanho mutation was skipped so Git recovery can continue.\n",
		hookName,
		operation.Reason,
	)
	cmd.PrintErrln("Inspect 'git status' before choosing a recovery command.")
	for _, command := range operation.NextCommands {
		cmd.PrintErrf("  %s\n", command)
	}
	if operation.Type == infraGit.OperationRebase {
		cmd.PrintErrln("  --abort restores the pre-rebase state; --quit keeps the current HEAD, index, and working tree.")
	}
	return true
}

func wrapGitOperationGuard(command string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", command, err)
}
