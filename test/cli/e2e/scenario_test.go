package e2e

// The scenario matrix (sanho-v0.2.md §9 rule 5): the sandbox S-matrix
// from the 2026-08-07 audit, rewritten for v0.2 semantics.
//
// Each scenario is named for the audit finding it keeps retired:
//
//	S1  onboarding, fresh and reuse
//	S2  propagation between two workspaces through one canonical
//	S3  same-file one-hunk conflict, two workspaces, whole loop
//	S4  different-file concurrent edits — both publish, worktree untouched
//	S5  offline: commits succeed (C1), push fails closed
//	S6  same-file TWO-hunk conflict, push to publish  (C2)
//	S7  amend/reword and rebase still publish          (H4)
//	S8  branch switching re-derives the base
//	S9  clean --dry-run is byte-identical              (M4)
//	S10 symlink, executable mode and binary round-trip (H1)
//	S11 --json schemas, and errors never on stdout

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// --- S1: onboarding ----------------------------------------------------

// TestS1OnboardingFreshAndReuse covers both head states `sanho init`
// decides between when canonical has content: adopt it, or keep local
// docs that already carry provenance.
func TestS1OnboardingFreshAndReuse(t *testing.T) {
	t.Parallel()
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})

	// Fresh: canonical's docs become this workspace's docs, staged for a
	// commit the user makes (P3 — the tool never authors commits).
	ws := w.newWorkspace("fresh")
	out := ws.initWorkspace()
	requireContains(t, "init output", out.stdout, "workspace initialized")
	requireContains(t, "init output", out.stdout, "git add .gitignore && git commit")
	requireEqual(t, "docs/api.md", ws.readDocs("api.md"), "canonical api\n")
	requireContains(t, "base file", readFile(t, ws.basePath()), w.canonicalHead())
	ws.git("commit", "-m", "docs: adopt canonical docs")

	// Reuse: the same checkout, deregistered and re-initialized. Its docs
	// are already there and its history carries docs-base trailers, so
	// init derives the base from provenance and never touches the files.
	ws.commitDocs("docs: local work", map[string]string{"api.md": "local work\n"})
	stampedBase := w.canonicalHead()
	ws.sanho("clean", "-y")

	reuse := ws.initWorkspace()
	requireEqual(t, "docs/api.md after reuse init", ws.readDocs("api.md"), "local work\n")
	requireContains(t, "reuse base", readFile(t, ws.basePath()), stampedBase)
	requireContains(t, "reuse summary", reuse.stdout, "docs base  : "+stampedBase[:12])

	// And the refusal that guards it: docs with no provenance could be
	// anything, so adopting canonical's head as their base would assert
	// an ancestry that is not true.
	stranger := w.newWorkspace("stranger")
	stranger.writeDocs(map[string]string{"api.md": "docs from somewhere else\n"})
	stranger.git("add", "-A")
	stranger.git("commit", "-m", "docs: unrelated docs")

	refused := stranger.run("init",
		"--project", projectName, "--docs-repo-url", w.origin, "--actor-email", actorEmail)
	requireExit(t, "init over unprovenanced docs", refused, 1)
	requireContains(t, "refusal", refused.stderr, "no docs-base/docs-version commits")
	requireContains(t, "refusal", refused.stderr, "--force")
	requireEqual(t, "docs left alone", stranger.readDocs("api.md"), "docs from somewhere else\n")
}

// --- S2: propagation ---------------------------------------------------

// TestS2PropagationBetweenTwoWorkspaces is the whole point of the tool:
// one workspace publishes, the other consumes, and the registry says who
// is where.
func TestS2PropagationBetweenTwoWorkspaces(t *testing.T) {
	t.Parallel()
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})

	author := w.setup("author")
	reader := w.setup("reader")

	author.commitDocs("docs: publish a guide", map[string]string{"guide.md": "the guide\n"})
	requireExit(t, "author push", author.push(), 0)

	published := w.canonicalHead()
	requireEqual(t, "canonical guide.md", w.canonicalFile(published, "guide.md"), "the guide\n")

	// The reader knows nothing until it fetches: no polling, no daemon.
	before := reader.sanho("status")
	requireContains(t, "reader status before refresh", before.stdout, "up to date")

	refreshed := reader.sanho("status", "--refresh")
	requireContains(t, "reader status", refreshed.stdout, "behind 1")
	requireContains(t, "reader status", refreshed.stdout, "will merge cleanly")

	synced := reader.sanho("sync")
	requireContains(t, "reader sync", synced.stdout, "synced docs to "+published[:12])
	requireEqual(t, "reader docs/guide.md", reader.readDocs("guide.md"), "the guide\n")
	requireEqual(t, "reader sync commit", reader.headSubject(), "docs: sync to "+published[:12])

	// The registry carries both checkouts, and status renders the other
	// one as a sibling (§5.7, §5.8).
	siblings := reader.sanho("status", "--json").stdout
	var document struct {
		Siblings []struct {
			WorkspaceID string `json:"workspace_id"`
			VsHead      string `json:"vs_head"`
		} `json:"siblings"`
	}
	if err := json.Unmarshal([]byte(siblings), &document); err != nil {
		t.Fatalf("parse status JSON: %v\n%s", err, siblings)
	}
	if len(document.Siblings) != 1 || !strings.Contains(document.Siblings[0].WorkspaceID, author.dir) {
		t.Fatalf("siblings = %+v, want the author workspace", document.Siblings)
	}
}

// --- S3: same-file one-hunk conflict, end to end -----------------------

// TestS3SameFileConflictAcrossTwoWorkspaces is the audit's central loop
// with two real checkouts rather than an out-of-band canonical advance:
// push rejected (template 3) → sync (template 2) → resolve → commit →
// push publishes.
func TestS3SameFileConflictAcrossTwoWorkspaces(t *testing.T) {
	t.Parallel()
	w := newWorld(t, map[string]string{"api.md": "line one\nline two\n"})

	first := w.setup("first")
	second := w.setup("second")

	first.commitDocs("docs: first edit", map[string]string{"api.md": "line one\nFIRST\n"})
	requireExit(t, "first push", first.push(), 0)

	second.commitDocs("docs: second edit", map[string]string{"api.md": "line one\nSECOND\n"})
	rejected := second.push()
	requireExit(t, "conflicting push", rejected, 1)
	requireContains(t, "rejection", rejected.combined(), "sanho: your docs changes conflict with upstream (base ")
	requireContains(t, "rejection", rejected.combined(), "Run 'sanho sync', resolve, commit, then push again.")
	requireContains(t, "rejection", rejected.combined(), "error: push rejected")

	// Nothing reached canonical.
	requireEqual(t, "canonical after rejection",
		w.canonicalFile(w.canonicalHead(), "api.md"), "line one\nFIRST\n")

	conflicted := second.sanho("sync")
	requireContains(t, "sync", conflicted.stdout, "sanho: merged docs with upstream — 1 files have conflicts:")
	requireContains(t, "sync", conflicted.stdout, "  docs/api.md")
	requireContains(t, "markers", second.readDocs("api.md"), "<<<<<<< sanho-ours")
	requireContains(t, "markers", second.readDocs("api.md"), ">>>>>>> sanho-upstream")

	second.writeDocs(map[string]string{"api.md": "line one\nFIRST\nSECOND\n"})
	second.git("add", "docs/api.md")
	second.git("commit", "-m", "docs: keep both edits")
	requireExit(t, "push after resolving", second.push(), 0)

	requireEqual(t, "canonical after resolution",
		w.canonicalFile(w.canonicalHead(), "api.md"), "line one\nFIRST\nSECOND\n")

	// And the first workspace consumes the reconciled result.
	first.sanho("sync")
	requireEqual(t, "first docs after sync", first.readDocs("api.md"), "line one\nFIRST\nSECOND\n")
}

// --- S4: different-file concurrency ------------------------------------

// TestS4DifferentFileEditsBothPublish is §5.3 case ③ doing its job: the
// second push merges canonical-side and continues in the same
// invocation, with zero user friction and — the invariant that matters —
// without touching the worktree.
func TestS4DifferentFileEditsBothPublish(t *testing.T) {
	t.Parallel()
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})

	alpha := w.setup("alpha")
	bravo := w.setup("bravo")

	alpha.commitDocs("docs: alpha section", map[string]string{"alpha.md": "alpha\n"})
	bravo.commitDocs("docs: bravo section", map[string]string{"bravo.md": "bravo\n"})

	requireExit(t, "alpha push", alpha.push(), 0)

	bravoDocsBefore := snapshotTree(t, bravo.docsPath())
	push := bravo.push()
	requireExit(t, "bravo push", push, 0)
	requireContains(t, "bravo push output", push.combined(), "published docs")

	// Worktree inviolability (§5.3): pre-push merged in the private
	// clone and published; the checkout is byte-for-byte as it was.
	requireSameTree(t, "bravo docs after its own push", bravoDocsBefore, snapshotTree(t, bravo.docsPath()))

	head := w.canonicalHead()
	requireEqual(t, "canonical alpha.md", w.canonicalFile(head, "alpha.md"), "alpha\n")
	requireEqual(t, "canonical bravo.md", w.canonicalFile(head, "bravo.md"), "bravo\n")
	requireEqual(t, "canonical api.md", w.canonicalFile(head, "api.md"), "canonical api\n")

	// Status then shows "behind (your own merge)", and sync brings it in.
	status := bravo.sanho("status", "--refresh")
	requireContains(t, "bravo status", status.stdout, "behind 1")
	bravo.sanho("sync")
	requireEqual(t, "bravo docs/alpha.md", bravo.readDocs("alpha.md"), "alpha\n")
	requireEqual(t, "bravo docs/bravo.md", bravo.readDocs("bravo.md"), "bravo\n")
}

// --- S5: offline -------------------------------------------------------

// TestS5OfflineCommitsSucceedAndPushFailsClosed is P2 at CLI level, and
// the audit's Critical C1 in regression form: the commit path never
// opens a connection, and the push boundary is where a missing canonical
// stops the world.
func TestS5OfflineCommitsSucceedAndPushFailsClosed(t *testing.T) {
	t.Parallel()
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})
	ws := w.setup("offline")

	w.takeCanonicalOffline()

	// A non-docs commit.
	writeFile(t, ws.path("src", "main.go"), "package main\n")
	ws.git("add", "-A")
	code := ws.gitExit("commit", "-m", "feat: add main")
	requireExit(t, "code commit while offline", code, 0)

	// And a docs commit, stamped from purely local data (§5.1).
	ws.writeDocs(map[string]string{"api.md": "written while offline\n"})
	ws.git("add", "-A")
	docs := ws.gitExit("commit", "-m", "docs: offline edit")
	requireExit(t, "docs commit while offline", docs, 0)
	requireContains(t, "offline commit message", ws.headMessage(), "docs-base: ")

	// The push fails closed with the §5.9 pair: a cause line naming the
	// repository, and an action line.
	push := ws.push()
	requireExit(t, "push while offline", push, 1)
	requireContains(t, "offline push", push.combined(), "sanho: canonical repository unreachable")
	requireContains(t, "offline push", push.combined(), w.origin)
	requireContains(t, "offline push", push.combined(), "Check network access to the docs repository, then push again.")
	requireContains(t, "offline push", push.combined(), "error: push rejected")
	requireNotContains(t, "offline push", push.combined(), "goroutine")

	// Reads still answer, from the last fetch, saying how old it is.
	status := ws.sanho("status")
	requireContains(t, "offline status", status.stdout, "canonical data is")

	w.bringCanonicalOnline()
	requireExit(t, "push once canonical is back", ws.push(), 0)
	requireEqual(t, "canonical api.md",
		w.canonicalFile(w.canonicalHead(), "api.md"), "written while offline\n")
}

// --- S6: two-hunk conflict ---------------------------------------------

// twoHunkSeed keeps the two edited lines far enough apart that git
// reports two conflict regions rather than one. That separation is the
// whole point: audit C2 was a merge layer that misread the number of
// conflicts, and a one-hunk fixture cannot catch it.
const twoHunkSeed = "line 01\nALPHA\nline 03\nline 04\nline 05\nline 06\nline 07\nline 08\nBRAVO\nline 10\n"

// TestS6TwoHunkConflictReachesResolution is C2's exact blind spot at CLI
// level: two conflicting regions in one file must both materialize, and
// the loop must still reach a published resolution.
func TestS6TwoHunkConflictReachesResolution(t *testing.T) {
	t.Parallel()
	w := newWorld(t, map[string]string{"api.md": twoHunkSeed})
	ws := w.setup("two-hunk")

	mine := strings.NewReplacer("ALPHA", "MINE-ALPHA", "BRAVO", "MINE-BRAVO").Replace(twoHunkSeed)
	theirs := strings.NewReplacer("ALPHA", "THEIRS-ALPHA", "BRAVO", "THEIRS-BRAVO").Replace(twoHunkSeed)

	ws.commitDocs("docs: my two edits", map[string]string{"api.md": mine})
	w.advanceCanonical(map[string]string{"api.md": theirs}, "canonical: their two edits")

	rejected := ws.push()
	requireExit(t, "push with two conflicting hunks", rejected, 1)
	requireContains(t, "rejection", rejected.combined(), "your docs changes conflict with upstream")

	conflicted := ws.sanho("sync")
	requireContains(t, "sync", conflicted.stdout, "1 files have conflicts")

	// Two hunks, counted. A merge layer that collapsed them into one
	// would pass every single-conflict test and still be C2.
	materialized := ws.readDocs("api.md")
	if got := strings.Count(materialized, "<<<<<<< sanho-ours"); got != 2 {
		t.Fatalf("materialized %d conflict hunks, want 2:\n%s", got, materialized)
	}
	if got := strings.Count(materialized, ">>>>>>> sanho-upstream"); got != 2 {
		t.Fatalf("materialized %d closing markers, want 2:\n%s", got, materialized)
	}
	requireContains(t, "hunk one", materialized, "MINE-ALPHA")
	requireContains(t, "hunk one", materialized, "THEIRS-ALPHA")
	requireContains(t, "hunk two", materialized, "MINE-BRAVO")
	requireContains(t, "hunk two", materialized, "THEIRS-BRAVO")

	// A commit that resolves only one hunk is still blocked.
	half := strings.NewReplacer("ALPHA", "RESOLVED-ALPHA", "BRAVO", "MINE-BRAVO").Replace(twoHunkSeed)
	ws.writeDocs(map[string]string{"api.md": halfResolved(materialized, half)})
	ws.git("add", "docs/api.md")
	stillBlocked := ws.gitExit("commit", "-m", "docs: half resolved")
	requireExit(t, "commit with one hunk left", stillBlocked, 1)
	requireContains(t, "gate", stillBlocked.combined(), "still have conflicts")

	resolved := strings.NewReplacer("ALPHA", "RESOLVED-ALPHA", "BRAVO", "RESOLVED-BRAVO").Replace(twoHunkSeed)
	ws.writeDocs(map[string]string{"api.md": resolved})
	ws.git("add", "docs/api.md")
	ws.git("commit", "-m", "docs: resolve both hunks")

	requireExit(t, "push after resolving both hunks", ws.push(), 0)
	requireEqual(t, "canonical api.md", w.canonicalFile(w.canonicalHead(), "api.md"), resolved)
}

// halfResolved keeps the second conflict region intact while replacing
// everything before it, so the fixture really does stage one unresolved
// hunk rather than a file with no markers at all.
func halfResolved(materialized, resolvedPrefix string) string {
	index := strings.Index(materialized, "<<<<<<< sanho-ours")
	second := strings.Index(materialized[index+1:], "<<<<<<< sanho-ours")
	if second < 0 {
		return materialized
	}
	return resolvedPrefix + materialized[index+1+second:]
}

// --- S7: amend, reword, rebase -----------------------------------------

// TestS7AmendRewordAndRebaseStillPublish is audit H4: a message-only
// amend wipes the trailer the previous commit carried, and v0.1 then
// could not publish. §5.1's second stamping condition restores it, and
// the push must still work.
func TestS7AmendRewordAndRebaseStillPublish(t *testing.T) {
	t.Parallel()
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})
	ws := w.setup("amend")

	ws.commitDocs("docs: draft", map[string]string{"api.md": "amended content\n"})
	requireContains(t, "original commit", ws.headMessage(), "docs-base: ")

	// The reword. It rewrites the message, dropping the trailer block,
	// and commit-msg puts it back from local data alone.
	ws.git("commit", "--amend", "-m", "docs: a better subject")
	message := ws.headMessage()
	requireEqual(t, "reworded subject", ws.headSubject(), "docs: a better subject")
	requireContains(t, "reworded commit", message, "docs-base: ")
	if got := strings.Count(message, "docs-base:"); got != 1 {
		t.Fatalf("docs-base appears %d times after the amend, want 1:\n%s", got, message)
	}

	requireExit(t, "push after the amend", ws.push(), 0)
	requireEqual(t, "canonical api.md",
		w.canonicalFile(w.canonicalHead(), "api.md"), "amended content\n")

	// And a rebase, which moves HEAD through post-rewrite.
	ws.git("checkout", "--quiet", "-b", "topic")
	ws.commitDocs("docs: topic work", map[string]string{"topic.md": "topic\n"})

	ws.git("checkout", "--quiet", "main")
	writeFile(t, ws.path("src", "app.go"), "package app\n")
	ws.git("add", "-A")
	ws.git("commit", "-m", "feat: unrelated code")

	ws.git("checkout", "--quiet", "topic")
	ws.git("rebase", "main")
	requireContains(t, "rebased commit", ws.headMessage(), "docs-base: ")

	ws.git("checkout", "--quiet", "main")
	ws.git("merge", "--ff-only", "topic")
	requireExit(t, "push after the rebase", ws.push(), 0)
	requireEqual(t, "canonical topic.md",
		w.canonicalFile(w.canonicalHead(), "topic.md"), "topic\n")
}

// --- S8: branch switching ----------------------------------------------

// TestS8BranchSwitchingReDerivesTheBase is §5.10: the base is a property
// of the checked-out content, so it follows the trailers of whatever
// history HEAD now names — it is never carried across from the branch
// you left.
func TestS8BranchSwitchingReDerivesTheBase(t *testing.T) {
	t.Parallel()
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})
	ws := w.setup("branches")

	first := w.canonicalHead()
	ws.git("branch", "side")

	second := w.advanceCanonical(map[string]string{
		"api.md":   "canonical api\n",
		"guide.md": "upstream guide\n",
	}, "canonical: add guide")
	ws.sanho("sync")
	requireContains(t, "base on main", readFile(t, ws.basePath()), second)

	toSide := ws.git("checkout", "--quiet", "side")
	requireContains(t, "base on side", readFile(t, ws.basePath()), first)
	requireContains(t, "post-checkout", toSide.combined(), "sanho: docs base re-derived as "+first[:12])
	if fileExists(t, ws.docsPath("guide.md")) {
		t.Error("the side branch carries a file only main has")
	}

	toMain := ws.git("checkout", "--quiet", "main")
	requireContains(t, "base back on main", readFile(t, ws.basePath()), second)
	requireContains(t, "post-checkout", toMain.combined(), "sanho: docs base re-derived as "+second[:12])
	requireEqual(t, "docs/guide.md", ws.readDocs("guide.md"), "upstream guide\n")
}

// --- S9: clean ---------------------------------------------------------

// TestS9CleanDryRunIsByteIdenticalThenCleanRemovesEverything is audit
// M4: a v0.1 dry-run that deleted state while reporting what it "would"
// do. The check is byte-level and covers the sanho-owned parts of `.git`
// and the registry, not only the visible workspace files.
func TestS9CleanDryRunIsByteIdenticalThenCleanRemovesEverything(t *testing.T) {
	t.Parallel()
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})
	ws := w.setup("clean")
	ws.commitDocs("docs: some work", map[string]string{"guide.md": "guide\n"})
	requireExit(t, "push", ws.push(), 0)

	beforeWorkspace := snapshotTree(t, ws.dir)
	beforeHome := snapshotTree(t, w.home)
	beforeDigest := treeDigest(beforeWorkspace) + treeDigest(beforeHome)

	dry := ws.sanho("clean", "--dry-run")
	requireContains(t, "dry run", dry.stdout, "would remove")
	requireContains(t, "dry run", dry.stdout, "nothing was changed")
	requireContains(t, "dry run", dry.stdout, ws.cloneDir())

	afterWorkspace := snapshotTree(t, ws.dir)
	afterHome := snapshotTree(t, w.home)
	requireSameTree(t, "workspace after --dry-run", beforeWorkspace, afterWorkspace)
	requireSameTree(t, "sanho home after --dry-run", beforeHome, afterHome)
	requireEqual(t, "whole-tree digest after --dry-run",
		treeDigest(afterWorkspace)+treeDigest(afterHome), beforeDigest)

	// The unconfirmed real run is a refusal, and changes nothing either.
	refused := ws.run("clean")
	requireExit(t, "clean without -y", refused, 1)
	requireEqual(t, "whole-tree digest after a refused clean",
		treeDigest(snapshotTree(t, ws.dir))+treeDigest(snapshotTree(t, w.home)), beforeDigest)

	ws.sanho("clean", "-y")
	for _, path := range []string{ws.path(".sanho.json"), ws.basePath(), ws.cloneDir()} {
		if fileExists(t, path) {
			t.Errorf("%s survived clean", path)
		}
	}
	for _, name := range []string{"pre-commit", "commit-msg", "pre-push", "post-checkout", "post-merge", "post-rewrite"} {
		if fileExists(t, ws.hookPath(name)) {
			t.Errorf("hook %s survived clean", name)
		}
	}
	// Docs are the user's, and stay unless --remove-docs was asked for.
	requireEqual(t, "docs/guide.md after clean", ws.readDocs("guide.md"), "guide\n")

	var state struct {
		Workspaces []struct {
			LocalPath string `json:"local_path"`
		} `json:"workspaces"`
	}
	dump := ws.sanho("state", "--all", "--json").stdout
	if err := json.Unmarshal([]byte(dump), &state); err != nil {
		t.Fatalf("parse state JSON: %v\n%s", err, dump)
	}
	for _, entry := range state.Workspaces {
		if entry.LocalPath == ws.dir {
			t.Fatal("the registry entry survived clean")
		}
	}
}

// --- S10: file fidelity ------------------------------------------------

// TestS10SymlinkModeAndBinaryRoundTrip is audit H1 retired by
// construction: v0.2 moves docs as git objects, so symlinks, file modes
// and binary content are git's problem and git gets them right. The test
// proves it end to end — publish from one workspace, sync into another,
// compare bytes, modes and link targets.
func TestS10SymlinkModeAndBinaryRoundTrip(t *testing.T) {
	t.Parallel()
	w := newWorld(t, map[string]string{"api.md": "canonical api\n"})

	author := w.setup("fidelity-author")
	reader := w.setup("fidelity-reader")

	binary := "\x00\x01\x02PNG\x00\xff\xfe binary payload \x00\n"
	author.writeDocs(map[string]string{
		"guide.md": "the guide\n",
		"logo.bin": binary,
	})
	writeExecutable(t, author.docsPath("run.sh"), "#!/bin/sh\necho hello\n")
	if err := os.Symlink("guide.md", author.docsPath("latest.md")); err != nil {
		t.Fatalf("create the docs symlink: %v", err)
	}

	author.git("add", "-A")
	author.git("commit", "-m", "docs: add a symlink, a script and a binary")
	requireExit(t, "author push", author.push(), 0)

	// A binary blob must not trip the marker detector (§5.4 binary skip);
	// a false positive here would have rejected the push outright.
	requireExit(t, "reader sync", reader.run("sync"), 0)

	requireEqual(t, "reader docs/guide.md", reader.readDocs("guide.md"), "the guide\n")
	requireEqual(t, "reader docs/logo.bin", reader.readDocs("logo.bin"), binary)

	script, err := os.Lstat(reader.docsPath("run.sh"))
	if err != nil {
		t.Fatalf("stat the synced script: %v", err)
	}
	if script.Mode()&0o111 == 0 {
		t.Errorf("docs/run.sh arrived with mode %v, want the executable bit set", script.Mode())
	}

	link, err := os.Lstat(reader.docsPath("latest.md"))
	if err != nil {
		t.Fatalf("stat the synced symlink: %v", err)
	}
	if link.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("docs/latest.md arrived as %v, want a symlink", link.Mode())
	}
	target, err := os.Readlink(reader.docsPath("latest.md"))
	if err != nil {
		t.Fatalf("read the synced symlink: %v", err)
	}
	requireEqual(t, "symlink target", target, "guide.md")

	// And the two docs directories are identical, entry for entry.
	requireSameTree(t, "docs after the round trip",
		snapshotTree(t, author.docsPath()), snapshotTree(t, reader.docsPath()))

	// Canonical itself records the modes git records.
	modes := w.git(w.origin, "ls-tree", "-r", w.canonicalHead()).stdout
	requireContains(t, "canonical tree", modes, "100755 blob")
	requireContains(t, "canonical tree", modes, "120000 blob")
	if got := w.canonicalPaths(w.canonicalHead()); len(got) != 5 {
		t.Errorf("canonical holds %v, want five entries", got)
	}
}

// --- S11: JSON ---------------------------------------------------------

// TestS11JSONSchemasAndErrorChannel is the agent-facing contract (§5.8):
// stable documents on stdout, diagnostics on stderr, and never both.
func TestS11JSONSchemasAndErrorChannel(t *testing.T) {
	t.Parallel()
	w := newWorld(t, map[string]string{"api.md": "line one\nline two\n"})
	ws := w.setup("json")

	target := w.advanceCanonical(map[string]string{
		"api.md":   "line one\nline two\n",
		"guide.md": "upstream guide\n",
	}, "canonical: add guide")

	// status --json
	var status struct {
		Project     string `json:"project"`
		WorkspaceID string `json:"workspace_id"`
		Base        *struct {
			Commit string `json:"commit"`
			Tree   string `json:"tree"`
		} `json:"base"`
		Canonical struct {
			Head              string `json:"head"`
			Empty             bool   `json:"empty"`
			FetchedEver       bool   `json:"fetched_ever"`
			DataAgeSeconds    int64  `json:"data_age_seconds"`
			PublicationBranch string `json:"publication_branch"`
		} `json:"canonical"`
		Relation struct {
			Known  bool `json:"known"`
			Behind int  `json:"behind"`
		} `json:"relation"`
		SyncPreview struct {
			Known     bool     `json:"known"`
			Clean     bool     `json:"clean"`
			Conflicts []string `json:"conflicts"`
		} `json:"sync_preview"`
		SyncInProgress bool `json:"sync_in_progress"`
	}
	out := ws.sanho("status", "--refresh", "--json")
	if strings.TrimSpace(out.stderr) != "" {
		t.Errorf("status --json wrote to stderr on success: %q", out.stderr)
	}
	if err := json.Unmarshal([]byte(out.stdout), &status); err != nil {
		t.Fatalf("parse status JSON: %v\n%s", err, out.stdout)
	}
	if status.Project != projectName || status.WorkspaceID == "" {
		t.Errorf("status identity = %+v, want project %s and a workspace id", status, projectName)
	}
	if status.Base == nil || status.Base.Commit == "" || status.Base.Tree == "" {
		t.Errorf("status base = %+v, want a recorded commit/tree pair", status.Base)
	}
	if status.Canonical.Head != target || status.Canonical.Empty || !status.Canonical.FetchedEver {
		t.Errorf("status canonical = %+v, want a fetched head at %s", status.Canonical, target)
	}
	if status.Canonical.PublicationBranch != "main" {
		t.Errorf("publication branch = %q, want main", status.Canonical.PublicationBranch)
	}
	if !status.Relation.Known || status.Relation.Behind != 1 {
		t.Errorf("status relation = %+v, want known and behind 1", status.Relation)
	}
	if !status.SyncPreview.Known || !status.SyncPreview.Clean || status.SyncPreview.Conflicts == nil {
		t.Errorf("status sync preview = %+v, want a known clean prediction with [] conflicts", status.SyncPreview)
	}
	if status.SyncInProgress {
		t.Error("sync_in_progress = true on a workspace with no sync")
	}

	// state --json
	var state struct {
		Home     string `json:"home"`
		Scope    string `json:"scope"`
		Projects []struct {
			Name        string `json:"name"`
			DocsRepoURL string `json:"docs_repo_url"`
			Head        string `json:"head"`
		} `json:"projects"`
		Workspaces []struct {
			WorkspaceID string `json:"workspace_id"`
			LocalPath   string `json:"local_path"`
			BaseCommit  string `json:"base_commit"`
			ActorEmail  string `json:"actor_email"`
		} `json:"workspaces"`
	}
	dump := ws.sanho("state", "--json")
	if err := json.Unmarshal([]byte(dump.stdout), &state); err != nil {
		t.Fatalf("parse state JSON: %v\n%s", err, dump.stdout)
	}
	if state.Home != w.home || state.Scope != projectName {
		t.Errorf("state header = %+v, want home %s scoped to %s", state, w.home, projectName)
	}
	if len(state.Projects) != 1 || state.Projects[0].DocsRepoURL != w.origin || state.Projects[0].Head != target {
		t.Errorf("state projects = %+v, want one project at %s, head %s", state.Projects, w.origin, target)
	}
	if len(state.Workspaces) != 1 || state.Workspaces[0].LocalPath != ws.dir ||
		state.Workspaces[0].BaseCommit == "" || state.Workspaces[0].ActorEmail != actorEmail {
		t.Errorf("state workspaces = %+v, want this checkout fully recorded", state.Workspaces)
	}

	// sync --json, clean and then conflicted: a conflicted sync is a
	// success reported in the document, not in the exit code.
	var sync struct {
		Status string `json:"status"`
		Base   *struct {
			Commit string `json:"commit"`
		} `json:"base"`
		Commit    string   `json:"commit"`
		Conflicts []string `json:"conflicts"`
	}
	clean := ws.sanho("sync", "--json")
	if err := json.Unmarshal([]byte(clean.stdout), &sync); err != nil {
		t.Fatalf("parse sync JSON: %v\n%s", err, clean.stdout)
	}
	if sync.Status != "synced" || sync.Base == nil || sync.Base.Commit != target ||
		sync.Commit == "" || sync.Conflicts == nil {
		t.Fatalf("sync JSON = %+v, want a synced document at %s with [] conflicts", sync, target)
	}

	ws.commitDocs("docs: my edit", map[string]string{"api.md": "line one\nMINE\n"})
	w.advanceCanonical(map[string]string{
		"api.md":   "line one\nTHEIRS\n",
		"guide.md": "upstream guide\n",
	}, "canonical: their edit")

	conflicted := ws.run("sync", "--json")
	requireExit(t, "conflicted sync", conflicted, 0)
	if err := json.Unmarshal([]byte(conflicted.stdout), &sync); err != nil {
		t.Fatalf("parse conflicted sync JSON: %v\n%s", err, conflicted.stdout)
	}
	if sync.Status != "conflicts" || len(sync.Conflicts) != 1 || sync.Conflicts[0] != "docs/api.md" {
		t.Fatalf("conflicted sync JSON = %+v, want status conflicts on docs/api.md", sync)
	}

	aborted := ws.sanho("sync", "--abort", "--json")
	if err := json.Unmarshal([]byte(aborted.stdout), &sync); err != nil {
		t.Fatalf("parse abort JSON: %v\n%s", err, aborted.stdout)
	}
	if sync.Status != statusAborted {
		t.Fatalf("abort status = %q, want %q", sync.Status, statusAborted)
	}

	// A failure under --json puts a machine-readable envelope on stdout
	// and the prose on stderr (§5.8, F-M9). Before this an agent had
	// nothing on the JSON channel to branch on and had to match English.
	outside := w.newWorkspace("outside")
	for _, args := range [][]string{
		{"status", "--json"},
		{"sync", "--json"},
		{"doctor", "--json"},
	} {
		res := outside.run(args...)
		requireExit(t, "sanho "+strings.Join(args, " ")+" outside a workspace", res, 1)

		var envelope struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(res.stdout), &envelope); err != nil {
			t.Errorf("sanho %s stdout is not a JSON document: %v\n%s",
				strings.Join(args, " "), err, res.stdout)
			continue
		}
		if envelope.Error.Code != "not_in_workspace" {
			t.Errorf("sanho %s error code = %q, want not_in_workspace",
				strings.Join(args, " "), envelope.Error.Code)
		}
		if envelope.Error.Message == "" {
			t.Errorf("sanho %s error envelope carries no message", strings.Join(args, " "))
		}
		requireContains(t, "stderr", res.stderr, "not a sanho workspace")
	}
}

// statusAborted is `sanho sync --abort`'s own outcome word: a distinct
// result, not a kind of sync.
const statusAborted = "aborted"
