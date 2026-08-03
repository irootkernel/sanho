package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceSyncUnreachableCommits(t *testing.T) {
	repo := t.TempDir()
	runWorkspaceSyncTestGit(t, repo, "init", "--initial-branch=main")
	runWorkspaceSyncTestGit(t, repo, "config", "user.email", "test@example.com")
	runWorkspaceSyncTestGit(t, repo, "config", "user.name", "Test User")
	runWorkspaceSyncTestGit(t, repo, "commit", "--allow-empty", "-m", "base")
	base := runWorkspaceSyncTestGit(t, repo, "rev-parse", "HEAD")
	runWorkspaceSyncTestGit(t, repo, "commit", "--allow-empty", "-m", "head")
	head := runWorkspaceSyncTestGit(t, repo, "rev-parse", "HEAD")
	runWorkspaceSyncTestGit(t, repo, "switch", "-c", "side", base)
	runWorkspaceSyncTestGit(t, repo, "commit", "--allow-empty", "-m", "side")
	side := runWorkspaceSyncTestGit(t, repo, "rev-parse", "HEAD")

	syncer := NewWorkspaceSync(nil, nil)
	tests := []struct {
		name    string
		commits []string
		want    []string
	}{
		{name: "empty"},
		{name: "all reachable", commits: []string{base, head, base}},
		{name: "unreachable", commits: []string{base, side}, want: []string{side}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := syncer.UnreachableCommits(context.Background(), repo, test.commits, head)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Join(got, "\n") != strings.Join(test.want, "\n") {
				t.Fatalf("unreachable=%v want %v", got, test.want)
			}
		})
	}
}

func TestWorkspaceSyncUnreachableCommitsHonorsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewWorkspaceSync(nil, nil).UnreachableCommits(ctx, t.TempDir(), []string{"commit"}, "head")
	if err == nil {
		t.Fatal("canceled reachability check succeeded")
	}
}

func runWorkspaceSyncTestGit(t *testing.T, repo string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "HOME="+filepath.Join(t.TempDir(), "home"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}
