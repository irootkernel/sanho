package cli

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	infraGit "github.com/irootkernel/sanho/internal/infra/git"
)

type workspaceMutationPermit struct {
	verifiedRebasePostRewrite   bool
	verifiedPostRewriteMappings bool
	workDir                     string
	rewrittenHead               string
	rewriteCommand              string
	rewriteMappings             []gitRewriteMapping
}

type gitRewriteSource struct {
	file          *os.File
	initialInfo   os.FileInfo
	initialOffset int64
	captureErr    error
}

func captureGitRewriteSource(reader io.Reader) gitRewriteSource {
	file, ok := reader.(*os.File)
	if !ok {
		return gitRewriteSource{captureErr: errors.New("rewrite input is not a file descriptor")}
	}
	info, err := file.Stat()
	if err != nil {
		return gitRewriteSource{file: file, captureErr: fmt.Errorf("inspect rewrite input: %w", err)}
	}
	offset, err := file.Seek(0, io.SeekCurrent)
	if err != nil {
		return gitRewriteSource{
			file:        file,
			initialInfo: info,
			captureErr:  fmt.Errorf("inspect rewrite input offset: %w", err),
		}
	}
	return gitRewriteSource{file: file, initialInfo: info, initialOffset: offset}
}

func requireWorkspaceMutationSafe(ctx context.Context, workDir string) error {
	return requireWorkspaceMutationSafeWithPermit(ctx, workDir, workspaceMutationPermit{})
}

func requireNoUnmergedEntries(ctx context.Context, workDir string) error {
	if !infraGit.NewDetector().HasGitDir(workDir) {
		return nil
	}
	gitClient := infraGit.NewClient()
	unmerged, err := gitClient.HasUnmergedEntries(ctx, workDir)
	if err != nil {
		return err
	}
	if unmerged {
		return errors.New("git index contains unmerged entries; resolve conflicts before running Sanho mutations")
	}
	return nil
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
	blocked := &infraGit.GitOperationBlockedError{Operation: operation}
	return &workspaceMutationBlockedError{
		cause:     blocked,
		operation: operation,
	}
}

func inspectPostRewriteMutation(
	ctx context.Context,
	workDir, command string,
	mappings []gitRewriteMapping,
	source gitRewriteSource,
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
		if len(mappings) == 0 {
			return workspaceMutationPermit{}, operation, nil
		}
		permit, err := validatePostRewritePermit(ctx, workDir, command, mappings)
		return permit, operation, err
	}
	if operation.Type != infraGit.OperationRebase || command != "rebase" || len(mappings) == 0 {
		return workspaceMutationPermit{}, operation, nil
	}
	if err := validateGitOwnedRewriteSource(ctx, workDir, source); err != nil {
		return workspaceMutationPermit{}, operation, err
	}
	permit, err := validatePostRewritePermit(ctx, workDir, command, mappings)
	if err != nil {
		return workspaceMutationPermit{}, operation, err
	}
	permit.verifiedRebasePostRewrite = true
	return permit, operation, nil
}

func validatePostRewritePermit(
	ctx context.Context,
	workDir, command string,
	mappings []gitRewriteMapping,
) (workspaceMutationPermit, error) {
	rewrittenHead, err := validatePostRewriteMappings(ctx, workDir, mappings)
	if err != nil {
		return workspaceMutationPermit{}, err
	}
	return workspaceMutationPermit{
		verifiedPostRewriteMappings: true,
		workDir:                     workDir,
		rewrittenHead:               rewrittenHead,
		rewriteCommand:              command,
		rewriteMappings:             append([]gitRewriteMapping(nil), mappings...),
	}, nil
}

func (p workspaceMutationPermit) validatesPostRewrite(
	workDir, command string,
	mappings []gitRewriteMapping,
) bool {
	if !p.verifiedPostRewriteMappings ||
		filepath.Clean(p.workDir) != filepath.Clean(workDir) ||
		p.rewriteCommand != command ||
		len(p.rewriteMappings) != len(mappings) {
		return false
	}
	for index := range mappings {
		if p.rewriteMappings[index] != mappings[index] {
			return false
		}
	}
	return true
}

func validateGitOwnedRewriteSource(
	ctx context.Context,
	workDir string,
	source gitRewriteSource,
) error {
	if source.captureErr != nil {
		return source.captureErr
	}
	if source.file == nil || source.initialInfo == nil {
		return errors.New("rewrite input file evidence is unavailable")
	}
	if !source.initialInfo.Mode().IsRegular() {
		return errors.New("rewrite input is not a regular Git metadata file")
	}
	if source.initialOffset != 0 {
		return fmt.Errorf("rewrite input did not start at offset zero: %d", source.initialOffset)
	}

	mergeDir, err := resolvePostRewriteGitPath(ctx, workDir, "rebase-merge")
	if err != nil {
		return err
	}
	applyDir, err := resolvePostRewriteGitPath(ctx, workDir, "rebase-apply")
	if err != nil {
		return err
	}
	mergeActive, err := directoryExists(mergeDir)
	if err != nil {
		return fmt.Errorf("inspect merge rebase metadata: %w", err)
	}
	applyActive, err := directoryExists(applyDir)
	if err != nil {
		return fmt.Errorf("inspect apply rebase metadata: %w", err)
	}
	if mergeActive == applyActive {
		return errors.New("exactly one Git rebase backend must be active")
	}

	rewrittenPath := filepath.Join(mergeDir, "rewritten-list")
	if applyActive {
		rewrittenPath = filepath.Join(applyDir, "rewritten")
	}
	expectedInfo, err := os.Lstat(rewrittenPath)
	if err != nil {
		return fmt.Errorf("inspect Git-owned rewrite input: %w", err)
	}
	if !expectedInfo.Mode().IsRegular() || !os.SameFile(source.initialInfo, expectedInfo) {
		return errors.New("rewrite input is not the active Git backend's rewritten file")
	}

	finalInputInfo, err := source.file.Stat()
	if err != nil {
		return fmt.Errorf("reinspect rewrite input: %w", err)
	}
	finalExpectedInfo, err := os.Lstat(rewrittenPath)
	if err != nil {
		return fmt.Errorf("reinspect Git-owned rewrite input: %w", err)
	}
	if !os.SameFile(source.initialInfo, finalInputInfo) ||
		!os.SameFile(finalInputInfo, finalExpectedInfo) ||
		finalInputInfo.Size() != source.initialInfo.Size() {
		return errors.New("git-owned rewrite input changed during validation")
	}
	return nil
}

func resolvePostRewriteGitPath(ctx context.Context, workDir, name string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", workDir, "rev-parse", "--git-path", name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("resolve Git path %s: %w\n%s", name, err, strings.TrimSpace(string(out)))
	}
	resolved := strings.TrimSpace(string(out))
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(workDir, resolved)
	}
	return filepath.Clean(resolved), nil
}

func directoryExists(path string) (bool, error) {
	info, err := os.Lstat(path)
	if err == nil {
		if !info.IsDir() {
			return false, fmt.Errorf("%s is not a directory", path)
		}
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func validatePostRewriteMappings(
	ctx context.Context,
	workDir string,
	mappings []gitRewriteMapping,
) (string, error) {
	objectFormat, err := gitObjectFormat(ctx, workDir)
	if err != nil {
		return "", err
	}
	objectIDLength := 40
	if objectFormat == "sha256" {
		objectIDLength = 64
	}
	var input strings.Builder
	for _, mapping := range mappings {
		if !isFullObjectID(mapping.Old, objectIDLength) || !isFullObjectID(mapping.New, objectIDLength) {
			return "", fmt.Errorf("rewrite mapping must contain full %s object IDs", objectFormat)
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
	candidates := make([]string, 0, len(mappings))
	for _, mapping := range mappings {
		if mapping.New == head {
			continue
		}
		if _, ok := checked[mapping.New]; ok {
			continue
		}
		checked[mapping.New] = struct{}{}
		candidates = append(candidates, mapping.New)
	}
	unreachable, err := syncer.UnreachableCommits(ctx, workDir, candidates, head)
	if err != nil {
		return "", fmt.Errorf("validate rewritten commit reachability: %w", err)
	}
	if len(unreachable) > 0 {
		return "", fmt.Errorf("rewritten commit %s is not reachable from HEAD %s", unreachable[0], head)
	}
	return head, nil
}

func gitObjectFormat(ctx context.Context, workDir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", workDir, "rev-parse", "--show-object-format")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("resolve Git object format: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	format := strings.TrimSpace(string(out))
	if format != "sha1" && format != "sha256" {
		return "", fmt.Errorf("unsupported Git object format %q", format)
	}
	return format, nil
}

func isFullObjectID(value string, length int) bool {
	if len(value) != length || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
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
	if e.operation.Type == infraGit.OperationRebase && !e.operation.Orphaned {
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
	blockNormalCommit bool,
) (bool, error) {
	detector := infraGit.NewDetector()
	if !detector.HasGitDir(workDir) {
		return false, nil
	}
	operation, err := detector.DetectOperation(ctx, workDir)
	if err != nil {
		cmd.PrintErrf(
			"sanho hook %s: warning: Git operation state could not be inspected; Sanho mutation was skipped: %v\n",
			hookName,
			err,
		)
		if blockNormalCommit {
			return true, fmt.Errorf("sanho hook %s: inspect Git operation state: %w", hookName, err)
		}
		return true, nil
	}
	if !operation.Active {
		return false, nil
	}
	if blockNormalCommit && operation.Orphaned && operation.Type == infraGit.OperationRebase {
		blocked := &infraGit.GitOperationBlockedError{Operation: operation}
		return true, &workspaceMutationBlockedError{cause: blocked, operation: operation}
	}
	printMutationHookSkip(cmd, hookName, operation)
	return true, nil
}

func printMutationHookSkip(
	cmd *cobra.Command,
	hookName string,
	operation infraGit.GitOperation,
) {
	cmd.PrintErrf(
		"sanho hook %s: %s. Sanho reconciliation is deferred until Git operation metadata is clear.\n",
		hookName,
		operation.Reason,
	)
}

func wrapGitOperationGuard(command string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", command, err)
}
