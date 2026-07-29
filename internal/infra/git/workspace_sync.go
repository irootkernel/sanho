package git

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/irootkernel/sanho/internal/infra/fs"
)

// WorkspaceSync performs docs-only Git operations without disturbing the
// caller's staged and unstaged changes.
type WorkspaceSync struct {
	builder *fs.SnapshotBuilder
	applier *fs.SnapshotApplier
}

func NewWorkspaceSync(builder *fs.SnapshotBuilder, applier *fs.SnapshotApplier) *WorkspaceSync {
	return &WorkspaceSync{builder: builder, applier: applier}
}

func (s *WorkspaceSync) IsRepository(ctx context.Context, repoPath string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "rev-parse", "--is-inside-work-tree")
	out, err := cmd.CombinedOutput()
	if err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			return false, nil
		}
		return false, fmt.Errorf("check Git repository: %w", err)
	}
	return strings.TrimSpace(string(out)) == "true", nil
}

func (s *WorkspaceSync) ResolveTransactionDir(ctx context.Context, repoPath string) (string, error) {
	out, err := runWorkspaceGit(ctx, repoPath, nil, "rev-parse", "--git-path", "sanho/pull-commit")
	if err != nil {
		return "", err
	}
	path := strings.TrimSpace(string(out))
	if !filepath.IsAbs(path) {
		path = filepath.Join(repoPath, path)
	}
	return filepath.Clean(path), nil
}

func (s *WorkspaceSync) ResolvePulledDocsDir(ctx context.Context, repoPath string) (string, error) {
	out, err := runWorkspaceGit(ctx, repoPath, nil, "rev-parse", "--git-path", "sanho/pulled-docs")
	if err != nil {
		return "", err
	}
	path := strings.TrimSpace(string(out))
	if !filepath.IsAbs(path) {
		path = filepath.Join(repoPath, path)
	}
	return filepath.Clean(path), nil
}

func (s *WorkspaceSync) ResolveWorkspaceReportPath(ctx context.Context, repoPath string) (string, error) {
	out, err := runWorkspaceGit(ctx, repoPath, nil, "rev-parse", "--git-path", "sanho/workspace-report.json")
	if err != nil {
		return "", err
	}
	path := strings.TrimSpace(string(out))
	if !filepath.IsAbs(path) {
		path = filepath.Join(repoPath, path)
	}
	return filepath.Clean(path), nil
}

func (s *WorkspaceSync) Head(ctx context.Context, repoPath string) (string, error) {
	out, err := runWorkspaceGit(ctx, repoPath, nil, "rev-parse", "--verify", "HEAD")
	if err != nil {
		cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "symbolic-ref", "-q", "HEAD")
		if symbolicOut, symbolicErr := cmd.CombinedOutput(); symbolicErr == nil && strings.TrimSpace(string(symbolicOut)) != "" {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (s *WorkspaceSync) ResolveRef(ctx context.Context, repoPath, ref string) (string, error) {
	out, err := runWorkspaceGit(ctx, repoPath, nil, "rev-parse", "--verify", ref+"^{commit}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (s *WorkspaceSync) PathsDifferFromIndex(ctx context.Context, repoPath string, paths []string) (bool, error) {
	for _, path := range paths {
		commandArgs := []string{"-C", repoPath, "diff", "--quiet", "--", path}
		cmd := exec.CommandContext(ctx, "git", commandArgs...)
		cmd.Env = os.Environ()
		err := cmd.Run()
		if err == nil {
			continue
		}
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return true, nil
		}
		return false, fmt.Errorf("compare worktree path %s with index: %w", path, err)
	}
	return false, nil
}

func (s *WorkspaceSync) IsAncestor(ctx context.Context, repoPath, ancestor, descendant string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "merge-base", "--is-ancestor", ancestor, descendant)
	cmd.Env = os.Environ()
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("check commit ancestry: %w", err)
}

func (s *WorkspaceSync) IsDocsSyncCommit(
	ctx context.Context,
	repoPath, commit, expectedParent, subject, docsHash string,
) (bool, error) {
	out, err := runWorkspaceGit(ctx, repoPath, nil, "show", "-s", "--format=%P%n%s%n%b", commit)
	if err != nil {
		return false, err
	}
	parts := strings.SplitN(string(out), "\n", 3)
	if len(parts) != 3 {
		return false, nil
	}
	if (expectedParent != "" && strings.TrimSpace(parts[0]) != expectedParent) ||
		strings.TrimSpace(parts[1]) != strings.TrimSpace(subject) {
		return false, nil
	}
	expectedTrailer := "docs-version: " + docsHash
	for _, line := range strings.Split(parts[2], "\n") {
		if strings.TrimSpace(line) == expectedTrailer {
			return true, nil
		}
	}
	return false, nil
}

// BuildIndexDocsSnapshot builds a docs snapshot from the index selected by
// GIT_INDEX_FILE. During pre-commit this is Git's prepared commit index.
func (s *WorkspaceSync) BuildIndexDocsSnapshot(ctx context.Context, repoPath, docsDir string) ([]byte, error) {
	tempDir, err := os.MkdirTemp("", "sanho-index-snapshot-*")
	if err != nil {
		return nil, fmt.Errorf("create index snapshot directory: %w", err)
	}
	defer func() {
		_ = os.RemoveAll(tempDir)
	}()

	prefix := tempDir + string(os.PathSeparator)
	if _, err := runWorkspaceGit(ctx, repoPath, nil, "checkout-index", "--all", "--prefix="+prefix); err != nil {
		return nil, fmt.Errorf("materialize Git index: %w", err)
	}
	docsPath := filepath.Join(tempDir, docsDir)
	if err := os.MkdirAll(docsPath, 0755); err != nil {
		return nil, fmt.Errorf("create empty index docs directory: %w", err)
	}
	return s.builder.Build(docsPath)
}

// StageDocsSnapshot replaces only docsDir in the index selected by
// GIT_INDEX_FILE while leaving the real working tree untouched.
func (s *WorkspaceSync) StageDocsSnapshot(ctx context.Context, repoPath, docsDir string, snapshot []byte) error {
	tempDir, err := os.MkdirTemp("", "sanho-stage-docs-*")
	if err != nil {
		return fmt.Errorf("create staging worktree: %w", err)
	}
	defer func() {
		_ = os.RemoveAll(tempDir)
	}()

	prefix := tempDir + string(os.PathSeparator)
	if _, err := runWorkspaceGit(ctx, repoPath, nil, "checkout-index", "--all", "--prefix="+prefix); err != nil {
		return fmt.Errorf("materialize Git index: %w", err)
	}

	if err := os.RemoveAll(filepath.Join(tempDir, docsDir)); err != nil {
		return fmt.Errorf("clear staged docs: %w", err)
	}
	if err := s.applier.Apply(snapshot, tempDir, docsDir); err != nil {
		return fmt.Errorf("apply staged docs snapshot: %w", err)
	}

	env := map[string]string{"GIT_WORK_TREE": tempDir}
	if indexFile := os.Getenv("GIT_INDEX_FILE"); indexFile != "" && !filepath.IsAbs(indexFile) {
		env["GIT_INDEX_FILE"] = filepath.Join(repoPath, indexFile)
	}
	if _, err := runWorkspaceGit(ctx, repoPath, env, "add", "-A", "--", docsDir); err != nil {
		return fmt.Errorf("update Git index docs: %w", err)
	}
	return nil
}

func (s *WorkspaceSync) ResetIndexDocsToHead(ctx context.Context, repoPath, docsDir string) error {
	if _, err := runWorkspaceGit(
		ctx,
		repoPath,
		nil,
		"restore",
		"--staged",
		"--source=HEAD",
		"--",
		docsDir,
	); err != nil {
		return fmt.Errorf("reset staged docs to HEAD: %w", err)
	}
	return nil
}

// CreateDocsSyncCommit creates a docs-only commit on top of HEAD with Git
// plumbing, then advances HEAD using compare-and-swap. Hooks are not invoked.
func (s *WorkspaceSync) CreateDocsSyncCommit(
	ctx context.Context,
	repoPath, docsDir string,
	snapshot []byte,
	subject, docsHash string,
) (string, error) {
	oldHead, err := s.Head(ctx, repoPath)
	if err != nil {
		return "", fmt.Errorf("resolve HEAD: %w", err)
	}
	commit, err := s.buildDocsSyncCommit(ctx, repoPath, oldHead, docsDir, snapshot, subject, docsHash)
	if err != nil {
		return "", err
	}

	expectedOld := oldHead
	if expectedOld == "" {
		expectedOld, err = zeroObjectID(ctx, repoPath)
		if err != nil {
			return "", err
		}
	}
	if _, err := runWorkspaceGit(ctx, repoPath, nil, "update-ref", "HEAD", commit, expectedOld); err != nil {
		return "", fmt.Errorf("advance HEAD to sync commit: %w", err)
	}
	return commit, nil
}

func (s *WorkspaceSync) buildDocsSyncCommit(
	ctx context.Context,
	repoPath, baseCommit, docsDir string,
	snapshot []byte,
	subject, docsHash string,
) (string, error) {
	tempDir, err := os.MkdirTemp("", "sanho-sync-commit-*")
	if err != nil {
		return "", fmt.Errorf("create sync commit directory: %w", err)
	}
	defer func() {
		_ = os.RemoveAll(tempDir)
	}()

	indexPath := filepath.Join(tempDir, "index")
	workTree := filepath.Join(tempDir, "worktree")
	if err := os.MkdirAll(workTree, 0755); err != nil {
		return "", fmt.Errorf("create sync worktree: %w", err)
	}
	env := map[string]string{
		"GIT_INDEX_FILE": indexPath,
		"GIT_WORK_TREE":  workTree,
	}

	if baseCommit == "" {
		if _, err := runWorkspaceGit(ctx, repoPath, env, "read-tree", "--empty"); err != nil {
			return "", fmt.Errorf("initialize empty sync index: %w", err)
		}
	} else {
		if _, err := runWorkspaceGit(ctx, repoPath, env, "read-tree", baseCommit); err != nil {
			return "", fmt.Errorf("initialize sync index: %w", err)
		}
	}
	prefix := workTree + string(os.PathSeparator)
	if _, err := runWorkspaceGit(ctx, repoPath, env, "checkout-index", "--all", "--prefix="+prefix); err != nil {
		return "", fmt.Errorf("materialize sync worktree: %w", err)
	}
	if err := os.RemoveAll(filepath.Join(workTree, docsDir)); err != nil {
		return "", fmt.Errorf("clear sync docs: %w", err)
	}
	if err := s.applier.Apply(snapshot, workTree, docsDir); err != nil {
		return "", fmt.Errorf("apply remote docs snapshot: %w", err)
	}
	if _, err := runWorkspaceGit(ctx, repoPath, env, "add", "-A", "--", docsDir); err != nil {
		return "", fmt.Errorf("stage sync docs: %w", err)
	}

	treeOut, err := runWorkspaceGit(ctx, repoPath, env, "write-tree")
	if err != nil {
		return "", fmt.Errorf("write sync tree: %w", err)
	}
	tree := strings.TrimSpace(string(treeOut))
	message := strings.TrimSpace(subject) + "\n\n" + "docs-version: " + docsHash + "\n"
	commitArgs := []string{"commit-tree", tree}
	if baseCommit != "" {
		commitArgs = append(commitArgs, "-p", baseCommit)
	}
	commitOut, err := runWorkspaceGitWithInput(ctx, repoPath, env, message, commitArgs...)
	if err != nil {
		return "", fmt.Errorf("create sync commit: %w", err)
	}
	return strings.TrimSpace(string(commitOut)), nil
}

func zeroObjectID(ctx context.Context, repoPath string) (string, error) {
	formatOut, err := runWorkspaceGit(ctx, repoPath, nil, "rev-parse", "--show-object-format")
	if err != nil {
		return "", fmt.Errorf("resolve object format: %w", err)
	}
	if strings.TrimSpace(string(formatOut)) == "sha256" {
		return strings.Repeat("0", 64), nil
	}
	return strings.Repeat("0", 40), nil
}

// ApplyWorktreeDocsSnapshot atomically replaces docsDir in the real worktree.
func (s *WorkspaceSync) ApplyWorktreeDocsSnapshot(repoPath, docsDir string, snapshot []byte) error {
	docsPath := filepath.Join(repoPath, docsDir)
	parent := filepath.Dir(docsPath)
	tempDir, err := os.MkdirTemp(parent, ".sanho-worktree-*")
	if err != nil {
		return fmt.Errorf("create worktree snapshot directory: %w", err)
	}
	defer func() {
		_ = os.RemoveAll(tempDir)
	}()

	if err := s.applier.Apply(snapshot, tempDir, "docs"); err != nil {
		return fmt.Errorf("apply worktree docs snapshot: %w", err)
	}
	replacement := filepath.Join(tempDir, "docs")
	backup := docsPath + ".sanho-backup"
	if err := os.RemoveAll(backup); err != nil {
		return fmt.Errorf("clear worktree backup: %w", err)
	}
	if err := os.Rename(docsPath, backup); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("backup worktree docs: %w", err)
	}
	if err := os.Rename(replacement, docsPath); err != nil {
		_ = os.Rename(backup, docsPath)
		return fmt.Errorf("replace worktree docs: %w", err)
	}
	if err := os.RemoveAll(backup); err != nil {
		return fmt.Errorf("remove worktree backup: %w", err)
	}
	return nil
}

func runWorkspaceGit(ctx context.Context, repoPath string, overrides map[string]string, args ...string) ([]byte, error) {
	return runWorkspaceGitWithInput(ctx, repoPath, overrides, "", args...)
}

func runWorkspaceGitWithInput(
	ctx context.Context,
	repoPath string,
	overrides map[string]string,
	input string,
	args ...string,
) ([]byte, error) {
	commandArgs := append([]string{"-C", repoPath}, args...)
	cmd := exec.CommandContext(ctx, "git", commandArgs...)
	cmd.Env = mergedEnvironment(overrides)
	if input != "" {
		cmd.Stdin = strings.NewReader(input)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git %s failed: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

func mergedEnvironment(overrides map[string]string) []string {
	if len(overrides) == 0 {
		return os.Environ()
	}
	env := make([]string, 0, len(os.Environ())+len(overrides))
	for _, entry := range os.Environ() {
		key := entry
		if idx := strings.IndexByte(entry, '='); idx >= 0 {
			key = entry[:idx]
		}
		if _, replaced := overrides[key]; !replaced {
			env = append(env, entry)
		}
	}
	for key, value := range overrides {
		env = append(env, key+"="+value)
	}
	return env
}
