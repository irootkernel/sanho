package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/irootkernel/sanho/internal/domain/client"
	"github.com/irootkernel/sanho/internal/infra/fs"
)

type gitRewriteMapping struct {
	Old string
	New string
}

func runPostRewriteHook(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), hookStatusTimeout)
	defer cancel()

	command := ""
	if len(args) > 0 {
		command = args[0]
	}
	mappings, err := readGitRewriteMappings(cmd.InOrStdin())
	if err != nil {
		cmd.PrintErrf("sanho post-rewrite: warning: failed to read rewrite mappings: %v\n", err)
		return nil
	}
	workDir, err := os.Getwd()
	if err != nil {
		cmd.PrintErrf("sanho post-rewrite: warning: failed to get current directory: %v\n", err)
		return nil
	}
	config, err := fs.NewFileConfigLoader().Load(workDir)
	if err != nil {
		if !errors.Is(err, fs.ErrConfigNotFound) {
			cmd.PrintErrf("sanho post-rewrite: warning: failed to load config: %v\n", err)
		}
		return nil
	}
	completed, err := newPullCommitEngine(nil).reconcileAfterRewrite(
		ctx,
		workDir,
		config,
		command,
		mappings,
	)
	if err != nil {
		cmd.PrintErrf("sanho post-rewrite: warning: pull-commit reconciliation skipped: %v\n", err)
	} else if completed {
		cmd.Println("sanho post-rewrite: reconciled completed pull-commit transaction.")
	}
	return runHookStatus(cmd, "post-rewrite", true)
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
	for _, mapping := range mappings {
		if _, err := e.workspaceSync.CommitTree(ctx, workDir, mapping.New); err != nil {
			return false, fmt.Errorf("invalid rewritten commit %s: %w", mapping.New, err)
		}
		state.Rewrites = appendPullCommitRewrite(state.Rewrites, fs.PullCommitRewrite{
			Command: command,
			Old:     mapping.Old,
			New:     mapping.New,
		})
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
	head, err := e.workspaceSync.Head(ctx, workDir)
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

func appendPullCommitRewrite(
	rewrites []fs.PullCommitRewrite,
	rewrite fs.PullCommitRewrite,
) []fs.PullCommitRewrite {
	for _, existing := range rewrites {
		if existing == rewrite {
			return rewrites
		}
	}
	return append(rewrites, rewrite)
}
