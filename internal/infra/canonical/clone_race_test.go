package canonical

// M3, and the race the first cut of its fix still had.
//
// A lock around the CONSTRUCTION is not enough, because the OBSERVATION
// is unsynchronized: Ensure stats the clone path before taking the lock,
// so a second process could see the directory the winner had just
// created and take the "already exists" branch while it was still being
// built.
//
// What it opened then was worse than a half-built clone. The clone lives
// inside the application repository's git directory, so `git rev-parse`
// run in a directory that is not yet a repository walks UP and resolves
// to the APPLICATION repository — and every call the Store made from
// there targeted it. `reconcileExisting` read the app's own
// `remote.origin.url`, found it different from the canonical URL, and
// ran `git remote set-url`; by the time that landed the winner's
// `git init --bare` had completed, so it failed with "No such remote
// 'origin'" and the loser's cleanup removed the winner's clone.
//
// Two things close it, and both are pinned here: the clone is built out
// of place and renamed in, so the final path is never partial; and Open
// refuses a directory git resolves to somebody else's repository.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// TestOpenRefusesADirectoryThatResolvesToAnotherRepository is the root
// cause, pinned deterministically.
//
// It needs no race at all: an empty `…/.git/sanho/canonical` inside a
// real repository is exactly the state the loser observed, and opening
// it used to succeed — against the application repository.
func TestOpenRefusesADirectoryThatResolvesToAnotherRepository(t *testing.T) {
	app := newWorkRepo(t, "app")
	gitRun(t, app, "commit", "--quiet", "--allow-empty", "-m", "seed")
	gitRun(t, app, "remote", "add", "origin", "/somewhere/the/user/owns.git")

	appGitDir := filepath.Join(app, ".git")
	cloneDir := CloneDir(appGitDir)
	if err := os.MkdirAll(cloneDir, 0700); err != nil {
		t.Fatalf("create the clone directory: %v", err)
	}

	// Sanity: this really is the confusing state — git resolves the empty
	// directory to the application repository.
	resolved := gitLine(t, cloneDir, "rev-parse", "--absolute-git-dir")
	if samePath(resolved, cloneDir) {
		t.Fatalf("the fixture is not reproducing upward resolution: %s resolved to itself", cloneDir)
	}

	store, err := Open(appGitDir, "/the/canonical/docs.git")
	if err == nil {
		t.Fatalf("Open succeeded on a directory that is not its own repository (bound to %s)", store.dir)
	}
	if !strings.Contains(err.Error(), "not a git repository") {
		t.Fatalf("error = %v, want it to name the directory as not a repository of its own", err)
	}

	// And the application repository's own remote is untouched, which is
	// what the old binding went on to rewrite.
	if got := gitLine(t, app, "config", "--get", "remote.origin.url"); got != "/somewhere/the/user/owns.git" {
		t.Fatalf("the application repository's origin is now %q", got)
	}
}

// TestEnsureNeverExposesAPartialClone hammers the observation side.
//
// One goroutine builds the clone; another watches the path continuously
// and requires every observation to be one of exactly two states —
// absent, or a complete clone with `origin` set and the publication
// branch resolved. The intermediate state the old construction went
// through (an empty directory, then a bare repository with no remote,
// then one with a remote but no fetch) must not be reachable.
//
// This is the deterministic form of the failure the e2e race test only
// reproduced by luck: it does not depend on winning a timing window,
// because it looks thousands of times over the whole construction.
func TestEnsureNeverExposesAPartialClone(t *testing.T) {
	origin := newOrigin(t, "main", map[string]entry{"a.md": text("a\n")})
	app := newWorkRepo(t, "app")
	gitRun(t, app, "commit", "--quiet", "--allow-empty", "-m", "seed")
	gitRun(t, app, "remote", "add", "origin", "/somewhere/the/user/owns.git")

	appGitDir := filepath.Join(app, ".git")
	cloneDir := CloneDir(appGitDir)

	done := make(chan struct{})
	var observations int
	var problems []string
	var mu sync.Mutex

	var watcher sync.WaitGroup
	watcher.Add(1)
	go func() {
		defer watcher.Done()
		for {
			select {
			case <-done:
				return
			default:
			}
			info, err := os.Stat(cloneDir)
			if err != nil || !info.IsDir() {
				continue // absent: one of the two legal states
			}
			mu.Lock()
			observations++
			if problem := describePartialClone(cloneDir); problem != "" {
				problems = append(problems, problem)
			}
			mu.Unlock()
		}
	}()

	store, err := Ensure(context.Background(), appGitDir, origin)
	close(done)
	watcher.Wait()

	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if store.Branch() != "main" {
		t.Fatalf("branch = %q, want main", store.Branch())
	}

	mu.Lock()
	defer mu.Unlock()
	if observations == 0 {
		t.Fatal("the watcher never saw the clone directory; the test proves nothing")
	}
	if len(problems) > 0 {
		t.Fatalf("the clone directory was observable in %d partial state(s) out of %d looks; first: %s",
			len(problems), observations, problems[0])
	}
	t.Logf("%d observations of the clone directory, all complete", observations)
}

// describePartialClone returns a non-empty description when the clone
// directory exists but is not a finished clone. The three checks are the
// three states the old in-place construction passed through.
func describePartialClone(dir string) string {
	if _, err := os.Stat(filepath.Join(dir, "HEAD")); err != nil {
		return "the directory exists but is not a git repository yet"
	}
	config, err := os.ReadFile(filepath.Join(dir, "config"))
	if err != nil {
		return "the repository has no config yet"
	}
	text := string(config)
	if !strings.Contains(text, `[remote "`+remoteName+`"]`) {
		return "the repository has no " + remoteName + " remote yet"
	}
	if !strings.Contains(text, branchConfigKey[len("sanho."):]) {
		return "the publication branch has not been resolved yet"
	}
	return ""
}

// TestEnsureAdoptsAConcurrentWinnersClone is the lock's own job: a
// waiter must adopt the clone the winner built instead of building a
// second one over it.
func TestEnsureAdoptsAConcurrentWinnersClone(t *testing.T) {
	const racers = 4

	origin := newOrigin(t, "main", map[string]entry{"a.md": text("a\n")})
	app := newWorkRepo(t, "app")
	gitRun(t, app, "commit", "--quiet", "--allow-empty", "-m", "seed")
	gitRun(t, app, "remote", "add", "origin", "/somewhere/the/user/owns.git")
	appGitDir := filepath.Join(app, ".git")

	var wg sync.WaitGroup
	stores := make([]*Store, racers)
	errs := make([]error, racers)
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			stores[i], errs[i] = Ensure(context.Background(), appGitDir, origin)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("racer %d: Ensure: %v", i, err)
		}
		if stores[i].Branch() != "main" {
			t.Fatalf("racer %d: branch = %q, want main", i, stores[i].Branch())
		}
	}

	// Exactly one built it, and everybody else adopted that one.
	built := 0
	for _, store := range stores {
		if store.Fresh() {
			built++
		}
	}
	if built != 1 {
		t.Fatalf("%d of %d racers built a clone, want exactly 1", built, racers)
	}

	// The clone works, is its own repository, and left no staging
	// directory behind.
	cloneDir := CloneDir(appGitDir)
	if err := requireOwnRepository(context.Background(), cloneDir); err != nil {
		t.Fatalf("the surviving clone is not a repository of its own: %v", err)
	}
	if _, _, err := stores[0].Head(context.Background()); err != nil {
		t.Fatalf("the surviving clone has no canonical head: %v", err)
	}
	entries, err := os.ReadDir(filepath.Dir(cloneDir))
	if err != nil {
		t.Fatalf("read the clone parent: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), cloneStagingPrefix) {
			t.Errorf("a staging directory survived: %s", entry.Name())
		}
	}

	// And the application repository's own remote was never touched.
	if got := gitLine(t, app, "config", "--get", "remote.origin.url"); got != "/somewhere/the/user/owns.git" {
		t.Fatalf("the application repository's origin is now %q", got)
	}
}

// TestBuildCloneRemovesStagingLeftovers covers the crash path: a process
// killed mid-build leaves a staging directory, and the next builder —
// which holds the lock, so nothing else can be using it — clears it
// rather than accumulating one per crash.
func TestBuildCloneRemovesStagingLeftovers(t *testing.T) {
	origin := newOrigin(t, "main", map[string]entry{"a.md": text("a\n")})
	commonDir := t.TempDir()
	cloneDir := CloneDir(commonDir)

	parent := filepath.Dir(cloneDir)
	if err := os.MkdirAll(parent, 0700); err != nil {
		t.Fatalf("create the clone parent: %v", err)
	}
	leftover := filepath.Join(parent, cloneStagingPrefix+"crashed")
	if err := os.MkdirAll(filepath.Join(leftover, "objects"), 0700); err != nil {
		t.Fatalf("create the leftover: %v", err)
	}

	if _, err := Ensure(context.Background(), commonDir, origin); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if _, err := os.Stat(leftover); !os.IsNotExist(err) {
		t.Fatalf("the leftover staging directory survived (%v)", err)
	}
}
