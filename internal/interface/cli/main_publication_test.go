package cli

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/irootkernel/sanho/internal/domain/client"
	"github.com/irootkernel/sanho/internal/infra/fs"
	infraGit "github.com/irootkernel/sanho/internal/infra/git"
	"github.com/spf13/cobra"
)

func TestPublishMainBeforeTargetPublishesEntireLocalMain(t *testing.T) {
	repo, remote, syncCommit := setupMainPublicationRepository(t)
	writeMainPublicationFile(t, repo, "main.txt", "user main\n")
	runMainPublicationTestGit(t, repo, "add", "main.txt")
	runMainPublicationTestGit(t, repo, "commit", "--no-verify", "-m", "user main")
	localMain := runMainPublicationTestGit(t, repo, "rev-parse", "HEAD")
	runMainPublicationTestGit(t, repo, "switch", "-c", "feature")
	writeMainPublicationFile(t, repo, "feature.txt", "feature\n")
	runMainPublicationTestGit(t, repo, "add", "feature.txt")
	runMainPublicationTestGit(t, repo, "commit", "--no-verify", "-m", "feature")
	featureHead := runMainPublicationTestGit(t, repo, "rev-parse", "HEAD")

	command := &cobra.Command{}
	output := new(bytes.Buffer)
	command.SetOut(output)
	command.SetErr(output)
	err := publishMainBeforeTarget(context.Background(), repo, []prePushUpdate{{
		LocalRef:  "refs/heads/feature",
		LocalOID:  featureHead,
		RemoteRef: "refs/heads/feature",
		RemoteOID: strings.Repeat("0", 40),
	}}, command)
	if err != nil {
		t.Fatalf("publish main: %v\n%s", err, output.String())
	}
	if got := remoteMainPublicationHead(t, remote); got != localMain {
		t.Fatalf("origin/main=%s want local main %s", got, localMain)
	}
	if ancestor := runMainPublicationTestGit(t, repo, "merge-base", "--is-ancestor", syncCommit, localMain); ancestor != "" {
		t.Fatalf("unexpected merge-base output %q", ancestor)
	}
	store := mainPublicationTestStore(t, repo)
	if _, exists, err := store.Load(); err != nil || exists {
		t.Fatalf("publication state remained: exists=%v err=%v", exists, err)
	}
}

func TestPublishMainBeforeTargetLetsDirectMainPushProceed(t *testing.T) {
	repo, remote, _ := setupMainPublicationRepository(t)
	localMain := runMainPublicationTestGit(t, repo, "rev-parse", "refs/heads/main")
	remoteBefore := remoteMainPublicationHead(t, remote)
	command := &cobra.Command{}
	err := publishMainBeforeTarget(context.Background(), repo, []prePushUpdate{{
		LocalRef:  "refs/heads/main",
		LocalOID:  localMain,
		RemoteRef: "refs/heads/main",
		RemoteOID: remoteBefore,
	}}, command)
	if err != nil {
		t.Fatal(err)
	}
	if got := remoteMainPublicationHead(t, remote); got != remoteBefore {
		t.Fatalf("pre-push moved origin/main: %s -> %s", remoteBefore, got)
	}
	if _, exists, err := mainPublicationTestStore(t, repo).Load(); err != nil || !exists {
		t.Fatalf("direct main pre-push cleared state before push result: exists=%v err=%v", exists, err)
	}
}

func TestPublishMainBeforeTargetBlocksDivergedOriginMain(t *testing.T) {
	repo, remote, _ := setupMainPublicationRepository(t)
	other := filepath.Join(t.TempDir(), "other")
	runMainPublicationTestGit(t, filepath.Dir(other), "clone", remote, other)
	runMainPublicationTestGit(t, other, "config", "user.email", "other@example.com")
	runMainPublicationTestGit(t, other, "config", "user.name", "Other User")
	writeMainPublicationFile(t, other, "remote.txt", "remote\n")
	runMainPublicationTestGit(t, other, "add", "remote.txt")
	runMainPublicationTestGit(t, other, "commit", "-m", "remote main")
	runMainPublicationTestGit(t, other, "push", "origin", "main")

	command := &cobra.Command{}
	output := new(bytes.Buffer)
	command.SetErr(output)
	err := publishMainBeforeTarget(context.Background(), repo, []prePushUpdate{{
		LocalRef:  "refs/heads/feature",
		LocalOID:  runMainPublicationTestGit(t, repo, "rev-parse", "HEAD"),
		RemoteRef: "refs/heads/feature",
		RemoteOID: strings.Repeat("0", 40),
	}}, command)
	if err == nil || !strings.Contains(output.String(), "diverged") {
		t.Fatalf("error=%v output=%q", err, output.String())
	}
	if _, exists, loadErr := mainPublicationTestStore(t, repo).Load(); loadErr != nil || !exists {
		t.Fatalf("blocked publication state missing: exists=%v err=%v", exists, loadErr)
	}
}

func TestReadPrePushUpdates(t *testing.T) {
	command := &cobra.Command{}
	command.SetIn(strings.NewReader(
		"refs/heads/feature 1111 refs/heads/feature 0000\n" +
			"(delete) 0000 refs/tags/old 2222\n",
	))
	updates, err := readPrePushUpdates(command)
	if err != nil {
		t.Fatal(err)
	}
	if len(updates) != 2 || !hasNonDeleteBranchUpdate(updates) {
		t.Fatalf("updates=%+v", updates)
	}
	if _, ok := findRemoteMainUpdate(updates); ok {
		t.Fatalf("unexpected main update: %+v", updates)
	}
}

func setupMainPublicationRepository(t *testing.T) (string, string, string) {
	t.Helper()
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	repo := filepath.Join(root, "repo")
	runMainPublicationTestGit(t, root, "init", "--bare", "--initial-branch=main", remote)
	runMainPublicationTestGit(t, root, "init", "--initial-branch=main", repo)
	runMainPublicationTestGit(t, repo, "config", "user.email", "test@example.com")
	runMainPublicationTestGit(t, repo, "config", "user.name", "Test User")
	runMainPublicationTestGit(t, repo, "config", "commit.gpgsign", "false")
	writeMainPublicationFile(t, repo, "docs/readme.md", "base\n")
	runMainPublicationTestGit(t, repo, "add", "docs/readme.md")
	runMainPublicationTestGit(t, repo, "commit", "--no-verify", "-m", "base")
	runMainPublicationTestGit(t, repo, "remote", "add", "origin", remote)
	runMainPublicationTestGit(t, repo, "push", "-u", "origin", "main")

	snapshotRoot := t.TempDir()
	writeMainPublicationFile(t, snapshotRoot, "docs/readme.md", "synced\n")
	snapshot, err := fs.NewSnapshotBuilder().Build(filepath.Join(snapshotRoot, "docs"))
	if err != nil {
		t.Fatal(err)
	}
	syncer := infraGit.NewWorkspaceSync(fs.NewSnapshotBuilder(), fs.NewSnapshotApplier())
	parent := runMainPublicationTestGit(t, repo, "rev-parse", "HEAD")
	syncCommit, err := syncer.CreateDocsSyncCommit(
		context.Background(),
		repo,
		"docs",
		snapshot,
		client.DefaultDocsSyncCommitMessage,
		"docs-1",
	)
	if err != nil {
		t.Fatal(err)
	}
	store := mainPublicationTestStore(t, repo)
	if err := store.Ensure(parent, fs.MainPublicationCommit{
		Commit:   syncCommit,
		Parent:   parent,
		DocsHash: "docs-1",
		Subject:  client.DefaultDocsSyncCommitMessage,
	}); err != nil {
		t.Fatal(err)
	}
	return repo, remote, syncCommit
}

func mainPublicationTestStore(t *testing.T, repo string) *fs.MainPublicationStore {
	t.Helper()
	syncer := infraGit.NewWorkspaceSync(fs.NewSnapshotBuilder(), fs.NewSnapshotApplier())
	path, err := syncer.ResolveMainPublicationPath(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	return fs.NewMainPublicationStore(path)
}

func remoteMainPublicationHead(t *testing.T, remote string) string {
	t.Helper()
	out, err := exec.Command("git", "--git-dir", remote, "rev-parse", "refs/heads/main").CombinedOutput()
	if err != nil {
		t.Fatalf("resolve remote main: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func runMainPublicationTestGit(t *testing.T, repo string, args ...string) string {
	t.Helper()
	commandArgs := append([]string{"-C", repo}, args...)
	out, err := exec.Command("git", commandArgs...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeMainPublicationFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
