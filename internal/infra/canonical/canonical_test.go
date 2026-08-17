package canonical

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newOrigin creates a bare canonical docs repository seeded with one
// commit on branch. The canonical repository is docs-only, so files land
// at the repository root — its root tree is its docs tree.
func newOrigin(t *testing.T, branch string, files map[string]entry) string {
	t.Helper()

	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	if err := os.MkdirAll(origin, 0755); err != nil {
		t.Fatalf("create %s: %v", origin, err)
	}
	gitRun(t, origin, "init", "--bare", "--quiet", "-b", branch)

	seed := filepath.Join(root, "seed")
	if err := os.MkdirAll(seed, 0755); err != nil {
		t.Fatalf("create %s: %v", seed, err)
	}
	gitRun(t, seed, "init", "--quiet", "-b", branch)
	materialize(t, seed, files)
	gitRun(t, seed, "add", "-A", "--", ".")
	gitRun(t, seed, "commit", "--quiet", "-m", "canonical: seed")
	gitRun(t, seed, "push", "--quiet", origin, branch)

	return origin
}

// commitToOrigin adds one commit to origin out of band, the way a
// concurrent publisher from another machine would.
func commitToOrigin(t *testing.T, origin, branch string, files map[string]entry) string {
	t.Helper()

	work := filepath.Join(t.TempDir(), "racer")
	if err := os.MkdirAll(work, 0755); err != nil {
		t.Fatalf("create %s: %v", work, err)
	}
	gitRun(t, work, "clone", "--quiet", "--branch", branch, origin, ".")
	materialize(t, work, files)
	gitRun(t, work, "add", "-A", "--", ".")
	gitRun(t, work, "commit", "--quiet", "-m", "canonical: racer")
	gitRun(t, work, "push", "--quiet", "origin", "HEAD:"+branch)

	return gitLine(t, work, "rev-parse", "HEAD")
}

// ensureStore builds a clone of origin under a fresh common dir.
func ensureStore(t *testing.T, origin string) *Store {
	t.Helper()
	store, err := Ensure(context.Background(), t.TempDir(), origin)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	return store
}

func TestEnsureCreatesCloneOnMain(t *testing.T) {
	origin := newOrigin(t, "main", map[string]entry{"a.md": text("a\n")})
	commonDir := t.TempDir()

	store, err := Ensure(context.Background(), commonDir, origin)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	if got, want := store.Dir(), CloneDir(commonDir); got != want {
		t.Errorf("Dir() = %q, want %q", got, want)
	}
	if got := store.Branch(); got != "main" {
		t.Errorf("Branch() = %q, want main", got)
	}
	if got := store.URL(); got != origin {
		t.Errorf("URL() = %q, want %q", got, origin)
	}
	if bare := gitLine(t, store.Dir(), "rev-parse", "--is-bare-repository"); bare != "true" {
		t.Errorf("clone is not bare: %q", bare)
	}
	if url := gitLine(t, store.Dir(), "config", "--get", "remote.origin.url"); url != origin {
		t.Errorf("origin URL = %q, want %q", url, origin)
	}
	if branch := gitLine(t, store.Dir(), "config", "--get", branchConfigKey); branch != "main" {
		t.Errorf("persisted %s = %q, want main", branchConfigKey, branch)
	}
}

func TestEnsureFallsBackToMaster(t *testing.T) {
	origin := newOrigin(t, "master", map[string]entry{"a.md": text("a\n")})

	store := ensureStore(t, origin)
	if got := store.Branch(); got != "master" {
		t.Fatalf("Branch() = %q, want master", got)
	}

	head, tree, err := store.Head(context.Background())
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if head == "" || tree == "" {
		t.Fatalf("Head() = (%q, %q), want both populated", head, tree)
	}
}

// TestOpenReadsBackPersistedBranch asserts the publication branch
// survives process boundaries without a network round-trip.
func TestOpenReadsBackPersistedBranch(t *testing.T) {
	origin := newOrigin(t, "master", map[string]entry{"a.md": text("a\n")})
	commonDir := t.TempDir()

	if _, err := Ensure(context.Background(), commonDir, origin); err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	reopened, err := Open(commonDir, origin)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got := reopened.Branch(); got != "master" {
		t.Fatalf("Branch() after Open = %q, want master", got)
	}
}

func TestOpenRejectsMissingClone(t *testing.T) {
	if _, err := Open(t.TempDir(), "https://example.test/docs.git"); err == nil {
		t.Fatal("Open accepted a common dir with no clone")
	}
}

// TestEnsureIsIdempotentAndRepairsOrigin asserts a second Ensure reuses
// the clone and reconciles a changed docs repository URL.
func TestEnsureIsIdempotentAndRepairsOrigin(t *testing.T) {
	origin := newOrigin(t, "main", map[string]entry{"a.md": text("a\n")})
	moved := newOrigin(t, "main", map[string]entry{"a.md": text("moved\n")})
	commonDir := t.TempDir()
	ctx := context.Background()

	first, err := Ensure(ctx, commonDir, origin)
	if err != nil {
		t.Fatalf("first Ensure: %v", err)
	}
	head, _, err := first.Head(ctx)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}

	second, err := Ensure(ctx, commonDir, moved)
	if err != nil {
		t.Fatalf("second Ensure: %v", err)
	}
	if second.Dir() != first.Dir() {
		t.Errorf("second Ensure moved the clone: %q then %q", first.Dir(), second.Dir())
	}
	if url := gitLine(t, second.Dir(), "config", "--get", "remote.origin.url"); url != moved {
		t.Errorf("origin URL = %q, want the reconciled %q", url, moved)
	}
	// Reconciliation must not fetch: the previously fetched head is
	// still what the read paths serve.
	stillHead, _, err := second.Head(ctx)
	if err != nil {
		t.Fatalf("Head after reconcile: %v", err)
	}
	if stillHead != head {
		t.Errorf("reconcile changed the cached head from %s to %s", head, stillHead)
	}
}

func TestEnsureFailsOnUnreachableOrigin(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent.git")
	commonDir := t.TempDir()

	_, err := Ensure(context.Background(), commonDir, missing)
	if err == nil {
		t.Fatal("Ensure accepted an unreachable origin")
	}
	if !errors.Is(err, ErrUnreachable) {
		t.Fatalf("error = %v, want it to wrap ErrUnreachable", err)
	}
	if _, statErr := os.Stat(CloneDir(commonDir)); !os.IsNotExist(statErr) {
		t.Fatalf("a failed Ensure left a half-built clone at %s", CloneDir(commonDir))
	}
}

func TestFetchRecordsAgeAndAdvancesHead(t *testing.T) {
	origin := newOrigin(t, "main", map[string]entry{"a.md": text("a\n")})
	store := ensureStore(t, origin)
	ctx := context.Background()

	age, ok := store.Age()
	if !ok {
		t.Fatal("Age() reported no fetch after Ensure")
	}
	if age < 0 || age > time.Minute {
		t.Fatalf("Age() = %v, want a fresh duration", age)
	}

	before, _, err := store.Head(ctx)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}

	advanced := commitToOrigin(t, origin, "main", map[string]entry{"a.md": text("a\n"), "b.md": text("b\n")})

	// Before fetching, the clone still serves the previous head: reads
	// are explicitly last-fetch based (the private-clone contract).
	cached, _, err := store.Head(ctx)
	if err != nil {
		t.Fatalf("Head before fetch: %v", err)
	}
	if cached != before {
		t.Fatalf("Head() moved without a fetch: %s -> %s", before, cached)
	}

	if err := store.Fetch(ctx); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	after, tree, err := store.Head(ctx)
	if err != nil {
		t.Fatalf("Head after fetch: %v", err)
	}
	if after != advanced {
		t.Fatalf("Head() = %s after fetch, want %s", after, advanced)
	}
	if tree == "" {
		t.Fatal("Head() returned an empty docs tree")
	}
}

func TestAgeReportsNoFetchWhenMarkerAbsent(t *testing.T) {
	origin := newOrigin(t, "main", map[string]entry{"a.md": text("a\n")})
	store := ensureStore(t, origin)

	if err := os.Remove(filepath.Join(store.Dir(), fetchMarkerName)); err != nil {
		t.Fatalf("remove fetch marker: %v", err)
	}
	if _, ok := store.Age(); ok {
		t.Fatal("Age() reported a fetch time with no marker file")
	}

	if err := os.WriteFile(filepath.Join(store.Dir(), fetchMarkerName), []byte("not a timestamp"), 0644); err != nil {
		t.Fatalf("write corrupt marker: %v", err)
	}
	if _, ok := store.Age(); ok {
		t.Fatal("Age() guessed an age from a corrupt marker file")
	}
}

func TestFetchFailsClosedOnUnreachableOrigin(t *testing.T) {
	origin := newOrigin(t, "main", map[string]entry{"a.md": text("a\n")})
	store := ensureStore(t, origin)

	if err := os.RemoveAll(origin); err != nil {
		t.Fatalf("remove origin: %v", err)
	}
	err := store.Fetch(context.Background())
	if err == nil {
		t.Fatal("Fetch succeeded against a deleted origin")
	}
	if !errors.Is(err, ErrUnreachable) {
		t.Fatalf("error = %v, want it to wrap ErrUnreachable", err)
	}
}

func TestResolveCommitIsAncestorAndDistance(t *testing.T) {
	origin := newOrigin(t, "main", map[string]entry{"a.md": text("a\n")})
	store := ensureStore(t, origin)
	ctx := context.Background()

	first, _, err := store.Head(ctx)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	second := commitToOrigin(t, origin, "main", map[string]entry{"a.md": text("a2\n")})
	if err := store.Fetch(ctx); err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	for _, oid := range []string{first, second} {
		present, err := store.ResolveCommit(ctx, oid)
		if err != nil {
			t.Fatalf("ResolveCommit(%s): %v", oid, err)
		}
		if !present {
			t.Errorf("ResolveCommit(%s) = false, want true", oid)
		}
	}

	for _, oid := range []string{"", "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef", "not-an-oid"} {
		present, err := store.ResolveCommit(ctx, oid)
		if err != nil {
			t.Fatalf("ResolveCommit(%q): %v", oid, err)
		}
		if present {
			t.Errorf("ResolveCommit(%q) = true, want false", oid)
		}
	}

	ancestor, err := store.IsAncestor(ctx, first, second)
	if err != nil {
		t.Fatalf("IsAncestor: %v", err)
	}
	if !ancestor {
		t.Error("IsAncestor(first, second) = false, want true")
	}

	reverse, err := store.IsAncestor(ctx, second, first)
	if err != nil {
		t.Fatalf("IsAncestor reverse: %v", err)
	}
	if reverse {
		t.Error("IsAncestor(second, first) = true, want false")
	}

	self, err := store.IsAncestor(ctx, second, second)
	if err != nil {
		t.Fatalf("IsAncestor self: %v", err)
	}
	if !self {
		t.Error("IsAncestor(x, x) = false, want true (a commit is its own ancestor here)")
	}

	behind, ahead, err := store.Distance(ctx, first, second)
	if err != nil {
		t.Fatalf("Distance: %v", err)
	}
	if behind != 1 || ahead != 0 {
		t.Errorf("Distance(first, second) = (behind %d, ahead %d), want (1, 0)", behind, ahead)
	}
}

func TestFindCommitByDocsTree(t *testing.T) {
	origin := newOrigin(t, "main", map[string]entry{"a.md": text("a\n")})
	store := ensureStore(t, origin)
	ctx := context.Background()

	first, firstTree, err := store.Head(ctx)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	commitToOrigin(t, origin, "main", map[string]entry{"a.md": text("a2\n")})
	if err := store.Fetch(ctx); err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	found, ok, err := store.FindCommitByDocsTree(ctx, firstTree)
	if err != nil {
		t.Fatalf("FindCommitByDocsTree: %v", err)
	}
	if !ok || found != first {
		t.Fatalf("FindCommitByDocsTree(%s) = (%s, %v), want (%s, true)", firstTree, found, ok, first)
	}

	_, ok, err = store.FindCommitByDocsTree(ctx, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	if err != nil {
		t.Fatalf("FindCommitByDocsTree for an absent tree: %v", err)
	}
	if ok {
		t.Error("FindCommitByDocsTree found a tree that is not in history")
	}

	_, ok, err = store.FindCommitByDocsTree(ctx, "")
	if err != nil {
		t.Fatalf("FindCommitByDocsTree(\"\"): %v", err)
	}
	if ok {
		t.Error("FindCommitByDocsTree(\"\") reported a match")
	}
}

func TestFetchFromAppMakesTipTreesAddressable(t *testing.T) {
	origin := newOrigin(t, "main", map[string]entry{"a.md": text("a\n")})
	store := ensureStore(t, origin)
	ctx := context.Background()

	app := newWorkRepo(t, "app")
	materialize(t, app, map[string]entry{"docs/a.md": text("app a\n"), "README.md": text("readme\n")})
	gitRun(t, app, "add", "-A", "--", ".")
	gitRun(t, app, "commit", "--quiet", "-m", "docs: add a")
	tip := gitLine(t, app, "rev-parse", "HEAD")
	docsTree := gitLine(t, app, "rev-parse", "HEAD:docs")
	appGitDir := filepath.Join(app, ".git")

	if err := store.FetchFromApp(ctx, appGitDir, tip); err != nil {
		t.Fatalf("FetchFromApp: %v", err)
	}
	if got := gitLine(t, store.Dir(), "cat-file", "-t", docsTree); got != "tree" {
		t.Fatalf("docs tree %s is not addressable in the clone: %q", docsTree, got)
	}

	// A commit that is not any branch's tip must import too: the local
	// transport lifts the unadvertised-object restriction.
	materialize(t, app, map[string]entry{"docs/a.md": text("app a2\n"), "README.md": text("readme\n")})
	gitRun(t, app, "add", "-A", "--", ".")
	gitRun(t, app, "commit", "--quiet", "-m", "docs: change a")
	buried := gitLine(t, app, "rev-parse", "HEAD")
	gitRun(t, app, "reset", "--quiet", "--hard", "HEAD~1")

	if err := store.FetchFromApp(ctx, appGitDir, buried); err != nil {
		t.Fatalf("FetchFromApp for an unadvertised commit: %v", err)
	}
	if got := gitLine(t, store.Dir(), "cat-file", "-t", buried); got != "commit" {
		t.Fatalf("unadvertised commit %s did not import: %q", buried, got)
	}
}

func TestGcAutoMaintainsThePrivateClone(t *testing.T) {
	origin := newOrigin(t, "main", map[string]entry{"a.md": text("a\n")})
	store := ensureStore(t, origin)
	// Force the auto threshold low and keep maintenance in the process so
	// this exercises the configured repository rather than merely
	// scheduling detached work.
	gitRun(t, store.Dir(), "config", "gc.auto", "1")
	gitRun(t, store.Dir(), "config", "gc.autoDetach", "false")
	gitRun(t, store.Dir(), "hash-object", "-w", "--stdin", "--path", "unreachable.txt")

	if err := store.GcAuto(context.Background()); err != nil {
		t.Fatalf("GcAuto: %v", err)
	}
	if got := gitLine(t, store.Dir(), "rev-parse", "--is-bare-repository"); got != "true" {
		t.Fatalf("private clone stopped being bare after maintenance: %q", got)
	}
}

func TestFetchIntoApp(t *testing.T) {
	origin := newOrigin(t, "main", map[string]entry{"a.md": text("canonical a\n")})
	store := ensureStore(t, origin)
	ctx := context.Background()

	app := newWorkRepo(t, "app")
	materialize(t, app, map[string]entry{"docs/a.md": text("app a\n")})
	gitRun(t, app, "add", "-A", "--", ".")
	gitRun(t, app, "commit", "--quiet", "-m", "docs: add a")

	head, _, err := store.Head(ctx)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}

	imported, err := store.FetchIntoApp(ctx, filepath.Join(app, ".git"))
	if err != nil {
		t.Fatalf("FetchIntoApp: %v", err)
	}
	if imported != head {
		t.Fatalf("FetchIntoApp returned %s, want the canonical head %s", imported, head)
	}
	if got := gitLine(t, app, "cat-file", "-t", head); got != "commit" {
		t.Fatalf("canonical head is not addressable app-side: %q", got)
	}
}

func TestCommitDocsTreeUsesActorIdentity(t *testing.T) {
	origin := newOrigin(t, "main", map[string]entry{"a.md": text("a\n")})
	store := ensureStore(t, origin)
	ctx := context.Background()

	head, tree, err := store.Head(ctx)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}

	const message = "[SANHO] Publish docs from app/main (1 app commits)\n\nsource: ws @ tip\n"
	newHead, err := store.CommitDocsTree(ctx, tree, head, "Publisher", "publisher@example.test", message)
	if err != nil {
		t.Fatalf("CommitDocsTree: %v", err)
	}

	fields := gitRun(t, store.Dir(), "log", "-1", "--format=%an%n%ae%n%cn%n%ce%n%P%n%T", newHead)
	lines := strings.Split(strings.TrimRight(fields, "\n"), "\n")
	want := []string{"Publisher", "publisher@example.test", "Publisher", "publisher@example.test", head, tree}
	if len(lines) != len(want) {
		t.Fatalf("log output = %q", fields)
	}
	for i, value := range want {
		if lines[i] != value {
			t.Errorf("log field %d = %q, want %q", i, lines[i], value)
		}
	}

	body := gitRun(t, store.Dir(), "log", "-1", "--format=%B", newHead)
	if !strings.HasPrefix(body, message) {
		t.Errorf("commit message = %q, want it to start with %q", body, message)
	}
}

func TestCommitDocsTreeFallsBackToAnIdentityName(t *testing.T) {
	origin := newOrigin(t, "main", map[string]entry{"a.md": text("a\n")})
	store := ensureStore(t, origin)
	ctx := context.Background()

	head, tree, err := store.Head(ctx)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	newHead, err := store.CommitDocsTree(ctx, tree, head, "", "someone@example.test", "docs: x\n")
	if err != nil {
		t.Fatalf("CommitDocsTree with no author name: %v", err)
	}
	if got := gitLine(t, store.Dir(), "log", "-1", "--format=%an", newHead); got != "someone" {
		t.Errorf("author name = %q, want the address local part", got)
	}
}

func TestIdentityName(t *testing.T) {
	tests := []struct {
		name, email, want string
	}{
		{"Ada", "ada@example.test", "Ada"},
		{"  ", "ada@example.test", "ada"},
		{"", "ada@example.test", "ada"},
		{"", "no-at-sign", "sanho"},
		{"", "", "sanho"},
		{"", "@example.test", "sanho"},
	}
	for _, test := range tests {
		if got := identityName(test.name, test.email); got != test.want {
			t.Errorf("identityName(%q, %q) = %q, want %q", test.name, test.email, got, test.want)
		}
	}
}

func TestPushHeadPublishesAndUpdatesTracking(t *testing.T) {
	origin := newOrigin(t, "main", map[string]entry{"a.md": text("a\n")})
	store := ensureStore(t, origin)
	ctx := context.Background()

	head, tree, err := store.Head(ctx)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	newHead, err := store.CommitDocsTree(ctx, tree, head, "P", "p@example.test", "docs: publish\n")
	if err != nil {
		t.Fatalf("CommitDocsTree: %v", err)
	}

	if err := store.PushHead(ctx, newHead, head); err != nil {
		t.Fatalf("PushHead: %v", err)
	}
	if got := gitLine(t, origin, "rev-parse", "refs/heads/main"); got != newHead {
		t.Fatalf("origin head = %s, want %s", got, newHead)
	}

	// A successful push refreshes the remote-tracking ref, so a
	// subsequent Head serves the state that was just published — the
	// multi-tip loop depends on this.
	after, _, err := store.Head(ctx)
	if err != nil {
		t.Fatalf("Head after push: %v", err)
	}
	if after != newHead {
		t.Fatalf("Head() = %s after push, want %s", after, newHead)
	}
}

// TestPushHeadLosesTheLease is the CAS contract at infra level: the
// lease is enforced against the *server*, so a stale local tracking ref
// cannot let a losing publisher through.
func TestPushHeadLosesTheLease(t *testing.T) {
	origin := newOrigin(t, "main", map[string]entry{"a.md": text("a\n")})
	store := ensureStore(t, origin)
	ctx := context.Background()

	head, tree, err := store.Head(ctx)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	mine, err := store.CommitDocsTree(ctx, tree, head, "P", "p@example.test", "docs: mine\n")
	if err != nil {
		t.Fatalf("CommitDocsTree: %v", err)
	}

	// Someone else publishes first.
	raced := commitToOrigin(t, origin, "main", map[string]entry{"a.md": text("racer\n")})
	if raced == head {
		t.Fatal("racer did not advance origin")
	}

	err = store.PushHead(ctx, mine, head)
	if err == nil {
		t.Fatal("PushHead succeeded against a moved origin")
	}
	if !errors.Is(err, ErrNonFastForward) {
		t.Fatalf("error = %v, want it to wrap ErrNonFastForward", err)
	}
	if got := gitLine(t, origin, "rev-parse", "refs/heads/main"); got != raced {
		t.Fatalf("origin head = %s, want the racer's %s (a lost race must change nothing)", got, raced)
	}
}

// TestPushHeadEnforcesTheLeaseAgainstAFreshTrackingRef covers the reason
// the expected value is passed explicitly: even when the local
// remote-tracking ref has already caught up with the winner, the losing
// publisher must still be rejected.
func TestPushHeadEnforcesTheLeaseAgainstAFreshTrackingRef(t *testing.T) {
	origin := newOrigin(t, "main", map[string]entry{"a.md": text("a\n")})
	store := ensureStore(t, origin)
	ctx := context.Background()

	head, tree, err := store.Head(ctx)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	mine, err := store.CommitDocsTree(ctx, tree, head, "P", "p@example.test", "docs: mine\n")
	if err != nil {
		t.Fatalf("CommitDocsTree: %v", err)
	}

	raced := commitToOrigin(t, origin, "main", map[string]entry{"a.md": text("racer\n")})
	if err := store.Fetch(ctx); err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	err = store.PushHead(ctx, mine, head)
	if !errors.Is(err, ErrNonFastForward) {
		t.Fatalf("error = %v, want it to wrap ErrNonFastForward", err)
	}
	if got := gitLine(t, origin, "rev-parse", "refs/heads/main"); got != raced {
		t.Fatalf("origin head = %s, want %s", got, raced)
	}
}

func TestPushHeadFailsClosedOnUnreachableOrigin(t *testing.T) {
	origin := newOrigin(t, "main", map[string]entry{"a.md": text("a\n")})
	store := ensureStore(t, origin)
	ctx := context.Background()

	head, tree, err := store.Head(ctx)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	newHead, err := store.CommitDocsTree(ctx, tree, head, "P", "p@example.test", "docs: publish\n")
	if err != nil {
		t.Fatalf("CommitDocsTree: %v", err)
	}
	if err := os.RemoveAll(origin); err != nil {
		t.Fatalf("remove origin: %v", err)
	}

	err = store.PushHead(ctx, newHead, head)
	if err == nil {
		t.Fatal("PushHead succeeded against a deleted origin")
	}
	if !errors.Is(err, ErrUnreachable) {
		t.Fatalf("error = %v, want it to wrap ErrUnreachable", err)
	}
	if errors.Is(err, ErrNonFastForward) {
		t.Fatalf("a transport failure was misreported as a lost race: %v", err)
	}
}

func TestIsRejectionClassifiesGitOutput(t *testing.T) {
	tests := []struct {
		name   string
		stderr string
		want   bool
	}{
		{"lease failure", " ! [rejected]        abc -> main (stale info)\n", true},
		{"plain non fast forward", " ! [rejected]        abc -> main (fetch first)\n", true},
		{"remote rejection", "remote: error\n ! [remote rejected] abc -> main (non-fast-forward)\n", true},
		{"ref lock contention", "error: cannot lock ref 'refs/heads/main'\n", true},
		{"transport failure", "fatal: Could not read from remote repository.\n", false},
		{"empty", "", false},
	}
	for _, test := range tests {
		if got := isRejection(test.stderr); got != test.want {
			t.Errorf("%s: isRejection() = %v, want %v", test.name, got, test.want)
		}
	}
}

func TestFirstMeaningfulLine(t *testing.T) {
	tests := []struct {
		name   string
		stderr string
		want   string
	}{
		{
			name:   "skips hints",
			stderr: "To /tmp/origin.git\n ! [rejected] abc -> main (stale info)\nhint: fetch first\n",
			want:   "! [rejected] abc -> main (stale info)",
		},
		{
			name:   "falls back to the transport line when nothing else exists",
			stderr: "To /tmp/origin.git\nhint: nothing\n",
			want:   "To /tmp/origin.git",
		},
		{
			name:   "reports the absence of diagnostics rather than an empty string",
			stderr: "\n\n",
			want:   "git reported no diagnostics",
		},
	}
	for _, test := range tests {
		if got := firstMeaningfulLine(test.stderr); got != test.want {
			t.Errorf("%s: firstMeaningfulLine() = %q, want %q", test.name, got, test.want)
		}
	}
}

func TestLogReadsCanonicalHistoryNewestFirst(t *testing.T) {
	origin := newOrigin(t, "main", map[string]entry{"a.md": text("a\n")})
	store := ensureStore(t, origin)
	ctx := context.Background()

	first, _, err := store.Head(ctx)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	second := commitToOrigin(t, origin, "main", map[string]entry{"b.md": text("b\n")})
	if err := store.Fetch(ctx); err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	entries, err := store.Log(ctx, 10, "")
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("Log returned %d entries, want 2", len(entries))
	}
	if entries[0].Commit != second || entries[1].Commit != first {
		t.Fatalf("Log order = [%s %s], want newest first [%s %s]",
			entries[0].Commit, entries[1].Commit, second, first)
	}
	if entries[0].Tree == "" || entries[0].Tree == entries[1].Tree {
		t.Errorf("Log did not report distinct trees: %q and %q", entries[0].Tree, entries[1].Tree)
	}
	if entries[0].CommittedAt.IsZero() {
		t.Error("Log reported a zero commit date")
	}
	if entries[0].CommittedAt.Before(entries[1].CommittedAt) {
		t.Errorf("Log dates run backwards: %s before %s", entries[0].CommittedAt, entries[1].CommittedAt)
	}
	if !strings.Contains(entries[0].Message, "canonical: racer") {
		t.Errorf("Log message = %q, want the commit subject", entries[0].Message)
	}
}

// TestLogPreservesWholeMessages is the property `sanho log` depends on:
// a publication body is multi-line, so nothing line-oriented can delimit
// one commit from the next.
func TestLogPreservesWholeMessages(t *testing.T) {
	origin := newOrigin(t, "main", map[string]entry{"a.md": text("a\n")})
	work := filepath.Join(t.TempDir(), "author")
	if err := os.MkdirAll(work, 0755); err != nil {
		t.Fatalf("create %s: %v", work, err)
	}
	gitRun(t, work, "clone", "--quiet", "--branch", "main", origin, ".")
	materialize(t, work, map[string]entry{"b.md": text("b\n")})
	gitRun(t, work, "add", "-A", "--", ".")
	body := "subject line\n\nsource: p:/w @ 1111111111111111111111111111111111111111\ncommits:\n  - docs: one\n  - docs: two"
	gitRun(t, work, "commit", "--quiet", "-m", body)
	gitRun(t, work, "push", "--quiet", "origin", "HEAD:main")

	store := ensureStore(t, origin)
	entries, err := store.Log(context.Background(), 1, "")
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("Log returned %d entries, want 1", len(entries))
	}
	if got := strings.TrimRight(entries[0].Message, "\n"); got != body {
		t.Fatalf("Log message =\n%q\nwant\n%q", got, body)
	}
}

func TestLogBoundsAndFiltersByPath(t *testing.T) {
	origin := newOrigin(t, "main", map[string]entry{"a.md": text("a\n")})
	store := ensureStore(t, origin)
	ctx := context.Background()

	first, _, err := store.Head(ctx)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	// Keep a.md so the second commit genuinely does not touch it —
	// materialize clears the worktree, and dropping it would make this a
	// deletion that `git log -- a.md` rightly reports.
	second := commitToOrigin(t, origin, "main", map[string]entry{"a.md": text("a\n"), "b.md": text("b\n")})
	if err := store.Fetch(ctx); err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	bounded, err := store.Log(ctx, 1, "")
	if err != nil {
		t.Fatalf("Log with a bound: %v", err)
	}
	if len(bounded) != 1 || bounded[0].Commit != second {
		t.Fatalf("Log(1) = %d entries, want just %s", len(bounded), second)
	}

	// Canonical is docs-only, so a canonical path is a docs-root-relative one.
	filtered, err := store.Log(ctx, 10, "a.md")
	if err != nil {
		t.Fatalf("Log with a path: %v", err)
	}
	if len(filtered) != 1 || filtered[0].Commit != first {
		t.Fatalf("Log(path a.md) = %d entries, want just %s", len(filtered), first)
	}

	absent, err := store.Log(ctx, 10, "nothing-here.md")
	if err != nil {
		t.Fatalf("Log for an unknown path: %v", err)
	}
	if len(absent) != 0 {
		t.Fatalf("Log for an unknown path returned %d entries, want none", len(absent))
	}
}

// TestLogReportsAnEmptyBranch keeps "nothing published yet" in the same
// vocabulary Head uses, rather than surfacing git's unknown-revision
// failure to callers that have a correct answer for the state.
func TestLogReportsAnEmptyBranch(t *testing.T) {
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	if err := os.MkdirAll(origin, 0755); err != nil {
		t.Fatalf("create %s: %v", origin, err)
	}
	gitRun(t, origin, "init", "--bare", "--quiet", "-b", "main")

	store := ensureStore(t, origin)
	if _, err := store.Log(context.Background(), 10, ""); !errors.Is(err, ErrEmptyBranch) {
		t.Fatalf("Log on an empty branch = %v, want ErrEmptyBranch", err)
	}
}

// TestLogSurvivesAMessageCarryingADelimiter is the framing regression.
//
// An earlier Log separated records with %x1e, and git stores that byte in
// a commit message verbatim — so one commit made directly in the
// canonical repository broke the entire listing. NUL is the only
// delimiter git guarantees cannot appear in a log message, which is why
// the format now uses it for records as well as fields. (A forged record
// is unreachable for the same reason: it would need NUL separators, and
// `git commit` refuses "a NUL byte in commit log message".)
func TestLogSurvivesAMessageCarryingADelimiter(t *testing.T) {
	origin := newOrigin(t, "main", map[string]entry{"a.md": text("a\n")})
	work := filepath.Join(t.TempDir(), "author")
	if err := os.MkdirAll(work, 0755); err != nil {
		t.Fatalf("create %s: %v", work, err)
	}
	gitRun(t, work, "clone", "--quiet", "--branch", "main", origin, ".")
	materialize(t, work, map[string]entry{"a.md": text("a\n"), "b.md": text("b\n")})
	gitRun(t, work, "add", "-A", "--", ".")

	// Record separators in the subject and in the body, plus a tail
	// shaped like the fields a record carries. -F because a separator
	// cannot be smuggled through argv on every platform.
	message := "real \x1esubject\n\nbody \x1e " + strings.Repeat("d", 40) +
		" \x1e 2000-01-01T00:00:00+00:00 \x1e forged entry\n"
	messageFile := filepath.Join(t.TempDir(), "message")
	if err := os.WriteFile(messageFile, []byte(message), 0600); err != nil {
		t.Fatalf("write commit message: %v", err)
	}
	gitRun(t, work, "commit", "--quiet", "-F", messageFile)
	gitRun(t, work, "push", "--quiet", "origin", "HEAD:main")

	store := ensureStore(t, origin)
	entries, err := store.Log(context.Background(), 10, "")
	if err != nil {
		t.Fatalf("Log over a message carrying a delimiter: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("Log returned %d entries, want the seed and the delimiter-carrying commit", len(entries))
	}
	for _, e := range entries {
		if len(e.Commit) != 40 || len(e.Tree) != 40 {
			t.Errorf("entry has misaligned fields: commit=%q tree=%q", e.Commit, e.Tree)
		}
		if e.Commit == strings.Repeat("d", 40) {
			t.Fatalf("Log reported a commit %s that git never listed", e.Commit)
		}
	}
	// The message survives byte for byte, separators included.
	if got := strings.TrimRight(entries[0].Message, "\n"); got != strings.TrimRight(message, "\n") {
		t.Fatalf("Log message =\n%q\nwant\n%q", got, message)
	}
}

// TestLogReadsACommitWithAnEmptyMessage pins the positional grouping: an
// empty field is data, so nothing may skip it and shift every later
// commit's fields by one.
func TestLogReadsACommitWithAnEmptyMessage(t *testing.T) {
	origin := newOrigin(t, "main", map[string]entry{"a.md": text("a\n")})
	work := filepath.Join(t.TempDir(), "author")
	if err := os.MkdirAll(work, 0755); err != nil {
		t.Fatalf("create %s: %v", work, err)
	}
	gitRun(t, work, "clone", "--quiet", "--branch", "main", origin, ".")
	materialize(t, work, map[string]entry{"a.md": text("a\n"), "b.md": text("b\n")})
	gitRun(t, work, "add", "-A", "--", ".")
	gitRun(t, work, "commit", "--quiet", "--allow-empty-message", "-m", "")
	gitRun(t, work, "push", "--quiet", "origin", "HEAD:main")

	store := ensureStore(t, origin)
	entries, err := store.Log(context.Background(), 10, "")
	if err != nil {
		t.Fatalf("Log over an empty message: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("Log returned %d entries, want 2", len(entries))
	}
	if strings.TrimSpace(entries[0].Message) != "" {
		t.Errorf("newest message = %q, want empty", entries[0].Message)
	}
	// The seed's fields must not have shifted.
	if !strings.Contains(entries[1].Message, "canonical: seed") {
		t.Errorf("older message = %q, want the seed subject", entries[1].Message)
	}
	for _, e := range entries {
		if len(e.Commit) != 40 || len(e.Tree) != 40 {
			t.Errorf("entry has misaligned fields: commit=%q tree=%q", e.Commit, e.Tree)
		}
	}
}
