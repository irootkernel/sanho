package git

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// OperationType identifies user-owned Git operation metadata in a worktree.
type OperationType string

const (
	OperationNone       OperationType = "none"
	OperationRebase     OperationType = "rebase"
	OperationAM         OperationType = "am"
	OperationMerge      OperationType = "merge"
	OperationCherryPick OperationType = "cherry_pick"
	OperationRevert     OperationType = "revert"
	OperationBisect     OperationType = "bisect"
	OperationSequencer  OperationType = "sequencer"
	OperationMultiple   OperationType = "multiple"
)

// OperationClassification describes whether workspace mutation is safe.
type OperationClassification string

const (
	OperationClear   OperationClassification = "clear"
	OperationBlocked OperationClassification = "blocked"
)

// GitOperation is the stable description of Git operation metadata detected
// for one worktree.
type GitOperation struct {
	Active         bool
	Type           OperationType
	Classification OperationClassification
	Reason         string
	NextCommands   []string
	DetectedTypes  []OperationType
}

// GitOperationBlockedError is returned when a Sanho workspace mutation is
// unsafe because user-owned Git operation metadata is present.
type GitOperationBlockedError struct {
	Operation GitOperation
}

func (e *GitOperationBlockedError) Error() string {
	return e.Operation.Reason
}

// DetectOperation inspects worktree-specific Git metadata without changing it.
func (d *Detector) DetectOperation(ctx context.Context, repoPath string) (GitOperation, error) {
	detected := make(map[OperationType]struct{})

	rebaseMerge, err := gitPathExists(ctx, repoPath, "rebase-merge")
	if err != nil {
		return GitOperation{}, err
	}
	if rebaseMerge {
		detected[OperationRebase] = struct{}{}
	}

	rebaseApply, err := gitPathExists(ctx, repoPath, "rebase-apply")
	if err != nil {
		return GitOperation{}, err
	}
	if rebaseApply {
		applying, err := gitPathExists(ctx, repoPath, "rebase-apply/applying")
		if err != nil {
			return GitOperation{}, err
		}
		if applying {
			detected[OperationAM] = struct{}{}
		} else {
			detected[OperationRebase] = struct{}{}
		}
	}

	markers := []struct {
		path     string
		typeName OperationType
	}{
		{path: "REBASE_HEAD", typeName: OperationRebase},
		{path: "MERGE_HEAD", typeName: OperationMerge},
		{path: "CHERRY_PICK_HEAD", typeName: OperationCherryPick},
		{path: "REVERT_HEAD", typeName: OperationRevert},
		{path: "BISECT_LOG", typeName: OperationBisect},
		{path: "BISECT_START", typeName: OperationBisect},
	}
	for _, marker := range markers {
		exists, err := gitPathExists(ctx, repoPath, marker.path)
		if err != nil {
			return GitOperation{}, err
		}
		if exists {
			detected[marker.typeName] = struct{}{}
		}
	}

	sequencer, err := gitPathExists(ctx, repoPath, "sequencer")
	if err != nil {
		return GitOperation{}, err
	}
	if sequencer {
		sequencerType, err := detectSequencerType(ctx, repoPath)
		if err != nil {
			return GitOperation{}, err
		}
		detected[sequencerType] = struct{}{}
	}

	if len(detected) == 0 {
		return GitOperation{
			Type:           OperationNone,
			Classification: OperationClear,
			NextCommands:   make([]string, 0),
			DetectedTypes:  make([]OperationType, 0),
		}, nil
	}

	types := make([]OperationType, 0, len(detected))
	for operationType := range detected {
		types = append(types, operationType)
	}
	sort.Slice(types, func(i, j int) bool { return types[i] < types[j] })

	operationType := types[0]
	if len(types) > 1 {
		operationType = OperationMultiple
	}
	return GitOperation{
		Active:         true,
		Type:           operationType,
		Classification: OperationBlocked,
		Reason:         operationReason(operationType, types),
		NextCommands:   operationNextCommands(operationType),
		DetectedTypes:  types,
	}, nil
}

// RequireNoOperation fails closed when Git operation metadata is present.
func (d *Detector) RequireNoOperation(ctx context.Context, repoPath string) error {
	operation, err := d.DetectOperation(ctx, repoPath)
	if err != nil {
		return fmt.Errorf("inspect Git operation state: %w", err)
	}
	if operation.Active {
		return &GitOperationBlockedError{Operation: operation}
	}
	return nil
}

func requireWorkspaceMutationSafe(ctx context.Context, repoPath string) error {
	if err := NewDetector().RequireNoOperation(ctx, repoPath); err != nil {
		return fmt.Errorf("refuse unsafe workspace mutation: %w", err)
	}
	return nil
}

func detectSequencerType(ctx context.Context, repoPath string) (OperationType, error) {
	path, err := resolveGitPath(ctx, repoPath, "sequencer/todo")
	if err != nil {
		return OperationSequencer, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return OperationSequencer, nil
		}
		return OperationSequencer, fmt.Errorf("read Git sequencer todo: %w", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		switch fields[0] {
		case "pick":
			return OperationCherryPick, nil
		case "revert":
			return OperationRevert, nil
		default:
			return OperationSequencer, nil
		}
	}
	return OperationSequencer, nil
}

func gitPathExists(ctx context.Context, repoPath, name string) (bool, error) {
	path, err := resolveGitPath(ctx, repoPath, name)
	if err != nil {
		return false, err
	}
	_, err = os.Lstat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("inspect Git path %s: %w", name, err)
}

func resolveGitPath(ctx context.Context, repoPath, name string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "rev-parse", "--git-path", name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("resolve Git path %s: %w\n%s", name, err, strings.TrimSpace(string(out)))
	}
	path := strings.TrimSpace(string(out))
	if !filepath.IsAbs(path) {
		path = filepath.Join(repoPath, path)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve absolute Git path %s: %w", name, err)
	}
	return filepath.Clean(abs), nil
}

func operationReason(operationType OperationType, detected []OperationType) string {
	if operationType == OperationMultiple {
		values := make([]string, 0, len(detected))
		for _, value := range detected {
			values = append(values, string(value))
		}
		return fmt.Sprintf(
			"multiple Git operation metadata sets are present (%s); Sanho workspace mutations are blocked",
			strings.Join(values, ", "),
		)
	}
	return fmt.Sprintf(
		"Git %s operation metadata is present; Sanho workspace mutations are blocked",
		strings.ReplaceAll(string(operationType), "_", "-"),
	)
}

func operationNextCommands(operationType OperationType) []string {
	switch operationType {
	case OperationRebase:
		return []string{"git status", "git rebase --continue", "git rebase --abort", "git rebase --quit"}
	case OperationAM:
		return []string{"git status", "git am --continue", "git am --skip", "git am --abort", "git am --quit"}
	case OperationMerge:
		return []string{"git status", "git merge --continue", "git merge --abort", "git merge --quit"}
	case OperationCherryPick:
		return []string{"git status", "git cherry-pick --continue", "git cherry-pick --abort", "git cherry-pick --quit"}
	case OperationRevert:
		return []string{"git status", "git revert --continue", "git revert --abort", "git revert --quit"}
	case OperationBisect:
		return []string{"git status", "git bisect log", "git bisect reset"}
	default:
		return []string{"git status"}
	}
}
