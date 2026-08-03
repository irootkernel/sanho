package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/irootkernel/sanho/internal/infra/fs"
)

func TestWorkspaceMutatorsRejectGitOperationWithoutChangingRepository(t *testing.T) {
	repo := initDetectorRepo(t)
	runDetectorGit(t, repo, "config", "user.email", "test@example.com")
	runDetectorGit(t, repo, "config", "user.name", "Test User")
	writeOperationGuardFile(t, repo, "docs/readme.md", "base\n")
	runDetectorGit(t, repo, "add", "docs/readme.md")
	runDetectorGit(t, repo, "commit", "-m", "base")

	snapshotRoot := t.TempDir()
	writeOperationGuardFile(t, snapshotRoot, "docs/readme.md", "changed\n")
	snapshot, err := fs.NewSnapshotBuilder().Build(filepath.Join(snapshotRoot, "docs"))
	if err != nil {
		t.Fatal(err)
	}
	makeDetectorGitPath(t, repo, "rebase-merge", true, "")

	ctx := context.Background()
	syncer := NewWorkspaceSync(fs.NewSnapshotBuilder(), fs.NewSnapshotApplier())
	before := captureOperationGuardState(t, repo)
	tests := []struct {
		name string
		run  func() error
	}{
		{
			name: "stage docs",
			run: func() error {
				return syncer.StageDocsSnapshot(ctx, repo, "docs", snapshot)
			},
		},
		{
			name: "reset index docs",
			run: func() error {
				return syncer.ResetIndexDocsToHead(ctx, repo, "docs")
			},
		},
		{
			name: "create docs commit",
			run: func() error {
				_, err := syncer.CreateDocsSyncCommit(ctx, repo, "docs", snapshot, "sync", "docs-1")
				return err
			},
		},
		{
			name: "create main-based docs commit",
			run: func() error {
				_, err := syncer.CreateMainBasedDocsSyncCommit(ctx, repo, "docs", snapshot, "sync", "docs-1")
				return err
			},
		},
		{
			name: "apply worktree docs",
			run: func() error {
				return syncer.ApplyWorktreeDocsSnapshot(ctx, repo, "docs", snapshot)
			},
		},
		{
			name: "create recovery checkpoint",
			run: func() error {
				_, err := syncer.CreateRecoveryCheckpoint(ctx, repo, "transaction-1")
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.run()
			var blocked *GitOperationBlockedError
			if !errors.As(err, &blocked) {
				t.Fatalf("error = %v, want GitOperationBlockedError", err)
			}
			after := captureOperationGuardState(t, repo)
			if after != before {
				t.Fatalf("repository changed\nbefore: %+v\nafter:  %+v", before, after)
			}
		})
	}
}

type operationGuardState struct {
	head     string
	index    string
	status   string
	refs     string
	docs     string
	metadata bool
}

func captureOperationGuardState(t *testing.T, repo string) operationGuardState {
	t.Helper()
	metadataPath := runDetectorGit(t, repo, "rev-parse", "--git-path", "rebase-merge")
	if !filepath.IsAbs(metadataPath) {
		metadataPath = filepath.Join(repo, metadataPath)
	}
	_, err := os.Stat(metadataPath)
	return operationGuardState{
		head:     runDetectorGit(t, repo, "rev-parse", "HEAD"),
		index:    runDetectorGit(t, repo, "write-tree"),
		status:   runDetectorGit(t, repo, "status", "--porcelain=v2", "--untracked-files=all"),
		refs:     runDetectorGit(t, repo, "for-each-ref", "--format=%(refname) %(objectname)"),
		docs:     string(readOperationGuardFile(t, repo, "docs/readme.md")),
		metadata: err == nil,
	}
}

func writeOperationGuardFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func readOperationGuardFile(t *testing.T, root, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}
