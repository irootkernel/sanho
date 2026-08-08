// Package e2e is the sanho v0.2 scenario and guidance-closure suite
// (AGENTS.md testing rules).
//
// It drives the built binary as a black box, through real `git` against
// real repositories on disk. Nothing here is mocked below the git
// boundary, which is the testing contract applied to the outermost layer: every
// assertion is about what a user or an agent would observe.
//
// The suite has three parts.
//
//	closure_test.go     one fixture per catalog scenario: reach the
//	                    advising state, prove the message appears, run
//	                    the command it names, require success (D3).
//	scenario_test.go    the audit's S-matrix rewritten for v0.2
//	                    semantics — onboarding, propagation, conflicts,
//	                    offline, amend, branch switching, clean, file
//	                    fidelity, JSON.
//	concurrency_test.go process-level races: the registry flock and two
//	                    workspaces publishing into one canonical.
//
// The e2e suite spawns processes rather than running code in-process, so
// it runs without `-race`; the in-process suites carry that (Makefile
// `test-unit`, `test-int`).
package e2e

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// cliBinary is the built binary under test, from SANHO_CLI_BINARY.
var cliBinary string

func TestMain(m *testing.M) {
	cliBinary = os.Getenv("SANHO_CLI_BINARY")
	if cliBinary == "" {
		// A stale or absent binary would silently test the wrong thing,
		// so the suite refuses to guess where one might be.
		println("SANHO_CLI_BINARY is not set; build the CLI first (make cli-build) and point SANHO_CLI_BINARY at bin/sanho")
		os.Exit(0)
	}
	if _, err := os.Stat(cliBinary); err != nil {
		println("SANHO_CLI_BINARY does not exist:", cliBinary)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// projectName is the project every fixture registers. Scenarios that
// need a second project name it explicitly.
const projectName = "product"

// actorEmail is the identity recorded on canonical commits.
const actorEmail = "author@example.test"

// world is one isolated universe: a bare canonical docs repository, a
// private SANHO_HOME, and the `sanho` shim the installed hooks invoke.
//
// A world can hold several workspaces. That is the point: propagation,
// concurrency and the sibling table are all facts about *two* checkouts
// sharing one canonical repository and one registry, which is exactly
// how a team uses sanho on one machine.
type world struct {
	t *testing.T
	// root is the temp directory everything lives under.
	root string
	// origin is the bare canonical docs repository.
	origin string
	// home is this world's SANHO_HOME.
	home string
	// binDir holds the `sanho` shim and the non-interactive editor.
	binDir string
	// editor is the GIT_EDITOR script, so an advised `git commit` with
	// no -m can be run verbatim.
	editor string
}

// newWorld seeds a canonical repository with docsFiles (nil means an
// empty canonical with no commits at all).
func newWorld(t *testing.T, docsFiles map[string]string) *world {
	t.Helper()

	root := resolvePath(t, t.TempDir())
	w := &world{
		t:      t,
		root:   root,
		origin: filepath.Join(root, "origin.git"),
		home:   filepath.Join(root, "sanho-home"),
		binDir: filepath.Join(root, "bin"),
	}
	w.editor = filepath.Join(w.binDir, "sanho-e2e-editor")

	for _, dir := range []string{w.origin, w.home, w.binDir} {
		mkdirAll(t, dir)
	}
	w.installShims()

	runGit(t, root, w.env(), "init", "--quiet", "-b", "main", "--bare", w.origin)
	if docsFiles != nil {
		w.seedCanonical(docsFiles)
	}
	return w
}

// installShims writes the two executables the fixtures rely on.
//
// The `sanho` shim is what makes real `git commit` and `git push` run
// the code under test: the installed hook lines invoke `sanho` by name
// (the hook contract). The editor shim is what lets the closure suite run the
// advised `git add docs/ && git commit` verbatim — with no -m, git needs
// an editor, and a test that added -m would not be running the advised
// command any more.
func (w *world) installShims() {
	w.t.Helper()

	shim := filepath.Join(w.binDir, "sanho")
	writeExecutable(w.t, shim, "#!/bin/sh\nexec "+cliBinary+" \"$@\"\n")

	// Prepend a subject line; git strips the comment block that follows.
	writeExecutable(w.t, w.editor,
		"#!/bin/sh\n"+
			"{ printf 'docs: resolve\\n\\n'; cat \"$1\"; } > \"$1.e2e\"\n"+
			"mv \"$1.e2e\" \"$1\"\n")
}

// env is the environment every command in this world runs with.
func (w *world) env() []string {
	return append(os.Environ(),
		"SANHO_HOME="+w.home,
		"PATH="+w.binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_SYSTEM="+os.DevNull,
		"GIT_AUTHOR_NAME=Test Author",
		"GIT_AUTHOR_EMAIL="+actorEmail,
		"GIT_COMMITTER_NAME=Test Committer",
		"GIT_COMMITTER_EMAIL=committer@example.test",
		"GIT_EDITOR="+w.editor,
		"EDITOR="+w.editor,
		// Publication and hooks must never wait on a credential prompt.
		"GIT_TERMINAL_PROMPT=0",
		// GIT_TRACE off: the concurrency suite reads exit codes and
		// messages, and trace output on stderr would drown them.
		"GIT_TRACE=0",
	)
}

// seedCanonical publishes an initial commit into the bare origin.
func (w *world) seedCanonical(files map[string]string) {
	w.t.Helper()

	seed := filepath.Join(w.root, "canonical-seed")
	mkdirAll(w.t, seed)
	runGit(w.t, seed, w.env(), "init", "--quiet", "-b", "main")
	for name, content := range files {
		writeFile(w.t, filepath.Join(seed, filepath.FromSlash(name)), content)
	}
	runGit(w.t, seed, w.env(), "add", "-A")
	runGit(w.t, seed, w.env(), "commit", "-m", "canonical: seed")
	runGit(w.t, seed, w.env(), "push", "--quiet", w.origin, "main")
}

// advanceCanonical lands one commit on origin out of band, the way
// another team member's push would. files replaces canonical's whole
// content, so an omitted path is a deletion.
func (w *world) advanceCanonical(files map[string]string, message string) string {
	w.t.Helper()
	return w.rewriteCanonical(files, message, false)
}

// rewriteCanonical is advanceCanonical plus the publication contract shape: with
// orphan set, the new commit has no parent and is force-pushed, so every
// previously published commit becomes unreachable.
func (w *world) rewriteCanonical(files map[string]string, message string, orphan bool) string {
	w.t.Helper()

	work := filepath.Join(w.t.TempDir(), "upstream")
	mkdirAll(w.t, work)
	runGit(w.t, work, w.env(), "clone", "--quiet", w.origin, ".")
	if orphan {
		runGit(w.t, work, w.env(), "checkout", "--quiet", "--orphan", "rewritten")
	}
	replaceContent(w.t, work, files)
	runGit(w.t, work, w.env(), "add", "-A")
	runGit(w.t, work, w.env(), "commit", "-m", message)
	if orphan {
		runGit(w.t, work, w.env(), "push", "--quiet", "--force", "origin", "HEAD:main")
	} else {
		runGit(w.t, work, w.env(), "push", "--quiet", "origin", "HEAD:main")
	}
	return strings.TrimSpace(runGit(w.t, work, w.env(), "rev-parse", "HEAD").stdout)
}

// canonicalHead reads origin's published head directly, so assertions
// never depend on any clone being fresh.
func (w *world) canonicalHead() string {
	w.t.Helper()
	return strings.TrimSpace(w.git(w.origin, "rev-parse", "refs/heads/main").stdout)
}

func (w *world) canonicalFile(rev, path string) string {
	w.t.Helper()
	return w.git(w.origin, "show", rev+":"+path).stdout
}

func (w *world) canonicalPaths(rev string) []string {
	w.t.Helper()
	var paths []string
	for _, line := range strings.Split(w.git(w.origin, "ls-tree", "-r", "--name-only", rev).stdout, "\n") {
		if strings.TrimSpace(line) != "" {
			paths = append(paths, strings.TrimSpace(line))
		}
	}
	sort.Strings(paths)
	return paths
}

func (w *world) git(dir string, args ...string) result {
	w.t.Helper()
	return runGit(w.t, dir, w.env(), args...)
}

// --- workspaces --------------------------------------------------------

// workspace is one application repository bound to the world's canonical
// repository. Each has its own code remote: pushing the app somewhere is
// only what fires the pre-push hook, and separate remotes keep two
// workspaces from colliding on app history they do not share.
type workspace struct {
	w *world
	// name identifies the checkout in messages and directory names.
	name string
	// dir is the application repository's worktree root.
	dir string
	// codeOrigin is this workspace's own bare app remote.
	codeOrigin string
	// home overrides the world's SANHO_HOME when a scenario models a
	// different machine. Empty means the world's shared registry.
	home string
}

// newWorkspace creates an application repository with a README, a code
// remote, and `main` already tracking it — so a bare `git push`, which
// is what several messages advise, is a runnable command.
func (w *world) newWorkspace(name string) *workspace {
	w.t.Helper()

	ws := &workspace{
		w:          w,
		name:       name,
		dir:        filepath.Join(w.root, name),
		codeOrigin: filepath.Join(w.root, name+"-code.git"),
	}
	mkdirAll(w.t, ws.dir)
	runGit(w.t, w.root, w.env(), "init", "--quiet", "-b", "main", "--bare", ws.codeOrigin)
	runGit(w.t, ws.dir, w.env(), "init", "--quiet", "-b", "main")
	ws.git("remote", "add", "origin", ws.codeOrigin)

	writeFile(w.t, filepath.Join(ws.dir, "README.md"), "readme for "+name+"\n")
	ws.git("add", "-A")
	ws.git("commit", "-m", "chore: seed repository")
	ws.git("push", "--quiet", "-u", "origin", "main")

	ws.dir = resolvePath(w.t, ws.dir)
	return ws
}

// initWorkspace runs `sanho init` against this world's canonical origin.
func (ws *workspace) initWorkspace(extra ...string) result {
	ws.w.t.Helper()
	args := append([]string{
		"init",
		"--project", projectName,
		"--docs-repo-url", ws.w.origin,
		"--actor-email", actorEmail,
	}, extra...)
	return ws.sanho(args...)
}

// initAndAdopt is initWorkspace followed by the commit init tells the
// user to make: fresh mode stages canonical's docs and leaves that commit
// to the user.
func (ws *workspace) initAndAdopt(extra ...string) *workspace {
	ws.w.t.Helper()
	ws.initWorkspace(extra...)
	// `git add .gitignore && git commit` is what init advises, and doing
	// it here is not decoration: leaving .gitignore uncommitted means any
	// checkout back past this commit deletes it, after which `git add -A`
	// commits `.sanho.json` into a branch and a later checkout deletes
	// the workspace out from under the hooks.
	ws.git("add", ".gitignore")
	ws.git("commit", "-m", "docs: adopt canonical docs")
	return ws
}

// setup is newWorkspace + initAndAdopt, the starting point of most
// scenarios.
func (w *world) setup(name string) *workspace {
	w.t.Helper()
	return w.newWorkspace(name).initAndAdopt()
}

// setupIsolated gives one workspace its own machine-local registry before
// initialization. The canonical origin remains shared, which is the boundary
// the publication concurrency scenarios exercise.
func (w *world) setupIsolated(name string) *workspace {
	w.t.Helper()
	ws := w.newWorkspace(name)
	ws.home = filepath.Join(w.root, name+"-sanho-home")
	mkdirAll(w.t, ws.home)
	return ws.initAndAdopt()
}

func (ws *workspace) env() []string {
	env := ws.w.env()
	if ws.home == "" {
		return env
	}
	for i, value := range env {
		if strings.HasPrefix(value, "SANHO_HOME=") {
			env[i] = "SANHO_HOME=" + ws.home
			return env
		}
	}
	return append(env, "SANHO_HOME="+ws.home)
}

func (ws *workspace) path(parts ...string) string {
	return filepath.Join(append([]string{ws.dir}, parts...)...)
}

func (ws *workspace) docsPath(parts ...string) string {
	return filepath.Join(append([]string{ws.dir, "docs"}, parts...)...)
}

func (ws *workspace) cloneDir() string { return ws.path(".git", "sanho", "canonical") }

func (ws *workspace) hookPath(name string) string { return ws.path(".git", "hooks", name) }

func (ws *workspace) basePath() string { return ws.path(".sanho_base.json") }

// run invokes the CLI in this workspace and returns whatever it did.
func (ws *workspace) run(args ...string) result {
	ws.w.t.Helper()
	return execute(ws.w.t, ws.dir, ws.env(), cliBinary, args...)
}

// sanho invokes the CLI and fails the test on a non-zero exit.
func (ws *workspace) sanho(args ...string) result {
	ws.w.t.Helper()
	res := ws.run(args...)
	if res.exitCode != 0 {
		ws.w.t.Fatalf("[%s] sanho %s failed with exit %d\nstdout:\n%s\nstderr:\n%s",
			ws.name, strings.Join(args, " "), res.exitCode, res.stdout, res.stderr)
	}
	return res
}

// git runs git in this workspace and fails the test on a non-zero exit.
func (ws *workspace) git(args ...string) result {
	ws.w.t.Helper()
	return runGit(ws.w.t, ws.dir, ws.env(), args...)
}

// gitExit runs git and returns whatever it did, including a failure.
func (ws *workspace) gitExit(args ...string) result {
	ws.w.t.Helper()
	return execute(ws.w.t, ws.dir, ws.env(), "git", args...)
}

// shell runs a command line verbatim through /bin/sh. It is how the
// closure suite executes advised commands exactly as printed, including
// the `&&` compounds that the guidance contract template 2 names.
func (ws *workspace) shell(command string) result {
	ws.w.t.Helper()
	return execute(ws.w.t, ws.dir, ws.env(), "/bin/sh", "-c", command)
}

// push sends the current branch to the app's own remote, which is what
// fires the pre-push hook.
func (ws *workspace) push() result {
	ws.w.t.Helper()
	return ws.gitExit("push", "--quiet", "origin", "main")
}

// commitDocs writes docs files and commits them.
func (ws *workspace) commitDocs(message string, files map[string]string) result {
	ws.w.t.Helper()
	ws.writeDocs(files)
	ws.git("add", "-A")
	return ws.git("commit", "-m", message)
}

func (ws *workspace) writeDocs(files map[string]string) {
	ws.w.t.Helper()
	for name, content := range files {
		writeFile(ws.w.t, ws.docsPath(filepath.FromSlash(name)), content)
	}
}

func (ws *workspace) readDocs(name string) string {
	ws.w.t.Helper()
	return readFile(ws.w.t, ws.docsPath(filepath.FromSlash(name)))
}

func (ws *workspace) headSubject() string {
	ws.w.t.Helper()
	return strings.TrimSpace(ws.git("log", "-1", "--format=%s").stdout)
}

func (ws *workspace) headMessage() string {
	ws.w.t.Helper()
	return ws.git("log", "-1", "--format=%B").stdout
}

// staleFetchMarker backdates the clone's last-fetch stamp, which is the
// only input to the private-clone contract data-age line.
func (ws *workspace) staleFetchMarker(stamp string) {
	ws.w.t.Helper()
	writeFile(ws.w.t, filepath.Join(ws.cloneDir(), "sanho-last-fetch"), stamp+"\n")
}

func (ws *workspace) removeFetchMarker() {
	ws.w.t.Helper()
	if err := os.Remove(filepath.Join(ws.cloneDir(), "sanho-last-fetch")); err != nil {
		ws.w.t.Fatalf("remove the fetch marker: %v", err)
	}
}

// takeCanonicalOffline moves the canonical repository out from under
// its own URL, which is the offline state as every workspace in this
// world experiences it.
//
// Rewriting the *clone's* origin URL would not do: `canonical.Ensure`
// reconciles the clone's remote back to the workspace config on every
// write path (the private-clone contract), so a doctored clone silently repairs itself. Moving
// the repository leaves the configured URL intact and simply makes it
// unreachable — which is what being offline is.
func (w *world) takeCanonicalOffline() {
	w.t.Helper()
	if err := os.Rename(w.origin, w.origin+".offline"); err != nil {
		w.t.Fatalf("take canonical offline: %v", err)
	}
}

func (w *world) bringCanonicalOnline() {
	w.t.Helper()
	if err := os.Rename(w.origin+".offline", w.origin); err != nil {
		w.t.Fatalf("bring canonical online: %v", err)
	}
}

// freezeClone makes the private clone read-only for the rest of the
// test. Reads (rev-parse, rev-list, log) still work; anything that must
// write an object or a ref fails — which is the one state in which the
// the commit-hook contract freshness prediction cannot be computed.
func (ws *workspace) freezeClone() {
	t := ws.w.t
	t.Helper()

	chmodTree(t, ws.cloneDir(), 0o500, 0o400)
	t.Cleanup(func() { chmodTree(t, ws.cloneDir(), 0o700, 0o600) })
}

// --- process execution -------------------------------------------------

// result is one command invocation's outcome.
type result struct {
	stdout   string
	stderr   string
	exitCode int
}

// combined is stdout and stderr together, for assertions that do not
// care which stream a line arrived on.
func (r result) combined() string { return r.stdout + r.stderr }

func execute(t *testing.T, dir string, env []string, program string, args ...string) result {
	t.Helper()

	res, err := tryExecute(dir, env, program, args...)
	if err != nil {
		t.Fatalf("run %s %s in %s: %v", program, strings.Join(args, " "), dir, err)
	}
	return res
}

// tryExecute is execute without the testing.T. The concurrency suite
// runs commands from goroutines, where calling t.Fatalf is not allowed,
// so the failure to *start* a process is returned rather than reported.
// A non-zero exit is not an error here — it is the result.
func tryExecute(dir string, env []string, program string, args ...string) (result, error) {
	cmd := exec.Command(program, args...)
	cmd.Dir = dir
	cmd.Env = env

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	code := 0
	if err := cmd.Run(); err != nil {
		exitErr := &exec.ExitError{}
		if !asExitError(err, &exitErr) {
			return result{}, err
		}
		code = exitErr.ExitCode()
	}
	return result{stdout: stdout.String(), stderr: stderr.String(), exitCode: code}, nil
}

func asExitError(err error, target **exec.ExitError) bool {
	exitErr, ok := err.(*exec.ExitError)
	if ok {
		*target = exitErr
	}
	return ok
}

func runGit(t *testing.T, dir string, env []string, args ...string) result {
	t.Helper()

	res := execute(t, dir, env, "git", args...)
	if res.exitCode != 0 {
		t.Fatalf("git %s in %s failed with exit %d\nstdout:\n%s\nstderr:\n%s",
			strings.Join(args, " "), dir, res.exitCode, res.stdout, res.stderr)
	}
	return res
}

// --- filesystem --------------------------------------------------------

func resolvePath(t *testing.T, path string) string {
	t.Helper()
	// macOS puts t.TempDir() under /var, a symlink to /private/var. sanho
	// resolves symlinks when it records absolute paths, so the fixture
	// resolves them too — otherwise every path comparison against the
	// registry would silently never match.
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("resolve %s: %v", path, err)
	}
	return resolved
}

func mkdirAll(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create %s: %v", dir, err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	mkdirAll(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	writeFile(t, path, content)
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatalf("chmod %s: %v", path, err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func fileExists(t *testing.T, path string) bool {
	t.Helper()
	switch _, err := os.Lstat(path); {
	case err == nil:
		return true
	case os.IsNotExist(err):
		return false
	default:
		t.Fatalf("stat %s: %v", path, err)
		return false
	}
}

func removeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove %s: %v", path, err)
	}
}

// replaceContent makes dir hold exactly files, leaving .git alone.
func replaceContent(t *testing.T, dir string, files map[string]string) {
	t.Helper()

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
		writeFile(t, filepath.Join(dir, filepath.FromSlash(name)), content)
	}
}

// chmodTree applies dirMode to directories and fileMode to files under
// root, deepest last on the way in so a directory stays traversable
// while its children are still being changed.
func chmodTree(t *testing.T, root string, dirMode, fileMode fs.FileMode) {
	t.Helper()

	var paths []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	// Files first, then directories deepest-first: a directory loses its
	// write bit only once nothing inside it still needs changing.
	sort.Slice(paths, func(i, j int) bool { return len(paths[i]) > len(paths[j]) })
	for _, path := range paths {
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		mode := fileMode
		if info.IsDir() {
			mode = dirMode
		}
		if err := os.Chmod(path, mode); err != nil {
			t.Fatalf("chmod %s: %v", path, err)
		}
	}
}

// --- tree fidelity -----------------------------------------------------

// treeEntry is one path's full identity: its mode bits, and either its
// content digest or its symlink target. Comparing these is what makes
// the `clean --dry-run` no-op check (audit M4) and the symlink/mode
// round-trip (audit H1) byte-level rather than approximate.
type treeEntry struct {
	mode   fs.FileMode
	digest string
	target string
}

func (e treeEntry) String() string {
	if e.target != "" {
		return fmt.Sprintf("symlink %04o -> %s", e.mode.Perm(), e.target)
	}
	return fmt.Sprintf("file %04o %s", e.mode.Perm(), e.digest)
}

// snapshotTree records every path under root, skipping the parts of
// `.git` that git itself churns on any read (index mtime cache, logs,
// FETCH_HEAD). `.git/sanho` and `.git/hooks` — everything sanho owns
// inside the git directory — are kept, which is what the testing contract's dry-run check
// is actually about.
func snapshotTree(t *testing.T, root string) map[string]treeEntry {
	t.Helper()

	entries := map[string]treeEntry{}
	if _, err := os.Lstat(root); os.IsNotExist(err) {
		return entries
	}

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		slashed := filepath.ToSlash(relative)
		if entry.IsDir() {
			if skipDirectory(slashed) {
				return filepath.SkipDir
			}
			return nil
		}
		recorded, recordErr := describePath(path, entry)
		if recordErr != nil {
			return recordErr
		}
		entries[slashed] = recorded
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", root, err)
	}
	return entries
}

// skipDirectory names the git-internal directories whose contents move
// on their own. `.git/sanho` and `.git/hooks` are deliberately not here.
func skipDirectory(relative string) bool {
	switch relative {
	case ".git/objects", ".git/logs", ".git/refs", ".git/info", ".git/branches":
		return true
	default:
		return false
	}
}

// volatileGitFile names the `.git` files git rewrites as a side effect
// of ordinary reads, which would make a byte-identity check report
// noise rather than a change of state.
var volatileGitFile = map[string]bool{
	".git/index":          true,
	".git/FETCH_HEAD":     true,
	".git/ORIG_HEAD":      true,
	".git/COMMIT_EDITMSG": true,
	".git/packed-refs":    true,
	".git/gc.log":         true,
}

func describePath(path string, entry fs.DirEntry) (treeEntry, error) {
	info, err := entry.Info()
	if err != nil {
		return treeEntry{}, err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		if err != nil {
			return treeEntry{}, err
		}
		return treeEntry{mode: info.Mode(), target: target}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return treeEntry{}, err
	}
	sum := sha256.Sum256(data)
	return treeEntry{mode: info.Mode(), digest: hex.EncodeToString(sum[:])}, nil
}

// requireSameTree reports every difference rather than the first, so a
// failing no-op check names everything that moved.
func requireSameTree(t *testing.T, label string, before, after map[string]treeEntry) {
	t.Helper()

	for name, want := range before {
		if volatileGitFile[name] {
			continue
		}
		got, ok := after[name]
		if !ok {
			t.Errorf("%s: %s disappeared", label, name)
			continue
		}
		if got != want {
			t.Errorf("%s: %s changed\nbefore: %s\nafter:  %s", label, name, want, got)
		}
	}
	for name := range after {
		if volatileGitFile[name] {
			continue
		}
		if _, ok := before[name]; !ok {
			t.Errorf("%s: %s appeared", label, name)
		}
	}
}

// treeDigest folds a snapshot into one hash, so a scenario can assert
// "nothing at all moved" in a single comparison as well as per path.
func treeDigest(entries map[string]treeEntry) string {
	names := make([]string, 0, len(entries))
	for name := range entries {
		if volatileGitFile[name] {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)

	hash := sha256.New()
	for _, name := range names {
		// hash.Hash never reports a write error; the assignment is for
		// errcheck's benefit, not for a failure that can happen.
		_, _ = fmt.Fprintf(hash, "%s\x00%s\x00", name, entries[name])
	}
	return hex.EncodeToString(hash.Sum(nil))
}

// --- assertions --------------------------------------------------------

func requireContains(t *testing.T, label, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Fatalf("%s: expected to contain %q, got:\n%s", label, needle, haystack)
	}
}

func requireNotContains(t *testing.T, label, haystack, needle string) {
	t.Helper()
	if strings.Contains(haystack, needle) {
		t.Fatalf("%s: expected NOT to contain %q, got:\n%s", label, needle, haystack)
	}
}

func requireEqual(t *testing.T, label, got, want string) {
	t.Helper()
	if got != want {
		t.Fatalf("%s = %q, want %q", label, got, want)
	}
}

func requireExit(t *testing.T, label string, res result, want int) {
	t.Helper()
	if res.exitCode != want {
		t.Fatalf("%s exited %d, want %d\nstdout:\n%s\nstderr:\n%s",
			label, res.exitCode, want, res.stdout, res.stderr)
	}
}
