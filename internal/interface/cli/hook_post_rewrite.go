package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/irootkernel/sanho/internal/domain/client"
	"github.com/irootkernel/sanho/internal/infra/fs"
)

type gitRewriteMapping struct {
	Old string
	New string
}

// postRewriteReconciliationTimeout allows large rewrite mapping sets to be
// validated without extending the separate short status request budget.
const postRewriteReconciliationTimeout = 30 * time.Second

func runPostRewriteHook(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), postRewriteReconciliationTimeout)
	defer cancel()

	command := ""
	if len(args) > 0 {
		command = args[0]
	}
	workDir, err := os.Getwd()
	if err != nil {
		cmd.PrintErrf("sanho post-rewrite: warning: failed to get current directory: %v\n", err)
		return nil
	}
	rewriteInput := cmd.InOrStdin()
	source := captureGitRewriteSource(rewriteInput)
	mappings, err := readGitRewriteMappings(rewriteInput)
	if err != nil {
		cmd.PrintErrf("sanho post-rewrite: warning: failed to read rewrite mappings: %v\n", err)
		return nil
	}
	permit, operation, err := inspectPostRewriteMutation(ctx, workDir, command, mappings, source)
	if err != nil {
		cmd.PrintErrf(
			"sanho post-rewrite: warning: rewrite evidence could not be validated; Sanho mutation was skipped: %v\n",
			err,
		)
		if operation.Active {
			printMutationHookSkip(cmd, "post-rewrite", operation)
		}
		return nil
	}
	if operation.Active {
		printMutationHookSkip(cmd, "post-rewrite", operation)
		return nil
	}
	config, err := fs.NewFileConfigLoader().Load(workDir)
	if err != nil {
		if !errors.Is(err, fs.ErrConfigNotFound) {
			cmd.PrintErrf("sanho post-rewrite: warning: failed to load config: %v\n", err)
		}
		return nil
	}
	completed, err := newPullCommitEngine(nil).reconcileAfterRewriteWithPermit(
		ctx,
		workDir,
		config,
		command,
		mappings,
		permit,
	)
	if err != nil {
		cmd.PrintErrf("sanho post-rewrite: warning: pull-commit reconciliation skipped: %v\n", err)
	} else if completed {
		cmd.Println("sanho post-rewrite: reconciled completed pull-commit transaction.")
	}
	return runHookStatusWithPermit(cmd, "post-rewrite", true, permit)
}

func readGitRewriteMappings(reader io.Reader) ([]gitRewriteMapping, error) {
	scanner := bufio.NewScanner(reader)
	mappings := make([]gitRewriteMapping, 0)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			return nil, fmt.Errorf("invalid rewrite mapping %q", scanner.Text())
		}
		mappings = append(mappings, gitRewriteMapping{Old: fields[0], New: fields[1]})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return mappings, nil
}

func (e *pullCommitEngine) reconcileAfterRewrite(
	ctx context.Context,
	workDir string,
	config *client.WorkspaceConfig,
	command string,
	mappings []gitRewriteMapping,
) (bool, error) {
	permit, err := validatePostRewritePermit(ctx, workDir, command, mappings)
	if err != nil {
		return false, err
	}
	return e.reconcileAfterRewriteWithPermit(
		ctx,
		workDir,
		config,
		command,
		mappings,
		permit,
	)
}

func (e *pullCommitEngine) reconcileAfterRewriteWithPermit(
	ctx context.Context,
	workDir string,
	config *client.WorkspaceConfig,
	command string,
	mappings []gitRewriteMapping,
	permit workspaceMutationPermit,
) (bool, error) {
	if !permit.validatesPostRewrite(workDir, command, mappings) {
		return false, errors.New("post-rewrite mappings were not validated for this workspace")
	}
	if err := requireWorkspaceMutationSafeWithPermit(ctx, workDir, permit); err != nil {
		return false, err
	}
	head, err := e.workspaceSync.Head(ctx, workDir)
	if err != nil {
		return false, fmt.Errorf("revalidate rewritten HEAD: %w", err)
	}
	if head != permit.rewrittenHead {
		return false, fmt.Errorf("rewritten HEAD changed after mapping validation: got %s, want %s", head, permit.rewrittenHead)
	}
	store, err := e.store(ctx, workDir)
	if err != nil {
		return false, err
	}
	state, exists, err := store.Load()
	if err != nil || !exists {
		return false, err
	}
	if state.Phase == fs.PullCommitPhaseCompleted {
		return true, store.Remove()
	}
	if state.Phase != fs.PullCommitPhasePrepared {
		return false, nil
	}

	originalPrepared := pullCommitPreparedHead(state)
	preparedMapped := false
	knownRewrites := make(map[fs.PullCommitRewrite]struct{}, len(state.Rewrites)+len(mappings))
	for _, rewrite := range state.Rewrites {
		knownRewrites[rewrite] = struct{}{}
	}
	for _, mapping := range mappings {
		rewrite := fs.PullCommitRewrite{
			Command: command,
			Old:     mapping.Old,
			New:     mapping.New,
		}
		if _, exists := knownRewrites[rewrite]; !exists {
			state.Rewrites = append(state.Rewrites, rewrite)
			knownRewrites[rewrite] = struct{}{}
		}
		if state.SyncCommit == mapping.Old && state.SyncCommit != originalPrepared {
			valid, err := e.workspaceSync.IsDocsSyncCommit(
				ctx,
				workDir,
				mapping.New,
				"",
				config.DocsSyncCommitMessage,
				string(state.RemoteHash),
			)
			if err != nil {
				return false, err
			}
			if !valid {
				return false, fmt.Errorf("rewritten docs sync commit %s no longer has the expected identity", mapping.New)
			}
			state.SyncCommit = mapping.New
		}
		if pullCommitPreparedHead(state) == mapping.Old {
			state.PreparedHead = mapping.New
			preparedMapped = true
		}
	}

	if command != "amend" || !preparedMapped {
		return false, store.Save(state)
	}
	head, err = e.workspaceSync.Head(ctx, workDir)
	if err != nil {
		return false, err
	}
	descendant, err := e.workspaceSync.IsAncestor(ctx, workDir, state.PreparedHead, head)
	if err != nil {
		return false, err
	}
	if !descendant {
		return false, fmt.Errorf("amended prepared commit %s is not contained in current HEAD %s", state.PreparedHead, head)
	}
	if state.SyncCommit != originalPrepared {
		containsSync, err := e.workspaceSync.IsAncestor(ctx, workDir, state.SyncCommit, head)
		if err != nil {
			return false, err
		}
		if !containsSync {
			return false, fmt.Errorf("current HEAD %s does not contain docs sync commit %s", head, state.SyncCommit)
		}
	}
	if state.PreparedTree != "" {
		tree, err := e.workspaceSync.CommitTree(ctx, workDir, state.PreparedHead)
		if err != nil {
			return false, err
		}
		if tree != state.PreparedTree {
			return false, fmt.Errorf("amended commit tree %s does not match prepared index tree %s", tree, state.PreparedTree)
		}
	}
	assessment := pullCommitAssessment{State: state, Exists: true, Head: head}
	return true, e.completeTransaction(ctx, workDir, assessment, "post-rewrite-amend")
}
