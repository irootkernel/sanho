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
