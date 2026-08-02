package git

import (
	"context"
	"testing"

	"github.com/irootkernel/sanho/internal/infra/fs"
)

func TestCreateRecoveryCheckpointPreservesHeadIndexAndWorktree(t *testing.T) {
	repo := t.TempDir()
	runMainSyncGit(t, repo, "init", "--initial-branch=main")
	runMainSyncGit(t, repo, "config", "user.email", "test@example.com")
	runMainSyncGit(t, repo, "config", "user.name", "Test User")
	writeMainSyncFile(t, repo, "base.txt", "base\n")
	runMainSyncGit(t, repo, "add", "base.txt")
	runMainSyncGit(t, repo, "commit", "--no-verify", "-m", "base")
	head := runMainSyncGit(t, repo, "rev-parse", "HEAD")

	writeMainSyncFile(t, repo, "staged.txt", "staged\n")
	runMainSyncGit(t, repo, "add", "staged.txt")
	writeMainSyncFile(t, repo, "base.txt", "unstaged\n")
	writeMainSyncFile(t, repo, "untracked.txt", "untracked\n")

	syncer := NewWorkspaceSync(fs.NewSnapshotBuilder(), fs.NewSnapshotApplier())
	checkpoint, err := syncer.CreateRecoveryCheckpoint(context.Background(), repo, "transaction-1")
	if err != nil {
		t.Fatal(err)
	}
	if got := runMainSyncGit(t, repo, "rev-parse", checkpoint.HeadRef); got != head {
		t.Fatalf("head backup=%s want %s", got, head)
	}
	if got := runMainSyncGit(t, repo, "show", checkpoint.IndexRef+":staged.txt"); got != "staged\n" {
		t.Fatalf("index backup staged.txt=%q", got)
	}
	if got := runMainSyncGit(t, repo, "show", checkpoint.IndexRef+":base.txt"); got != "base\n" {
		t.Fatalf("index backup base.txt=%q", got)
	}
	if got := runMainSyncGit(t, repo, "show", checkpoint.WorktreeRef+":base.txt"); got != "unstaged\n" {
		t.Fatalf("worktree backup base.txt=%q", got)
	}
	if got := runMainSyncGit(t, repo, "show", checkpoint.WorktreeRef+":untracked.txt"); got != "untracked\n" {
		t.Fatalf("worktree backup untracked.txt=%q", got)
	}

	repeated, err := syncer.CreateRecoveryCheckpoint(context.Background(), repo, "transaction-1")
	if err != nil {
		t.Fatal(err)
	}
	if repeated != checkpoint {
		t.Fatalf("repeated checkpoint=%+v want %+v", repeated, checkpoint)
	}
}

func TestCreateRecoveryCheckpointRejectsUnsafeTransactionID(t *testing.T) {
	syncer := NewWorkspaceSync(fs.NewSnapshotBuilder(), fs.NewSnapshotApplier())
	if _, err := syncer.CreateRecoveryCheckpoint(context.Background(), t.TempDir(), "unsafe\nref"); err == nil {
		t.Fatal("unsafe transaction id was accepted")
	}
}
