package cli

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

func TestSkipMutationHookFailsClosedWhenNormalCommitCannotInspectOperation(t *testing.T) {
	repo := t.TempDir()
	runGitOperationGuardGit(t, repo, "init")
	gitDir := runGitOperationGuardGit(t, repo, "rev-parse", "--git-dir")
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(repo, gitDir)
	}
	for _, backend := range []string{"rebase-merge", "rebase-apply"} {
		if err := os.MkdirAll(filepath.Join(gitDir, backend), 0700); err != nil {
			t.Fatal(err)
		}
	}
	cmd := &cobra.Command{}
	cmd.SetErr(new(bytes.Buffer))

	skip, err := skipMutationHookDuringGitOperation(context.Background(), repo, "pre-commit", cmd, true)
	if !skip || err == nil {
		t.Fatalf("normal commit guard skip=%t err=%v, want fail-closed error", skip, err)
	}

	skip, err = skipMutationHookDuringGitOperation(context.Background(), repo, "post-checkout", cmd, false)
	if !skip || err != nil {
		t.Fatalf("lifecycle guard skip=%t err=%v, want non-mutating successful skip", skip, err)
	}
}

func runGitOperationGuardGit(t *testing.T, repo string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(bytes.TrimSpace(out))
}
