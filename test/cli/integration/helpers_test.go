// Package integration drives the built `sanho` binary as a black box
// against real git repositories (AGENTS.md testing rules).
//
// Every test builds its own world: a bare canonical origin, an
// application repository, an isolated SANHO_HOME, and its own git
// identity. Nothing here reaches the developer's ~/.gitconfig or
// ~/.sanho, so a run is reproducible and leaves no trace.
//
// The suite is a smoke matrix, not the full one: it proves each command
// and hook does its job end to end, in the shapes the four principles
// turn on (offline commit succeeds, push publishes, conflicts route
// through sync, dry-run changes nothing). P5 rebuilds the exhaustive
// scenario matrix on top of it.
package integration

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
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

// world is one isolated fixture: a canonical origin, an app repository
// with sanho on PATH, and a private SANHO_HOME.
type world struct {
	t *testing.T
	// origin is the bare canonical docs repository.
	origin string
	// codeOrigin is the application repository's own remote. Pushing the
	// app there — rather than into the docs repository — is what a real
	// workspace does, and it keeps canonical history free of app commits.
	codeOrigin string
	// app is the application repository's worktree root.
	app string
	// home is this test's SANHO_HOME.
	home string
	// binDir holds a `sanho` shim for historical v0.1 hook fixtures.
	binDir string
}

// newWorld seeds a canonical repository with docsFiles (nil means an
// empty canonical with no commits at all) and an application repository
// with a README and no docs directory.
func newWorld(t *testing.T, docsFiles map[string]string) *world {
	t.Helper()

	root := t.TempDir()
	w := &world{
		t:          t,
		origin:     filepath.Join(root, "origin.git"),
		codeOrigin: filepath.Join(root, "code.git"),
		app:        filepath.Join(root, "app"),
		home:       filepath.Join(root, "sanho-home"),
		binDir:     filepath.Join(root, "bin"),
	}
	for _, dir := range []string{w.origin, w.codeOrigin, w.app, w.home, w.binDir} {
		mkdirAll(t, dir)
	}
	// macOS puts t.TempDir() under /var, a symlink to /private/var. sanho
	// resolves symlinks when it records absolute paths, so the fixture
	// resolves them too — otherwise every path comparison against the
	// registry would silently never match.
	w.app = resolvePath(t, w.app)

	w.gitInit(w.origin, "--bare")
	w.gitInit(w.codeOrigin, "--bare")
	if docsFiles != nil {
		w.seedCanonical(docsFiles)
	}

	w.gitInit(w.app)
	w.git(w.app, "remote", "add", "origin", w.codeOrigin)
	writeFile(t, filepath.Join(w.app, "README.md"), "readme\n")
	w.git(w.app, "add", "-A")
	w.git(w.app, "commit", "-m", "chore: seed repository")

	w.installShim()
	return w
}

// installShim puts a `sanho` executable on PATH that forwards to the
// binary under test. Current hooks bind the installing binary by
// absolute path; historical v0.1 hook fixtures still invoke this shim.
func (w *world) installShim() {
	w.t.Helper()

	shim := filepath.Join(w.binDir, "sanho")
	script := "#!/bin/sh\nexec " + cliBinary + " \"$@\"\n"
	writeFile(w.t, shim, script)
	if err := os.Chmod(shim, 0755); err != nil {
		w.t.Fatalf("chmod shim: %v", err)
	}
}

// env is the environment every command in this world runs with.
func (w *world) env() []string {
	return append(os.Environ(),
		"SANHO_HOME="+w.home,
		"PATH="+w.binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_SYSTEM="+os.DevNull,
		"GIT_AUTHOR_NAME=Test Author",
		"GIT_AUTHOR_EMAIL=author@example.test",
		"GIT_COMMITTER_NAME=Test Committer",
		"GIT_COMMITTER_EMAIL=committer@example.test",
	)
}

// result is one command invocation's outcome.
type result struct {
	stdout   string
	stderr   string
	exitCode int
}

// combined is stdout and stderr together, for assertions that do not
// care which stream a line arrived on.
func (r result) combined() string { return r.stdout + r.stderr }

// run invokes the CLI in dir.
func (w *world) run(dir string, args ...string) result {
	w.t.Helper()
	return w.exec(dir, cliBinary, args...)
}

// sanho invokes the CLI and fails the test on a non-zero exit.
func (w *world) sanho(dir string, args ...string) result {
	w.t.Helper()
	res := w.run(dir, args...)
	if res.exitCode != 0 {
		w.t.Fatalf("sanho %s failed with exit %d\nstdout:\n%s\nstderr:\n%s",
			strings.Join(args, " "), res.exitCode, res.stdout, res.stderr)
	}
	return res
}

// git runs git in dir and fails the test on a non-zero exit.
func (w *world) git(dir string, args ...string) result {
	w.t.Helper()
	res := w.gitExit(dir, args...)
	if res.exitCode != 0 {
		w.t.Fatalf("git %s in %s failed with exit %d\nstdout:\n%s\nstderr:\n%s",
			strings.Join(args, " "), dir, res.exitCode, res.stdout, res.stderr)
	}
	return res
}

// gitExit runs git and returns whatever it did, including a failure.
func (w *world) gitExit(dir string, args ...string) result {
	w.t.Helper()
	return w.exec(dir, "git", args...)
}

func (w *world) exec(dir, program string, args ...string) result {
	w.t.Helper()

	cmd := exec.Command(program, args...)
	cmd.Dir = dir
	cmd.Env = w.env()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if !asExitError(err, &exitErr) {
			w.t.Fatalf("run %s %s: %v", program, strings.Join(args, " "), err)
		}
		code = exitErr.ExitCode()
	}
	return result{stdout: stdout.String(), stderr: stderr.String(), exitCode: code}
}

func asExitError(err error, target **exec.ExitError) bool {
	if exitErr, ok := err.(*exec.ExitError); ok {
		*target = exitErr
		return true
	}
	return false
}

func (w *world) gitInit(dir string, extra ...string) {
	w.t.Helper()
	args := append([]string{"init", "--quiet", "-b", "main"}, extra...)
	w.git(dir, args...)
}

// seedCanonical publishes an initial commit into the bare origin.
func (w *world) seedCanonical(files map[string]string) {
	w.t.Helper()

	seed := filepath.Join(filepath.Dir(w.origin), "seed")
	mkdirAll(w.t, seed)
	w.gitInit(seed)
	for name, content := range files {
		writeFile(w.t, filepath.Join(seed, filepath.FromSlash(name)), content)
	}
	w.git(seed, "add", "-A")
	w.git(seed, "commit", "-m", "canonical: seed")
	w.git(seed, "push", "--quiet", w.origin, "main")
}

// advanceCanonical lands one commit on origin out of band, the way
// another workspace's push would. files replaces canonical's whole
// content, so an omitted path is a deletion.
func (w *world) advanceCanonical(files map[string]string, message string) string {
	w.t.Helper()

	work := filepath.Join(w.t.TempDir(), "upstream")
	mkdirAll(w.t, work)
	w.git(work, "clone", "--quiet", w.origin, ".")
	replaceContent(w.t, work, files)
	w.git(work, "add", "-A")
	w.git(work, "commit", "-m", message)
	w.git(work, "push", "--quiet", "origin", "HEAD:main")

	return strings.TrimSpace(w.git(work, "rev-parse", "HEAD").stdout)
}

// rewriteCanonical replaces canonical history wholesale: an orphan
// commit, force-pushed, so nothing previously published stays reachable.
// It is the state `--rebase-onto` exists for.
func (w *world) rewriteCanonical(files map[string]string, message string) string {
	w.t.Helper()

	work := filepath.Join(w.t.TempDir(), "rewrite")
	mkdirAll(w.t, work)
	w.git(work, "clone", "--quiet", w.origin, ".")
	w.git(work, "checkout", "--quiet", "--orphan", "rewritten")
	replaceContent(w.t, work, files)
	w.git(work, "add", "-A")
	w.git(work, "commit", "-m", message)
	w.git(work, "push", "--quiet", "--force", "origin", "HEAD:main")

	return strings.TrimSpace(w.git(work, "rev-parse", "HEAD").stdout)
}

// initWorkspace runs `sanho init` with this world's canonical origin.
func (w *world) initWorkspace(extra ...string) result {
	w.t.Helper()
	args := append([]string{
		"init",
		"--project", "product",
		"--docs-repo-url", w.origin,
		"--actor-email", "author@example.test",
	}, extra...)
	return w.sanho(w.app, args...)
}

// initAndAdoptDocs is initWorkspace followed by the commit init tells
// the user to make: fresh mode stages canonical's docs, and P3 leaves
// the commit to the user.
func (w *world) initAndAdoptDocs(extra ...string) result {
	w.t.Helper()
	out := w.initWorkspace(extra...)
	w.git(w.app, "commit", "-m", "docs: adopt canonical docs")
	return out
}

// push sends the current branch to the app's own remote, which is what
// fires the pre-push hook.
func (w *world) push() result {
	w.t.Helper()
	return w.gitExit(w.app, "push", "--quiet", "origin", "main")
}

// commitDocs writes docs files in the app repository and commits them.
func (w *world) commitDocs(message string, files map[string]string) result {
	w.t.Helper()
	for name, content := range files {
		writeFile(w.t, filepath.Join(w.app, "docs", filepath.FromSlash(name)), content)
	}
	w.git(w.app, "add", "-A")
	return w.git(w.app, "commit", "-m", message)
}

// canonicalHead reads origin's published head directly, so assertions
// never depend on any clone being fresh.
func (w *world) canonicalHead() string {
	w.t.Helper()
	return strings.TrimSpace(w.git(w.origin, "rev-parse", "refs/heads/main").stdout)
}

func (w *world) canonicalSubject(rev string) string {
	w.t.Helper()
	return strings.TrimSpace(w.git(w.origin, "log", "-1", "--format=%s", rev).stdout)
}

func (w *world) canonicalFile(rev, path string) string {
	w.t.Helper()
	return w.git(w.origin, "show", rev+":"+path).stdout
}

func (w *world) headMessage() string {
	w.t.Helper()
	return w.git(w.app, "log", "-1", "--format=%B").stdout
}

func (w *world) appPath(parts ...string) string {
	return filepath.Join(append([]string{w.app}, parts...)...)
}

func (w *world) hookPath(name string) string {
	return filepath.Join(w.app, ".git", "hooks", name)
}

// --- filesystem helpers ------------------------------------------------

func resolvePath(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("resolve %s: %v", path, err)
	}
	return resolved
}

func mkdirAll(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("create %s: %v", dir, err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	mkdirAll(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
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
	_, err := os.Stat(path)
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	t.Fatalf("stat %s: %v", path, err)
	return false
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

// snapshotTree records every file's path and content under root, for the
// byte-for-byte no-op assertions.
func snapshotTree(t *testing.T, root string) map[string]string {
	t.Helper()

	files := map[string]string{}
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return files
	}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !entry.Type().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(relative)] = string(data)
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", root, err)
	}
	return files
}

func requireSameTree(t *testing.T, label string, before, after map[string]string) {
	t.Helper()

	for name, content := range before {
		got, ok := after[name]
		if !ok {
			t.Errorf("%s: %s disappeared", label, name)
			continue
		}
		if got != content {
			t.Errorf("%s: %s changed\nbefore:\n%s\nafter:\n%s", label, name, content, got)
		}
	}
	for name := range after {
		if _, ok := before[name]; !ok {
			t.Errorf("%s: %s appeared", label, name)
		}
	}
}

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
