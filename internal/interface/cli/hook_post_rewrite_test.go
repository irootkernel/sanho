package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/irootkernel/sanho/internal/domain/client"
	"github.com/irootkernel/sanho/internal/infra/fs"
)

func TestInspectPostRewriteMutationAllowsVerifiedActiveRebase(t *testing.T) {
	ctx := context.Background()
	repo := t.TempDir()
	runPullCommitTestGit(t, repo, "init", "--initial-branch=main")
	runPullCommitTestGit(t, repo, "config", "user.email", "test@example.com")
	runPullCommitTestGit(t, repo, "config", "user.name", "Test User")
	runPullCommitTestGit(t, repo, "commit", "--allow-empty", "--no-verify", "-m", "old one")
	oldCommitOne := runPullCommitTestGit(t, repo, "rev-parse", "HEAD")
	runPullCommitTestGit(t, repo, "commit", "--allow-empty", "--no-verify", "-m", "old two")
	oldCommitTwo := runPullCommitTestGit(t, repo, "rev-parse", "HEAD")
	runPullCommitTestGit(t, repo, "commit", "--allow-empty", "--no-verify", "-m", "new")
	newCommit := runPullCommitTestGit(t, repo, "rev-parse", "HEAD")
	runPullCommitTestGit(t, repo, "commit", "--allow-empty", "--no-verify", "-m", "future")
	futureCommit := runPullCommitTestGit(t, repo, "rev-parse", "HEAD")
	runPullCommitTestGit(t, repo, "reset", "--hard", newCommit)
	makePostRewriteGitPath(t, repo, "rebase-merge", true, "")

	mappings := []gitRewriteMapping{
		{Old: oldCommitOne, New: newCommit},
		{Old: oldCommitTwo, New: newCommit},
	}
	permit, operation, err := inspectPostRewriteMutation(ctx, repo, "rebase", mappings)
	if err != nil {
		t.Fatal(err)
	}
	if !permit.verifiedRebasePostRewrite || !operation.Active || operation.Type != "rebase" {
		t.Fatalf("permit=%+v operation=%+v", permit, operation)
	}
	if err := requireWorkspaceMutationSafeWithPermit(ctx, repo, permit); err != nil {
		t.Fatalf("verified permit was rejected: %v", err)
	}
	wrongWorkDir := permit
	wrongWorkDir.workDir = t.TempDir()
	if err := requireWorkspaceMutationSafeWithPermit(ctx, repo, wrongWorkDir); err == nil {
		t.Fatal("permit was accepted for a different worktree")
	}
	runPullCommitTestGit(t, repo, "update-ref", "HEAD", futureCommit, newCommit)
	if err := requireWorkspaceMutationSafeWithPermit(ctx, repo, permit); err == nil {
		t.Fatal("permit was accepted after HEAD changed")
	}
}

func TestInspectPostRewriteMutationUsesLinkedWorktreeOperation(t *testing.T) {
	ctx := context.Background()
	mainRepo := t.TempDir()
	runPullCommitTestGit(t, mainRepo, "init", "--initial-branch=main")
	runPullCommitTestGit(t, mainRepo, "config", "user.email", "test@example.com")
	runPullCommitTestGit(t, mainRepo, "config", "user.name", "Test User")
	runPullCommitTestGit(t, mainRepo, "commit", "--allow-empty", "--no-verify", "-m", "base")
	base := runPullCommitTestGit(t, mainRepo, "rev-parse", "HEAD")
	linked := filepath.Join(t.TempDir(), "linked")
	runPullCommitTestGit(t, mainRepo, "worktree", "add", "-b", "linked", linked)
	runPullCommitTestGit(t, linked, "commit", "--allow-empty", "--no-verify", "-m", "rewritten")
	rewritten := runPullCommitTestGit(t, linked, "rev-parse", "HEAD")
	makePostRewriteGitPath(t, linked, "rebase-merge", true, "")

	permit, operation, err := inspectPostRewriteMutation(
		ctx,
		linked,
		"rebase",
		[]gitRewriteMapping{{Old: base, New: rewritten}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !permit.verifiedRebasePostRewrite || !operation.Active {
		t.Fatalf("permit=%+v operation=%+v", permit, operation)
	}
	if err := requireWorkspaceMutationSafeWithPermit(ctx, linked, permit); err != nil {
		t.Fatalf("linked worktree permit was rejected: %v", err)
	}

	mainPermit, mainOperation, err := inspectPostRewriteMutation(
		ctx,
		mainRepo,
		"rebase",
		[]gitRewriteMapping{{Old: base, New: rewritten}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if mainPermit.verifiedRebasePostRewrite || mainOperation.Active {
		t.Fatalf("main permit=%+v operation=%+v", mainPermit, mainOperation)
	}
}

func TestInspectPostRewriteMutationRejectsUnverifiedEvidence(t *testing.T) {
	ctx := context.Background()
	repo := t.TempDir()
	runPullCommitTestGit(t, repo, "init", "--initial-branch=main")
	runPullCommitTestGit(t, repo, "config", "user.email", "test@example.com")
	runPullCommitTestGit(t, repo, "config", "user.name", "Test User")
	runPullCommitTestGit(t, repo, "commit", "--allow-empty", "--no-verify", "-m", "base")
	base := runPullCommitTestGit(t, repo, "rev-parse", "HEAD")
	runPullCommitTestGit(t, repo, "switch", "-c", "side")
	runPullCommitTestGit(t, repo, "commit", "--allow-empty", "--no-verify", "-m", "side")
	unreachable := runPullCommitTestGit(t, repo, "rev-parse", "HEAD")
	runPullCommitTestGit(t, repo, "switch", "main")
	runPullCommitTestGit(t, repo, "commit", "--allow-empty", "--no-verify", "-m", "head")
	head := runPullCommitTestGit(t, repo, "rev-parse", "HEAD")
	makePostRewriteGitPath(t, repo, "rebase-merge", true, "")

	tests := []struct {
		name     string
		command  string
		mappings []gitRewriteMapping
		wantErr  bool
	}{
		{name: "empty mappings", command: "rebase"},
		{name: "wrong command", command: "amend", mappings: []gitRewriteMapping{{Old: base, New: head}}},
		{name: "missing commit", command: "rebase", mappings: []gitRewriteMapping{{Old: base, New: strings.Repeat("f", 40)}}, wantErr: true},
		{name: "unreachable commit", command: "rebase", mappings: []gitRewriteMapping{{Old: base, New: unreachable}}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			permit, operation, err := inspectPostRewriteMutation(ctx, repo, test.command, test.mappings)
			if (err != nil) != test.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, test.wantErr)
			}
			if permit.verifiedRebasePostRewrite || !operation.Active {
				t.Fatalf("permit=%+v operation=%+v", permit, operation)
			}
		})
	}
}

func makePostRewriteGitPath(t *testing.T, repo, name string, directory bool, content string) {
	t.Helper()
	path := runPullCommitTestGit(t, repo, "rev-parse", "--git-path", name)
	if !filepath.IsAbs(path) {
		path = filepath.Join(repo, path)
	}
	if directory {
		if err := os.MkdirAll(path, 0755); err != nil {
			t.Fatal(err)
		}
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

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
