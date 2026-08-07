// Package canonical manages the workspace-private clone of the
// canonical docs repository and all git operations against it
// (sanho-v0.2.md §5.2–§5.4): fetch policy and data-age, object exchange
// with the app repository, tree-level 3-way merges via
// `git merge-tree --write-tree`, and low-level publication (commit-tree
// + compare-and-swap push).
//
// Case decisions live in domain/publish; flow orchestration in
// usecase/publish and usecase/docsync. This package owns mechanics.
package canonical

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	pubdom "github.com/irootkernel/sanho/internal/domain/publish"
	"github.com/irootkernel/sanho/internal/infra/fsx"
	"github.com/irootkernel/sanho/internal/infra/gitx"
)

// ErrNonFastForward is the CAS-loss sentinel: origin rejected the push
// because its head moved. Callers refetch and retry (§5.3, ≤3 attempts).
//
// It is the domain sentinel re-exported here: usecase/publish must be
// able to recognize it without importing infra (architecture layering),
// so the value itself is defined in domain/publish.
var ErrNonFastForward = pubdom.ErrNonFastForward

// ErrUnreachable wraps network failures reaching origin; write paths
// fail closed on it with the §5.9 message. Re-exported from
// domain/publish for the same reason as ErrNonFastForward.
var ErrUnreachable = pubdom.ErrUnreachable

// ErrEmptyBranch is Head's answer for a canonical repository into which
// nothing has ever been published. Re-exported from domain/publish for
// the same reason as the two sentinels above.
var ErrEmptyBranch = pubdom.ErrEmptyBranch

const (
	// CloneRelPath is the clone's location under the app repository's
	// git common dir (§5.2). Linked worktrees share one clone because
	// they share the common dir.
	CloneRelPath = "sanho/canonical"

	// fetchMarkerName is the timestamp file recording the last
	// successful fetch (§5.2 "fetch policy"). It lives inside the clone
	// directory and holds one RFC3339Nano line.
	fetchMarkerName = "sanho-last-fetch"

	// branchConfigKey persists the resolved publication branch inside
	// the clone's own git config, so Open can read it back without a
	// network round-trip.
	branchConfigKey = "sanho.branch"

	// defaultBranch / fallbackBranch implement §5.2's "main, falling
	// back to master" publication-branch rule.
	defaultBranch  = "main"
	fallbackBranch = "master"

	// remoteName is the only remote the clone ever has.
	remoteName = "origin"

	// allHeadsRefspec keeps every canonical branch mirrored under
	// refs/remotes/origin/*. Fetching the whole namespace (rather than a
	// single branch) keeps the main→master fallback decidable offline
	// and succeeds against a canonical repository that has no branches
	// yet.
	allHeadsRefspec = "+refs/heads/*:refs/remotes/origin/*"

	// networkTimeout bounds a single fetch/push (§5.2 "bounded
	// timeout"); local object exchange gets the gitx default.
	networkTimeout = 120 * time.Second
)

// DefaultBranch is the publication branch §5.2 assumes before a clone
// has been consulted. User-facing guidance that must name a ref while
// the clone is unavailable uses it rather than inventing a name.
const DefaultBranch = defaultBranch

// Store is a handle on the private bare clone at
// <git-common-dir>/sanho/canonical.
type Store struct {
	dir    string // clone directory
	url    string // origin URL (from workspace config)
	branch string // resolved publication branch (main|master)
}

// CloneDir returns where the private clone lives for an application
// repository whose `git rev-parse --git-common-dir` is appCommonDir.
func CloneDir(appCommonDir string) string {
	return filepath.Join(appCommonDir, filepath.FromSlash(CloneRelPath))
}

// Open returns a Store for an existing clone; Ensure creates the clone
// (and resolves the publication branch) when absent. appCommonDir is
// the app repo's `git rev-parse --git-common-dir`.
//
// Open performs one local git call (to read back the persisted
// publication branch) and therefore uses its own background context: it
// never touches the network, so there is nothing for a caller-supplied
// context to cancel meaningfully. A clone whose config carries no
// sanho.branch — i.e. one not created by Ensure — is opened on the
// default branch.
func Open(appCommonDir, url string) (*Store, error) {
	dir := CloneDir(appCommonDir)
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("canonical: open clone %s: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("canonical: clone path %s is not a directory", dir)
	}

	ctx := context.Background()
	run := gitx.New(dir)
	if _, err := run.Run(ctx, "rev-parse", "--git-dir"); err != nil {
		return nil, fmt.Errorf("canonical: %s is not a git repository: %w", dir, err)
	}

	s := &Store{dir: dir, url: url, branch: defaultBranch}
	branch, err := s.configuredBranch(ctx)
	if err != nil {
		return nil, err
	}
	if branch != "" {
		s.branch = branch
	}
	return s, nil
}

// Ensure opens the clone, creating it when absent: a bare repository
// with `origin` pointing at url, a first fetch of every canonical
// branch, and the publication branch resolved (main, else master) and
// persisted in the clone config.
//
// The clone is built with `git init --bare` + `git remote add` rather
// than `git clone --bare`, because that is what gives the clone a normal
// `+refs/heads/*:refs/remotes/origin/*` fetch refspec: canonical state
// then lives under refs/remotes/origin/*, leaving refs/heads/* and the
// merge temp refs (§5.4) free of remote content.
func Ensure(ctx context.Context, appCommonDir, url string) (*Store, error) {
	dir := CloneDir(appCommonDir)
	switch _, err := os.Stat(dir); {
	case err == nil:
		s, openErr := Open(appCommonDir, url)
		if openErr != nil {
			return nil, openErr
		}
		if err := s.reconcileExisting(ctx); err != nil {
			return nil, err
		}
		return s, nil
	case !os.IsNotExist(err):
		return nil, fmt.Errorf("canonical: inspect clone %s: %w", dir, err)
	}

	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("canonical: create clone directory %s: %w", dir, err)
	}

	s, err := initClone(ctx, dir, url)
	if err != nil {
		// A half-built clone is worse than none: the next Ensure would
		// take the "already exists" path and inherit the damage.
		_ = os.RemoveAll(dir)
		return nil, err
	}
	return s, nil
}

func initClone(ctx context.Context, dir, url string) (*Store, error) {
	run := gitx.New(dir)
	if _, err := run.Run(ctx, "init", "--bare", "--quiet"); err != nil {
		return nil, fmt.Errorf("canonical: init clone %s: %w", dir, err)
	}
	if _, err := run.Run(ctx, "remote", "add", remoteName, url); err != nil {
		return nil, fmt.Errorf("canonical: set origin %s: %w", url, err)
	}

	s := &Store{dir: dir, url: url, branch: defaultBranch}
	if err := s.Fetch(ctx); err != nil {
		return nil, err
	}

	branch, err := s.detectBranch(ctx)
	if err != nil {
		return nil, err
	}
	s.branch = branch
	if err := s.persistBranch(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

// reconcileExisting brings an already-present clone in line with the
// workspace config: origin URL and a persisted publication branch. It
// never fetches, so it stays usable offline.
func (s *Store) reconcileExisting(ctx context.Context) error {
	run := gitx.New(s.dir)
	current, err := s.configValue(ctx, "remote."+remoteName+".url")
	if err != nil {
		return err
	}
	switch {
	case current == "":
		if _, err := run.Run(ctx, "remote", "add", remoteName, s.url); err != nil {
			return fmt.Errorf("canonical: set origin %s: %w", s.url, err)
		}
	case current != s.url:
		if _, err := run.Run(ctx, "remote", "set-url", remoteName, s.url); err != nil {
			return fmt.Errorf("canonical: update origin to %s: %w", s.url, err)
		}
	}

	configured, err := s.configuredBranch(ctx)
	if err != nil {
		return err
	}
	if configured != "" {
		s.branch = configured
		return nil
	}
	branch, err := s.detectBranch(ctx)
	if err != nil {
		return err
	}
	s.branch = branch
	return s.persistBranch(ctx)
}

// detectBranch implements §5.2's publication-branch rule against the
// already-fetched remote-tracking refs: main when it exists, else
// master, else main (the name a first publication will create).
func (s *Store) detectBranch(ctx context.Context) (string, error) {
	for _, candidate := range []string{defaultBranch, fallbackBranch} {
		exists, err := s.refExists(ctx, remoteRefFor(candidate))
		if err != nil {
			return "", err
		}
		if exists {
			return candidate, nil
		}
	}
	return defaultBranch, nil
}

// refExists reports whether ref resolves in the clone.
func (s *Store) refExists(ctx context.Context, ref string) (bool, error) {
	res, err := gitx.New(s.dir).RunExit(ctx, "rev-parse", "--verify", "--quiet", ref)
	if err != nil {
		return false, fmt.Errorf("canonical: resolve %s in %s: %w", ref, s.dir, err)
	}
	return res.ExitCode == 0, nil
}

func (s *Store) persistBranch(ctx context.Context) error {
	if _, err := gitx.New(s.dir).Run(ctx, "config", branchConfigKey, s.branch); err != nil {
		return fmt.Errorf("canonical: record publication branch in %s: %w", s.dir, err)
	}
	return nil
}

func (s *Store) configuredBranch(ctx context.Context) (string, error) {
	return s.configValue(ctx, branchConfigKey)
}

// configValue reads one local config key; an unset key is "" (git exits
// 1), which is not an error here.
func (s *Store) configValue(ctx context.Context, key string) (string, error) {
	res, err := gitx.New(s.dir).RunExit(ctx, "config", "--local", "--get", key)
	if err != nil {
		return "", fmt.Errorf("canonical: read config %s in %s: %w", key, s.dir, err)
	}
	if res.ExitCode != 0 {
		return "", nil
	}
	return firstLine(res.Stdout), nil
}

// Branch returns the resolved publication branch name; Dir the clone
// directory; URL the origin URL (all used by doctor/status rendering).
func (s *Store) Branch() string { return s.branch }
func (s *Store) Dir() string    { return s.dir }
func (s *Store) URL() string    { return s.url }

// remoteRef is the ref Head and friends read: the remote-tracking ref of
// the publication branch. Head deliberately does not use FETCH_HEAD —
// FETCH_HEAD is clobbered by every unrelated fetch (including the
// app-repo object imports this package performs), whereas the tracking
// ref is written only by Fetch's explicit refspec and by a successful
// PushHead, which is exactly the "last known canonical head" semantics
// the read paths need.
func (s *Store) remoteRef() string { return remoteRefFor(s.branch) }

func remoteRefFor(branch string) string { return "refs/remotes/" + remoteName + "/" + branch }

// Fetch updates the clone from origin (network runner). Records the
// fetch time for Age.
func (s *Store) Fetch(ctx context.Context) error {
	run := gitx.New(s.dir, gitx.WithNetwork(), gitx.WithTimeout(networkTimeout))
	if _, err := run.Run(ctx, "fetch", "--no-tags", "--prune", "--quiet", remoteName, allHeadsRefspec); err != nil {
		return fmt.Errorf("%w: fetch %s: %s", ErrUnreachable, s.url, gitDetail(err))
	}
	return s.recordFetch()
}

func (s *Store) recordFetch() error {
	stamp := []byte(time.Now().UTC().Format(time.RFC3339Nano))
	path := filepath.Join(s.dir, fetchMarkerName)
	if err := fsx.WriteFileAtomic(path, stamp, 0644); err != nil {
		return fmt.Errorf("canonical: record fetch time %s: %w", path, err)
	}
	return nil
}

// Age returns how old the last successful fetch is; ok=false when the
// clone has never fetched (or the marker is unreadable — callers render
// "never fetched", never a guessed age).
func (s *Store) Age() (age time.Duration, ok bool) {
	data, err := os.ReadFile(filepath.Join(s.dir, fetchMarkerName))
	if err != nil {
		return 0, false
	}
	at, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(string(data)))
	if err != nil {
		return 0, false
	}
	if age = time.Since(at); age < 0 {
		age = 0
	}
	return age, true
}

// Head returns the last-fetched canonical head commit and its docs
// tree OID (for a docs-only canonical repo, the root tree).
//
// A publication branch with no commits is reported as ErrEmptyBranch
// rather than as an opaque failure: an empty canonical repository is the
// ordinary starting state of a new project, and the flow layer has a
// correct answer for it (bootstrap publication; nothing to consume on
// sync/pull). Callers that cannot distinguish it would otherwise have to
// read the message text to tell "nothing published yet" from "the clone
// is broken".
func (s *Store) Head(ctx context.Context) (commit, tree string, err error) {
	run := gitx.New(s.dir)
	res, err := run.RunExit(ctx, "rev-parse", "--verify", "--quiet", s.remoteRef()+"^{commit}")
	if err != nil {
		return "", "", fmt.Errorf("canonical: resolve head of %s: %w", s.branch, err)
	}
	if res.ExitCode != 0 {
		return "", "", fmt.Errorf("%w: %s in %s", ErrEmptyBranch, s.branch, s.dir)
	}
	commit = firstLine(res.Stdout)

	tree, err = run.Line(ctx, "rev-parse", "--verify", commit+"^{tree}")
	if err != nil {
		return "", "", fmt.Errorf("canonical: resolve docs tree of %s: %w", commit, err)
	}
	return commit, tree, nil
}

// EmptyTree returns the clone's empty-tree OID, derived from git rather
// than hardcoded so it stays correct under SHA-256 repositories. It is
// the merge base for "these two sides share no history".
func (s *Store) EmptyTree(ctx context.Context) (string, error) {
	oid, err := gitx.New(s.dir).Line(ctx, "hash-object", "-t", "tree", os.DevNull)
	if err != nil {
		return "", fmt.Errorf("canonical: compute empty tree OID in %s: %w", s.dir, err)
	}
	return oid, nil
}

// DocsFileCount counts the files a canonical commit publishes. Canonical
// is docs-only, so every blob in the tree is a document.
func (s *Store) DocsFileCount(ctx context.Context, commit string) (int, error) {
	if commit == "" {
		return 0, nil
	}
	res, err := gitx.New(s.dir).Run(ctx, "ls-tree", "-r", "-z", "--name-only", commit)
	if err != nil {
		return 0, fmt.Errorf("canonical: count docs of %s in %s: %w", commit, s.dir, err)
	}
	count := 0
	for _, name := range strings.Split(string(res.Stdout), "\x00") {
		if strings.TrimSpace(name) != "" {
			count++
		}
	}
	return count, nil
}

// ResolveCommit reports whether oid exists in the clone, and
// IsAncestor whether a is an ancestor of (or equal to) b.
//
// A malformed or absent OID is reported as "not present" rather than as
// an error: the caller's next move (re-anchoring by docs-base-tree,
// §5.3 case ④) is the same either way.
func (s *Store) ResolveCommit(ctx context.Context, oid string) (bool, error) {
	if oid == "" {
		return false, nil
	}
	res, err := gitx.New(s.dir).RunExit(ctx, "cat-file", "-e", oid+"^{commit}")
	if err != nil {
		return false, fmt.Errorf("canonical: resolve commit %s in %s: %w", oid, s.dir, err)
	}
	return res.ExitCode == 0, nil
}

func (s *Store) IsAncestor(ctx context.Context, a, b string) (bool, error) {
	res, err := gitx.New(s.dir).RunExit(ctx, "merge-base", "--is-ancestor", a, b)
	if err != nil {
		return false, fmt.Errorf("canonical: ancestry check %s..%s: %w", a, b, err)
	}
	switch res.ExitCode {
	case 0:
		return true, nil
	case 1:
		return false, nil
	default:
		return false, fmt.Errorf("canonical: ancestry check %s..%s: exit %d: %s",
			a, b, res.ExitCode, strings.TrimSpace(string(res.Stderr)))
	}
}

// Distance returns (behind, ahead) counts between a and b via
// rev-list --left-right --count, for status rendering: behind is how
// many commits b has that a lacks, ahead how many a has that b lacks.
// Callers pass (local, canonical-head) to read "N behind, M ahead".
func (s *Store) Distance(ctx context.Context, a, b string) (behind, ahead int, err error) {
	line, err := gitx.New(s.dir).Line(ctx, "rev-list", "--left-right", "--count", a+"..."+b)
	if err != nil {
		return 0, 0, fmt.Errorf("canonical: distance %s...%s: %w", a, b, err)
	}
	fields := strings.Fields(line)
	if len(fields) != 2 {
		return 0, 0, fmt.Errorf("canonical: distance %s...%s: unexpected rev-list output %q", a, b, line)
	}
	ahead, err = strconv.Atoi(fields[0])
	if err != nil {
		return 0, 0, fmt.Errorf("canonical: distance %s...%s: parse ahead count: %w", a, b, err)
	}
	behind, err = strconv.Atoi(fields[1])
	if err != nil {
		return 0, 0, fmt.Errorf("canonical: distance %s...%s: parse behind count: %w", a, b, err)
	}
	return behind, ahead, nil
}

// FindCommitByDocsTree searches canonical history (newest first) for a
// commit whose docs tree equals tree — the docs-base-tree re-anchoring
// used for rewrite recovery (§5.3 case ④, D2).
//
// For a docs-only canonical repository the docs tree is the commit's
// root tree, so the whole scan is one `git log --format=%H %T`.
func (s *Store) FindCommitByDocsTree(ctx context.Context, tree string) (commit string, found bool, err error) {
	if tree == "" {
		return "", false, nil
	}
	res, err := gitx.New(s.dir).Run(ctx, "log", "--format=%H %T", s.remoteRef())
	if err != nil {
		return "", false, fmt.Errorf("canonical: scan history of %s for docs tree %s: %w", s.branch, tree, err)
	}
	for _, line := range strings.Split(string(res.Stdout), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		if fields[1] == tree {
			return fields[0], true, nil
		}
	}
	return "", false, nil
}

// FetchFromApp imports ref (an OID or ref name) from the app repository
// into the clone's object database via local transport, making the app
// tip's docs trees addressable here (§5.2 object exchange).
//
// Fetching a bare OID works because the local transport lifts the
// unadvertised-object restriction that applies to networked remotes;
// pushed tips therefore need no ref of their own on the clone side.
func (s *Store) FetchFromApp(ctx context.Context, appGitDir, ref string) error {
	if _, err := gitx.New(s.dir).Run(ctx, "fetch", "--no-tags", "--quiet", appGitDir, ref); err != nil {
		return fmt.Errorf("canonical: import %s from app repository %s: %w", ref, appGitDir, err)
	}
	return nil
}

// FetchIntoApp imports the last-fetched canonical head into the app
// repository's object database and returns its OID, so merges and
// checkouts can run app-side (used by sync/pull).
func (s *Store) FetchIntoApp(ctx context.Context, appGitDir string) (headCommit string, err error) {
	run := gitx.New(appGitDir)
	if _, err := run.Run(ctx, "fetch", "--no-tags", "--quiet", s.dir, s.remoteRef()); err != nil {
		return "", fmt.Errorf("canonical: import %s into app repository %s: %w", s.branch, appGitDir, err)
	}
	oid, err := run.Line(ctx, "rev-parse", "--verify", "FETCH_HEAD^{commit}")
	if err != nil {
		return "", fmt.Errorf("canonical: resolve imported head in %s: %w", appGitDir, err)
	}
	return oid, nil
}

// CommitDocsTree creates a canonical commit with the given docs tree,
// parent, author identity, and message; returns its OID. No push.
//
// An empty parent creates a root commit (first publication into an empty
// canonical repository). Author and committer are both the actor, so
// canonical `git log` attributes the publication to the person who
// pushed.
func (s *Store) CommitDocsTree(ctx context.Context, tree, parent, authorName, authorEmail, message string) (string, error) {
	name := identityName(authorName, authorEmail)
	run := gitx.New(s.dir, gitx.WithEnv(
		"GIT_AUTHOR_NAME="+name,
		"GIT_AUTHOR_EMAIL="+authorEmail,
		"GIT_COMMITTER_NAME="+name,
		"GIT_COMMITTER_EMAIL="+authorEmail,
	))

	args := []string{"commit-tree", tree}
	if parent != "" {
		args = append(args, "-p", parent)
	}
	args = append(args, "-m", message)

	oid, err := run.Line(ctx, args...)
	if err != nil {
		return "", fmt.Errorf("canonical: create commit for tree %s: %w", tree, err)
	}
	return oid, nil
}

// identityName keeps `git commit-tree` from failing on an empty ident:
// the workspace config carries an actor email but not necessarily a
// name, so fall back to the address' local part and finally to the tool
// name.
func identityName(name, email string) string {
	if strings.TrimSpace(name) != "" {
		return name
	}
	if local, _, ok := strings.Cut(email, "@"); ok && strings.TrimSpace(local) != "" {
		return local
	}
	return "sanho"
}

// PushHead compare-and-swap publishes newHead to origin's publication
// branch, expecting the remote to still be at expectedOld. A lost race
// returns ErrNonFastForward; network failure wraps ErrUnreachable.
// Never force-pushes.
//
// The CAS is `--force-with-lease=refs/heads/<branch>:<expectedOld>`,
// which sends expectedOld to the remote so the *server* enforces it.
// The weaker plain `--force-with-lease` would compare against the local
// remote-tracking ref, which a concurrent publisher's push can have
// already refreshed. Nothing here passes --force: every published commit
// has expectedOld as its parent, so a lease that holds is also a
// fast-forward.
//
// An empty expectedOld means "create the branch" and pushes without a
// lease; git itself then rejects the push if the ref already exists.
func (s *Store) PushHead(ctx context.Context, newHead, expectedOld string) error {
	args := []string{"push", "--quiet"}
	if expectedOld != "" {
		args = append(args, "--force-with-lease=refs/heads/"+s.branch+":"+expectedOld)
	}
	args = append(args, remoteName, newHead+":refs/heads/"+s.branch)

	run := gitx.New(s.dir, gitx.WithNetwork(), gitx.WithTimeout(networkTimeout))
	res, err := run.RunExit(ctx, args...)
	if err != nil {
		return fmt.Errorf("%w: push to %s: %s", ErrUnreachable, s.url, err)
	}
	if res.ExitCode == 0 {
		return nil
	}

	stderr := string(res.Stderr)
	if isRejection(stderr) {
		return fmt.Errorf("%w: %s expected %s: %s", ErrNonFastForward, s.branch, expectedOld, firstMeaningfulLine(stderr))
	}
	return fmt.Errorf("%w: push to %s: %s", ErrUnreachable, s.url, firstMeaningfulLine(stderr))
}

// rejectionSignatures are the stderr fragments git emits when the remote
// refuses an update because its ref moved: "stale info" is the
// --force-with-lease verdict, the others cover a plain non-fast-forward
// refusal. All of them mean the same thing here — refetch and re-decide.
var rejectionSignatures = []string{
	"stale info",
	"[rejected]",
	"non-fast-forward",
	"fetch first",
	"cannot lock ref",
}

func isRejection(stderr string) bool {
	lower := strings.ToLower(stderr)
	for _, signature := range rejectionSignatures {
		if strings.Contains(lower, signature) {
			return true
		}
	}
	return false
}

// firstMeaningfulLine picks the most informative single line out of git
// stderr, skipping progress noise and the "hint:" block, so §5.9's "never
// print raw Go error chains" rule has a usable one-liner to wrap.
func firstMeaningfulLine(stderr string) string {
	var fallback string
	for _, line := range strings.Split(stderr, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "", strings.HasPrefix(line, "hint:"):
			continue
		case strings.HasPrefix(line, "To "), strings.HasPrefix(line, "remote:"):
			if fallback == "" {
				fallback = line
			}
			continue
		default:
			return line
		}
	}
	if fallback != "" {
		return fallback
	}
	return "git reported no diagnostics"
}

// gitDetail renders a git failure as one readable line: the command's
// stderr when it exited non-zero, otherwise the transport-level error.
func gitDetail(err error) string {
	var exitErr *gitx.ExitError
	if errors.As(err, &exitErr) {
		return firstMeaningfulLine(string(exitErr.Result.Stderr))
	}
	return err.Error()
}

func firstLine(out []byte) string {
	text := string(out)
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		text = text[:i]
	}
	return strings.TrimSpace(text)
}
