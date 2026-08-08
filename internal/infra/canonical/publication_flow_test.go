package canonical

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/irootkernel/sanho/internal/domain/provenance"
	pubdom "github.com/irootkernel/sanho/internal/domain/publish"
	"github.com/irootkernel/sanho/internal/infra/appgit"
	"github.com/irootkernel/sanho/internal/infra/wsstate"
)

// The the publication contract publication flow, end to end over real git.
//
// These tests live in infra rather than beside usecase/publish because
// the repository's architecture gate (internal/architecture) forbids a
// usecase package from importing infra — in production code *and* in its
// test imports. Wiring canonical.Link, appgit.Repo and two real
// repositories together is therefore only legal here. usecase/publish's
// own tests cover the orchestration (retry budgets, gate ordering, error
// shapes) against port doubles; these cover the mechanics the doubles
// stand for, so that both halves are pinned against real git.

// flow is a publication fixture: an origin canonical repository, a
// workspace-private clone bound to an application repository, and the
// two adapters publication drives.
type flow struct {
	origin  string
	appDir  string
	store   *Store
	link    *Link
	app     *appgit.Repo
	factory *treeFactory
}

func newFlow(t *testing.T, canonicalFiles, docsFiles map[string]entry) *flow {
	t.Helper()

	origin := newOrigin(t, "main", canonicalFiles)

	appDir := newWorkRepo(t, "app")
	appFiles := map[string]entry{"README.md": text("readme\n")}
	for path, file := range docsFiles {
		appFiles["docs/"+path] = file
	}
	materialize(t, appDir, appFiles)
	gitRun(t, appDir, "add", "-A", "--", ".")
	gitRun(t, appDir, "commit", "--quiet", "-m", "docs: seed workspace")

	appGitDir := filepath.Join(appDir, ".git")
	store, err := Ensure(context.Background(), appGitDir, origin)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	return &flow{
		origin:  origin,
		appDir:  appDir,
		store:   store,
		link:    NewLink(store, appGitDir),
		app:     appgit.New(appDir, "docs", nil),
		factory: &treeFactory{dir: store.dir},
	}
}

// commitDocs replaces the app repo's docs directory and commits.
func (f *flow) commitDocs(t *testing.T, message string, docsFiles map[string]entry) string {
	t.Helper()

	docsRoot := filepath.Join(f.appDir, "docs")
	if err := os.RemoveAll(docsRoot); err != nil {
		t.Fatalf("clear docs: %v", err)
	}
	if err := os.MkdirAll(docsRoot, 0755); err != nil {
		t.Fatalf("create docs: %v", err)
	}
	materialize(t, docsRoot, docsFiles)

	gitRun(t, f.appDir, "add", "-A", "--", ".")
	gitRun(t, f.appDir, "commit", "--quiet", "-m", message)
	return gitLine(t, f.appDir, "rev-parse", "HEAD")
}

// publish performs one the publication contract publication attempt for tip against the
// currently fetched canonical head, using base as the recorded base. It
// returns the new canonical commit, the tree published, and the decided
// case.
func (f *flow) publish(t *testing.T, tip, baseCommit string) (newHead, publishedTree string, decided pubdom.Case) {
	t.Helper()
	ctx := context.Background()

	head, headTree, err := f.link.Head(ctx)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	tipTree, err := f.app.DocsTreeOf(ctx, tip)
	if err != nil {
		t.Fatalf("DocsTreeOf: %v", err)
	}

	known, err := f.link.ResolveCommit(ctx, baseCommit)
	if err != nil {
		t.Fatalf("ResolveCommit: %v", err)
	}
	ancestor := false
	if known {
		if ancestor, err = f.link.IsAncestor(ctx, baseCommit, head); err != nil {
			t.Fatalf("IsAncestor: %v", err)
		}
	}

	decided = pubdom.Decide(pubdom.Inputs{
		Base:           provenance.Base{Commit: baseCommit},
		TipDocsTree:    tipTree,
		Head:           head,
		HeadDocsTree:   headTree,
		BaseKnown:      known,
		BaseIsAncestor: ancestor,
	})

	if err := f.link.FetchFromApp(ctx, tip); err != nil {
		t.Fatalf("FetchFromApp: %v", err)
	}

	switch decided {
	case pubdom.CaseUpToDate:
		return "", "", decided
	case pubdom.CaseFastForward:
		publishedTree = tipTree
	case pubdom.CaseAutoMerge:
		merged, conflicts, clean, err := f.link.MergeDocs(ctx, baseCommit, tipTree, head)
		if err != nil {
			t.Fatalf("MergeDocs: %v", err)
		}
		if !clean {
			t.Fatalf("expected a clean merge, got conflicts %v", conflicts)
		}
		publishedTree = merged
	default:
		t.Fatalf("unexpected case %v", decided)
	}

	repoName, branch, err := f.app.RepoIdentity(ctx)
	if err != nil {
		t.Fatalf("RepoIdentity: %v", err)
	}
	subjects, err := f.app.DocsCommitSubjects(ctx, "", tip)
	if err != nil {
		t.Fatalf("DocsCommitSubjects: %v", err)
	}
	message := pubdom.CommitMeta{
		RepoName:    repoName,
		Branch:      branch,
		WorkspaceID: "flow-workspace",
		TipOID:      tip,
		Subjects:    subjects,
	}.Message()

	newHead, err = f.link.CommitDocsTree(ctx, publishedTree, head, "Publisher", "publisher@example.test", message)
	if err != nil {
		t.Fatalf("CommitDocsTree: %v", err)
	}
	if err := f.link.PushHead(ctx, newHead, head); err != nil {
		t.Fatalf("PushHead: %v", err)
	}
	return newHead, publishedTree, decided
}

// TestFlowCaseUpToDate: a tip whose docs already equal canonical
// publishes nothing (the publication contract).
func TestFlowCaseUpToDate(t *testing.T) {
	files := map[string]entry{"a.md": text("alpha\n")}
	f := newFlow(t, files, files)
	ctx := context.Background()

	head, headTree, err := f.link.Head(ctx)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	tip := gitLine(t, f.appDir, "rev-parse", "HEAD")
	tipTree, err := f.app.DocsTreeOf(ctx, tip)
	if err != nil {
		t.Fatalf("DocsTreeOf: %v", err)
	}
	if tipTree != headTree {
		t.Fatalf("fixture is wrong: tip tree %s != canonical tree %s", tipTree, headTree)
	}

	newHead, _, decided := f.publish(t, tip, head)
	if decided != pubdom.CaseUpToDate {
		t.Fatalf("case = %v, want up to date", decided)
	}
	if newHead != "" {
		t.Fatalf("case  published %s", newHead)
	}
	if got := gitLine(t, f.origin, "rev-parse", "refs/heads/main"); got != head {
		t.Fatalf("origin moved to %s, want %s", got, head)
	}
}

// TestFlowCaseFastForward: base == canonical head, so the tip's own docs
// tree is published on top of it (the publication contract), with the publication contract commit
// convention and the actor as author.
func TestFlowCaseFastForward(t *testing.T) {
	f := newFlow(t,
		map[string]entry{"a.md": text("alpha\n")},
		map[string]entry{"a.md": text("alpha\n")},
	)
	ctx := context.Background()

	base, _, err := f.link.Head(ctx)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	tip := f.commitDocs(t, "docs: extend alpha", map[string]entry{
		"a.md": text("alpha extended\n"),
		"b.md": text("beta\n"),
	})

	newHead, publishedTree, decided := f.publish(t, tip, base)
	if decided != pubdom.CaseFastForward {
		t.Fatalf("case = %v, want fast forward", decided)
	}

	if got := gitLine(t, f.origin, "rev-parse", "refs/heads/main"); got != newHead {
		t.Fatalf("origin head = %s, want the published %s", got, newHead)
	}
	if got := gitLine(t, f.origin, "rev-parse", newHead+"^{tree}"); got != publishedTree {
		t.Fatalf("published root tree = %s, want the tip docs tree %s", got, publishedTree)
	}
	if parent := gitLine(t, f.origin, "rev-parse", newHead+"^"); parent != base {
		t.Fatalf("published parent = %s, want %s", parent, base)
	}

	// The canonical repository is docs-only: the app's docs subtree
	// becomes the canonical root tree.
	names := gitRun(t, f.origin, "ls-tree", "-r", "--name-only", newHead)
	if strings.Contains(names, "docs/") || !strings.Contains(names, "a.md") || !strings.Contains(names, "b.md") {
		t.Fatalf("canonical layout is wrong:\n%s", names)
	}

	subject := gitLine(t, f.origin, "log", "-1", "--format=%s", newHead)
	if want := "[SANHO] Publish docs from app/main (2 app commits)"; subject != want {
		t.Fatalf("subject = %q, want %q", subject, want)
	}
	body := gitRun(t, f.origin, "log", "-1", "--format=%B", newHead)
	for _, fragment := range []string{
		"source: flow-workspace @ " + tip,
		"commits:\n  - docs: seed workspace\n  - docs: extend alpha\n",
	} {
		if !strings.Contains(body, fragment) {
			t.Errorf("commit body is missing %q:\n%s", fragment, body)
		}
	}
	if author := gitLine(t, f.origin, "log", "-1", "--format=%an <%ae>", newHead); author != "Publisher <publisher@example.test>" {
		t.Fatalf("author = %q, want the actor", author)
	}
}

// TestFlowCaseAutoMergeClean: upstream moved and the tip changed a
// different file, so publication merges and continues without user
// friction (the publication contract clean) — and never touches the worktree.
func TestFlowCaseAutoMergeClean(t *testing.T) {
	f := newFlow(t,
		map[string]entry{"a.md": text("alpha\n"), "b.md": text("beta\n")},
		map[string]entry{"a.md": text("alpha\n"), "b.md": text("beta\n")},
	)
	ctx := context.Background()

	base, _, err := f.link.Head(ctx)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}

	// Upstream edits b.md; the workspace edits a.md.
	commitToOrigin(t, f.origin, "main", map[string]entry{
		"a.md": text("alpha\n"),
		"b.md": text("beta upstream\n"),
	})
	if err := f.link.Fetch(ctx); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	tip := f.commitDocs(t, "docs: edit alpha", map[string]entry{
		"a.md": text("alpha local\n"),
		"b.md": text("beta\n"),
	})

	worktreeBefore, err := f.app.WorktreeDocsTree(ctx)
	if err != nil {
		t.Fatalf("WorktreeDocsTree: %v", err)
	}
	statusBefore := gitRun(t, f.appDir, "status", "--porcelain")

	newHead, publishedTree, decided := f.publish(t, tip, base)
	if decided != pubdom.CaseAutoMerge {
		t.Fatalf("case = %v, want auto merge", decided)
	}

	if got := gitRun(t, f.origin, "cat-file", "blob", newHead+":a.md"); got != "alpha local\n" {
		t.Errorf("published a.md = %q, want the workspace edit", got)
	}
	if got := gitRun(t, f.origin, "cat-file", "blob", newHead+":b.md"); got != "beta upstream\n" {
		t.Errorf("published b.md = %q, want the upstream edit", got)
	}

	// Worktree inviolability: publication changed nothing app-side.
	worktreeAfter, err := f.app.WorktreeDocsTree(ctx)
	if err != nil {
		t.Fatalf("WorktreeDocsTree after publish: %v", err)
	}
	if worktreeAfter != worktreeBefore {
		t.Fatalf("publication changed the docs worktree: %s -> %s", worktreeBefore, worktreeAfter)
	}
	if after := gitRun(t, f.appDir, "status", "--porcelain"); after != statusBefore {
		t.Fatalf("publication changed app status:\nbefore:\n%s\nafter:\n%s", statusBefore, after)
	}
	if head := gitLine(t, f.appDir, "rev-parse", "HEAD"); head != tip {
		t.Fatalf("publication moved app HEAD to %s", head)
	}

	// The published tree differs from the worktree tree, which is
	// exactly the state in which the publication contract step 6 must NOT advance the base.
	if publishedTree == worktreeAfter {
		t.Fatal("fixture is wrong: a merged publish should differ from the worktree")
	}
}

// TestFlowCaseAutoMergeConflicted: same region edited on both sides, so
// the merge conflicts, the paths are named, and origin is untouched
// (the publication contract conflicted).
func TestFlowCaseAutoMergeConflicted(t *testing.T) {
	f := newFlow(t,
		map[string]entry{"a.md": hunkFile("A", "B", "C"), "b.md": hunkFile("A", "B", "C")},
		map[string]entry{"a.md": hunkFile("A", "B", "C"), "b.md": hunkFile("A", "B", "C")},
	)
	ctx := context.Background()

	base, _, err := f.link.Head(ctx)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}

	// Two conflicting hunks in a.md and one in b.md: the multi-hunk
	// shape that wedged v0.1 (audit C2).
	commitToOrigin(t, f.origin, "main", map[string]entry{
		"a.md": hunkFile("A-upstream", "B-upstream", "C"),
		"b.md": hunkFile("A-upstream", "B", "C"),
	})
	if err := f.link.Fetch(ctx); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	tip := f.commitDocs(t, "docs: local edits", map[string]entry{
		"a.md": hunkFile("A-local", "B-local", "C"),
		"b.md": hunkFile("A-local", "B", "C"),
	})

	originBefore := gitLine(t, f.origin, "rev-parse", "refs/heads/main")
	tipTree, err := f.app.DocsTreeOf(ctx, tip)
	if err != nil {
		t.Fatalf("DocsTreeOf: %v", err)
	}
	head, _, err := f.link.Head(ctx)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if err := f.link.FetchFromApp(ctx, tip); err != nil {
		t.Fatalf("FetchFromApp: %v", err)
	}

	merged, conflicts, clean, err := f.link.MergeDocs(ctx, base, tipTree, head)
	if err != nil {
		t.Fatalf("MergeDocs: %v", err)
	}
	if clean {
		t.Fatal("expected a conflicted merge")
	}
	want := []string{"a.md", "b.md"}
	if strings.Join(conflicts, ",") != strings.Join(want, ",") {
		t.Fatalf("conflicts = %v, want %v", conflicts, want)
	}

	// The conflicted result carries both hunks in a.md, labeled by ref.
	body := gitRun(t, f.store.dir, "cat-file", "blob", merged+":a.md")
	if n := countMarkerStarts(body); n != 2 {
		t.Fatalf("a.md has %d conflict regions, want 2:\n%s", n, body)
	}
	if !strings.Contains(body, "<<<<<<< "+labelOurs) || !strings.Contains(body, ">>>>>>> "+labelUpstream) {
		t.Fatalf("conflict markers are not labeled by ref:\n%s", body)
	}

	// A rejected publish changes no remote ref (the guidance contract).
	if after := gitLine(t, f.origin, "rev-parse", "refs/heads/main"); after != originBefore {
		t.Fatalf("origin moved to %s during a rejected publish, want %s", after, originBefore)
	}
}

// TestFlowCaseUnknownBaseReanchors: canonical history was rewritten, but
// a commit with the recorded docs-base-tree still exists, so publication
// re-anchors and proceeds (the publication contract, D2).
func TestFlowCaseUnknownBaseReanchors(t *testing.T) {
	f := newFlow(t,
		map[string]entry{"a.md": text("alpha\n")},
		map[string]entry{"a.md": text("alpha\n")},
	)
	ctx := context.Background()

	staleBase, baseTree, err := f.link.Head(ctx)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}

	// Rewrite canonical: a fresh root commit carrying the identical
	// tree, then one more commit on top. The old base OID becomes
	// unreachable while its *content* survives — exactly what a squash
	// or rebase does.
	rewritten := newWorkRepo(t, "rewrite")
	materialize(t, rewritten, map[string]entry{"a.md": text("alpha\n")})
	gitRun(t, rewritten, "add", "-A", "--", ".")
	gitRun(t, rewritten, "commit", "--quiet", "-m", "canonical: rewritten root")
	anchorTree := gitLine(t, rewritten, "rev-parse", "HEAD^{tree}")
	if anchorTree != baseTree {
		t.Fatalf("fixture is wrong: rewritten tree %s != base tree %s", anchorTree, baseTree)
	}
	anchor := gitLine(t, rewritten, "rev-parse", "HEAD")
	materialize(t, rewritten, map[string]entry{"a.md": text("alpha\n"), "c.md": text("gamma\n")})
	gitRun(t, rewritten, "add", "-A", "--", ".")
	gitRun(t, rewritten, "commit", "--quiet", "-m", "canonical: after rewrite")
	gitRun(t, rewritten, "push", "--quiet", "--force", f.origin, "HEAD:main")

	if err := f.link.Fetch(ctx); err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	present, err := f.link.ResolveCommit(ctx, staleBase)
	if err != nil {
		t.Fatalf("ResolveCommit: %v", err)
	}
	if present {
		// The clone still holds the old objects locally; what matters
		// is that the old base is no longer an ancestor of head.
		head, _, err := f.link.Head(ctx)
		if err != nil {
			t.Fatalf("Head: %v", err)
		}
		ancestor, err := f.link.IsAncestor(ctx, staleBase, head)
		if err != nil {
			t.Fatalf("IsAncestor: %v", err)
		}
		if ancestor {
			t.Fatal("fixture is wrong: the rewritten history still contains the old base")
		}
	}

	found, ok, err := f.link.FindCommitByDocsTree(ctx, baseTree)
	if err != nil {
		t.Fatalf("FindCommitByDocsTree: %v", err)
	}
	if !ok {
		t.Fatal("re-anchoring failed: no commit carries the recorded docs-base-tree")
	}
	if found != anchor {
		t.Fatalf("re-anchored to %s, want %s", found, anchor)
	}

	// Publication resumes from the re-anchored base as an ordinary
	// case .
	tip := f.commitDocs(t, "docs: post-rewrite edit", map[string]entry{
		"a.md": text("alpha local\n"),
	})
	newHead, _, decided := f.publish(t, tip, found)
	if decided != pubdom.CaseAutoMerge {
		t.Fatalf("case = %v, want auto merge after re-anchoring", decided)
	}
	if got := gitRun(t, f.origin, "cat-file", "blob", newHead+":a.md"); got != "alpha local\n" {
		t.Errorf("published a.md = %q, want the local edit", got)
	}
	if got := gitRun(t, f.origin, "cat-file", "blob", newHead+":c.md"); got != "gamma\n" {
		t.Errorf("published c.md = %q, want the rewritten upstream file", got)
	}
}

// TestFlowCaseUnknownBaseWithoutAnAnchor: the recorded tree exists
// nowhere in the rewritten history, which is the state the publication contract rejects with
// the rewrite-recovery message.
func TestFlowCaseUnknownBaseWithoutAnAnchor(t *testing.T) {
	f := newFlow(t,
		map[string]entry{"a.md": text("alpha\n")},
		map[string]entry{"a.md": text("alpha\n")},
	)
	ctx := context.Background()

	_, baseTree, err := f.link.Head(ctx)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}

	rewritten := newWorkRepo(t, "rewrite")
	materialize(t, rewritten, map[string]entry{"a.md": text("something else entirely\n")})
	gitRun(t, rewritten, "add", "-A", "--", ".")
	gitRun(t, rewritten, "commit", "--quiet", "-m", "canonical: unrelated rewrite")
	gitRun(t, rewritten, "push", "--quiet", "--force", f.origin, "HEAD:main")

	if err := f.link.Fetch(ctx); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if _, ok, err := f.link.FindCommitByDocsTree(ctx, baseTree); err != nil || ok {
		t.Fatalf("FindCommitByDocsTree = (%v, %v), want (false, nil)", ok, err)
	}
}

// TestFlowCASRaceRetrySucceeds: a publisher that loses the lease
// refetches, re-merges against the winner and publishes — the publication contract
// bounded retry path over real git.
func TestFlowCASRaceRetrySucceeds(t *testing.T) {
	f := newFlow(t,
		map[string]entry{"a.md": text("alpha\n"), "b.md": text("beta\n")},
		map[string]entry{"a.md": text("alpha\n"), "b.md": text("beta\n")},
	)
	ctx := context.Background()

	base, _, err := f.link.Head(ctx)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	tip := f.commitDocs(t, "docs: local edit", map[string]entry{
		"a.md": text("alpha local\n"),
		"b.md": text("beta\n"),
	})
	tipTree, err := f.app.DocsTreeOf(ctx, tip)
	if err != nil {
		t.Fatalf("DocsTreeOf: %v", err)
	}
	if err := f.link.FetchFromApp(ctx, tip); err != nil {
		t.Fatalf("FetchFromApp: %v", err)
	}

	// Attempt 1 decides a fast-forward against the head it fetched...
	head, _, err := f.link.Head(ctx)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	attempt, err := f.link.CommitDocsTree(ctx, tipTree, head, "P", "p@example.test", "docs: attempt 1\n")
	if err != nil {
		t.Fatalf("CommitDocsTree: %v", err)
	}

	// ...but another publisher lands first.
	raced := commitToOrigin(t, f.origin, "main", map[string]entry{
		"a.md": text("alpha\n"),
		"b.md": text("beta upstream\n"),
	})

	err = f.link.PushHead(ctx, attempt, head)
	if !errors.Is(err, ErrNonFastForward) {
		t.Fatalf("PushHead error = %v, want ErrNonFastForward", err)
	}

	// Attempt 2: refetch and re-enter the case analysis. The merge must
	// be recomputed against the new head, never replayed.
	if err := f.link.Fetch(ctx); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	newHead, _, decided := f.publish(t, tip, base)
	if decided != pubdom.CaseAutoMerge {
		t.Fatalf("case on retry = %v, want auto merge against the winner", decided)
	}
	if parent := gitLine(t, f.origin, "rev-parse", newHead+"^"); parent != raced {
		t.Fatalf("published parent = %s, want the racer's commit %s", parent, raced)
	}
	if got := gitRun(t, f.origin, "cat-file", "blob", newHead+":a.md"); got != "alpha local\n" {
		t.Errorf("published a.md = %q, want the local edit", got)
	}
	if got := gitRun(t, f.origin, "cat-file", "blob", newHead+":b.md"); got != "beta upstream\n" {
		t.Errorf("published b.md = %q, want the racer's edit", got)
	}
}

// TestFlowMarkerGateRejectsCommittedMarkers: the pre-push gate reads
// committed docs blobs, so markers that were committed by mistake never
// reach canonical (the publication contract step 3).
func TestFlowMarkerGateRejectsCommittedMarkers(t *testing.T) {
	f := newFlow(t,
		map[string]entry{"a.md": text("alpha\n")},
		map[string]entry{"a.md": text("alpha\n")},
	)

	tip := f.commitDocs(t, "docs: oops, committed markers", map[string]entry{
		"a.md": text("intro\n<<<<<<< sanho-ours\nmine\n=======\ntheirs\n>>>>>>> sanho-upstream\n"),
		"b.md": text("clean\n"),
	})

	paths, err := f.app.ScanDocsBlobsAgainst(context.Background(), "", tip)
	if err != nil {
		t.Fatalf("ScanDocsBlobsForMarkers: %v", err)
	}
	if len(paths) != 1 || paths[0] != "docs/a.md" {
		t.Fatalf("marker paths = %v, want [docs/a.md]", paths)
	}
}

// TestFlowBaseAdvanceRule exercises the publication contract step 6 on both sides, against
// the real base file: the base moves only when the docs worktree equals
// what was published.
func TestFlowBaseAdvanceRule(t *testing.T) {
	// advanceBase mirrors the publication contract step 6 over the real wsstate base file.
	advanceBase := func(t *testing.T, f *flow, published, publishedTree string) bool {
		t.Helper()
		worktree, err := f.app.WorktreeDocsTree(context.Background())
		if err != nil {
			t.Fatalf("WorktreeDocsTree: %v", err)
		}
		if worktree != publishedTree {
			return false
		}
		if err := wsstate.SaveBase(f.appDir, provenance.Base{Commit: published, Tree: publishedTree}); err != nil {
			t.Fatalf("SaveBase: %v", err)
		}
		return true
	}

	loadBase := func(t *testing.T, f *flow) provenance.Base {
		t.Helper()
		base, ok, err := wsstate.LoadBase(f.appDir)
		if err != nil {
			t.Fatalf("LoadBase: %v", err)
		}
		if !ok {
			t.Fatal("LoadBase found no base file")
		}
		return base
	}

	t.Run("advances after a fast-forward publish", func(t *testing.T) {
		f := newFlow(t,
			map[string]entry{"a.md": text("alpha\n")},
			map[string]entry{"a.md": text("alpha\n")},
		)
		ctx := context.Background()

		base, baseTree, err := f.link.Head(ctx)
		if err != nil {
			t.Fatalf("Head: %v", err)
		}
		if err := wsstate.SaveBase(f.appDir, provenance.Base{Commit: base, Tree: baseTree}); err != nil {
			t.Fatalf("SaveBase: %v", err)
		}
		tip := f.commitDocs(t, "docs: edit", map[string]entry{"a.md": text("alpha local\n")})

		published, publishedTree, _ := f.publish(t, tip, base)
		if !advanceBase(t, f, published, publishedTree) {
			t.Fatal("the base did not advance after a fast-forward publish with a clean worktree")
		}
		if got, want := loadBase(t, f), (provenance.Base{Commit: published, Tree: publishedTree}); got != want {
			t.Fatalf("base file = %+v, want %+v", got, want)
		}
	})

	t.Run("does not advance after a merged publish", func(t *testing.T) {
		f := newFlow(t,
			map[string]entry{"a.md": text("alpha\n"), "b.md": text("beta\n")},
			map[string]entry{"a.md": text("alpha\n"), "b.md": text("beta\n")},
		)
		ctx := context.Background()

		base, baseTree, err := f.link.Head(ctx)
		if err != nil {
			t.Fatalf("Head: %v", err)
		}
		if err := wsstate.SaveBase(f.appDir, provenance.Base{Commit: base, Tree: baseTree}); err != nil {
			t.Fatalf("SaveBase: %v", err)
		}

		commitToOrigin(t, f.origin, "main", map[string]entry{
			"a.md": text("alpha\n"),
			"b.md": text("beta upstream\n"),
		})
		if err := f.link.Fetch(ctx); err != nil {
			t.Fatalf("Fetch: %v", err)
		}
		tip := f.commitDocs(t, "docs: edit", map[string]entry{
			"a.md": text("alpha local\n"),
			"b.md": text("beta\n"),
		})

		published, publishedTree, decided := f.publish(t, tip, base)
		if decided != pubdom.CaseAutoMerge {
			t.Fatalf("case = %v, want auto merge", decided)
		}
		if advanceBase(t, f, published, publishedTree) {
			t.Fatal("the base advanced although the worktree never saw the merged tree")
		}
		if got, want := loadBase(t, f), (provenance.Base{Commit: base, Tree: baseTree}); got != want {
			t.Fatalf("base file = %+v, want it left at %+v", got, want)
		}
	})
}

// TestFlowDocsFreeTipPublishesTheEmptyTree covers the "docs dir absent"
// corner: the tip's docs tree is the empty tree, which is a legitimate
// publication input rather than an error.
func TestFlowDocsFreeTipPublishesTheEmptyTree(t *testing.T) {
	f := newFlow(t,
		map[string]entry{"a.md": text("alpha\n")},
		map[string]entry{"a.md": text("alpha\n")},
	)
	ctx := context.Background()

	base, _, err := f.link.Head(ctx)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(f.appDir, "docs")); err != nil {
		t.Fatalf("remove docs: %v", err)
	}
	gitRun(t, f.appDir, "add", "-A", "--", ".")
	gitRun(t, f.appDir, "commit", "--quiet", "-m", "docs: remove all docs")
	tip := gitLine(t, f.appDir, "rev-parse", "HEAD")

	empty, err := f.app.EmptyTree(ctx)
	if err != nil {
		t.Fatalf("EmptyTree: %v", err)
	}
	tipTree, err := f.app.DocsTreeOf(ctx, tip)
	if err != nil {
		t.Fatalf("DocsTreeOf: %v", err)
	}
	if tipTree != empty {
		t.Fatalf("docs tree of a docs-free tip = %s, want the empty tree %s", tipTree, empty)
	}

	newHead, _, decided := f.publish(t, tip, base)
	if decided != pubdom.CaseFastForward {
		t.Fatalf("case = %v, want fast forward", decided)
	}
	if listing := strings.TrimSpace(gitRun(t, f.origin, "ls-tree", "-r", newHead)); listing != "" {
		t.Fatalf("published tree is not empty:\n%s", listing)
	}
}
