package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/irootkernel/sanho/internal/domain/client"
	"github.com/irootkernel/sanho/internal/infra/fs"
)

func TestReadGitRewriteMappings(t *testing.T) {
	mappings, err := readGitRewriteMappings(strings.NewReader(
		"aaaaaaaa bbbbbbbb\ncccccccc dddddddd extra\n",
	))
	if err != nil {
		t.Fatal(err)
	}
	if len(mappings) != 2 {
		t.Fatalf("mapping count=%d want 2", len(mappings))
	}
	if mappings[0].Old != "aaaaaaaa" || mappings[0].New != "bbbbbbbb" {
		t.Fatalf("first mapping=%+v", mappings[0])
	}
	if mappings[1].Old != "cccccccc" || mappings[1].New != "dddddddd" {
		t.Fatalf("second mapping=%+v", mappings[1])
	}
}

func TestReadGitRewriteMappingsRejectsMalformedInput(t *testing.T) {
	if _, err := readGitRewriteMappings(strings.NewReader("only-one-field\n")); err == nil {
		t.Fatal("malformed rewrite mapping was accepted")
	}
}

func TestPostRewriteRebaseUpdatesPreparedAnchorWithoutCompleting(t *testing.T) {
	ctx := context.Background()
	repo := t.TempDir()
	runPullCommitTestGit(t, repo, "init", "--initial-branch=main")
	runPullCommitTestGit(t, repo, "config", "user.email", "test@example.com")
	runPullCommitTestGit(t, repo, "config", "user.name", "Test User")
	writePullCommitTestFile(t, repo, "base.txt", "sync\n")
	runPullCommitTestGit(t, repo, "add", "base.txt")
	runPullCommitTestGit(t, repo, "commit", "--no-verify", "-m", "sync")
	syncCommit := runPullCommitTestGit(t, repo, "rev-parse", "HEAD")
	writePullCommitTestFile(t, repo, "feature.txt", "prepared\n")
	runPullCommitTestGit(t, repo, "add", "feature.txt")
	runPullCommitTestGit(t, repo, "commit", "--no-verify", "-m", "prepared")
	preparedHead := runPullCommitTestGit(t, repo, "rev-parse", "HEAD")
	runPullCommitTestGit(t, repo, "commit", "--amend", "--no-verify", "-m", "rebased")
	rewrittenHead := runPullCommitTestGit(t, repo, "rev-parse", "HEAD")

	engine := newPullCommitEngine(nil)
	store, err := engine.store(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(fs.PullCommitState{
		Version:      3,
		Phase:        fs.PullCommitPhasePrepared,
		OriginalHead: preparedHead,
		SyncCommit:   syncCommit,
		PreparedHead: preparedHead,
	}); err != nil {
		t.Fatal(err)
	}
	mapping := []gitRewriteMapping{{Old: preparedHead, New: rewrittenHead}}
	for range 2 {
		completed, err := engine.reconcileAfterRewrite(
			ctx,
			repo,
			&client.WorkspaceConfig{DocsSyncCommitMessage: client.DefaultDocsSyncCommitMessage},
			"rebase",
			mapping,
		)
		if err != nil {
			t.Fatal(err)
		}
		if completed {
			t.Fatal("rebase incorrectly completed prepared transaction")
		}
	}
	state, exists, err := store.Load()
	if err != nil || !exists {
		t.Fatalf("state exists=%v err=%v", exists, err)
	}
	if state.PreparedHead != rewrittenHead || state.Phase != fs.PullCommitPhasePrepared {
		t.Fatalf("state=%+v", state)
	}
	if len(state.Rewrites) != 1 {
		t.Fatalf("rewrite count=%d want 1", len(state.Rewrites))
	}
}
