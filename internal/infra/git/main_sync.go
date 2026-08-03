package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var (
	ErrMainDiverged           = errors.New("local main has diverged from origin/main")
	ErrPublishedFeature       = errors.New("published feature branch cannot be rewritten")
	ErrNonLinearFeature       = errors.New("feature branch contains merge commits")
	ErrMainSyncRebaseConflict = errors.New("main-based docs sync conflicts with local changes")
)

type MainSyncResult struct {
	Branch       string
	OriginalMain string
	MainBase     string
	SyncCommit   string
	PreparedHead string
}

// CreateMainBasedDocsSyncCommit creates the system docs commit on the latest
// acceptable main, rebases an unpublished linear feature branch when needed,
// and preserves the caller's staged and unstaged layers.
func (s *WorkspaceSync) CreateMainBasedDocsSyncCommit(
	ctx context.Context,
	repoPath, docsDir string,
	snapshot []byte,
	subject, docsHash string,
) (MainSyncResult, error) {
	if err := requireWorkspaceMutationSafe(ctx, repoPath); err != nil {
		return MainSyncResult{}, err
	}
	branchRef, err := symbolicHead(ctx, repoPath)
	if err != nil {
		return MainSyncResult{}, err
	}
	branch := strings.TrimPrefix(branchRef, "refs/heads/")
	currentHead, err := s.Head(ctx, repoPath)
	if err != nil {
		return MainSyncResult{}, err
	}
	originalMain, exists, err := resolveOptionalRef(ctx, repoPath, "refs/heads/main")
	if err != nil {
		return MainSyncResult{}, err
	}
	if !exists {
		return MainSyncResult{}, errors.New("local main branch does not exist")
	}

	mainBase, err := s.latestMain(ctx, repoPath, originalMain)
	if err != nil {
		return MainSyncResult{}, err
	}
	if branch != "main" {
		if err := ensureUnpublishedLinearFeature(ctx, repoPath, branchRef, branch, currentHead, mainBase); err != nil {
			return MainSyncResult{}, err
		}
	}

	syncCommit, err := s.buildDocsSyncCommit(
		ctx,
		repoPath,
		mainBase,
		docsDir,
		snapshot,
		subject,
		docsHash,
	)
	if err != nil {
		return MainSyncResult{}, err
	}

	originalStagedTree, originalWorkTree, workCommit, err := captureDirtyLayers(
		ctx,
		repoPath,
		currentHead,
		docsDir,
	)
	if err != nil {
		return MainSyncResult{}, err
	}
	rebaseBase := currentHead
	if branch != "main" {
		out, mergeErr := runWorkspaceGit(ctx, repoPath, nil, "merge-base", currentHead, mainBase)
		if mergeErr != nil {
			return MainSyncResult{}, fmt.Errorf("resolve feature base: %w", mergeErr)
		}
		rebaseBase = strings.TrimSpace(string(out))
	}

	finalBranch, finalStaged, finalWork, err := rebasePreparedLayers(
		ctx,
		repoPath,
		workCommit,
		rebaseBase,
		syncCommit,
	)
	if err != nil {
		return MainSyncResult{}, err
	}

	if err := updateMainAndBranchRefs(
		ctx,
		repoPath,
		branchRef,
		originalMain,
		currentHead,
		syncCommit,
		finalBranch,
	); err != nil {
		return MainSyncResult{}, err
	}
	if err := applyPreparedLayers(ctx, repoPath, finalWork, finalStaged); err != nil {
		rollbackErr := rollbackMainAndBranchRefs(
			ctx,
			repoPath,
			branchRef,
			originalMain,
			currentHead,
			syncCommit,
			finalBranch,
		)
		restoreErr := applyPreparedLayers(ctx, repoPath, originalWorkTree, originalStagedTree)
		return MainSyncResult{}, errors.Join(
			fmt.Errorf("apply rebased staged and unstaged layers: %w", err),
			rollbackErr,
			restoreErr,
		)
	}

	return MainSyncResult{
		Branch:       branch,
		OriginalMain: originalMain,
		MainBase:     mainBase,
		SyncCommit:   syncCommit,
		PreparedHead: finalBranch,
	}, nil
}

func (s *WorkspaceSync) latestMain(ctx context.Context, repoPath, localMain string) (string, error) {
	remoteCmd := exec.CommandContext(ctx, "git", "-C", repoPath, "remote", "get-url", "origin")
	remoteCmd.Env = os.Environ()
	if err := remoteCmd.Run(); err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			return localMain, nil
		}
		return "", fmt.Errorf("check origin remote: %w", err)
	}
	if _, err := runWorkspaceGit(ctx, repoPath, nil, "fetch", "--no-tags", "origin", "main"); err != nil {
		return "", fmt.Errorf("fetch origin/main: %w", err)
	}
	remoteMain, exists, err := resolveOptionalRef(ctx, repoPath, "refs/remotes/origin/main")
	if err != nil {
		return "", err
	}
	if !exists {
		return "", errors.New("origin/main does not exist")
	}
	localBehind, err := s.IsAncestor(ctx, repoPath, localMain, remoteMain)
	if err != nil {
		return "", err
	}
	if localBehind {
		return remoteMain, nil
	}
	remoteBehind, err := s.IsAncestor(ctx, repoPath, remoteMain, localMain)
	if err != nil {
		return "", err
	}
	if remoteBehind {
		return localMain, nil
	}
	return "", ErrMainDiverged
}

func symbolicHead(ctx context.Context, repoPath string) (string, error) {
	out, err := runWorkspaceGit(ctx, repoPath, nil, "symbolic-ref", "-q", "HEAD")
	if err != nil {
		return "", errors.New("detached HEAD is not supported for pull-commit")
	}
	ref := strings.TrimSpace(string(out))
	if !strings.HasPrefix(ref, "refs/heads/") {
		return "", fmt.Errorf("unsupported HEAD ref %q", ref)
	}
	return ref, nil
}

func ensureUnpublishedLinearFeature(
	ctx context.Context,
	repoPath, branchRef, branch, currentHead, mainBase string,
) error {
	upstreamOut, err := runWorkspaceGit(
		ctx,
		repoPath,
		nil,
		"for-each-ref",
		"--format=%(upstream)",
		branchRef,
	)
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(upstreamOut)) != "" {
		return ErrPublishedFeature
	}
	if _, exists, err := resolveOptionalRef(ctx, repoPath, "refs/remotes/origin/"+branch); err != nil {
		return err
	} else if exists {
		return ErrPublishedFeature
	}
	mergeBaseOut, err := runWorkspaceGit(ctx, repoPath, nil, "merge-base", currentHead, mainBase)
	if err != nil {
		return fmt.Errorf("resolve feature merge base: %w", err)
	}
	mergeBase := strings.TrimSpace(string(mergeBaseOut))
	merges, err := runWorkspaceGit(ctx, repoPath, nil, "rev-list", "--merges", mergeBase+".."+currentHead)
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(merges)) != "" {
		return ErrNonLinearFeature
	}
	return nil
}

func captureDirtyLayers(
	ctx context.Context,
	repoPath, currentHead, docsDir string,
) (originalStagedTree, originalWorkTree, workCommit string, returnErr error) {
	out, err := runWorkspaceGit(ctx, repoPath, nil, "write-tree")
	if err != nil {
		return "", "", "", fmt.Errorf("capture staged tree: %w", err)
	}
	originalStagedTree = strings.TrimSpace(string(out))

	tempDir, err := os.MkdirTemp("", "sanho-dirty-layers-*")
	if err != nil {
		return "", "", "", err
	}
	defer func() {
		_ = os.RemoveAll(tempDir)
	}()
	tempIndex := filepath.Join(tempDir, "index")
	hooksDir := filepath.Join(tempDir, "hooks")
	if err := os.Mkdir(hooksDir, 0700); err != nil {
		return "", "", "", err
	}
	env := map[string]string{
		"GIT_INDEX_FILE": tempIndex,
		"GIT_WORK_TREE":  repoPath,
	}
	if _, err := runWorkspaceGit(ctx, repoPath, env, "read-tree", originalStagedTree); err != nil {
		return "", "", "", fmt.Errorf("initialize worktree capture index: %w", err)
	}
	if _, err := runWorkspaceGit(ctx, repoPath, env, "add", "-A"); err != nil {
		return "", "", "", fmt.Errorf("capture unstaged and untracked changes: %w", err)
	}
	out, err = runWorkspaceGit(ctx, repoPath, env, "write-tree")
	if err != nil {
		return "", "", "", fmt.Errorf("write captured worktree tree: %w", err)
	}
	originalWorkTree = strings.TrimSpace(string(out))

	if _, err := runWorkspaceGit(ctx, repoPath, env, "read-tree", originalStagedTree); err != nil {
		return "", "", "", fmt.Errorf("initialize staged replay index: %w", err)
	}
	if _, err := runWorkspaceGit(
		ctx,
		repoPath,
		env,
		"-c",
		"core.hooksPath="+hooksDir,
		"restore",
		"--staged",
		"--source="+currentHead,
		"--",
		docsDir,
	); err != nil {
		return "", "", "", fmt.Errorf("exclude docs from staged replay layer: %w", err)
	}
	out, err = runWorkspaceGit(ctx, repoPath, env, "write-tree")
	if err != nil {
		return "", "", "", fmt.Errorf("write staged replay tree: %w", err)
	}
	replayStagedTree := strings.TrimSpace(string(out))

	if _, err := runWorkspaceGit(ctx, repoPath, env, "read-tree", originalWorkTree); err != nil {
		return "", "", "", fmt.Errorf("initialize worktree replay index: %w", err)
	}
	if _, err := runWorkspaceGit(
		ctx,
		repoPath,
		env,
		"-c",
		"core.hooksPath="+hooksDir,
		"restore",
		"--staged",
		"--source="+currentHead,
		"--",
		docsDir,
	); err != nil {
		return "", "", "", fmt.Errorf("exclude docs from worktree replay layer: %w", err)
	}
	out, err = runWorkspaceGit(ctx, repoPath, env, "write-tree")
	if err != nil {
		return "", "", "", fmt.Errorf("write worktree replay tree: %w", err)
	}
	replayWorkTree := strings.TrimSpace(string(out))

	stagedCommit, err := createSyntheticCommit(ctx, repoPath, replayStagedTree, currentHead, "sanho staged layer")
	if err != nil {
		return "", "", "", err
	}
	workCommit, err = createSyntheticCommit(ctx, repoPath, replayWorkTree, stagedCommit, "sanho worktree layer")
	if err != nil {
		return "", "", "", err
	}
	return originalStagedTree, originalWorkTree, workCommit, nil
}

func createSyntheticCommit(
	ctx context.Context,
	repoPath, tree, parent, message string,
) (string, error) {
	out, err := runWorkspaceGitWithInput(
		ctx,
		repoPath,
		nil,
		message+"\n",
		"commit-tree",
		tree,
		"-p",
		parent,
	)
	if err != nil {
		return "", fmt.Errorf("create %s commit: %w", message, err)
	}
	return strings.TrimSpace(string(out)), nil
}

func rebasePreparedLayers(
	ctx context.Context,
	repoPath, workCommit, rebaseBase, syncCommit string,
) (finalBranch, finalStaged, finalWork string, returnErr error) {
	tempDir, err := os.MkdirTemp("", "sanho-main-sync-*")
	if err != nil {
		return "", "", "", err
	}
	hooksDir := tempDir + "-hooks"
	if err := os.Mkdir(hooksDir, 0700); err != nil {
		_ = os.RemoveAll(tempDir)
		return "", "", "", err
	}
	defer func() {
		_ = os.RemoveAll(hooksDir)
	}()
	if err := os.Remove(tempDir); err != nil {
		return "", "", "", err
	}
	defer func() {
		_ = os.RemoveAll(tempDir)
	}()
	if _, err := runWorkspaceGit(
		ctx,
		repoPath,
		nil,
		"-c",
		"core.hooksPath="+hooksDir,
		"clone",
		"--no-checkout",
		"--shared",
		repoPath,
		tempDir,
	); err != nil {
		return "", "", "", fmt.Errorf("create isolated sync clone: %w", err)
	}
	if _, err := runWorkspaceGit(
		ctx,
		tempDir,
		nil,
		"-c",
		"core.hooksPath="+hooksDir,
		"checkout",
		"--detach",
		workCommit,
	); err != nil {
		return "", "", "", fmt.Errorf("materialize isolated sync clone: %w", err)
	}
	if _, err := runWorkspaceGit(
		ctx,
		tempDir,
		nil,
		"-c",
		"rebase.autoStash=false",
		"-c",
		"core.hooksPath="+hooksDir,
		"rebase",
		"--reapply-cherry-picks",
		"--empty=keep",
		"--onto",
		syncCommit,
		rebaseBase,
	); err != nil {
		_, _ = runWorkspaceGit(ctx, tempDir, nil, "-c", "core.hooksPath="+hooksDir, "rebase", "--abort")
		return "", "", "", fmt.Errorf("%w: %v", ErrMainSyncRebaseConflict, err)
	}
	headOut, err := runWorkspaceGit(ctx, tempDir, nil, "rev-parse", "HEAD", "HEAD^", "HEAD^^")
	if err != nil {
		return "", "", "", err
	}
	lines := strings.Fields(string(headOut))
	if len(lines) != 3 {
		return "", "", "", fmt.Errorf("unexpected prepared layer history: %q", strings.TrimSpace(string(headOut)))
	}
	if _, err := runWorkspaceGit(
		ctx,
		repoPath,
		nil,
		"fetch",
		"--no-tags",
		tempDir,
		lines[0],
	); err != nil {
		return "", "", "", fmt.Errorf("import prepared sync objects: %w", err)
	}
	return lines[2], lines[1], lines[0], nil
}

func updateMainAndBranchRefs(
	ctx context.Context,
	repoPath, branchRef, originalMain, currentHead, syncCommit, finalBranch string,
) error {
	var input strings.Builder
	input.WriteString("start\n")
	fmt.Fprintf(&input, "update refs/heads/main %s %s\n", syncCommit, originalMain)
	if branchRef != "refs/heads/main" {
		fmt.Fprintf(&input, "update %s %s %s\n", branchRef, finalBranch, currentHead)
	}
	input.WriteString("prepare\ncommit\n")
	if _, err := runWorkspaceGitWithInput(ctx, repoPath, nil, input.String(), "update-ref", "--stdin"); err != nil {
		return fmt.Errorf("atomically advance main and current branch: %w", err)
	}
	return nil
}

func rollbackMainAndBranchRefs(
	ctx context.Context,
	repoPath, branchRef, originalMain, currentHead, syncCommit, finalBranch string,
) error {
	var input strings.Builder
	input.WriteString("start\n")
	fmt.Fprintf(&input, "update refs/heads/main %s %s\n", originalMain, syncCommit)
	if branchRef != "refs/heads/main" {
		fmt.Fprintf(&input, "update %s %s %s\n", branchRef, currentHead, finalBranch)
	}
	input.WriteString("prepare\ncommit\n")
	_, err := runWorkspaceGitWithInput(ctx, repoPath, nil, input.String(), "update-ref", "--stdin")
	if err != nil {
		return fmt.Errorf("roll back main and current branch refs: %w", err)
	}
	return nil
}

func applyPreparedLayers(ctx context.Context, repoPath, workTreeish, stagedTreeish string) error {
	if _, err := runWorkspaceGit(ctx, repoPath, nil, "read-tree", "--reset", "-u", workTreeish); err != nil {
		return err
	}
	if _, err := runWorkspaceGit(ctx, repoPath, nil, "read-tree", "--reset", stagedTreeish); err != nil {
		return err
	}
	return nil
}

func resolveOptionalRef(ctx context.Context, repoPath, ref string) (string, bool, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err == nil {
		return strings.TrimSpace(string(out)), true, nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
		return "", false, nil
	}
	return "", false, fmt.Errorf("resolve ref %s: %w\n%s", ref, err, strings.TrimSpace(string(out)))
}
