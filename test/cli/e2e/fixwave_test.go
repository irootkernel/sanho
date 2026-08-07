package e2e

// The P7 fix-wave regressions, against the real binary and real git.
//
// Each test here reproduces one reviewer scenario end to end. They are
// deliberately black-box: every one of these bugs was invisible from
// inside the code (exit 0, a plausible message, a silent no-op) and only
// showed up in what the repositories afterwards contained.

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// --- F-C1: a multi-ref push may not lose a branch's docs ----------------

// TestMultiRefPushPreservesEveryBranchsDocs is R2-C1's repro.
//
// Two branches with different docs trees in ONE `git push`. The old Run
// published branch 1, advanced the base to it, then decided branch 2
// against the NEW base — which made it a fast-forward, so branch 2's
// tree replaced branch 1's wholesale and branch 1's file was deleted
// from canonical. Exit 0, no message, no way to notice.
func TestMultiRefPushPreservesEveryBranchsDocs(t *testing.T) {
	t.Parallel()

	for _, order := range [][]string{{"main", "topic"}, {"topic", "main"}} {
		t.Run(strings.Join(order, "_then_"), func(t *testing.T) {
			t.Parallel()

			w := newWorld(t, defaultCanonicalDocs())
			ws := w.setup("multi-" + order[0])

			ws.commitDocs("docs: main-side note", map[string]string{"main.md": "from main\n"})
			ws.git("checkout", "--quiet", "-b", "topic", "HEAD~")
			ws.commitDocs("docs: topic-side note", map[string]string{"topic.md": "from topic\n"})

			push := ws.gitExit(append([]string{"push", "--quiet", "origin"}, order...)...)
			requireExit(t, "two-branch push", push, 0)

			// Both branches' documents are in canonical, and so is the
			// content it already had.
			want := []string{"api.md", "main.md", "topic.md"}
			if got := w.canonicalPaths(w.canonicalHead()); !reflect.DeepEqual(got, want) {
				t.Fatalf("canonical holds %v, want %v — a multi-ref push lost a branch's docs", got, want)
			}
			requireEqual(t, "canonical main.md", w.canonicalFile(w.canonicalHead(), "main.md"), "from main\n")
			requireEqual(t, "canonical topic.md", w.canonicalFile(w.canonicalHead(), "topic.md"), "from topic\n")

			// One canonical commit per distinct docs tree, and the hook
			// said so rather than reporting only the last.
			if strings.Count(push.combined(), "published docs") != 2 {
				t.Errorf("push reported %d publications, want 2:\n%s",
					strings.Count(push.combined(), "published docs"), push.combined())
			}
		})
	}
}

// TestPushAllPublishesEveryBranch is the `git push --all` half of the
// same repro: four branches, four documents, none silently dropped.
func TestPushAllPublishesEveryBranch(t *testing.T) {
	t.Parallel()

	w := newWorld(t, defaultCanonicalDocs())
	ws := w.setup("push-all")
	adopt := strings.TrimSpace(ws.git("rev-parse", "HEAD").stdout)

	for _, name := range []string{"b1", "b2", "b3", "b4"} {
		ws.git("checkout", "--quiet", "-B", name, adopt)
		ws.commitDocs("docs: "+name, map[string]string{name + ".md": name + "\n"})
	}

	push := ws.gitExit("push", "--quiet", "--all", "origin")
	requireExit(t, "git push --all", push, 0)

	want := []string{"api.md", "b1.md", "b2.md", "b3.md", "b4.md"}
	if got := w.canonicalPaths(w.canonicalHead()); !reflect.DeepEqual(got, want) {
		t.Fatalf("canonical holds %v, want %v — `git push --all` lost a branch's docs", got, want)
	}
}

// TestAConflictingSiblingRejectsTheWholePush is F-H1.
//
// One branch merges cleanly and one conflicts. Publishing the clean one
// first and then rejecting made "no remote ref was changed" a lie AND
// left canonical carrying half a push. Evaluate-then-publish validates
// everything before the first write.
func TestAConflictingSiblingRejectsTheWholePush(t *testing.T) {
	t.Parallel()

	w := newWorld(t, defaultCanonicalDocs())
	ws := w.setup("half-push")
	before := w.canonicalHead()

	// Branch A adds a new file; it would merge cleanly.
	ws.commitDocs("docs: clean addition", map[string]string{"guide.md": "mine\n"})
	// Branch B edits the same line canonical is about to change.
	ws.git("checkout", "--quiet", "-b", "topic", "HEAD~")
	ws.commitDocs("docs: conflicting edit", map[string]string{"api.md": "line one\nMINE\n"})
	w.advanceCanonical(map[string]string{"api.md": "line one\nTHEIRS\n"}, "canonical: their edit")

	push := ws.gitExit("push", "--quiet", "origin", "main", "topic")
	requireExit(t, "push with one conflicting branch", push, 1)

	after := w.canonicalHead()
	if after == before {
		t.Fatalf("the fixture did not advance canonical; the test proves nothing")
	}
	// Canonical is exactly where the out-of-band commit left it: nothing
	// this push produced landed.
	if paths := w.canonicalPaths(after); len(paths) != 1 || paths[0] != "api.md" {
		t.Fatalf("canonical holds %v after a rejected push, want only api.md", paths)
	}
	requireContains(t, "rejection", push.combined(), "no remote ref was changed")
	requireNotContains(t, "rejection", push.combined(), "published docs")
}

// --- F-H2: a docs-free branch must not empty canonical ------------------

// TestDocsFreeBranchRefusesAndCanOverride is R2-H2's repro plus its
// documented escape hatch.
func TestDocsFreeBranchRefusesAndCanOverride(t *testing.T) {
	t.Parallel()

	w := newWorld(t, defaultCanonicalDocs())
	ws := w.setup("docs-free")

	ws.git("rm", "--quiet", "-r", "docs")
	ws.git("commit", "-m", "docs: remove the docs directory")

	refused := ws.push()
	requireExit(t, "push of a docs-free branch", refused, 1)
	requireContains(t, "refusal", refused.combined(), "publishing it would delete")
	requireContains(t, "refusal", refused.combined(), "SANHO_ALLOW_DOCS_DELETION=1")
	requireEqual(t, "canonical api.md",
		w.canonicalFile(w.canonicalHead(), "api.md"), "line one\nline two\n")

	// Stated explicitly, the deletion is allowed: it is a legitimate
	// operation, just never one to infer.
	allowed := ws.pushWithEnv("SANHO_ALLOW_DOCS_DELETION=1")
	requireExit(t, "push with the override set", allowed, 0)
	if paths := w.canonicalPaths(w.canonicalHead()); len(paths) != 0 {
		t.Fatalf("canonical still holds %v after an explicit empty publication", paths)
	}
}

// --- F-H3: linked worktrees are managed, not silently inert -------------

// TestLinkedWorktreeIsManaged is R2-H3's repro.
//
// `.sanho.json` is gitignored, so `git worktree add` produces a checkout
// without it — and every hook there found no workspace and did nothing.
// No marker gate, no provenance stamp, no publication: sanho installed
// and completely inert, with no message to say so.
func TestLinkedWorktreeIsManaged(t *testing.T) {
	t.Parallel()

	w := newWorld(t, defaultCanonicalDocs())
	main := w.setup("worktree-main")

	linkedDir := filepath.Join(w.root, "worktree-linked")
	main.git("worktree", "add", "--quiet", "-b", "feature", linkedDir)
	linked := &workspace{w: w, name: "linked", dir: resolvePath(t, linkedDir), codeOrigin: main.codeOrigin}

	if fileExists(t, linked.path(".sanho.json")) {
		t.Fatal("the linked worktree carries its own config; this test would prove nothing")
	}

	// 1. The §5.6 marker gate fires there.
	linked.writeDocs(map[string]string{
		"api.md": "<<<<<<< sanho-ours\nmine\n=======\ntheirs\n>>>>>>> sanho-upstream\n",
	})
	linked.git("add", "docs/api.md")
	blocked := linked.gitExit("commit", "-m", "docs: markers")
	requireExit(t, "commit with markers in a linked worktree", blocked, 1)
	requireContains(t, "gate", blocked.combined(), "staged docs contain conflict markers")

	// 2. commit-msg stamps provenance there.
	linked.writeDocs(map[string]string{"api.md": "line one\nfrom the linked worktree\n"})
	linked.git("add", "docs/api.md")
	linked.git("commit", "-m", "docs: edit from the linked worktree")
	requireContains(t, "stamped message", linked.headMessage(), "docs-base:")

	// 3. And pre-push publishes from there, into the clone the two
	//    worktrees share through the common git dir.
	push := linked.gitExit("push", "--quiet", "origin", "feature")
	requireExit(t, "push from a linked worktree", push, 0)
	requireContains(t, "push output", push.combined(), "published docs")
	requireEqual(t, "canonical api.md",
		w.canonicalFile(w.canonicalHead(), "api.md"), "line one\nfrom the linked worktree\n")

	// The registry keeps ONE row for the checkout, keyed by the main
	// worktree, rather than one per linked worktree.
	state := main.sanho("state", "--json").stdout
	if got := strings.Count(state, `"workspace_id"`); got != 1 {
		t.Errorf("registry holds %d workspace rows, want 1 for a checkout with two worktrees:\n%s", got, state)
	}
}

// --- C3 (v0.2 review wave 4): a linked worktree's hook environment must
// not leak into other repositories ---------------------------------------

// TestLinkedWorktreePushLeavesTheAppRepositoryUntouched is C3's repro
// (sanho-v0.2.md §7 C3, gitx.Runner.env()).
//
// git exports an absolute GIT_DIR into the environment of every hook it
// runs inside a LINKED worktree. Before the fix, gitx.Runner inherited
// that unfiltered, so a git command sanho issued from inside such a hook
// — even one explicitly rooted at a completely different repository,
// such as the private canonical clone — silently ran against the linked
// worktree's repository instead. TestLinkedWorktreeIsManaged (above)
// already proves publication from a linked worktree still lands the
// right content in canonical; this test proves the app repository comes
// out the other side of that same push UNCHANGED — its own `origin`
// (the code remote, unrelated to canonical) byte-identical, its own
// `refs/remotes/origin/*` byte-identical — and that a freshness line
// computed mid-hook names canonical's true distance rather than
// whatever the app repository's own history happens to say.
func TestLinkedWorktreePushLeavesTheAppRepositoryUntouched(t *testing.T) {
	t.Parallel()

	w := newWorld(t, defaultCanonicalDocs())
	main := w.setup("wt-integrity-main")
	linked := linkedWorktree(t, main, "wt-integrity-feature")

	// The linked worktree starts from the same recorded base as main, so
	// canonical's next advance below is a known, exact "1 behind".
	requireExit(t, "first sync in the linked worktree", linked.run("sync"), 0)

	// The state a pre-fix binary corrupts: the app repository's OWN
	// "origin" — its code remote, nothing to do with canonical — and its
	// remote-tracking refs. Read from `main` (the main worktree) so the
	// comparison does not depend on which worktree's view is asked.
	remoteBefore := strings.TrimSpace(main.git("config", "--get", "remote.origin.url").stdout)
	refsBefore := main.git("for-each-ref", "--format=%(refname) %(objectname)", "refs/remotes/origin").stdout
	// The WHOLE local config, not only the remote URL. C3's misdirection
	// ran `git remote set-url` against the app repository, but any
	// clone-scoped `git config` sanho issues could land there the same
	// way — `sanho.branch`, a fetch refspec, anything the private clone
	// configures. Comparing the entire local config is what makes the
	// claim "the application repository is untouched" rather than "this
	// one key is untouched".
	configBefore := main.git("config", "--list", "--local").stdout

	// Canonical advances out of band, giving the pre-commit freshness
	// check below a known, verifiable answer: exactly one commit. The
	// private canonical clone only learns of it once refreshed — a plain
	// CLI invocation, not a git hook, so it is unaffected by C3 itself —
	// and the recorded base is untouched, which is what leaves the
	// worktree exactly one commit behind for the commit below.
	w.advanceCanonical(map[string]string{"api.md": "line one\nTHEIRS\n"}, "canonical: their edit")
	linked.sanho("status", "--refresh")

	// Commit fresh, non-conflicting docs content IN THE LINKED WORKTREE.
	// This is where `pre-commit`/`commit-msg` fire with git's
	// hook-exported, worktree-scoped GIT_DIR in the child's environment.
	linked.writeDocs(map[string]string{"other.md": "from the linked worktree\n"})
	linked.git("add", "docs/other.md")
	committed := linked.gitExit("commit", "-m", "docs: edit from the linked worktree")
	requireExit(t, "commit in the linked worktree", committed, 0)

	// The pre-commit freshness line must name canonical's true distance.
	// The reviewer's reproduction showed a plausible-looking but WRONG
	// line here, computed by misdirecting the canonical clone's Runner
	// at the app repository's own history.
	requireContains(t, "pre-commit freshness line", committed.combined(),
		"sanho: docs base is 1 commits behind")

	linkedHead := strings.TrimSpace(linked.git("rev-parse", "HEAD").stdout)

	// Publication itself still succeeds and reaches canonical.
	push := linked.gitExit("push", "--quiet", "origin", "wt-integrity-feature")
	requireExit(t, "push from the linked worktree", push, 0)
	requireContains(t, "push output", push.combined(), "published docs")
	requireEqual(t, "canonical docs/other.md",
		w.canonicalFile(w.canonicalHead(), "other.md"), "from the linked worktree\n")

	// The app repository's OWN remote is untouched — byte-identical, not
	// merely "still a valid URL". A pre-fix binary rewrote it to the
	// canonical clone's own path (`canonical.reconcileExisting`'s
	// `git remote set-url origin <canonical-url>`, misdirected).
	remoteAfter := strings.TrimSpace(main.git("config", "--get", "remote.origin.url").stdout)
	requireEqual(t, "app repository remote.origin.url", remoteAfter, remoteBefore)

	// The app repository's remote-tracking refs: every PRE-EXISTING ref
	// is byte-identical to before, and the only addition is exactly the
	// ordinary tracking ref `git push` itself creates for a branch
	// pushed for the first time — pointing at the commit this test just
	// made, in the app's own code remote. A pre-fix binary replaced the
	// whole set with canonical's own branches instead of adding this one
	// (sanho-v0.2.md §7 C3's reproduction showed
	// "refs/remotes/origin/main refs/remotes/origin/wtbranch" — canonical
	// content under the app's own ref names).
	wantRefsAfter := refsBefore + "refs/remotes/origin/wt-integrity-feature " + linkedHead + "\n"
	refsAfter := main.git("for-each-ref", "--format=%(refname) %(objectname)", "refs/remotes/origin").stdout
	requireEqual(t, "app repository refs/remotes/origin/*", refsAfter, wantRefsAfter)

	// And no configuration of the application repository moved at all.
	// `git push` itself writes no local config, so this is an exact
	// equality rather than an allowance for one expected addition.
	requireEqual(t, "app repository local git config",
		main.git("config", "--list", "--local").stdout, configBefore)

	// The workspace state the hooks own is still the workspace's: the
	// canonical clone lives under the common dir and the app repository's
	// own HEAD is where the user left it in each worktree.
	requireEqual(t, "main worktree branch",
		strings.TrimSpace(main.git("rev-parse", "--abbrev-ref", "HEAD").stdout), "main")
	requireEqual(t, "linked worktree branch",
		strings.TrimSpace(linked.git("rev-parse", "--abbrev-ref", "HEAD").stdout), "wt-integrity-feature")
}

// --- F-H7: `init --force` must not destroy uncommitted docs -------------

func TestInitForceRefusesOverUncommittedDocs(t *testing.T) {
	t.Parallel()

	w := newWorld(t, defaultCanonicalDocs())
	ws := w.setup("force-dirty")

	ws.writeDocs(map[string]string{"api.md": "line one\nwork in progress\n"})

	refused := ws.run("init",
		"--project", projectName, "--docs-repo-url", w.origin, "--actor-email", actorEmail,
		"--force", "-y")
	requireExit(t, "init --force over uncommitted docs", refused, 1)
	requireContains(t, "refusal", refused.combined(), "commit or stash your docs changes first")

	// The uncommitted work is untouched: it is in no commit, so nothing
	// could have brought it back.
	requireEqual(t, "docs/api.md", ws.readDocs("api.md"), "line one\nwork in progress\n")
}

// --- pushWithEnv --------------------------------------------------------

// pushWithEnv is push with extra environment, for the one flag that is
// an environment variable rather than a command-line option.
func (ws *workspace) pushWithEnv(extra ...string) result {
	ws.w.t.Helper()
	env := append(ws.w.env(), extra...)
	return execute(ws.w.t, ws.dir, env, "git", "push", "--quiet", "origin", "HEAD")
}
