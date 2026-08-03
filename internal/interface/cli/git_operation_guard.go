package cli

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	infraGit "github.com/irootkernel/sanho/internal/infra/git"
)

type workspaceMutationPermit struct {
	verifiedRebasePostRewrite bool
	workDir                   string
	rewrittenHead             string
}

func requireWorkspaceMutationSafe(ctx context.Context, workDir string) error {
	return requireWorkspaceMutationSafeWithPermit(ctx, workDir, workspaceMutationPermit{})
}

func requireWorkspaceMutationSafeWithPermit(
	ctx context.Context,
	workDir string,
	permit workspaceMutationPermit,
) error {
	detector := infraGit.NewDetector()
	if !detector.HasGitDir(workDir) {
		return nil
	}
	operation, err := detector.DetectOperation(ctx, workDir)
	if err != nil {
		return err
	}
	if !operation.Active {
		return nil
	}
	if permit.allows(workDir, operation) {
		head, err := infraGit.NewWorkspaceSync(nil, nil).Head(ctx, workDir)
		if err != nil {
			return fmt.Errorf("revalidate rewritten HEAD: %w", err)
		}
		if head == permit.rewrittenHead {
			return nil
		}
	}
	blocked := &infraGit.GitOperationBlockedError{Operation: operation}
	return &workspaceMutationBlockedError{
		cause:     blocked,
		operation: operation,
	}
}

func (p workspaceMutationPermit) allows(workDir string, operation infraGit.GitOperation) bool {
	return p.verifiedRebasePostRewrite &&
		filepath.Clean(p.workDir) == filepath.Clean(workDir) &&
		operation.Type == infraGit.OperationRebase
}

func inspectPostRewriteMutation(
	ctx context.Context,
	workDir, command string,
	mappings []gitRewriteMapping,
) (workspaceMutationPermit, infraGit.GitOperation, error) {
	detector := infraGit.NewDetector()
	clear := infraGit.GitOperation{
		Type:           infraGit.OperationNone,
		Classification: infraGit.OperationClear,
		NextCommands:   make([]string, 0),
	}
	if !detector.HasGitDir(workDir) {
		return workspaceMutationPermit{}, clear, nil
	}
	operation, err := detector.DetectOperation(ctx, workDir)
	if err != nil {
		return workspaceMutationPermit{}, clear, err
	}
	if !operation.Active {
		return workspaceMutationPermit{}, operation, nil
	}
	if operation.Type != infraGit.OperationRebase || command != "rebase" || len(mappings) == 0 {
		return workspaceMutationPermit{}, operation, nil
	}
	rewrittenHead, err := validatePostRewriteMappings(ctx, workDir, mappings)
	if err != nil {
		return workspaceMutationPermit{}, operation, err
	}
	return workspaceMutationPermit{
		verifiedRebasePostRewrite: true,
		workDir:                   workDir,
		rewrittenHead:             rewrittenHead,
	}, operation, nil
}

func validatePostRewriteMappings(
	ctx context.Context,
	workDir string,
	mappings []gitRewriteMapping,
) (string, error) {
	var input strings.Builder
	for _, mapping := range mappings {
		if mapping.Old == "" || mapping.New == "" {
			return "", errors.New("rewrite mapping contains an empty object name")
		}
		input.WriteString(mapping.Old)
		input.WriteByte('\n')
		input.WriteString(mapping.New)
		input.WriteByte('\n')
	}
	cmd := exec.CommandContext(
		ctx,
		"git",
		"-C",
		workDir,
		"cat-file",
		"--batch-check=%(objectname) %(objecttype)",
	)
	cmd.Stdin = strings.NewReader(input.String())
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("validate rewrite commit objects: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != len(mappings)*2 {
		return "", fmt.Errorf("validate rewrite commit objects: got %d results, want %d", len(lines), len(mappings)*2)
	}
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[1] != "commit" {
			return "", fmt.Errorf("rewrite object is not a commit: %s", line)
		}
	}

	syncer := infraGit.NewWorkspaceSync(nil, nil)
	head, err := syncer.Head(ctx, workDir)
	if err != nil {
		return "", fmt.Errorf("resolve rewritten HEAD: %w", err)
	}
	checked := make(map[string]struct{}, len(mappings))
	for _, mapping := range mappings {
		if mapping.New == head {
			continue
		}
		if _, ok := checked[mapping.New]; ok {
			continue
		}
		checked[mapping.New] = struct{}{}
		reachable, err := syncer.IsAncestor(ctx, workDir, mapping.New, head)
		if err != nil {
			return "", fmt.Errorf("validate rewritten commit reachability: %w", err)
		}
		if !reachable {
			return "", fmt.Errorf("rewritten commit %s is not reachable from HEAD %s", mapping.New, head)
		}
	}
	return head, nil
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
	printMutationHookSkip(cmd, hookName, operation)
	return true
}

func printMutationHookSkip(
	cmd *cobra.Command,
	hookName string,
	operation infraGit.GitOperation,
) {
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
}

func wrapGitOperationGuard(command string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", command, err)
}
