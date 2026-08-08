package e2e

// The fourth review wave's Medium findings, driven through the real
// binary. Each one is a state the previous build reached and answered
// wrongly at exit 0 or with the wrong error.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTagPushIsNotPublicationsBusiness is M1.
//
// The the publication contract step-1 filter used to run INSIDE the use case, behind the
// sync gate and behind `canonical.Ensure`. So a push carrying only tags
// — which publishes nothing — was refused because an unrelated sync was
// unfinished, and the same push with canonical unreachable failed on a
// clone fetch it never needed.
func TestTagPushIsNotPublicationsBusiness(t *testing.T) {
	t.Parallel()

	t.Run("during an unfinished sync", func(t *testing.T) {
		t.Parallel()

		w := newWorld(t, defaultCanonicalDocs())
		ws := conflictedSync(t, w)

		ws.git("tag", "v1.0.0")
		push := ws.gitExit("push", "--quiet", "origin", "v1.0.0")
		requireExit(t, "tag push during a sync window", push, 0)
		requireNotContains(t, "tag push output", push.combined(), "finish the sync first")
	})

	t.Run("with canonical unreachable", func(t *testing.T) {
		t.Parallel()

		w := newWorld(t, defaultCanonicalDocs())
		ws := w.setup("tag-offline")
		ws.git("tag", "v2.0.0")
		w.takeCanonicalOffline()
		t.Cleanup(w.bringCanonicalOnline)

		push := ws.gitExit("push", "--quiet", "origin", "v2.0.0")
		requireExit(t, "tag push with canonical unreachable", push, 0)
		requireNotContains(t, "tag push output", push.combined(), "unreachable")
	})

	t.Run("branch deletion", func(t *testing.T) {
		t.Parallel()

		w := newWorld(t, defaultCanonicalDocs())
		ws := w.setup("delete-branch")
		ws.git("branch", "throwaway")
		ws.git("push", "--quiet", "origin", "throwaway")
		w.takeCanonicalOffline()
		t.Cleanup(w.bringCanonicalOnline)

		push := ws.gitExit("push", "--quiet", "origin", "--delete", "throwaway")
		requireExit(t, "branch deletion with canonical unreachable", push, 0)
	})
}

// TestDoctorRunsOverACorruptConfig is M4.
//
// Doctor is the command a user reaches for when something is wrong, and
// a `.sanho.json` that parses as JSON but is neither a v0.1 config nor a
// v2 one stopped it before a single check ran — the one state it was
// most needed in.
func TestDoctorRunsOverACorruptConfig(t *testing.T) {
	t.Parallel()

	w := newWorld(t, defaultCanonicalDocs())
	ws := w.setup("corrupt-config")

	// Parses as JSON, carries neither socket_path nor schema_version.
	writeFile(t, ws.path(".sanho.json"), "{\"project\": \"product\"}\n")

	doctor := ws.run("doctor")
	requireExit(t, "doctor over a corrupt config", doctor, 0)
	requireContains(t, "doctor", doctor.combined(), "is unreadable")
	// The other checks really ran.
	requireContains(t, "doctor", doctor.combined(), "hooks")
	requireContains(t, "doctor", doctor.combined(), "registry")

	// And the machine envelope names the state rather than calling it a
	// bug in sanho.
	status := ws.run("status", "--json")
	requireExit(t, "status over a corrupt config", status, 1)
	requireContains(t, "status --json", status.stdout, "\"config_corrupt\"")
}

// TestASubdirectoryIsNotAWorkspace is M6.
//
// Linked-worktree support let a directory with no `.sanho.json` borrow
// the MAIN worktree's config. The check was "what is the first record of
// `git worktree list`", which every SUBDIRECTORY of a checkout also
// answers — so standing in `docs/` or `src/` made sanho treat that
// directory as a workspace root and build every path from it.
func TestASubdirectoryIsNotAWorkspace(t *testing.T) {
	t.Parallel()

	w := newWorld(t, defaultCanonicalDocs())
	ws := w.setup("subdir")
	mkdirAll(t, ws.path("src", "internal"))

	for _, sub := range []string{"docs", filepath.Join("src", "internal")} {
		res := execute(t, ws.path(sub), w.env(), cliBinary, "status")
		requireExit(t, "status in "+sub, res, 1)
		requireContains(t, "status in "+sub, res.combined(), "not a sanho workspace")
	}

	// The workspace root itself still is one.
	requireExit(t, "status at the root", ws.run("status"), 0)
}

// TestPreCommitPredictsFromTheIndex is M7.
//
// The freshness warning describes what `sanho sync` would do to the
// commit BEING MADE, so its local side is the index. Predicting from
// HEAD answered about the previous commit — which is the one reading
// guaranteed to describe something the user is no longer doing: staging
// the very resolution that removes the conflict still warned about it.
func TestPreCommitPredictsFromTheIndex(t *testing.T) {
	t.Parallel()

	w := newWorld(t, map[string]string{"api.md": "line one\nline two\n"})
	ws := w.setup("index-preview")

	// A committed local edit that conflicts with what canonical is about
	// to publish.
	ws.commitDocs("docs: my edit", map[string]string{"api.md": "line one\nMINE\n"})
	w.advanceCanonical(map[string]string{"api.md": "line one\nTHEIRS\n"}, "canonical: their edit")
	ws.sanho("status", "--refresh")

	// HEAD still carries the conflicting content, and the index no longer
	// does: staging canonical's own line is a resolution the commit being
	// made would carry.
	ws.writeDocs(map[string]string{"api.md": "line one\nTHEIRS\n"})
	ws.git("add", "docs/api.md")

	commit := ws.gitExit("commit", "-m", "docs: take the upstream line")
	requireExit(t, "commit", commit, 0)
	requireContains(t, "pre-commit warning", commit.combined(), "commits behind")
	requireNotContains(t, "pre-commit warning", commit.combined(), "will report conflicts")
}

// TestPostCheckoutStandsDownOnAFileCheckout is M8's second half.
//
// `git checkout -- docs/api.md` moves no ref, so there is no HEAD
// movement to re-derive from — and restoring one document is precisely
// the state the hook contract step 1 stands down for, reached by a route that used
// to skip the test entirely.
func TestPostCheckoutStandsDownOnAFileCheckout(t *testing.T) {
	t.Parallel()

	w := newWorld(t, defaultCanonicalDocs())
	ws := w.setup("file-checkout")
	before := readFile(t, ws.basePath())

	// The hook line has to CARRY git's arguments for the stand-down to be
	// decidable at all. Without "$@" the command sees nothing, cannot
	// tell a file checkout from a branch checkout, and re-runs the whole
	// derivation for a command that moved no ref.
	requireContains(t, "post-checkout hook", readFile(t, ws.hookPath("post-checkout")),
		`hook post-checkout "$@"`)

	ws.writeDocs(map[string]string{"api.md": "line one\nedited\n"})
	restore := ws.gitExit("checkout", "--", "docs/api.md")
	requireExit(t, "file checkout", restore, 0)

	requireEqual(t, "post-checkout output on a file checkout", restore.combined(), "")
	requireEqual(t, "base file", readFile(t, ws.basePath()), before)
}

// TestCloneCreationIsSerializedAcrossLinkedWorktrees is M3.
//
// Two linked worktrees share the git common dir and therefore share the
// private canonical clone. Two write paths starting at once with no
// clone present both ran `git init --bare` into the same directory and
// both fetched; the loser's cleanup then removed the winner's freshly
// built clone out from under a publication already using it.
func TestCloneCreationIsSerializedAcrossLinkedWorktrees(t *testing.T) {
	t.Parallel()

	w := newWorld(t, defaultCanonicalDocs())
	ws := w.setup("clone-race")

	linkedDir := filepath.Join(w.root, "clone-race-linked")
	ws.git("worktree", "add", "--quiet", "-b", "linked", linkedDir)
	linked := &workspace{w: w, name: "clone-race-linked", dir: resolvePath(t, linkedDir), codeOrigin: ws.codeOrigin}

	// Remove the clone so both sides have to create it.
	if err := os.RemoveAll(ws.cloneDir()); err != nil {
		t.Fatalf("remove the clone: %v", err)
	}

	type outcome struct {
		label string
		res   result
	}
	sites := []*workspace{ws, linked}
	results := make(chan outcome, len(sites))
	for _, site := range sites {
		go func(site *workspace) {
			res, err := tryExecute(site.dir, w.env(), cliBinary, "sync")
			if err != nil {
				res = result{stderr: err.Error(), exitCode: -1}
			}
			results <- outcome{label: site.name, res: res}
		}(site)
	}

	// BOTH workers are drained before anything is asserted. Failing on
	// the first result left the other `sanho sync` still writing into the
	// clone while the test returned, and t.TempDir's cleanup then raced
	// it — reporting "directory not empty" on top of the real failure and
	// making the teardown itself look flaky.
	gathered := make([]outcome, 0, len(sites))
	for range sites {
		gathered = append(gathered, <-results)
	}
	for _, got := range gathered {
		if got.res.exitCode != 0 {
			t.Errorf("%s: concurrent first sync exited %d\nstdout:\n%s\nstderr:\n%s",
				got.label, got.res.exitCode, got.res.stdout, got.res.stderr)
		}
	}
	if t.Failed() {
		t.FailNow()
	}

	// One clone, intact, on the publication branch — not a directory a
	// loser's cleanup half-removed, and not one bound to the application
	// repository by an upward `git rev-parse` walk.
	head := execute(t, ws.cloneDir(), w.env(), "git", "rev-parse", "refs/remotes/origin/main")
	requireExit(t, "canonical clone head", head, 0)
	requireEqual(t, "canonical clone head", strings.TrimSpace(head.stdout), w.canonicalHead())

	// The clone is its OWN repository: an upward resolution to the app
	// repository is the binding that made the loser rewrite the wrong
	// remote.
	gitDir := execute(t, ws.cloneDir(), w.env(), "git", "rev-parse", "--absolute-git-dir")
	requireEqual(t, "clone git dir", resolvePath(t, strings.TrimSpace(gitDir.stdout)), resolvePath(t, ws.cloneDir()))

	// The application repository's own remote is untouched.
	requireEqual(t, "app remote.origin.url",
		strings.TrimSpace(ws.git("config", "--get", "remote.origin.url").stdout), ws.codeOrigin)

	// And the out-of-place construction left nothing behind.
	entries, err := os.ReadDir(filepath.Dir(ws.cloneDir()))
	if err != nil {
		t.Fatalf("read the clone parent: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "canonical.building-") {
			t.Errorf("a staging directory survived the build: %s", entry.Name())
		}
	}
}
