// Package docsync_test drives usecase/docsync end to end over real
// git: a real canonical origin, a real workspace-private clone, a real
// application repository, and the real workspace state files
// (sanho-v0.2.md §5.5).
//
// It lives under test/ rather than beside either half because the
// repository's architecture gate (internal/architecture) forbids the
// dependency in *both* directions — a usecase package may not import
// infra, and an infra package may not import usecase — and the gate
// inspects test imports too. test/ is where this repository already
// composes layers for integration coverage.
//
// The two adapters below are the wiring P3b installs in interface/cli,
// which is the one production package allowed to see both layers.
package docsync_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/irootkernel/sanho/internal/domain/provenance"
	"github.com/irootkernel/sanho/internal/infra/appgit"
	"github.com/irootkernel/sanho/internal/infra/canonical"
	"github.com/irootkernel/sanho/internal/infra/gitx"
	"github.com/irootkernel/sanho/internal/infra/wsstate"
	"github.com/irootkernel/sanho/internal/usecase/docsync"
)

// TestMain isolates the suite from the developer's git configuration:
// sync depends on default git behavior (merge drivers, autocrlf, hooks,
// signing), so a stray ~/.gitconfig must not change what it observes.
func TestMain(m *testing.M) {
	for key, value := range map[string]string{
		"GIT_CONFIG_GLOBAL":   os.DevNull,
		"GIT_CONFIG_SYSTEM":   os.DevNull,
		"GIT_AUTHOR_NAME":     "Test Author",
		"GIT_AUTHOR_EMAIL":    "author@example.test",
		"GIT_COMMITTER_NAME":  "Test Committer",
		"GIT_COMMITTER_EMAIL": "committer@example.test",
	} {
		if err := os.Setenv(key, value); err != nil {
			fmt.Fprintf(os.Stderr, "set %s: %v\n", key, err)
			os.Exit(1)
		}
	}
	os.Exit(m.Run())
}

const docsDir = "docs"

// appPort adapts appgit.Repo to docsync.AppRepoPort.
//
// Everything but the merge is appgit's own. The merge is composed here
// because it is the one operation that spans both infra packages:
// canonical.MergeTree is told to run in the *app worktree*, which is
// where §5.5 puts the sync merge and where all three trees live once
// FetchIntoApp has imported canonical. Conflict paths come out of the
// merge relative to the docs root, so they are prefixed here — the port
// contract is repository-relative paths, matching the marker scanner
// and §5.9's `docs/api.md` rendering.
type appPort struct{ *appgit.Repo }

func (a appPort) MergeDocs(ctx context.Context, baseTree, oursTree, theirsTree string) (string, []string, bool, error) {
	result, err := canonical.MergeTree(ctx, a.WorkDir(), baseTree, oursTree, theirsTree)
	if err != nil {
		return "", nil, false, err
	}
	conflicts := make([]string, 0, len(result.Conflicts))
	for _, name := range result.Conflicts {
		conflicts = append(conflicts, path.Join(a.DocsDir(), name))
	}
	return result.Tree, conflicts, result.Clean, nil
}

// statePort adapts wsstate to docsync.StatePort. Every method is a
// direct wsstate call, including ClearBase — removing the base file
// outright is the only way to express "no base is recorded", since the
// schema has no representation for an empty OID.
type statePort struct{ workDir, gitDir string }

func (s statePort) LoadBase() (provenance.Base, bool, error) { return wsstate.LoadBase(s.workDir) }

func (s statePort) SaveBase(base provenance.Base) error { return wsstate.SaveBase(s.workDir, base) }

func (s statePort) ClearBase() error { return wsstate.ClearBase(s.workDir) }

func (s statePort) LoadSyncNote() (docsync.SyncNote, bool, error) {
	note, ok, err := wsstate.LoadSyncNote(s.gitDir)
	switch {
	case errors.Is(err, wsstate.ErrSyncNoteCorrupt):
		// The production adapter's contract: a note that cannot be parsed
		// still exists, and the sentinel crosses the layer boundary as
		// the use case's own.
		return docsync.SyncNote{}, true, fmt.Errorf("%w: %v", docsync.ErrSyncNoteCorrupt, err)
	case err != nil || !ok:
		return docsync.SyncNote{}, false, err
	}
	return docsync.SyncNote{
		PrevBase:            note.PrevBase,
		Target:              note.Target,
		EntryHead:           note.EntryHead,
		EntryDocsTree:       note.EntryDocsTree,
		PreDatesEntryRecord: note.PreDatesEntryRecord(),
	}, true, nil
}

func (s statePort) SaveSyncNote(note docsync.SyncNote) error {
	return wsstate.SaveSyncNote(s.gitDir, wsstate.SyncNote{
		PrevBase:      note.PrevBase,
		Target:        note.Target,
		StartedAt:     time.Now().UTC(),
		EntryHead:     note.EntryHead,
		EntryDocsTree: note.EntryDocsTree,
	})
}

func (s statePort) ClearSyncNote() error { return wsstate.ClearSyncNote(s.gitDir) }

// flow is one workspace: a canonical origin, the private clone bound to
// an application repository, and the use case wired over them.
type flow struct {
	origin string
	appDir string
	gitDir string
	store  *canonical.Store
	repo   *appgit.Repo
	use    *docsync.UseCase
}

// newFlow seeds a canonical repository with canonicalFiles and an app
// repository whose docs/ holds docsFiles, then clones canonical into
// the workspace. No base is recorded yet; each test decides.
func newFlow(t *testing.T, canonicalFiles, docsFiles map[string]string) *flow {
	t.Helper()
	return newFlowAt(t, newOrigin(t, canonicalFiles), docsFiles)
}

// newEmptyCanonicalFlow is newFlow against a canonical repository that
// has never been published into — the state a brand-new docs repo is in
// (sanho-v0.2.md §5.3 bootstrap).
func newEmptyCanonicalFlow(t *testing.T, docsFiles map[string]string) *flow {
	t.Helper()
	return newFlowAt(t, newEmptyOrigin(t), docsFiles)
}

func newFlowAt(t *testing.T, origin string, docsFiles map[string]string) *flow {
	t.Helper()

	appDir := newRepo(t, "app")

	writeFile(t, appDir, ".gitignore", wsstate.BaseFileName+"\n")
	writeFile(t, appDir, "README.md", "readme\n")
	writeFile(t, appDir, "src/app.go", "package main\n")
	for name, content := range docsFiles {
		writeFile(t, appDir, docsDir+"/"+name, content)
	}
	gitRun(t, appDir, "add", "-A", "--", ".")
	gitRun(t, appDir, "commit", "--quiet", "-m", "docs: seed workspace")

	gitDir := filepath.Join(appDir, ".git")
	store, err := canonical.Ensure(context.Background(), gitDir, origin)
	if err != nil {
		t.Fatalf("Ensure canonical clone: %v", err)
	}
	repo := appgit.New(appDir, docsDir, nil)

	return &flow{
		origin: origin,
		appDir: appDir,
		gitDir: gitDir,
		store:  store,
		repo:   repo,
		use: &docsync.UseCase{
			Canonical: canonical.NewLink(store, gitDir),
			App:       appPort{Repo: repo},
			State:     statePort{workDir: appDir, gitDir: gitDir},
		},
	}
}

// sync runs `sanho sync` and fails the test on error.
func (f *flow) sync(t *testing.T, opts docsync.Options) docsync.Result {
	t.Helper()
	result, err := f.use.Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	return result
}

// syncErr runs `sanho sync` expecting a refusal.
func (f *flow) syncErr(t *testing.T, opts docsync.Options) error {
	t.Helper()
	result, err := f.use.Run(context.Background(), opts)
	if err == nil {
		t.Fatalf("sync succeeded with %+v, want a refusal", result)
	}
	return err
}

// pull runs `sanho pull` and fails the test on error.
func (f *flow) pull(t *testing.T, withCommit bool) docsync.Result {
	t.Helper()
	result, err := f.use.Pull(context.Background(), withCommit)
	if err != nil {
		t.Fatalf("pull: %v", err)
	}
	return result
}

// upstream lands one canonical commit out of band, the way another
// workspace's push would. docsFiles replaces canonical's whole content,
// so a file omitted from it is a deletion.
func (f *flow) upstream(t *testing.T, files map[string]string) string {
	t.Helper()

	work := newRepo(t, "upstream")
	gitRun(t, work, "remote", "add", "origin", f.origin)
	gitRun(t, work, "fetch", "--quiet", "origin", "main")
	gitRun(t, work, "checkout", "--quiet", "-B", "main", "FETCH_HEAD")
	replaceTree(t, work, files)
	gitRun(t, work, "add", "-A", "--", ".")
	gitRun(t, work, "commit", "--quiet", "-m", "canonical: upstream change")
	gitRun(t, work, "push", "--quiet", "origin", "HEAD:main")

	return gitLine(t, work, "rev-parse", "HEAD")
}

// rewriteCanonical replaces canonical history with a fresh root commit
// carrying root, then — when tip is non-nil — one more commit carrying
// it, and force-pushes the result. This is a squash or rebase as the
// workspace experiences it: the recorded base OID stops being reachable
// while its content may or may not survive somewhere in the new
// history.
func (f *flow) rewriteCanonical(t *testing.T, root, tip map[string]string) {
	t.Helper()

	work := newRepo(t, "rewrite")
	replaceTree(t, work, root)
	gitRun(t, work, "add", "-A", "--", ".")
	gitRun(t, work, "commit", "--quiet", "-m", "canonical: rewritten root")
	if tip != nil {
		replaceTree(t, work, tip)
		gitRun(t, work, "add", "-A", "--", ".")
		gitRun(t, work, "commit", "--quiet", "-m", "canonical: after the rewrite")
	}
	gitRun(t, work, "push", "--quiet", "--force", f.origin, "HEAD:main")
}

// writeDocs replaces the app repository's docs directory with files.
func (f *flow) writeDocs(t *testing.T, files map[string]string) {
	t.Helper()
	replaceTree(t, filepath.Join(f.appDir, docsDir), files)
}

// commitAll commits everything in the app repository.
func (f *flow) commitAll(t *testing.T, message string) string {
	t.Helper()
	gitRun(t, f.appDir, "add", "-A", "--", ".")
	gitRun(t, f.appDir, "commit", "--quiet", "-m", message)
	return gitLine(t, f.appDir, "rev-parse", "HEAD")
}

func (f *flow) head(t *testing.T) string {
	t.Helper()
	return gitLine(t, f.appDir, "rev-parse", "HEAD")
}

// canonicalHead reads origin's published head directly, so assertions
// never depend on the clone being fresh.
func (f *flow) canonicalHead(t *testing.T) (commit, tree string) {
	t.Helper()
	commit = gitLine(t, f.origin, "rev-parse", "refs/heads/main")
	return commit, gitLine(t, f.origin, "rev-parse", commit+"^{tree}")
}

func (f *flow) setBase(t *testing.T, base provenance.Base) {
	t.Helper()
	if err := wsstate.SaveBase(f.appDir, base); err != nil {
		t.Fatalf("SaveBase: %v", err)
	}
}

// adoptCanonicalHeadAsBase records the current canonical head as the
// base, the state a freshly initialized workspace is in.
func (f *flow) adoptCanonicalHeadAsBase(t *testing.T) provenance.Base {
	t.Helper()
	commit, tree := f.canonicalHead(t)
	base := provenance.Base{Commit: commit, Tree: tree}
	f.setBase(t, base)
	return base
}

func (f *flow) base(t *testing.T) provenance.Base {
	t.Helper()
	base, ok, err := wsstate.LoadBase(f.appDir)
	if err != nil {
		t.Fatalf("LoadBase: %v", err)
	}
	if !ok {
		t.Fatal("no base file is recorded")
	}
	return base
}

func (f *flow) hasBase(t *testing.T) bool {
	t.Helper()
	_, ok, err := wsstate.LoadBase(f.appDir)
	if err != nil {
		t.Fatalf("LoadBase: %v", err)
	}
	return ok
}

func (f *flow) note(t *testing.T) (wsstate.SyncNote, bool) {
	t.Helper()
	note, ok, err := wsstate.LoadSyncNote(f.gitDir)
	if err != nil {
		t.Fatalf("LoadSyncNote: %v", err)
	}
	return note, ok
}

// docsSnapshot reads the docs worktree as path → content.
func (f *flow) docsSnapshot(t *testing.T) map[string]string {
	t.Helper()

	root := filepath.Join(f.appDir, docsDir)
	files := map[string]string{}
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return files
	}
	err := filepath.WalkDir(root, func(full string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		content, err := os.ReadFile(full)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, full)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(relative)] = string(content)
		return nil
	})
	if err != nil {
		t.Fatalf("read docs worktree: %v", err)
	}
	return files
}

func (f *flow) status(t *testing.T) string {
	t.Helper()
	return strings.TrimSpace(gitRun(t, f.appDir, "status", "--porcelain"))
}

// commitsSince counts commits added on top of oid.
func (f *flow) commitsSince(t *testing.T, oid string) int {
	t.Helper()
	out := strings.TrimSpace(gitRun(t, f.appDir, "rev-list", "--count", oid+"..HEAD"))
	count := 0
	if _, err := fmt.Sscanf(out, "%d", &count); err != nil {
		t.Fatalf("parse commit count %q: %v", out, err)
	}
	return count
}

// changedPaths lists the paths one commit touched.
func (f *flow) changedPaths(t *testing.T, commit string) []string {
	t.Helper()
	paths := strings.Fields(gitRun(t, f.appDir, "diff-tree", "--no-commit-id", "--name-only", "-r", commit))
	sort.Strings(paths)
	return paths
}

// breakOrigin points the private clone at a path that does not exist,
// which is the offline state as the clone experiences it.
func (f *flow) breakOrigin(t *testing.T) {
	t.Helper()
	gitRun(t, f.store.Dir(), "remote", "set-url", "origin", filepath.Join(t.TempDir(), "vanished.git"))
}

// --- plain git helpers -------------------------------------------------

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	res, err := gitx.New(dir).Run(context.Background(), args...)
	if err != nil {
		t.Fatalf("git %s in %s: %v", strings.Join(args, " "), dir, err)
	}
	return string(res.Stdout)
}

func gitLine(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := gitx.New(dir).Line(context.Background(), args...)
	if err != nil {
		t.Fatalf("git %s in %s: %v", strings.Join(args, " "), dir, err)
	}
	return out
}

func newRepo(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("create %s: %v", dir, err)
	}
	gitRun(t, dir, "init", "--quiet", "-b", "main")
	return dir
}

// newEmptyOrigin creates a bare canonical repository with no commits at
// all — no branch exists yet, so canonical Head reports ErrEmptyBranch.
func newEmptyOrigin(t *testing.T) string {
	t.Helper()

	origin := filepath.Join(t.TempDir(), "origin.git")
	if err := os.MkdirAll(origin, 0755); err != nil {
		t.Fatalf("create %s: %v", origin, err)
	}
	gitRun(t, origin, "init", "--bare", "--quiet", "-b", "main")
	return origin
}

// newOrigin creates a bare canonical repository seeded with files.
func newOrigin(t *testing.T, files map[string]string) string {
	t.Helper()

	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	if err := os.MkdirAll(origin, 0755); err != nil {
		t.Fatalf("create %s: %v", origin, err)
	}
	gitRun(t, origin, "init", "--bare", "--quiet", "-b", "main")

	seed := filepath.Join(root, "seed")
	if err := os.MkdirAll(seed, 0755); err != nil {
		t.Fatalf("create %s: %v", seed, err)
	}
	gitRun(t, seed, "init", "--quiet", "-b", "main")
	replaceTree(t, seed, files)
	gitRun(t, seed, "add", "-A", "--", ".")
	gitRun(t, seed, "commit", "--quiet", "-m", "canonical: seed")
	gitRun(t, seed, "push", "--quiet", origin, "main")

	return origin
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatalf("create directory for %s: %v", full, err)
	}
	if err := os.WriteFile(full, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", full, err)
	}
}

// replaceTree makes dir hold exactly files, so an omitted path is a
// deletion. The .git directory is left alone.
func replaceTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()

	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("create %s: %v", dir, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	for _, entry := range entries {
		if entry.Name() == ".git" {
			continue
		}
		if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
			t.Fatalf("clear %s: %v", entry.Name(), err)
		}
	}
	for name, content := range files {
		writeFile(t, dir, name, content)
	}
}

// hunkFile builds a file with three widely separated regions. The gap
// is 20 lines because git coalesces conflict regions that sit closer
// than about 13 lines of common context — the P1 empirical finding that
// makes multi-hunk conflicts reproducible.
func hunkFile(first, second, third string) string {
	gap := make([]string, 20)
	for i := range gap {
		gap[i] = fmt.Sprintf("context line %02d", i)
	}
	spacer := strings.Join(gap, "\n")
	return first + "\n" + spacer + "\n" + second + "\n" + spacer + "\n" + third + "\n"
}

// countMarkerStarts counts conflict regions in merged content.
func countMarkerStarts(content string) int {
	count := 0
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "<<<<<<< ") {
			count++
		}
	}
	return count
}

// requireDocs asserts the docs worktree holds exactly want.
func requireDocs(t *testing.T, f *flow, want map[string]string) {
	t.Helper()

	got := f.docsSnapshot(t)
	if len(got) != len(want) {
		t.Fatalf("docs hold %v, want %v", sortedKeys(got), sortedKeys(want))
	}
	for name, content := range want {
		if got[name] != content {
			t.Fatalf("docs/%s = %q, want %q", name, got[name], content)
		}
	}
}

func sortedKeys(files map[string]string) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
