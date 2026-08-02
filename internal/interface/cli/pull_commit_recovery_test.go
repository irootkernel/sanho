package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/irootkernel/sanho/internal/domain/client"
	"github.com/irootkernel/sanho/internal/infra/fs"
)

func TestPullCommitRecoverLegacySiblingRewritePreservesGitState(t *testing.T) {
	ctx := context.Background()
	repo := t.TempDir()
	runPullCommitTestGit(t, repo, "init", "--initial-branch=main")
	runPullCommitTestGit(t, repo, "config", "user.email", "test@example.com")
	runPullCommitTestGit(t, repo, "config", "user.name", "Test User")

	writePullCommitTestFile(t, repo, "docs/readme.md", "synced\n")
	runPullCommitTestGit(t, repo, "add", "docs/readme.md")
	runPullCommitTestGit(t, repo, "commit", "--no-verify", "-m", "sync")
	syncCommit := runPullCommitTestGit(t, repo, "rev-parse", "HEAD")
	writePullCommitTestFile(t, repo, "feature.txt", "prepared\n")
	runPullCommitTestGit(t, repo, "add", "feature.txt")
	runPullCommitTestGit(t, repo, "commit", "--no-verify", "-m", "prepared")
	preparedHead := runPullCommitTestGit(t, repo, "rev-parse", "HEAD")
	writePullCommitTestFile(t, repo, "feature.txt", "amended\n")
	runPullCommitTestGit(t, repo, "add", "feature.txt")
	runPullCommitTestGit(t, repo, "commit", "--amend", "--no-verify", "-m", "amended")

	writePullCommitTestFile(t, repo, "staged.txt", "staged\n")
	runPullCommitTestGit(t, repo, "add", "staged.txt")
	writePullCommitTestFile(t, repo, "feature.txt", "unstaged after amend\n")
	writePullCommitTestFile(t, repo, "untracked.txt", "untracked\n")

	engine := newPullCommitEngine(nil)
	store, err := engine.store(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(fs.PullCommitState{
		Version:      2,
		Phase:        fs.PullCommitPhasePrepared,
		OriginalHead: preparedHead,
		SyncCommit:   syncCommit,
		PreparedHead: preparedHead,
		BaseHash:     "base-docs",
		RemoteHash:   "remote-docs",
	}); err != nil {
		t.Fatal(err)
	}

	headBefore := runPullCommitTestGit(t, repo, "rev-parse", "HEAD")
	indexBefore := runPullCommitTestGit(t, repo, "write-tree")
	statusBefore := runPullCommitTestGit(t, repo, "status", "--porcelain=v1", "--untracked-files=all")
	workBefore := readPullCommitTestFile(t, repo, "feature.txt")

	assessment, err := engine.recover(ctx, repo, &client.WorkspaceConfig{DocsDir: "docs"})
	if err != nil {
		t.Fatal(err)
	}
	if assessment.Classification != pullCommitRecoverableRewrite {
		t.Fatalf("classification=%s", assessment.Classification)
	}
	if got := runPullCommitTestGit(t, repo, "rev-parse", "HEAD"); got != headBefore {
		t.Fatalf("HEAD=%s want %s", got, headBefore)
	}
	if got := runPullCommitTestGit(t, repo, "write-tree"); got != indexBefore {
		t.Fatalf("index tree=%s want %s", got, indexBefore)
	}
	if got := runPullCommitTestGit(t, repo, "status", "--porcelain=v1", "--untracked-files=all"); got != statusBefore {
		t.Fatalf("status changed:\n%s\nwant:\n%s", got, statusBefore)
	}
	if got := readPullCommitTestFile(t, repo, "feature.txt"); got != workBefore {
		t.Fatalf("worktree content=%q want %q", got, workBefore)
	}
	if exists, err := store.Exists(); err != nil || exists {
		t.Fatalf("transaction exists=%v err=%v", exists, err)
	}
	prefix := "refs/sanho/recovery/" + assessment.State.TransactionID
	refs := runPullCommitTestGit(t, repo, "for-each-ref", "--format=%(refname)", prefix)
	if len(strings.Fields(refs)) != 3 {
		t.Fatalf("recovery refs=%q", refs)
	}
	repeated, err := engine.recover(ctx, repo, &client.WorkspaceConfig{DocsDir: "docs"})
	if err != nil {
		t.Fatal(err)
	}
	if repeated.Exists || repeated.Classification != pullCommitCompleted {
		t.Fatalf("repeated recovery=%+v", repeated)
	}
}

func TestPullCommitRecoverPendingKeepsTransactionAndCreatesBackup(t *testing.T) {
	ctx := context.Background()
	repo := t.TempDir()
	runPullCommitTestGit(t, repo, "init", "--initial-branch=main")
	runPullCommitTestGit(t, repo, "config", "user.email", "test@example.com")
	runPullCommitTestGit(t, repo, "config", "user.name", "Test User")
	writePullCommitTestFile(t, repo, "base.txt", "base\n")
	runPullCommitTestGit(t, repo, "add", "base.txt")
	runPullCommitTestGit(t, repo, "commit", "--no-verify", "-m", "base")
	head := runPullCommitTestGit(t, repo, "rev-parse", "HEAD")

	engine := newPullCommitEngine(nil)
	store, err := engine.store(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(fs.PullCommitState{
		Version:       3,
		Phase:         fs.PullCommitPhasePrepared,
		TransactionID: "pending-transaction",
		OriginalHead:  head,
		SyncCommit:    head,
		PreparedHead:  head,
	}); err != nil {
		t.Fatal(err)
	}
	assessment, err := engine.recover(ctx, repo, &client.WorkspaceConfig{DocsDir: "docs"})
	if err == nil || !strings.Contains(err.Error(), "repeat the original git commit command") {
		t.Fatalf("recover error=%v", err)
	}
	if assessment.Classification != pullCommitPending {
		t.Fatalf("classification=%s", assessment.Classification)
	}
	state, exists, err := store.Load()
	if err != nil || !exists || state.Recovery == nil {
		t.Fatalf("state=%+v exists=%v err=%v", state, exists, err)
	}
}

func TestPullCommitRecoveryResumesAfterEveryMetadataMutation(t *testing.T) {
	steps := []string{
		"transaction-id-saved",
		"checkpoint-created",
		"checkpoint-recorded",
		"completion-recorded",
	}
	for _, step := range steps {
		t.Run(step, func(t *testing.T) {
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
			writePullCommitTestFile(t, repo, "feature.txt", "amended\n")
			runPullCommitTestGit(t, repo, "add", "feature.txt")
			runPullCommitTestGit(t, repo, "commit", "--amend", "--no-verify", "-m", "amended")

			engine := newPullCommitEngine(nil)
			store, err := engine.store(ctx, repo)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Save(fs.PullCommitState{
				Version:      2,
				Phase:        fs.PullCommitPhasePrepared,
				OriginalHead: preparedHead,
				SyncCommit:   syncCommit,
				PreparedHead: preparedHead,
			}); err != nil {
				t.Fatal(err)
			}

			interrupted := errors.New("injected recovery interruption")
			engine.recoveryStep = func(current string) error {
				if current == step {
					return interrupted
				}
				return nil
			}
			if _, err := engine.recover(ctx, repo, &client.WorkspaceConfig{}); !errors.Is(err, interrupted) {
				t.Fatalf("first recovery error=%v", err)
			}
			engine.recoveryStep = nil
			if _, err := engine.recover(ctx, repo, &client.WorkspaceConfig{}); err != nil {
				t.Fatalf("resumed recovery: %v", err)
			}
			if exists, err := store.Exists(); err != nil || exists {
				t.Fatalf("transaction exists=%v err=%v", exists, err)
			}
		})
	}
}
