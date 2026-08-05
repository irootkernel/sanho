package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/irootkernel/sanho/internal/domain/client"
	"github.com/irootkernel/sanho/internal/infra/fs"
	infraGit "github.com/irootkernel/sanho/internal/infra/git"
)

const (
	mainPublicationDocsHashOne = "1111111111111111111111111111111111111111"
	mainPublicationDocsHashTwo = "2222222222222222222222222222222222222222"
)

func TestPrePushPublishesMixedLocalMainBeforeFeature(t *testing.T) {
	fixture := setupMainPublicationIntegration(t)
	output, err := runMainPublicationIntegrationGit(
		fixture.Repo,
		"push",
		"-u",
		"origin",
		"feature",
	)
	if err != nil {
		t.Fatalf("push feature: %v\n%s", err, output)
	}
	if !strings.Contains(output, "origin/main publication completed") {
		t.Fatalf("push output did not report main publication:\n%s", output)
	}
	if got := mainPublicationIntegrationRemoteRef(t, fixture.Remote, "refs/heads/main"); got != fixture.LocalMain {
		t.Fatalf("origin/main=%s want %s", got, fixture.LocalMain)
	}
	if got := mainPublicationIntegrationRemoteRef(t, fixture.Remote, "refs/heads/feature"); got != fixture.FeatureHead {
		t.Fatalf("origin/feature=%s want %s", got, fixture.FeatureHead)
	}
	assertMainPublicationIntegrationState(t, fixture.Repo, false)
}

func TestPrePushKeepsPublishedMainWhenFeaturePushFails(t *testing.T) {
	fixture := setupMainPublicationIntegration(t)
	hookPath := filepath.Join(fixture.Remote, "hooks", "pre-receive")
	hook := "#!/bin/sh\nwhile read old new ref; do\n  if [ \"$ref\" = \"refs/heads/feature\" ]; then\n    echo feature-rejected >&2\n    exit 1\n  fi\ndone\n"
	if err := os.WriteFile(hookPath, []byte(hook), 0755); err != nil {
		t.Fatal(err)
	}
	output, err := runMainPublicationIntegrationGit(fixture.Repo, "push", "origin", "feature")
	if err == nil || !strings.Contains(output, "feature-rejected") {
		t.Fatalf("feature rejection err=%v output=%q", err, output)
	}
	if got := mainPublicationIntegrationRemoteRef(t, fixture.Remote, "refs/heads/main"); got != fixture.LocalMain {
		t.Fatalf("origin/main=%s want %s", got, fixture.LocalMain)
	}
	if mainPublicationIntegrationRefExists(t, fixture.Remote, "refs/heads/feature") {
		t.Fatal("feature ref was created despite rejection")
	}
	assertMainPublicationIntegrationState(t, fixture.Repo, false)

	if err := os.Remove(hookPath); err != nil {
		t.Fatal(err)
	}
	output, err = runMainPublicationIntegrationGit(fixture.Repo, "push", "origin", "feature")
	if err != nil {
		t.Fatalf("retry feature push: %v\n%s", err, output)
	}
	if got := mainPublicationIntegrationRemoteRef(t, fixture.Remote, "refs/heads/feature"); got != fixture.FeatureHead {
		t.Fatalf("origin/feature=%s want %s", got, fixture.FeatureHead)
	}
}

func TestPrePushBlocksFeatureWhenMainPublicationIsRejected(t *testing.T) {
	fixture := setupMainPublicationIntegration(t)
	remoteMainBefore := mainPublicationIntegrationRemoteRef(t, fixture.Remote, "refs/heads/main")
	hookPath := filepath.Join(fixture.Remote, "hooks", "pre-receive")
	hook := "#!/bin/sh\nwhile read old new ref; do\n  if [ \"$ref\" = \"refs/heads/main\" ]; then\n    echo main-rejected >&2\n    exit 1\n  fi\ndone\n"
	if err := os.WriteFile(hookPath, []byte(hook), 0755); err != nil {
		t.Fatal(err)
	}
	output, err := runMainPublicationIntegrationGit(fixture.Repo, "push", "origin", "feature")
	if err == nil || !strings.Contains(output, "main-rejected") ||
		!strings.Contains(output, "target push blocked") {
		t.Fatalf("main rejection err=%v output=%q", err, output)
	}
	if got := mainPublicationIntegrationRemoteRef(t, fixture.Remote, "refs/heads/main"); got != remoteMainBefore {
		t.Fatalf("origin/main changed: %s -> %s", remoteMainBefore, got)
	}
	if mainPublicationIntegrationRefExists(t, fixture.Remote, "refs/heads/feature") {
		t.Fatal("feature ref was created while main publication failed")
	}
	assertMainPublicationIntegrationState(t, fixture.Repo, true)
	state, _, loadErr := mainPublicationIntegrationStore(t, fixture.Repo).Load()
	if loadErr != nil || !strings.Contains(state.LastError, "main-rejected") || state.LastAttemptAt == nil {
		t.Fatalf("recorded failure=%+v err=%v", state, loadErr)
	}
}

func TestPrePushDirectMainPublishesCombinedHistory(t *testing.T) {
	fixture := setupMainPublicationIntegration(t)
	runMainPublicationIntegrationGitOrFatal(t, fixture.Repo, "switch", "main")
	writeMainPublicationIntegrationFile(t, fixture.Repo, "last-main.txt", "last main\n")
	runMainPublicationIntegrationGitOrFatal(t, fixture.Repo, "add", "last-main.txt")
	runMainPublicationIntegrationGitOrFatal(t, fixture.Repo, "commit", "--no-verify", "-m", "last main")
	mainHead := runMainPublicationIntegrationGitOrFatal(t, fixture.Repo, "rev-parse", "HEAD")
	output, err := runMainPublicationIntegrationGit(fixture.Repo, "push", "origin", "main")
	if err != nil {
		t.Fatalf("push main: %v\n%s", err, output)
	}
	if strings.Contains(output, "publishing local main") {
		t.Fatalf("direct main push invoked a nested main push:\n%s", output)
	}
	if got := mainPublicationIntegrationRemoteRef(t, fixture.Remote, "refs/heads/main"); got != mainHead {
		t.Fatalf("origin/main=%s want %s", got, mainHead)
	}
	assertMainPublicationIntegrationState(t, fixture.Repo, true)

	runMainPublicationIntegrationGitOrFatal(t, fixture.Repo, "switch", "-c", "probe")
	writeMainPublicationIntegrationFile(t, fixture.Repo, "probe.txt", "probe\n")
	runMainPublicationIntegrationGitOrFatal(t, fixture.Repo, "add", "probe.txt")
	runMainPublicationIntegrationGitOrFatal(t, fixture.Repo, "commit", "--no-verify", "-m", "probe")
	output, err = runMainPublicationIntegrationGit(fixture.Repo, "push", "origin", "probe")
	if err != nil {
		t.Fatalf("push probe: %v\n%s", err, output)
	}
	if strings.Contains(output, "publishing local main") {
		t.Fatalf("already-published main was pushed again:\n%s", output)
	}
	assertMainPublicationIntegrationState(t, fixture.Repo, false)
}

func TestPrePushUpgradesLegacyHookBeforePublication(t *testing.T) {
	fixture := setupMainPublicationIntegration(t)
	cliBinary := getCliBinary(t)
	hookPath := filepath.Join(fixture.Repo, ".git", "hooks", "pre-push")
	t.Setenv("PATH", filepath.Dir(cliBinary)+string(os.PathListSeparator)+os.Getenv("PATH"))
	legacyHook := "#!/bin/sh\nsanho hook pre-push\n"
	if err := os.WriteFile(hookPath, []byte(legacyHook), 0755); err != nil {
		t.Fatal(err)
	}
	output, err := runMainPublicationIntegrationGit(fixture.Repo, "push", "origin", "feature")
	if err == nil || !strings.Contains(output, "upgraded the installed pre-push hook") {
		t.Fatalf("legacy push err=%v output=%q", err, output)
	}
	if strings.Contains(output, "command not found") || strings.Contains(output, "origin: not found") {
		t.Fatalf("live legacy hook executed trailing remote arguments:\n%s", output)
	}
	data, readErr := os.ReadFile(hookPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !strings.Contains(string(data), `sanho hook pre-push "$@"`) ||
		strings.Contains(string(data), "\nsanho hook pre-push\n") {
		t.Fatalf("legacy hook was not upgraded:\n%s", data)
	}
	assertMainPublicationIntegrationState(t, fixture.Repo, true)

	output, err = runMainPublicationIntegrationGit(fixture.Repo, "push", "origin", "feature")
	if err != nil {
		t.Fatalf("retry upgraded push: %v\n%s", err, output)
	}
	assertMainPublicationIntegrationState(t, fixture.Repo, false)
}

func TestPrePushLegacyHookUpgradeDoesNotExecuteDirectURL(t *testing.T) {
	fixture := setupMainPublicationIntegration(t)
	cliBinary := getCliBinary(t)
	hookPath := filepath.Join(fixture.Repo, ".git", "hooks", "pre-push")
	t.Setenv("PATH", filepath.Dir(cliBinary)+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := os.WriteFile(hookPath, []byte("#!/bin/sh\nsanho hook pre-push\n"), 0755); err != nil {
		t.Fatal(err)
	}
	output, err := runMainPublicationIntegrationGit(fixture.Repo, "push", fixture.Remote, "feature")
	if err == nil || !strings.Contains(output, "upgraded the installed pre-push hook") {
		t.Fatalf("legacy direct-URL push err=%v output=%q", err, output)
	}
	if strings.Contains(output, "command not found") || strings.Contains(output, ": not found") {
		t.Fatalf("live legacy hook executed the remote URL:\n%s", output)
	}
	if mainPublicationIntegrationRefExists(t, fixture.Remote, "refs/heads/feature") {
		t.Fatal("legacy upgrade attempt changed the feature ref")
	}

	output, err = runMainPublicationIntegrationGit(fixture.Repo, "push", fixture.Remote, "feature")
	if err == nil || !strings.Contains(output, "git push origin main") {
		t.Fatalf("upgraded direct-URL retry did not preserve origin/main publication order: err=%v output=%q", err, output)
	}
	if mainPublicationIntegrationRefExists(t, fixture.Remote, "refs/heads/feature") {
		t.Fatal("blocked direct-URL retry changed the feature ref")
	}
	if output, err = runMainPublicationIntegrationGit(fixture.Repo, "push", "origin", "main"); err != nil {
		t.Fatalf("publish pending origin/main: %v\n%s", err, output)
	}
	if output, err = runMainPublicationIntegrationGit(fixture.Repo, "push", fixture.Remote, "feature"); err != nil {
		t.Fatalf("direct-URL push after origin/main publication: %v\n%s", err, output)
	}
	if !mainPublicationIntegrationRefExists(t, fixture.Remote, "refs/heads/feature") {
		t.Fatal("successful direct-URL retry did not create the feature ref")
	}
}

func TestPrePushBlocksNonOriginBranchPushUntilMainIsPublished(t *testing.T) {
	for _, tc := range []struct {
		name   string
		target func(t *testing.T, fixture mainPublicationIntegrationFixture) string
	}{
		{
			name: "alias remote",
			target: func(t *testing.T, fixture mainPublicationIntegrationFixture) string {
				runMainPublicationIntegrationGitOrFatal(t, fixture.Repo, "remote", "add", "backup", fixture.Remote)
				return "backup"
			},
		},
		{
			name: "direct URL",
			target: func(_ *testing.T, fixture mainPublicationIntegrationFixture) string {
				return fixture.Remote
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := setupMainPublicationIntegration(t)
			target := tc.target(t, fixture)
			remoteMainBefore := mainPublicationIntegrationRemoteRef(t, fixture.Remote, "refs/heads/main")

			output, err := runMainPublicationIntegrationGit(fixture.Repo, "push", target, "feature")
			if err == nil || !strings.Contains(output, "pending origin/main publication") ||
				!strings.Contains(output, "git push origin main") {
				t.Fatalf("non-origin push err=%v output=%q", err, output)
			}
			if got := mainPublicationIntegrationRemoteRef(t, fixture.Remote, "refs/heads/main"); got != remoteMainBefore {
				t.Fatalf("origin/main changed: %s -> %s", remoteMainBefore, got)
			}
			if mainPublicationIntegrationRefExists(t, fixture.Remote, "refs/heads/feature") {
				t.Fatal("feature ref was created while main publication was pending")
			}
			assertMainPublicationIntegrationState(t, fixture.Repo, true)

			mainOutput, mainErr := runMainPublicationIntegrationGit(fixture.Repo, "push", "origin", "main")
			if mainErr != nil {
				t.Fatalf("publish origin/main: %v\n%s", mainErr, mainOutput)
			}
			output, err = runMainPublicationIntegrationGit(fixture.Repo, "push", target, "feature")
			if err != nil {
				t.Fatalf("push after publication state cleared: %v\n%s", err, output)
			}
			if got := mainPublicationIntegrationRemoteRef(t, fixture.Remote, "refs/heads/feature"); got != fixture.FeatureHead {
				t.Fatalf("feature=%s want %s", got, fixture.FeatureHead)
			}
		})
	}
}

func TestPrePushLeavesTagAndDeletionPushesUnaffectedWhileMainIsPending(t *testing.T) {
	fixture := setupMainPublicationIntegration(t)
	runMainPublicationIntegrationGitOrFatal(t, fixture.Repo, "remote", "add", "backup", fixture.Remote)
	runMainPublicationIntegrationGitOrFatal(t, fixture.Repo, "tag", "pending-tag", fixture.FeatureHead)

	output, err := runMainPublicationIntegrationGit(fixture.Repo, "push", "backup", "refs/tags/pending-tag")
	if err != nil {
		t.Fatalf("tag push: %v\n%s", err, output)
	}
	if !mainPublicationIntegrationRefExists(t, fixture.Remote, "refs/tags/pending-tag") {
		t.Fatal("tag was not published")
	}
	assertMainPublicationIntegrationState(t, fixture.Repo, true)

	runMainPublicationIntegrationGitOrFatal(
		t,
		fixture.Repo,
		"--git-dir="+fixture.Remote,
		"update-ref",
		"refs/heads/delete-me",
		fixture.FeatureHead,
	)
	output, err = runMainPublicationIntegrationGit(fixture.Repo, "push", "backup", ":delete-me")
	if err != nil {
		t.Fatalf("deletion push: %v\n%s", err, output)
	}
	if mainPublicationIntegrationRefExists(t, fixture.Remote, "refs/heads/delete-me") {
		t.Fatal("branch deletion was blocked")
	}
	assertMainPublicationIntegrationState(t, fixture.Repo, true)
}

func TestCleanBlocksPendingMainPublication(t *testing.T) {
	fixture := setupMainPublicationIntegration(t)
	cmd := exec.Command(getCliBinary(t), "clean", "--yes", "--offline")
	cmd.Dir = fixture.Repo
	output, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "cannot clean while origin/main publication is pending") {
		t.Fatalf("clean err=%v output=%q", err, output)
	}
	assertMainPublicationIntegrationState(t, fixture.Repo, true)
	if _, statErr := os.Stat(filepath.Join(fixture.Repo, ".sanho.json")); statErr != nil {
		t.Fatalf("clean removed workspace config: %v", statErr)
	}
}

type mainPublicationIntegrationFixture struct {
	Repo        string
	Remote      string
	LocalMain   string
	FeatureHead string
}

func setupMainPublicationIntegration(t *testing.T) mainPublicationIntegrationFixture {
	t.Helper()
	cliBinary := getCliBinary(t)
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	repo := filepath.Join(root, "repo")
	runMainPublicationIntegrationGitOrFatal(t, root, "init", "--bare", "--initial-branch=main", remote)
	runMainPublicationIntegrationGitOrFatal(t, root, "init", "--initial-branch=main", repo)
	runMainPublicationIntegrationGitOrFatal(t, repo, "config", "user.email", "test@example.com")
	runMainPublicationIntegrationGitOrFatal(t, repo, "config", "user.name", "Test User")
	runMainPublicationIntegrationGitOrFatal(t, repo, "config", "commit.gpgsign", "false")
	writeMainPublicationIntegrationFile(t, repo, "docs/readme.md", "base\n")
	runMainPublicationIntegrationGitOrFatal(t, repo, "add", "docs/readme.md")
	runMainPublicationIntegrationGitOrFatal(t, repo, "commit", "--no-verify", "-m", "base")
	runMainPublicationIntegrationGitOrFatal(t, repo, "remote", "add", "origin", remote)
	runMainPublicationIntegrationGitOrFatal(t, repo, "push", "-u", "origin", "main")
	firstSnapshot := mainPublicationIntegrationSnapshot(t, "first sync\n")
	secondSnapshot := mainPublicationIntegrationSnapshot(t, "second sync\n")
	daemon := newUnixTestDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/projects/test-project/status":
			hash := r.URL.Query().Get("docs_hash")
			relation := "same"
			behind := 0
			if hash == mainPublicationDocsHashOne {
				relation = "behind"
				behind = 1
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"project": "test-project", "reference_workspace_id": "test-workspace-123",
				"reference_docs_hash": hash, "docs_head": mainPublicationDocsHashTwo,
				"reference_to_head": map[string]any{"status": relation, "ahead": 0, "behind": behind},
				"workspaces":        []any{},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/docs/snapshot":
			hash := r.URL.Query().Get("commit")
			snapshot := secondSnapshot
			if hash == mainPublicationDocsHashOne {
				snapshot = firstSnapshot
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"snapshot": snapshot, "commit": hash})
		case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/docs-hash"):
			_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(daemon.Close)
	setupSanhoConfig(t, repo, daemon.SocketPath)

	parent := runMainPublicationIntegrationGitOrFatal(t, repo, "rev-parse", "HEAD")
	firstSync := createMainPublicationIntegrationSync(t, repo, mainPublicationDocsHashOne, "first sync\n")
	runMainPublicationIntegrationGitOrFatal(t, repo, "reset", "--hard", firstSync)
	publicationStore := mainPublicationIntegrationStore(t, repo)
	if err := publicationStore.Ensure(parent, fs.MainPublicationCommit{
		Commit:   firstSync,
		Parent:   parent,
		DocsHash: mainPublicationDocsHashOne,
		Subject:  client.DefaultDocsSyncCommitMessage,
	}); err != nil {
		t.Fatal(err)
	}

	writeMainPublicationIntegrationFile(t, repo, "main-user.txt", "user main\n")
	runMainPublicationIntegrationGitOrFatal(t, repo, "add", "main-user.txt")
	runMainPublicationIntegrationGitOrFatal(t, repo, "commit", "--no-verify", "-m", "user main")
	secondParent := runMainPublicationIntegrationGitOrFatal(t, repo, "rev-parse", "HEAD")
	secondSync := createMainPublicationIntegrationSync(t, repo, mainPublicationDocsHashTwo, "second sync\n")
	runMainPublicationIntegrationGitOrFatal(t, repo, "reset", "--hard", secondSync)
	if err := publicationStore.Ensure(parent, fs.MainPublicationCommit{
		Commit:   secondSync,
		Parent:   secondParent,
		DocsHash: mainPublicationDocsHashTwo,
		Subject:  client.DefaultDocsSyncCommitMessage,
	}); err != nil {
		t.Fatal(err)
	}
	localMain := runMainPublicationIntegrationGitOrFatal(t, repo, "rev-parse", "refs/heads/main")

	runMainPublicationIntegrationGitOrFatal(t, repo, "switch", "-c", "feature")
	writeMainPublicationIntegrationFile(t, repo, "feature.txt", "feature\n")
	runMainPublicationIntegrationGitOrFatal(t, repo, "add", "feature.txt")
	runMainPublicationIntegrationGitOrFatal(t, repo, "commit", "--no-verify", "-m", "feature")
	featureHead := runMainPublicationIntegrationGitOrFatal(t, repo, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(repo, ".sanho_docs_hash"), []byte(mainPublicationDocsHashTwo+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	hookPath := filepath.Join(repo, ".git", "hooks", "pre-push")
	hook := fmt.Sprintf("#!/bin/sh\nexec %q hook pre-push \"$@\"\n", cliBinary)
	if err := os.WriteFile(hookPath, []byte(hook), 0755); err != nil {
		t.Fatal(err)
	}
	return mainPublicationIntegrationFixture{
		Repo:        repo,
		Remote:      remote,
		LocalMain:   localMain,
		FeatureHead: featureHead,
	}
}

func mainPublicationIntegrationSnapshot(t *testing.T, content string) []byte {
	t.Helper()
	root := t.TempDir()
	writeMainPublicationIntegrationFile(t, root, "readme.md", content)
	snapshot, err := fs.NewSnapshotBuilder().Build(root)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func createMainPublicationIntegrationSync(t *testing.T, repo, docsHash, content string) string {
	t.Helper()
	root := t.TempDir()
	writeMainPublicationIntegrationFile(t, root, "docs/readme.md", content)
	snapshot, err := fs.NewSnapshotBuilder().Build(filepath.Join(root, "docs"))
	if err != nil {
		t.Fatal(err)
	}
	syncer := infraGit.NewWorkspaceSync(fs.NewSnapshotBuilder(), fs.NewSnapshotApplier())
	commit, err := syncer.CreateDocsSyncCommit(
		context.Background(),
		repo,
		"docs",
		snapshot,
		client.DefaultDocsSyncCommitMessage,
		docsHash,
	)
	if err != nil {
		t.Fatal(err)
	}
	return commit
}

func mainPublicationIntegrationStore(t *testing.T, repo string) *fs.MainPublicationStore {
	t.Helper()
	syncer := infraGit.NewWorkspaceSync(fs.NewSnapshotBuilder(), fs.NewSnapshotApplier())
	path, err := syncer.ResolveMainPublicationPath(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	return fs.NewMainPublicationStore(path)
}

func assertMainPublicationIntegrationState(t *testing.T, repo string, want bool) {
	t.Helper()
	_, exists, err := mainPublicationIntegrationStore(t, repo).Load()
	if err != nil || exists != want {
		t.Fatalf("publication state exists=%v want=%v err=%v", exists, want, err)
	}
}

func mainPublicationIntegrationRemoteRef(t *testing.T, remote, ref string) string {
	t.Helper()
	out, err := exec.Command("git", "--git-dir", remote, "rev-parse", ref).CombinedOutput()
	if err != nil {
		t.Fatalf("resolve remote ref %s: %v\n%s", ref, err, out)
	}
	return strings.TrimSpace(string(out))
}

func mainPublicationIntegrationRefExists(t *testing.T, remote, ref string) bool {
	t.Helper()
	return exec.Command("git", "--git-dir", remote, "show-ref", "--verify", "--quiet", ref).Run() == nil
}

func runMainPublicationIntegrationGitOrFatal(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := runMainPublicationIntegrationGit(dir, args...)
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(out)
}

func runMainPublicationIntegrationGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func writeMainPublicationIntegrationFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
