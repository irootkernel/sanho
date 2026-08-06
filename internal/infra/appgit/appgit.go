// Package appgit is the application-repository adapter of sanho v0.2:
// the read side of publication (sanho-v0.2.md §5.3) and, later, of sync
// (§5.5). It answers the questions the flow layer asks about the repo it
// is installed in — the docs tree of a commit, whether a pushed tip
// carries unresolved conflict markers, which app commits a publication
// summarizes, how the repository names itself, and what the docs
// worktree currently hashes to.
//
// Two properties are load-bearing and are enforced by construction here:
//
//   - Every git invocation goes through infra/gitx (audit L7: argv-only,
//     no shell, non-interactive env, bounded timeouts).
//   - Nothing writes to the app repository's refs, index, or worktree.
//     "Worktree inviolability" (§5.3) is what lets publication run inside
//     `pre-push` without behaving differently from git's own push.
package appgit

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/irootkernel/sanho/internal/domain/markers"
	"github.com/irootkernel/sanho/internal/infra/gitx"
)

// DefaultDocsDir is the docs directory New falls back to, matching the
// workspace config default.
const DefaultDocsDir = "docs"

// MaxSubjects caps how many app-commit subjects DocsCommitSubjects
// returns. The canonical commit body embeds them (§5.3), and a first
// push of a long-lived branch can otherwise summarize an entire docs
// history into one message; the cap keeps the newest MaxSubjects, which
// is the part a reader of canonical `git log` is looking for.
const MaxSubjects = 100

// Repo is a handle on one application repository's docs directory.
type Repo struct {
	workDir string
	docsDir string
	git     *gitx.Runner

	emptyTreeOnce sync.Once
	emptyTree     string
	emptyTreeErr  error
}

// New returns a Repo rooted at workDir (the worktree root) for docsDir
// (a slash-separated path relative to it, defaulting to DefaultDocsDir).
//
// runner supplies the git policy for plain read commands; nil means the
// gitx default rooted at workDir. Calls that need extra environment —
// only WorktreeDocsTree, which redirects GIT_INDEX_FILE — build their own
// runner with default policy, because gitx.Runner is immutable and
// carries no accessor for its options.
func New(workDir, docsDir string, runner *gitx.Runner) *Repo {
	if docsDir == "" {
		docsDir = DefaultDocsDir
	}
	if runner == nil {
		runner = gitx.New(workDir)
	}
	return &Repo{workDir: workDir, docsDir: docsDir, git: runner}
}

// WorkDir and DocsDir report the configuration this Repo was built with.
func (r *Repo) WorkDir() string { return r.workDir }
func (r *Repo) DocsDir() string { return r.docsDir }

// EmptyTree returns the repository's empty-tree OID, the stand-in for
// "this commit has no docs directory". It is derived from git rather
// than hardcoded so the value stays correct under SHA-256 repositories,
// and it is computed at most once per Repo.
//
// `hash-object` without -w only computes: git resolves the empty tree
// natively in every repository, so nothing needs to be written to the
// app repository's object database to make the OID usable.
func (r *Repo) EmptyTree(ctx context.Context) (string, error) {
	r.emptyTreeOnce.Do(func() {
		oid, err := r.git.Line(ctx, "hash-object", "-t", "tree", os.DevNull)
		if err != nil {
			r.emptyTreeErr = fmt.Errorf("appgit: compute empty tree OID in %s: %w", r.workDir, err)
			return
		}
		r.emptyTree = oid
	})
	return r.emptyTree, r.emptyTreeErr
}

// DocsTreeOf returns the docs tree OID of a commit (empty-tree OID
// when the docs dir is absent).
func (r *Repo) DocsTreeOf(ctx context.Context, commit string) (string, error) {
	res, err := r.git.RunExit(ctx, "rev-parse", "--verify", "--quiet", commit+":"+r.docsDir)
	if err != nil {
		return "", fmt.Errorf("appgit: resolve docs tree of %s: %w", commit, err)
	}
	if res.ExitCode == 0 {
		return firstLine(res.Stdout), nil
	}

	// The lookup can fail for two very different reasons: the commit
	// does not exist (a caller bug or a corrupt hook input, which must
	// surface), or the commit simply has no docs directory (ordinary,
	// and means the empty tree).
	present, err := r.commitExists(ctx, commit)
	if err != nil {
		return "", err
	}
	if !present {
		return "", fmt.Errorf("appgit: commit %s does not exist in %s", commit, r.workDir)
	}
	return r.EmptyTree(ctx)
}

func (r *Repo) commitExists(ctx context.Context, commit string) (bool, error) {
	res, err := r.git.RunExit(ctx, "cat-file", "-e", commit+"^{commit}")
	if err != nil {
		return false, fmt.Errorf("appgit: resolve commit %s in %s: %w", commit, r.workDir, err)
	}
	return res.ExitCode == 0, nil
}

// ScanDocsBlobsForMarkers scans a commit's docs blobs (§5.4 detector);
// returns conflicted paths. Unreadable blobs error.
//
// The gate is fail-closed in both directions the v0.1 detector got wrong
// (audit H2): a blob larger than markers.MaxScanSize is an error naming
// the file rather than a silent pass, and any read failure propagates
// instead of being swallowed. Symlink and gitlink entries are skipped —
// their blob content is a path, not text that can carry markers.
func (r *Repo) ScanDocsBlobsForMarkers(ctx context.Context, commit string) ([]string, error) {
	res, err := r.git.Run(ctx, "ls-tree", "-r", "-z", "--long", commit, "--", r.docsDir)
	if err != nil {
		return nil, fmt.Errorf("appgit: list docs blobs of %s: %w", commit, err)
	}

	var conflicted []string
	for _, entry := range strings.Split(string(res.Stdout), "\x00") {
		if strings.TrimSpace(entry) == "" {
			continue
		}
		blob, err := parseLsTreeEntry(entry)
		if err != nil {
			return nil, fmt.Errorf("appgit: read docs listing of %s: %w", commit, err)
		}
		if !blob.scannable() {
			continue
		}
		if blob.size > markers.MaxScanSize {
			return nil, fmt.Errorf("appgit: %s in %s is %d bytes: %w",
				blob.path, commit, blob.size, markers.ErrTooLarge)
		}

		content, err := r.git.Run(ctx, "cat-file", "blob", blob.object)
		if err != nil {
			return nil, fmt.Errorf("appgit: read %s at %s: %w", blob.path, commit, err)
		}
		if markers.Scan(content.Stdout).HasMarkers {
			conflicted = append(conflicted, blob.path)
		}
	}
	return conflicted, nil
}

// lsTreeEntry is one `git ls-tree --long -z` record:
// "<mode> SP <type> SP <object> SP <size>HT<path>", where size is
// space-padded for regular objects and "-" for trees.
type lsTreeEntry struct {
	mode   string
	kind   string
	object string
	size   int64
	path   string
}

const (
	modeSymlink = "120000"
	modeGitlink = "160000"
)

func (e lsTreeEntry) scannable() bool {
	return e.kind == "blob" && e.mode != modeSymlink && e.mode != modeGitlink
}

func parseLsTreeEntry(entry string) (lsTreeEntry, error) {
	head, path, ok := strings.Cut(entry, "\t")
	if !ok {
		return lsTreeEntry{}, fmt.Errorf("malformed ls-tree entry %q", entry)
	}
	fields := strings.Fields(head)
	if len(fields) != 4 {
		return lsTreeEntry{}, fmt.Errorf("malformed ls-tree entry %q", entry)
	}

	parsed := lsTreeEntry{mode: fields[0], kind: fields[1], object: fields[2], path: path}
	if parsed.kind != "blob" {
		return parsed, nil
	}
	size, err := strconv.ParseInt(fields[3], 10, 64)
	if err != nil {
		return lsTreeEntry{}, fmt.Errorf("malformed size in ls-tree entry %q", entry)
	}
	parsed.size = size
	return parsed, nil
}

// DocsCommitSubjects lists subjects of commits since base that touched
// docs, oldest first (canonical commit body).
//
// base is advisory: publication passes the remote branch's previous OID,
// which is the empty string for a brand-new branch and can name a commit
// this repository no longer has after a rewrite. Both cases degrade to
// "the newest MaxSubjects docs commits reachable from tip" rather than
// failing — the subjects are commit-message prose, never a gate input.
func (r *Repo) DocsCommitSubjects(ctx context.Context, base, tip string) ([]string, error) {
	rangeSpec := tip
	if base != "" {
		usable, err := r.commitExists(ctx, base)
		if err != nil {
			return nil, err
		}
		if usable {
			rangeSpec = base + ".." + tip
		}
	}

	res, err := r.git.Run(ctx, "log", "--format=%s", "--reverse",
		"--max-count="+strconv.Itoa(MaxSubjects), rangeSpec, "--", r.docsDir)
	if err != nil {
		return nil, fmt.Errorf("appgit: list docs commit subjects for %s: %w", rangeSpec, err)
	}

	var subjects []string
	for _, line := range strings.Split(string(res.Stdout), "\n") {
		if line = strings.TrimRight(line, "\r"); line != "" {
			subjects = append(subjects, line)
		}
	}
	return subjects, nil
}

// RepoIdentity returns the app repo short name and current branch for
// the canonical commit subject: origin's URL basename without a .git
// suffix, falling back to the worktree directory name, and HEAD's
// branch, falling back to "HEAD" when detached.
func (r *Repo) RepoIdentity(ctx context.Context) (repoName, branch string, err error) {
	res, err := r.git.RunExit(ctx, "config", "--get", "remote.origin.url")
	if err != nil {
		return "", "", fmt.Errorf("appgit: read origin URL in %s: %w", r.workDir, err)
	}
	if res.ExitCode == 0 {
		repoName = repoNameFromURL(firstLine(res.Stdout))
	}
	if repoName == "" {
		repoName = filepath.Base(strings.TrimRight(filepath.Clean(r.workDir), string(filepath.Separator)))
	}

	// symbolic-ref answers correctly for an unborn HEAD too (where
	// rev-parse --abbrev-ref fails outright) and exits 1, quietly, when
	// HEAD is detached.
	res, err = r.git.RunExit(ctx, "symbolic-ref", "--short", "-q", "HEAD")
	if err != nil {
		return "", "", fmt.Errorf("appgit: read current branch in %s: %w", r.workDir, err)
	}
	branch = firstLine(res.Stdout)
	if res.ExitCode != 0 || branch == "" {
		branch = "HEAD"
	}
	return repoName, branch, nil
}

// repoNameFromURL reduces a remote URL to its repository name; it
// handles both path-like and scp-like ("host:owner/repo.git") forms
// because both reduce to "the last segment, minus .git".
func repoNameFromURL(url string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(url), "/")
	if trimmed == "" {
		return ""
	}
	if i := strings.LastIndexAny(trimmed, "/:"); i >= 0 {
		trimmed = trimmed[i+1:]
	}
	return strings.TrimSuffix(trimmed, ".git")
}

// WorktreeDocsTree returns the current worktree docs tree OID (for the
// base-advance rule, §5.3 step 6).
//
// Mechanism: a scratch index in a temp directory, selected with
// GIT_INDEX_FILE, is loaded from HEAD and then refreshed from the
// worktree for the docs pathspec only (`git add -A -- <docsDir>`);
// `git write-tree` turns it into a tree and the docs subtree is read
// back out. The repository's real index is never opened for writing, so
// this stays safe to run from a hook while the user has staged work
// pending.
//
// Seeding from HEAD (rather than starting empty) matters for one real
// case: a docs file that is tracked but also matches .gitignore. `git
// add` keeps tracked files regardless of ignore rules, so seeding
// reproduces exactly what a commit would contain — which is the
// comparison the base-advance rule needs.
func (r *Repo) WorktreeDocsTree(ctx context.Context) (string, error) {
	docsPath := filepath.Join(r.workDir, filepath.FromSlash(r.docsDir))
	switch _, err := os.Stat(docsPath); {
	case err == nil:
	case os.IsNotExist(err):
		return r.EmptyTree(ctx)
	default:
		return "", fmt.Errorf("appgit: inspect docs directory %s: %w", docsPath, err)
	}

	scratch, err := os.MkdirTemp("", "sanho-index-")
	if err != nil {
		return "", fmt.Errorf("appgit: create scratch index directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(scratch) }()

	run := gitx.New(r.workDir, gitx.WithEnv("GIT_INDEX_FILE="+filepath.Join(scratch, "index")))
	if err := seedScratchIndex(ctx, run); err != nil {
		return "", err
	}

	res, err := run.RunExit(ctx, "add", "-A", "--", r.docsDir)
	if err != nil {
		return "", fmt.Errorf("appgit: stage worktree docs into scratch index: %w", err)
	}
	if res.ExitCode != 0 {
		// An empty or fully ignored docs directory matches no pathspec.
		if strings.Contains(string(res.Stderr), "did not match any files") {
			return r.EmptyTree(ctx)
		}
		return "", fmt.Errorf("appgit: stage worktree docs into scratch index: exit %d: %s",
			res.ExitCode, strings.TrimSpace(string(res.Stderr)))
	}

	root, err := run.Line(ctx, "write-tree")
	if err != nil {
		return "", fmt.Errorf("appgit: write scratch index tree: %w", err)
	}

	sub, err := run.RunExit(ctx, "rev-parse", "--verify", "--quiet", root+":"+r.docsDir)
	if err != nil {
		return "", fmt.Errorf("appgit: resolve worktree docs tree: %w", err)
	}
	if sub.ExitCode != 0 {
		return r.EmptyTree(ctx)
	}
	return firstLine(sub.Stdout), nil
}

// seedScratchIndex loads HEAD into the scratch index, or empties it when
// HEAD is unborn.
func seedScratchIndex(ctx context.Context, run *gitx.Runner) error {
	res, err := run.RunExit(ctx, "read-tree", "HEAD")
	if err != nil {
		return fmt.Errorf("appgit: seed scratch index from HEAD: %w", err)
	}
	if res.ExitCode == 0 {
		return nil
	}
	if _, err := run.Run(ctx, "read-tree", "--empty"); err != nil {
		return fmt.Errorf("appgit: initialize empty scratch index: %w", err)
	}
	return nil
}

func firstLine(out []byte) string {
	text := string(out)
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		text = text[:i]
	}
	return strings.TrimSpace(text)
}
