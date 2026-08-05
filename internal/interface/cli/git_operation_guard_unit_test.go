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

	for _, test := range []struct {
		hook        string
		blockCommit bool
		wantErr     bool
	}{
		{hook: "pre-commit", blockCommit: true, wantErr: true},
		{hook: "commit-msg", blockCommit: true, wantErr: true},
		{hook: "post-checkout"},
		{hook: "post-merge"},
		{hook: "post-commit"},
		{hook: "post-rewrite"},
	} {
		t.Run(test.hook, func(t *testing.T) {
			skip, err := skipMutationHookDuringGitOperation(
				context.Background(), repo, test.hook, cmd, test.blockCommit,
			)
			if !skip || (err != nil) != test.wantErr {
				t.Fatalf("guard skip=%t err=%v wantErr=%t", skip, err, test.wantErr)
			}
		})
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
