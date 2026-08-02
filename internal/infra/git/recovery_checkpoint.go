package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// RecoveryCheckpoint identifies refs that preserve HEAD, the index, and the
// working tree before pull-commit recovery changes transaction metadata.
type RecoveryCheckpoint struct {
	HeadRef     string
	IndexRef    string
	WorktreeRef string
}

// CreateRecoveryCheckpoint creates an idempotent set of backup refs. Ignored
// files are not captured because recovery never modifies them.
func (s *WorkspaceSync) CreateRecoveryCheckpoint(
	ctx context.Context,
	repoPath, transactionID string,
) (RecoveryCheckpoint, error) {
	if !validRecoveryTransactionID(transactionID) {
		return RecoveryCheckpoint{}, errors.New("invalid recovery transaction id")
	}
	prefix := "refs/sanho/recovery/" + transactionID
	checkpoint := RecoveryCheckpoint{
		HeadRef:     prefix + "/head",
		IndexRef:    prefix + "/index",
		WorktreeRef: prefix + "/worktree",
	}
	refs := []string{checkpoint.HeadRef, checkpoint.IndexRef, checkpoint.WorktreeRef}
	allExist := true
	for _, ref := range refs {
		if _, exists, err := resolveOptionalRef(ctx, repoPath, ref); err != nil {
			return RecoveryCheckpoint{}, err
		} else if !exists {
			allExist = false
		}
	}
	if allExist {
		return checkpoint, nil
	}

	head, err := s.Head(ctx, repoPath)
	if err != nil {
		return RecoveryCheckpoint{}, err
	}
	indexTree, worktreeTree, err := captureRecoveryTrees(ctx, repoPath)
	if err != nil {
		return RecoveryCheckpoint{}, err
	}
	indexCommit, err := createSyntheticCommit(ctx, repoPath, indexTree, head, "sanho recovery index")
	if err != nil {
		return RecoveryCheckpoint{}, err
	}
	worktreeCommit, err := createSyntheticCommit(ctx, repoPath, worktreeTree, indexCommit, "sanho recovery worktree")
	if err != nil {
		return RecoveryCheckpoint{}, err
	}

	var input strings.Builder
	input.WriteString("start\n")
	fmt.Fprintf(&input, "create %s %s\n", checkpoint.HeadRef, head)
	fmt.Fprintf(&input, "create %s %s\n", checkpoint.IndexRef, indexCommit)
	fmt.Fprintf(&input, "create %s %s\n", checkpoint.WorktreeRef, worktreeCommit)
	input.WriteString("prepare\ncommit\n")
	if _, err := runWorkspaceGitWithInput(ctx, repoPath, nil, input.String(), "update-ref", "--stdin"); err != nil {
		return RecoveryCheckpoint{}, fmt.Errorf("create recovery refs: %w", err)
	}
	return checkpoint, nil
}

func validRecoveryTransactionID(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if !unicode.IsLetter(char) && !unicode.IsDigit(char) && char != '-' && char != '_' {
			return false
		}
	}
	return true
}

func captureRecoveryTrees(ctx context.Context, repoPath string) (string, string, error) {
	out, err := runWorkspaceGit(ctx, repoPath, nil, "write-tree")
	if err != nil {
		return "", "", fmt.Errorf("capture recovery index tree: %w", err)
	}
	indexTree := strings.TrimSpace(string(out))

	tempDir, err := os.MkdirTemp("", "sanho-recovery-*")
	if err != nil {
		return "", "", err
	}
	defer func() { _ = os.RemoveAll(tempDir) }()
	tempIndex := filepath.Join(tempDir, "index")
	env := map[string]string{
		"GIT_INDEX_FILE": tempIndex,
		"GIT_WORK_TREE":  repoPath,
	}
	if _, err := runWorkspaceGit(ctx, repoPath, env, "read-tree", indexTree); err != nil {
		return "", "", fmt.Errorf("initialize recovery worktree index: %w", err)
	}
	if _, err := runWorkspaceGit(ctx, repoPath, env, "add", "-A"); err != nil {
		return "", "", fmt.Errorf("capture recovery worktree: %w", err)
	}
	out, err = runWorkspaceGit(ctx, repoPath, env, "write-tree")
	if err != nil {
		return "", "", fmt.Errorf("write recovery worktree tree: %w", err)
	}
	return indexTree, strings.TrimSpace(string(out)), nil
}
