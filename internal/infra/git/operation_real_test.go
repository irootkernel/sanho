package git

import (
	"context"
	"os/exec"
	"testing"
)

func TestDetectorDetectsRealPausedGitOperations(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(t *testing.T) string
		wantType OperationType
	}{
		{name: "rebase without conflicts", setup: setupPausedRebaseWithoutConflict, wantType: OperationRebase},
		{name: "rebase with conflicts", setup: setupPausedRebaseConflict, wantType: OperationRebase},
		{name: "merge with conflicts", setup: setupPausedMergeConflict, wantType: OperationMerge},
		{name: "cherry-pick with conflicts", setup: setupPausedCherryPickConflict, wantType: OperationCherryPick},
		{name: "revert with conflicts", setup: setupPausedRevertConflict, wantType: OperationRevert},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := test.setup(t)
			operation, err := NewDetector().DetectOperation(context.Background(), repo)
			if err != nil {
				t.Fatal(err)
			}
			if !operation.Active || operation.Type != test.wantType {
				t.Fatalf("operation=%+v want %s", operation, test.wantType)
			}
		})
	}
}

func setupPausedRebaseWithoutConflict(t *testing.T) string {
	t.Helper()
	repo := setupOperationRepo(t)
	writeOperationGuardFile(t, repo, "second.txt", "second\n")
	runDetectorGit(t, repo, "add", "second.txt")
	runDetectorGit(t, repo, "commit", "-m", "second")
	runExpectedGitFailure(t, repo, "rebase", "--exec", "false", "HEAD~1")
	return repo
}

func setupPausedRebaseConflict(t *testing.T) string {
	t.Helper()
	repo, _ := setupDivergentConflictBranches(t)
	runDetectorGit(t, repo, "switch", "side")
	runExpectedGitFailure(t, repo, "rebase", "main")
	return repo
}

func setupPausedMergeConflict(t *testing.T) string {
	t.Helper()
	repo, _ := setupDivergentConflictBranches(t)
	runExpectedGitFailure(t, repo, "merge", "side")
	return repo
}

func setupPausedCherryPickConflict(t *testing.T) string {
	t.Helper()
	repo, sideCommit := setupDivergentConflictBranches(t)
	runExpectedGitFailure(t, repo, "cherry-pick", sideCommit)
	return repo
}

func setupPausedRevertConflict(t *testing.T) string {
	t.Helper()
	repo := setupOperationRepo(t)
	writeOperationGuardFile(t, repo, "shared.txt", "change\n")
	runDetectorGit(t, repo, "add", "shared.txt")
	runDetectorGit(t, repo, "commit", "-m", "change")
	change := runDetectorGit(t, repo, "rev-parse", "HEAD")
	writeOperationGuardFile(t, repo, "shared.txt", "later\n")
	runDetectorGit(t, repo, "add", "shared.txt")
	runDetectorGit(t, repo, "commit", "-m", "later")
	runExpectedGitFailure(t, repo, "revert", "--no-edit", change)
	return repo
}

func setupDivergentConflictBranches(t *testing.T) (string, string) {
	t.Helper()
	repo := setupOperationRepo(t)
	runDetectorGit(t, repo, "switch", "-c", "side")
	writeOperationGuardFile(t, repo, "shared.txt", "side\n")
	runDetectorGit(t, repo, "add", "shared.txt")
	runDetectorGit(t, repo, "commit", "-m", "side")
	sideCommit := runDetectorGit(t, repo, "rev-parse", "HEAD")
	runDetectorGit(t, repo, "switch", "main")
	writeOperationGuardFile(t, repo, "shared.txt", "main\n")
	runDetectorGit(t, repo, "add", "shared.txt")
	runDetectorGit(t, repo, "commit", "-m", "main")
	return repo, sideCommit
}

func setupOperationRepo(t *testing.T) string {
	t.Helper()
	repo := initDetectorRepo(t)
	runDetectorGit(t, repo, "config", "user.email", "test@example.com")
	runDetectorGit(t, repo, "config", "user.name", "Test User")
	writeOperationGuardFile(t, repo, "shared.txt", "base\n")
	runDetectorGit(t, repo, "add", "shared.txt")
	runDetectorGit(t, repo, "commit", "-m", "base")
	return repo
}

func runExpectedGitFailure(t *testing.T, repo string, args ...string) {
	t.Helper()
	commandArgs := append([]string{"-C", repo}, args...)
	if output, err := exec.Command("git", commandArgs...).CombinedOutput(); err == nil {
		t.Fatalf("git %v unexpectedly succeeded:\n%s", args, output)
	}
}
