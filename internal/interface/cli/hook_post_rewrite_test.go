package cli

import (
	"context"
	"fmt"
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
	mappings := []gitRewriteMapping{
		{Old: oldCommitOne, New: newCommit},
		{Old: oldCommitTwo, New: newCommit},
	}
	sourceFile := openPostRewriteSource(t, repo, "merge", mappings)
	permit, operation, err := inspectPostRewriteMutation(
		ctx,
		repo,
		"rebase",
		mappings,
		captureGitRewriteSource(sourceFile),
	)
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
	sourceFile := openPostRewriteSource(
		t,
		linked,
		"merge",
		[]gitRewriteMapping{{Old: base, New: rewritten}},
	)

	permit, operation, err := inspectPostRewriteMutation(
		ctx,
		linked,
		"rebase",
		[]gitRewriteMapping{{Old: base, New: rewritten}},
		captureGitRewriteSource(sourceFile),
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
		gitRewriteSource{},
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
	sourceFile := openPostRewriteSource(
		t,
		repo,
		"merge",
		[]gitRewriteMapping{{Old: base, New: head}},
	)

	tests := []struct {
		name     string
		command  string
		mappings []gitRewriteMapping
		wantErr  bool
	}{
		{name: "empty mappings", command: "rebase"},
		{name: "wrong command", command: "amend", mappings: []gitRewriteMapping{{Old: base, New: head}}},
		{name: "abbreviated commit", command: "rebase", mappings: []gitRewriteMapping{{Old: base, New: head[:12]}}, wantErr: true},
		{name: "missing commit", command: "rebase", mappings: []gitRewriteMapping{{Old: base, New: strings.Repeat("f", 40)}}, wantErr: true},
		{name: "unreachable commit", command: "rebase", mappings: []gitRewriteMapping{{Old: base, New: unreachable}}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			permit, operation, err := inspectPostRewriteMutation(
				ctx,
				repo,
				test.command,
				test.mappings,
				captureGitRewriteSource(sourceFile),
			)
			if (err != nil) != test.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, test.wantErr)
			}
			if permit.verifiedRebasePostRewrite || !operation.Active {
				t.Fatalf("permit=%+v operation=%+v", permit, operation)
			}
		})
	}
}

func TestInspectPostRewriteMutationRejectsNonGitOwnedSources(t *testing.T) {
	ctx := context.Background()
	repo := t.TempDir()
	runPullCommitTestGit(t, repo, "init", "--initial-branch=main")
	runPullCommitTestGit(t, repo, "config", "user.email", "test@example.com")
	runPullCommitTestGit(t, repo, "config", "user.name", "Test User")
	runPullCommitTestGit(t, repo, "commit", "--allow-empty", "--no-verify", "-m", "base")
	head := runPullCommitTestGit(t, repo, "rev-parse", "HEAD")
	mappings := []gitRewriteMapping{{Old: head, New: head}}
	gitSource := openPostRewriteSource(t, repo, "merge", mappings)

	otherPath := filepath.Join(t.TempDir(), "rewritten-list")
	if err := os.WriteFile(otherPath, []byte(head+" "+head+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	other, err := os.Open(otherPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := other.Close(); err != nil {
			t.Errorf("close unrelated rewrite input: %v", err)
		}
	})

	if _, _, err := inspectPostRewriteMutation(
		ctx, repo, "rebase", mappings, captureGitRewriteSource(strings.NewReader("forged")),
	); err == nil {
		t.Fatal("pipe-like rewrite input was accepted")
	}
	if _, _, err := inspectPostRewriteMutation(
		ctx, repo, "rebase", mappings, captureGitRewriteSource(other),
	); err == nil {
		t.Fatal("unrelated regular file was accepted")
	}
	if _, err := gitSource.Seek(1, 0); err != nil {
		t.Fatal(err)
	}
	if _, _, err := inspectPostRewriteMutation(
		ctx, repo, "rebase", mappings, captureGitRewriteSource(gitSource),
	); err == nil {
		t.Fatal("Git-owned rewrite input with a nonzero offset was accepted")
	}
}

func TestInspectPostRewriteMutationRequiresExactlyOneRebaseBackend(t *testing.T) {
	ctx := context.Background()
	repo := t.TempDir()
	runPullCommitTestGit(t, repo, "init", "--initial-branch=main")
	runPullCommitTestGit(t, repo, "config", "user.email", "test@example.com")
	runPullCommitTestGit(t, repo, "config", "user.name", "Test User")
	runPullCommitTestGit(t, repo, "commit", "--allow-empty", "--no-verify", "-m", "base")
	head := runPullCommitTestGit(t, repo, "rev-parse", "HEAD")
	mappings := []gitRewriteMapping{{Old: head, New: head}}
	mergeSource := openPostRewriteSource(t, repo, "merge", mappings)
	openPostRewriteSource(t, repo, "apply", mappings)

	if _, _, err := inspectPostRewriteMutation(
		ctx, repo, "rebase", mappings, captureGitRewriteSource(mergeSource),
	); err == nil {
		t.Fatal("simultaneous rebase backends were accepted")
	}
}

func TestInspectPostRewriteMutationAllowsApplyBackendSource(t *testing.T) {
	ctx := context.Background()
	repo := t.TempDir()
	runPullCommitTestGit(t, repo, "init", "--initial-branch=main")
	runPullCommitTestGit(t, repo, "config", "user.email", "test@example.com")
	runPullCommitTestGit(t, repo, "config", "user.name", "Test User")
	runPullCommitTestGit(t, repo, "commit", "--allow-empty", "--no-verify", "-m", "base")
	head := runPullCommitTestGit(t, repo, "rev-parse", "HEAD")
	mappings := []gitRewriteMapping{{Old: head, New: head}}
	sourceFile := openPostRewriteSource(t, repo, "apply", mappings)

	permit, operation, err := inspectPostRewriteMutation(
		ctx, repo, "rebase", mappings, captureGitRewriteSource(sourceFile),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !permit.verifiedRebasePostRewrite || operation.Type != "rebase" {
		t.Fatalf("permit=%+v operation=%+v", permit, operation)
	}
}

func openPostRewriteSource(
	t *testing.T,
	repo, backend string,
	mappings []gitRewriteMapping,
) *os.File {
	t.Helper()
	var content strings.Builder
	for _, mapping := range mappings {
		fmt.Fprintf(&content, "%s %s\n", mapping.Old, mapping.New)
	}
	return openRawPostRewriteSource(t, repo, backend, content.String())
}

func openRawPostRewriteSource(t *testing.T, repo, backend, content string) *os.File {
	t.Helper()
	directory := "rebase-" + backend
	filename := "rewritten-list"
	if backend == "apply" {
		filename = "rewritten"
	}
	makePostRewriteGitPath(t, repo, directory, true, "")
	makePostRewriteGitPath(t, repo, filepath.Join(directory, filename), false, content)
	path := runPullCommitTestGit(t, repo, "rev-parse", "--git-path", filepath.Join(directory, filename))
	if !filepath.IsAbs(path) {
		path = filepath.Join(repo, path)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := file.Close(); err != nil {
			t.Errorf("close Git rewrite input: %v", err)
		}
	})
	return file
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
		"aaaaaaaa bbbbbbbb\ncccccccc dddddddd future metadata is opaque\n",
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
	for _, input := range []string{"only-one-field\n", "\n"} {
		if _, err := readGitRewriteMappings(strings.NewReader(input)); err == nil {
			t.Fatalf("malformed rewrite mapping %q was accepted", input)
		}
	}
}

func TestInspectPostRewriteMutationAllowsOptionalExtraInfo(t *testing.T) {
	ctx := context.Background()
	repo := t.TempDir()
	runPullCommitTestGit(t, repo, "init", "--initial-branch=main")
	runPullCommitTestGit(t, repo, "config", "user.email", "test@example.com")
	runPullCommitTestGit(t, repo, "config", "user.name", "Test User")
	runPullCommitTestGit(t, repo, "commit", "--allow-empty", "--no-verify", "-m", "base")
	head := runPullCommitTestGit(t, repo, "rev-parse", "HEAD")
	sourceFile := openRawPostRewriteSource(
		t,
		repo,
		"merge",
		head+" "+head+" future metadata remains opaque\n",
	)
	source := captureGitRewriteSource(sourceFile)
	mappings, err := readGitRewriteMappings(sourceFile)
	if err != nil {
		t.Fatal(err)
	}
	permit, operation, err := inspectPostRewriteMutation(ctx, repo, "rebase", mappings, source)
	if err != nil {
		t.Fatal(err)
	}
	if !permit.verifiedRebasePostRewrite || !operation.Active {
		t.Fatalf("permit=%+v operation=%+v", permit, operation)
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
