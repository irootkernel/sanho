package cli

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/irootkernel/sanho/internal/domain/client"
	"github.com/irootkernel/sanho/internal/domain/docs"
	"github.com/irootkernel/sanho/internal/domain/workspace"
	"github.com/irootkernel/sanho/internal/infra/fs"
	"github.com/irootkernel/sanho/internal/infra/httpclient"
)

type fakePullCommitHTTPClient struct {
	head      docs.CommitHash
	snapshots map[docs.CommitHash]docs.DocsSnapshot
	reportErr error
	reports   []httpclient.ReportWorkspaceDocsHashRequest
}

func (f *fakePullCommitHTTPClient) DocsHead(context.Context, docs.ProjectName) (docs.CommitHash, error) {
	return f.head, nil
}

func (f *fakePullCommitHTTPClient) DocsSnapshot(
	_ context.Context,
	_ docs.ProjectName,
	commit docs.CommitHash,
) (docs.DocsSnapshot, docs.CommitHash, error) {
	snapshot, ok := f.snapshots[commit]
	if !ok {
		return nil, "", errors.New("snapshot not found")
	}
	return snapshot, commit, nil
}

func (f *fakePullCommitHTTPClient) ReportWorkspaceDocsHash(
	_ context.Context,
	_ workspace.WorkspaceID,
	req httpclient.ReportWorkspaceDocsHashRequest,
) error {
	f.reports = append(f.reports, req)
	return f.reportErr
}

func TestPullCommitPreservesStagedAndUnstagedDocsLayers(t *testing.T) {
	ctx := context.Background()
	repo := t.TempDir()
	runPullCommitTestGit(t, repo, "init")
	runPullCommitTestGit(t, repo, "config", "user.email", "test@example.com")
	runPullCommitTestGit(t, repo, "config", "user.name", "Test User")
	runPullCommitTestGit(t, repo, "config", "commit.gpgsign", "false")

	writePullCommitTestFile(t, repo, "docs/staged.txt", "base staged\n")
	writePullCommitTestFile(t, repo, "docs/unstaged.txt", "base unstaged\n")
	writePullCommitTestFile(t, repo, "docs/remote.txt", "base remote\n")
	runPullCommitTestGit(t, repo, "add", "docs")
	runPullCommitTestGit(t, repo, "commit", "--no-verify", "-m", "base")

	baseSnapshot := buildPullCommitTestSnapshot(t, map[string]string{
		"staged.txt":   "base staged\n",
		"unstaged.txt": "base unstaged\n",
		"remote.txt":   "base remote\n",
	})
	remoteSnapshot := buildPullCommitTestSnapshot(t, map[string]string{
		"staged.txt":   "base staged\n",
		"unstaged.txt": "base unstaged\n",
		"remote.txt":   "remote update\n",
	})

	writePullCommitTestFile(t, repo, "docs/staged.txt", "local staged\n")
	runPullCommitTestGit(t, repo, "add", "docs/staged.txt")
	writePullCommitTestFile(t, repo, "docs/unstaged.txt", "local unstaged\n")

	baseHash := docs.CommitHash("base-docs")
	remoteHash := docs.CommitHash("remote-docs")
	httpClient := &fakePullCommitHTTPClient{
		head: remoteHash,
		snapshots: map[docs.CommitHash]docs.DocsSnapshot{
			baseHash:   baseSnapshot,
			remoteHash: remoteSnapshot,
		},
	}
	engine := newPullCommitEngine(httpClient)
	config := &client.WorkspaceConfig{
		Project:               "test-project",
		DocsDir:               "docs",
		DocsHashFile:          ".sanho_docs_hash",
		DocsSyncCommitMessage: client.DefaultDocsSyncCommitMessage,
	}

	state, err := engine.start(ctx, repo, config, baseHash, remoteHash)
	if !errors.Is(err, errPullCommitRetry) {
		t.Fatalf("start error = %v, want errPullCommitRetry", err)
	}
	if state.SyncCommit == "" {
		t.Fatal("sync commit was not recorded")
	}
	syncCommit := state.SyncCommit

	if got := runPullCommitTestGit(t, repo, "show", "HEAD:docs/remote.txt"); got != "remote update" {
		t.Fatalf("system commit remote file = %q", got)
	}
	if got := runPullCommitTestGit(t, repo, "show", "HEAD:docs/staged.txt"); got != "base staged" {
		t.Fatalf("system commit included local staged change: %q", got)
	}
	if got := runPullCommitTestGit(t, repo, "show", "HEAD:docs/unstaged.txt"); got != "base unstaged" {
		t.Fatalf("system commit included local unstaged change: %q", got)
	}

	store, err := engine.store(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	state.Phase = fs.PullCommitPhaseReady
	state.SyncCommit = ""
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}
	state, exists, err := engine.resume(ctx, repo, config)
	if !exists || !errors.Is(err, errPullCommitRetry) || state.SyncCommit != syncCommit {
		t.Fatalf("recover sync commit state=%+v exists=%v err=%v", state, exists, err)
	}

	state, exists, err = engine.resume(ctx, repo, config)
	if err != nil || !exists || state.Phase != fs.PullCommitPhasePrepared {
		t.Fatalf("prepare resume state=%+v exists=%v err=%v", state, exists, err)
	}

	secondRemoteHash := docs.CommitHash("second-remote-docs")
	httpClient.snapshots[secondRemoteHash] = buildPullCommitTestSnapshot(t, map[string]string{
		"staged.txt":   "base staged\n",
		"unstaged.txt": "base unstaged\n",
		"remote.txt":   "remote update\n",
		"second.txt":   "second remote update\n",
	})
	state, err = engine.restartAfterOutdated(ctx, repo, config, remoteHash, secondRemoteHash)
	if !errors.Is(err, errPullCommitRetry) {
		t.Fatalf("repeated remote update error = %v", err)
	}
	if got := runPullCommitTestGit(t, repo, "show", "HEAD:docs/second.txt"); got != "second remote update" {
		t.Fatalf("second docs base commit content = %q", got)
	}
	state, exists, err = engine.resume(ctx, repo, config)
	if err != nil || !exists || state.Phase != fs.PullCommitPhasePrepared {
		t.Fatalf("second prepare resume state=%+v exists=%v err=%v", state, exists, err)
	}
	if got := runPullCommitTestGit(t, repo, "show", ":docs/staged.txt"); got != "local staged" {
		t.Fatalf("staged layer = %q", got)
	}
	if got := runPullCommitTestGit(t, repo, "show", ":docs/remote.txt"); got != "remote update" {
		t.Fatalf("remote update missing from staged base = %q", got)
	}
	if got := readPullCommitTestFile(t, repo, "docs/unstaged.txt"); got != "local unstaged\n" {
		t.Fatalf("unstaged layer = %q", got)
	}

	cachedNames := strings.Fields(runPullCommitTestGit(t, repo, "diff", "--cached", "--name-only"))
	if strings.Join(cachedNames, ",") != "docs/staged.txt" {
		t.Fatalf("cached changes = %v, want only docs/staged.txt", cachedNames)
	}
	workNames := strings.Fields(runPullCommitTestGit(t, repo, "diff", "--name-only"))
	if strings.Join(workNames, ",") != "docs/unstaged.txt" {
		t.Fatalf("working changes = %v, want only docs/unstaged.txt", workNames)
	}

	runPullCommitTestGit(t, repo, "commit", "--no-verify", "-m", "user change")
	exists, err = engine.hasTransaction(ctx, repo)
	if err != nil || exists {
		t.Fatalf("stale prepared transaction was not cleared before push: exists=%v err=%v", exists, err)
	}
	if got := runPullCommitTestGit(t, repo, "show", "HEAD:docs/staged.txt"); got != "local staged" {
		t.Fatalf("user commit content = %q", got)
	}
	if got := readPullCommitTestFile(t, repo, "docs/unstaged.txt"); got != "local unstaged\n" {
		t.Fatalf("unstaged content after user commit = %q", got)
	}
}

func TestPullCommitPreparedTransactionReconcilesAmendRewrite(t *testing.T) {
	ctx := context.Background()
	repo := t.TempDir()
	runPullCommitTestGit(t, repo, "init", "--initial-branch=main")
	runPullCommitTestGit(t, repo, "config", "user.email", "test@example.com")
	runPullCommitTestGit(t, repo, "config", "user.name", "Test User")
	runPullCommitTestGit(t, repo, "config", "commit.gpgsign", "false")

	writePullCommitTestFile(t, repo, "docs/readme.md", "synced\n")
	runPullCommitTestGit(t, repo, "add", "docs/readme.md")
	runPullCommitTestGit(t, repo, "commit", "--no-verify", "-m", "[SANHO] Update docs")
	syncCommit := runPullCommitTestGit(t, repo, "rev-parse", "HEAD")

	writePullCommitTestFile(t, repo, "feature.txt", "prepared\n")
	runPullCommitTestGit(t, repo, "add", "feature.txt")
	runPullCommitTestGit(t, repo, "commit", "--no-verify", "-m", "prepared feature")
	preparedHead := runPullCommitTestGit(t, repo, "rev-parse", "HEAD")

	writePullCommitTestFile(t, repo, "feature.txt", "amended\n")
	runPullCommitTestGit(t, repo, "add", "feature.txt")
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

	runPullCommitTestGit(t, repo, "commit", "--amend", "--no-verify", "-m", "amended feature")
	amendedHead := runPullCommitTestGit(t, repo, "rev-parse", "HEAD")
	if preparedHead == amendedHead {
		t.Fatal("amend did not rewrite HEAD")
	}
	if got := runPullCommitTestGit(t, repo, "rev-parse", preparedHead+"^"); got != syncCommit {
		t.Fatalf("prepared parent=%s want %s", got, syncCommit)
	}
	if got := runPullCommitTestGit(t, repo, "rev-parse", amendedHead+"^"); got != syncCommit {
		t.Fatalf("amended parent=%s want %s", got, syncCommit)
	}

	if err := engine.clearAfterCommit(ctx, repo); err != nil {
		t.Fatal(err)
	}
	if completed, err := engine.reconcileAfterRewrite(
		ctx,
		repo,
		&client.WorkspaceConfig{DocsSyncCommitMessage: client.DefaultDocsSyncCommitMessage},
		"amend",
		[]gitRewriteMapping{{Old: preparedHead, New: amendedHead}},
	); err != nil {
		t.Fatal(err)
	} else if !completed {
		t.Fatal("amend rewrite was not recognized as completed")
	}
	if exists, err := store.Exists(); err != nil {
		t.Fatal(err)
	} else if exists {
		t.Fatal("amended prepared transaction remained active")
	}
}

func TestPullCommitConflictBlocksUntilResolvedAndStaged(t *testing.T) {
	ctx := context.Background()
	repo := t.TempDir()
	runPullCommitTestGit(t, repo, "init")
	runPullCommitTestGit(t, repo, "config", "user.email", "test@example.com")
	runPullCommitTestGit(t, repo, "config", "user.name", "Test User")
	runPullCommitTestGit(t, repo, "config", "commit.gpgsign", "false")

	writePullCommitTestFile(t, repo, "docs/shared.txt", "base\n")
	runPullCommitTestGit(t, repo, "add", "docs")
	runPullCommitTestGit(t, repo, "commit", "--no-verify", "-m", "base")
	originalHead := runPullCommitTestGit(t, repo, "rev-parse", "HEAD")

	baseSnapshot := buildPullCommitTestSnapshot(t, map[string]string{"shared.txt": "base\n"})
	remoteSnapshot := buildPullCommitTestSnapshot(t, map[string]string{
		"shared.txt":    "remote\n",
		"remote-new.md": "must survive conflict resolution\n",
	})
	writePullCommitTestFile(t, repo, "docs/shared.txt", "local\n")
	runPullCommitTestGit(t, repo, "add", "docs/shared.txt")

	baseHash := docs.CommitHash("base-docs")
	remoteHash := docs.CommitHash("remote-docs")
	engine := newPullCommitEngine(&fakePullCommitHTTPClient{
		head: remoteHash,
		snapshots: map[docs.CommitHash]docs.DocsSnapshot{
			baseHash:   baseSnapshot,
			remoteHash: remoteSnapshot,
		},
	})
	config := &client.WorkspaceConfig{
		Project:               "test-project",
		DocsDir:               "docs",
		DocsHashFile:          ".sanho_docs_hash",
		DocsSyncCommitMessage: client.DefaultDocsSyncCommitMessage,
	}

	state, err := engine.start(ctx, repo, config, baseHash, remoteHash)
	if !errors.Is(err, errPullCommitConflict) {
		t.Fatalf("start error = %v, want conflict", err)
	}
	if got := runPullCommitTestGit(t, repo, "rev-parse", "HEAD"); got != originalHead {
		t.Fatalf("HEAD changed on conflict: %s", got)
	}
	if !strings.Contains(readPullCommitTestFile(t, repo, "docs/shared.txt"), "<<<<<<<") {
		t.Fatal("working file does not contain conflict markers")
	}

	writePullCommitTestFile(t, repo, "docs/shared.txt", "resolved local and remote\n")
	_, _, err = engine.resume(ctx, repo, config)
	if err == nil || strings.Contains(err.Error(), "conflict markers") {
		t.Fatalf("unstaged resolution error = %v, want staging requirement", err)
	}
	runPullCommitTestGit(t, repo, "add", "docs/shared.txt")
	state, exists, err := engine.resume(ctx, repo, config)
	if !exists || !errors.Is(err, errPullCommitRetry) {
		t.Fatalf("resume exists=%v error=%v", exists, err)
	}
	if state.SyncCommit == "" {
		t.Fatal("sync commit was not created after resolution")
	}
	if err := engine.finishManual(ctx, repo, config); err != nil {
		t.Fatal(err)
	}
	if got := runPullCommitTestGit(t, repo, "show", "HEAD:docs/shared.txt"); got != "remote" {
		t.Fatalf("system commit content = %q, want remote", got)
	}
	if got := runPullCommitTestGit(t, repo, "show", ":docs/shared.txt"); got != "resolved local and remote" {
		t.Fatalf("resolved staged content = %q", got)
	}
	if got := runPullCommitTestGit(t, repo, "show", ":docs/remote-new.md"); got != "must survive conflict resolution" {
		t.Fatalf("remote file was lost from resolved staged content: %q", got)
	}
}

func TestOverlayChangedSnapshotPathsKeepsUntouchedPulledFiles(t *testing.T) {
	engine := newPullCommitEngine(nil)
	original := buildPullCommitTestSnapshot(t, map[string]string{
		"existing.md": "old\n",
	})
	adopted := buildPullCommitTestSnapshot(t, map[string]string{
		"existing.md":   "remote\n",
		"remote-new.md": "pulled\n",
	})
	currentIndex := buildPullCommitTestSnapshot(t, map[string]string{
		"existing.md": "locally staged after pull\n",
		"local.md":    "local staged file\n",
	})

	normalized, err := engine.overlayChangedSnapshotPaths("docs", adopted, original, currentIndex)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := fs.NewSnapshotApplier().Apply(normalized, root, "docs"); err != nil {
		t.Fatal(err)
	}
	assertPullCommitTestFile(t, root, "docs/existing.md", "locally staged after pull\n")
	assertPullCommitTestFile(t, root, "docs/local.md", "local staged file\n")
	assertPullCommitTestFile(t, root, "docs/remote-new.md", "pulled\n")
}

func TestPulledDocsBaselineTreatsAdoptedWorktreeAsClean(t *testing.T) {
	ctx := context.Background()
	repo := t.TempDir()
	runPullCommitTestGit(t, repo, "init")
	runPullCommitTestGit(t, repo, "config", "user.email", "test@example.com")
	runPullCommitTestGit(t, repo, "config", "user.name", "Test User")
	writePullCommitTestFile(t, repo, "docs/readme.md", "old\n")
	runPullCommitTestGit(t, repo, "add", "docs")
	runPullCommitTestGit(t, repo, "commit", "--no-verify", "-m", "base")

	engine := newPullCommitEngine(nil)
	originalIndex, err := engine.workspaceSync.BuildIndexDocsSnapshot(ctx, repo, "docs")
	if err != nil {
		t.Fatal(err)
	}
	adopted := buildPullCommitTestSnapshot(t, map[string]string{
		"readme.md":    "new\n",
		"preserved.md": "remote file\n",
	})
	if err := engine.workspaceSync.ApplyWorktreeDocsSnapshot(repo, "docs", adopted); err != nil {
		t.Fatal(err)
	}
	if err := recordPulledDocsBaseline(
		ctx,
		repo,
		"old-docs",
		"new-docs",
		originalIndex,
		adopted,
		false,
	); err != nil {
		t.Fatal(err)
	}

	changed, err := engine.pulledDocsHaveLocalChanges(ctx, repo, "docs")
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("adopted pull snapshot was incorrectly treated as a local change")
	}

	writePullCommitTestFile(t, repo, "docs/readme.md", "local edit\n")
	changed, err = engine.pulledDocsHaveLocalChanges(ctx, repo, "docs")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("local edit after pull was not detected")
	}
}

func TestPullCommitReportFailurePreservesDirtyStateAndRetries(t *testing.T) {
	ctx := context.Background()
	repo := t.TempDir()
	runPullCommitTestGit(t, repo, "init", "--initial-branch=main")
	runPullCommitTestGit(t, repo, "config", "user.email", "test@example.com")
	runPullCommitTestGit(t, repo, "config", "user.name", "Test User")
	runPullCommitTestGit(t, repo, "config", "commit.gpgsign", "false")

	writePullCommitTestFile(t, repo, "docs/readme.md", "base\n")
	runPullCommitTestGit(t, repo, "add", "docs")
	runPullCommitTestGit(t, repo, "commit", "--no-verify", "-m", "base")
	baseSnapshot := buildPullCommitTestSnapshot(t, map[string]string{"readme.md": "base\n"})
	remoteSnapshot := buildPullCommitTestSnapshot(t, map[string]string{"readme.md": "remote\n"})
	writePullCommitTestFile(t, repo, "docs/local.md", "staged\n")
	runPullCommitTestGit(t, repo, "add", "docs/local.md")
	writePullCommitTestFile(t, repo, "docs/work.md", "unstaged\n")

	reportFailure := errors.New("daemon unavailable")
	httpClient := &fakePullCommitHTTPClient{
		head: "remote",
		snapshots: map[docs.CommitHash]docs.DocsSnapshot{
			"base":   baseSnapshot,
			"remote": remoteSnapshot,
		},
		reportErr: reportFailure,
	}
	config := &client.WorkspaceConfig{
		WorkspaceID:           "workspace-1",
		Project:               "project",
		ActorEmail:            "actor@example.com",
		DocsDir:               "docs",
		DocsHashFile:          ".sanho_docs_hash",
		DocsSyncCommitMessage: "[SANHO] Update docs",
	}
	engine := newPullCommitEngine(httpClient)
	state, err := engine.start(ctx, repo, config, "base", "remote")
	if err == nil || !strings.Contains(err.Error(), reportFailure.Error()) {
		t.Fatalf("start error=%v", err)
	}
	if state.PreparedHead == "" || state.SyncCommit == "" {
		t.Fatalf("state=%+v", state)
	}
	if got := strings.TrimSpace(runPullCommitTestGit(t, repo, "diff", "--cached", "--name-only")); got != "docs/local.md" {
		t.Fatalf("staged paths=%q", got)
	}
	if got := strings.TrimSpace(runPullCommitTestGit(t, repo, "ls-files", "--others", "--exclude-standard")); !strings.Contains(got, "docs/work.md") {
		t.Fatalf("untracked paths=%q missing docs/work.md", got)
	}

	httpClient.reportErr = nil
	resumed, exists, err := engine.resume(ctx, repo, config)
	if err != nil || !exists {
		t.Fatalf("resume state=%+v exists=%v err=%v", resumed, exists, err)
	}
	if !resumed.Reported || resumed.Phase != fs.PullCommitPhasePrepared {
		t.Fatalf("resumed state=%+v", resumed)
	}
	if len(httpClient.reports) != 2 {
		t.Fatalf("report calls=%d want 2", len(httpClient.reports))
	}
}

func buildPullCommitTestSnapshot(t *testing.T, files map[string]string) docs.DocsSnapshot {
	t.Helper()
	root := t.TempDir()
	docsDir := filepath.Join(root, "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		writePullCommitTestFile(t, root, filepath.Join("docs", name), content)
	}
	snapshot, err := fs.NewSnapshotBuilder().Build(docsDir)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func runPullCommitTestGit(t *testing.T, repo string, args ...string) string {
	t.Helper()
	commandArgs := append([]string{"-C", repo}, args...)
	out, err := exec.Command("git", commandArgs...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func writePullCommitTestFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func readPullCommitTestFile(t *testing.T, root, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func assertPullCommitTestFile(t *testing.T, root, name, want string) {
	t.Helper()
	if got := readPullCommitTestFile(t, root, name); got != want {
		t.Fatalf("%s=%q want %q", name, got, want)
	}
}
