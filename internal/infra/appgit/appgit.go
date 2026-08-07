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
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/irootkernel/sanho/internal/domain/markers"
	"github.com/irootkernel/sanho/internal/infra/gitx"
)

// ErrUnmergedIndex reports that the index carries unmerged (stage > 0)
// entries, which `git write-tree` refuses to turn into a tree.
//
// It is distinguishable on purpose. The pre-commit gate stamps
// provenance from the index tree, and an unmerged index is a state git
// itself will refuse to commit from — so the gate skips stamping and
// stays out of the way rather than reporting a sanho failure for a
// condition git is about to report itself (§5.1, §5.6 "never blocks").
var ErrUnmergedIndex = errors.New("the index has unmerged entries")

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

// IndexDocsTree returns the docs tree OID of the CURRENT index — the
// tree the commit being prepared will carry, which is the first input of
// the §5.1 stamping rule.
//
// `git write-tree` is the whole mechanism, and it is safe to call from
// inside `commit-msg` for two reasons. It writes tree objects to the
// object database and never alters an index *entry* — at most it
// refreshes the index's cache-tree extension, a pure optimization cache
// that git recomputes on its own anyway. And git runs commit hooks with
// GIT_INDEX_FILE pointing at the in-flight commit's index, which gitx
// inherits, so this reads exactly the content being committed rather
// than a stale `.git/index`.
//
// (WorktreeDocsTree needs a scratch index precisely because it must
// `git add` first; here the index is already exactly what is wanted.)
//
// An absent docs directory yields the empty tree, matching DocsTreeOf.
// An unmerged index yields ErrUnmergedIndex.
func (r *Repo) IndexDocsTree(ctx context.Context) (string, error) {
	res, err := r.git.RunExit(ctx, "write-tree")
	if err != nil {
		return "", fmt.Errorf("appgit: write index tree in %s: %w", r.workDir, err)
	}
	if res.ExitCode != 0 {
		stderr := string(res.Stderr)
		if strings.Contains(strings.ToLower(stderr), "unmerged") {
			return "", fmt.Errorf("appgit: write index tree in %s: %w", r.workDir, ErrUnmergedIndex)
		}
		return "", fmt.Errorf("appgit: write index tree in %s: exit %d: %s",
			r.workDir, res.ExitCode, strings.TrimSpace(stderr))
	}

	root := firstLine(res.Stdout)
	sub, err := r.git.RunExit(ctx, "rev-parse", "--verify", "--quiet", root+":"+r.docsDir)
	if err != nil {
		return "", fmt.Errorf("appgit: resolve index docs tree in %s: %w", r.workDir, err)
	}
	if sub.ExitCode != 0 {
		return r.EmptyTree(ctx)
	}
	return firstLine(sub.Stdout), nil
}

// ScanStagedDocsForMarkers applies the §5.4 detector to the docs content
// the commit being prepared would ADD OR CHANGE — the pre-commit gate of
// §5.6 step 1, which is what stops unresolved conflict markers from
// being committed in the first place.
//
// The scope is the staged *diff*, not the whole index (F-H4a), and that
// is a semantic decision as much as a performance one. The gate is about
// what this commit introduces; a marker already sitting in HEAD arrived
// through some earlier path (a `--no-verify` commit, a checkout, a
// v0.1-era commit) and blocking every unrelated commit until the user
// fixes a file they are not touching is a gate that punishes the wrong
// action. §5.3's push gate is where the whole published tree is
// protected. The cost side is the reason it was noticed: the old scan
// spent two git processes per docs file in the index on every commit,
// so a 4,000-file docs directory made `git commit` take 39 seconds.
//
// Unmerged entries (stage > 0) are skipped rather than scanned: their
// stages are the *inputs* of a merge git has not resolved, so they carry
// no marker text, and git refuses to commit from that index anyway. The
// gate stays fail-closed in the same two ways as the blob and worktree
// scanners (audit H2): oversized text is an error naming the file, never
// a silent pass, and read failures propagate. Symlink and gitlink
// entries are skipped — their content is a path, not text.
func (r *Repo) ScanStagedDocsForMarkers(ctx context.Context) ([]string, error) {
	changed, err := r.stagedDocsPaths(ctx)
	if err != nil {
		return nil, err
	}
	if len(changed) == 0 {
		return nil, nil
	}

	entries, err := r.stagedEntries(ctx, changed)
	if err != nil {
		return nil, err
	}
	return r.scanBlobs(ctx, entries, "staged")
}

// stagedDocsPaths lists the docs paths the staged commit adds, modifies,
// or renames into place, relative to HEAD.
//
// An unborn HEAD has no tree to diff against, so the empty tree stands
// in — which makes every staged docs path "added", exactly right for a
// first commit. Deletions (D) are excluded: a path being removed cannot
// carry a marker into the commit.
func (r *Repo) stagedDocsPaths(ctx context.Context) ([]string, error) {
	against, err := r.headOrEmptyTree(ctx)
	if err != nil {
		return nil, err
	}
	res, err := r.git.Run(ctx, "diff-index", "--cached", "-z", "--name-only",
		"--diff-filter=ACMRT", against, "--", r.docsDir)
	if err != nil {
		return nil, fmt.Errorf("appgit: list staged docs changes in %s: %w", r.workDir, err)
	}
	return splitNULPaths(res.Stdout), nil
}

// headOrEmptyTree names something diffable: HEAD when it exists, the
// empty tree when it does not.
func (r *Repo) headOrEmptyTree(ctx context.Context) (string, error) {
	res, err := r.git.RunExit(ctx, "rev-parse", "--verify", "--quiet", "HEAD^{commit}")
	if err != nil {
		return "", fmt.Errorf("appgit: resolve HEAD in %s: %w", r.workDir, err)
	}
	if res.ExitCode == 0 {
		return "HEAD", nil
	}
	return r.EmptyTree(ctx)
}

// stagedEntries reads the index rows for the named paths.
func (r *Repo) stagedEntries(ctx context.Context, paths []string) ([]scanTarget, error) {
	var targets []scanTarget

	for batch := range batches(paths) {
		args := append([]string{"ls-files", "--stage", "-z", "--"}, batch...)
		res, err := r.git.Run(ctx, args...)
		if err != nil {
			return nil, fmt.Errorf("appgit: list staged docs in %s: %w", r.workDir, err)
		}
		for _, record := range strings.Split(string(res.Stdout), "\x00") {
			if strings.TrimSpace(record) == "" {
				continue
			}
			entry, err := parseLsFilesStageEntry(record)
			if err != nil {
				return nil, fmt.Errorf("appgit: read staged docs listing in %s: %w", r.workDir, err)
			}
			if entry.stage != 0 || entry.mode == modeSymlink || entry.mode == modeGitlink {
				continue
			}
			targets = append(targets, scanTarget{path: entry.path, object: entry.object})
		}
	}
	return targets, nil
}

// lsFilesStageEntry is one `git ls-files --stage -z` record:
// "<mode> SP <object> SP <stage>HT<path>". Unlike ls-tree there is no
// size column, so sizes come from blobSize.
type lsFilesStageEntry struct {
	mode   string
	object string
	stage  int
	path   string
}

func parseLsFilesStageEntry(record string) (lsFilesStageEntry, error) {
	head, path, ok := strings.Cut(record, "\t")
	if !ok || path == "" {
		return lsFilesStageEntry{}, fmt.Errorf("malformed ls-files entry %q", record)
	}
	fields := strings.Fields(head)
	if len(fields) != 3 {
		return lsFilesStageEntry{}, fmt.Errorf("malformed ls-files entry %q", record)
	}
	stage, err := strconv.Atoi(fields[2])
	if err != nil {
		return lsFilesStageEntry{}, fmt.Errorf("malformed stage in ls-files entry %q", record)
	}
	return lsFilesStageEntry{mode: fields[0], object: fields[1], stage: stage, path: path}, nil
}

// ScanDocsBlobsForMarkers scans a commit's docs blobs (§5.4 detector);
// returns conflicted paths. Unreadable blobs error.
//
// The gate is fail-closed in both directions the v0.1 detector got wrong
// (audit H2): oversized text is an error naming the file rather than a
// silent pass, and any read failure propagates instead of being
// swallowed. Symlink and gitlink entries are skipped — their blob
// content is a path, not text that can carry markers.
func (r *Repo) ScanDocsBlobsForMarkers(ctx context.Context, commit string) ([]string, error) {
	return r.ScanDocsBlobsSince(ctx, "", commit)
}

// ScanDocsBlobsSince is the push-gate scan of §5.3 step 3, scoped to
// what the push would actually introduce (F-H4b).
//
// since is the branch's previous remote tip. When it resolves, only the
// docs files that DIFFER between it and the tip are scanned: everything
// else was in the tree the previous push published, and a push carrying
// markers is rejected, so anything already upstream passed this same
// gate. When since is empty or unknown to this repository — a brand-new
// branch, a rewritten history, a first push after installing sanho —
// the whole tree is scanned, which is the fail-closed answer whenever
// the induction cannot be relied on.
//
// The cost this removes is real: the full-tree scan ran one `cat-file
// -s` plus one `cat-file blob` per docs file on every push. What
// replaces it is one `cat-file --batch-check` and one `cat-file --batch`
// for the whole set.
func (r *Repo) ScanDocsBlobsSince(ctx context.Context, since, commit string) ([]string, error) {
	entries, err := r.docsBlobsToScan(ctx, since, commit)
	if err != nil {
		return nil, err
	}
	return r.scanBlobs(ctx, entries, commit)
}

// docsBlobsToScan lists the blobs one push-gate scan must read.
func (r *Repo) docsBlobsToScan(ctx context.Context, since, commit string) ([]scanTarget, error) {
	if since != "" {
		usable, err := r.commitExists(ctx, since)
		if err != nil {
			return nil, err
		}
		if usable {
			return r.changedDocsBlobs(ctx, since, commit)
		}
	}
	return r.allDocsBlobs(ctx, commit)
}

// changedDocsBlobs lists the docs blobs the tip adds or changes relative
// to since. `diff-tree --raw` hands over the destination object id
// directly, so no second lookup is needed to address the content.
func (r *Repo) changedDocsBlobs(ctx context.Context, since, commit string) ([]scanTarget, error) {
	res, err := r.git.Run(ctx, "diff-tree", "-r", "-z", "--no-renames",
		"--diff-filter=ACMRT", since, commit, "--", r.docsDir)
	if err != nil {
		return nil, fmt.Errorf("appgit: diff docs of %s..%s: %w", since, commit, err)
	}
	return parseDiffTreeRaw(string(res.Stdout))
}

// allDocsBlobs lists every docs blob of a commit.
func (r *Repo) allDocsBlobs(ctx context.Context, commit string) ([]scanTarget, error) {
	res, err := r.git.Run(ctx, "ls-tree", "-r", "-z", commit, "--", r.docsDir)
	if err != nil {
		return nil, fmt.Errorf("appgit: list docs blobs of %s: %w", commit, err)
	}

	var targets []scanTarget
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
		targets = append(targets, scanTarget{path: blob.path, object: blob.object})
	}
	return targets, nil
}

// scanTarget is one blob a scan must classify: where it lives and what
// object holds its content.
type scanTarget struct {
	path   string
	object string
}

// scanBlobs applies the §5.4 detector to a set of blobs with two git
// processes in total, whatever the set's size (F-H4c).
//
// The order is §5.4's as corrected by F-M8: sniff first, size second.
// A NUL byte in the first 8 KiB means binary, and binary content is
// skipped no matter how large — an illustration or a PDF under docs/ is
// not a conflict and must not block a commit merely for being big.
// Only content that is *text* and over markers.MaxScanSize is the
// fail-closed error, which is the case the cap was written for.
func (r *Repo) scanBlobs(ctx context.Context, targets []scanTarget, where string) ([]string, error) {
	if len(targets) == 0 {
		return nil, nil
	}

	sizes, err := r.batchSizes(ctx, targets)
	if err != nil {
		return nil, err
	}

	var small, large []scanTarget
	for _, target := range targets {
		if sizes[target.object] > markers.MaxScanSize {
			large = append(large, target)
			continue
		}
		small = append(small, target)
	}

	contents, err := r.batchContents(ctx, small)
	if err != nil {
		return nil, err
	}

	var conflicted []string
	for _, target := range small {
		if markers.Scan(contents[target.object]).HasMarkers {
			conflicted = append(conflicted, target.path)
		}
	}
	// Oversized blobs are read only far enough to classify them.
	for _, target := range large {
		binary, err := r.sniffBinary(ctx, target.object)
		if err != nil {
			return nil, fmt.Errorf("appgit: read %s in %s: %w", target.path, where, err)
		}
		if binary {
			continue
		}
		return nil, fmt.Errorf("appgit: %s in %s is %d bytes: %w",
			target.path, where, sizes[target.object], markers.ErrTooLarge)
	}
	return conflicted, nil
}

// batchSizes asks one `git cat-file --batch-check` child for every
// object's size. The object ids go in on stdin, which is the one thing
// gitx lets a caller stream: data, never command construction.
func (r *Repo) batchSizes(ctx context.Context, targets []scanTarget) (map[string]int64, error) {
	res, err := r.git.RunWithStdin(ctx, objectRequestStream(targets), "cat-file", "--batch-check")
	if err != nil {
		return nil, fmt.Errorf("appgit: read docs object sizes in %s: %w", r.workDir, err)
	}

	sizes := make(map[string]int64, len(targets))
	for _, line := range strings.Split(string(res.Stdout), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 {
			continue
		}
		size, parseErr := strconv.ParseInt(fields[2], 10, 64)
		if parseErr != nil {
			return nil, fmt.Errorf("appgit: parse size of %s in %s: %w", fields[0], r.workDir, parseErr)
		}
		sizes[fields[0]] = size
	}
	for _, target := range targets {
		if _, ok := sizes[target.object]; !ok {
			return nil, fmt.Errorf("appgit: object %s for %s is missing from %s",
				target.object, target.path, r.workDir)
		}
	}
	return sizes, nil
}

// batchContents reads every object's bytes from one `git cat-file
// --batch` child.
//
// The stream is `<oid> <type> <size>LF<content>LF` per request, so the
// declared size is what delimits the content — a blob containing a
// newline, or a header-shaped line, parses correctly because nothing is
// looked for, only counted.
func (r *Repo) batchContents(ctx context.Context, targets []scanTarget) (map[string][]byte, error) {
	if len(targets) == 0 {
		return map[string][]byte{}, nil
	}
	res, err := r.git.RunWithStdin(ctx, objectRequestStream(targets), "cat-file", "--batch")
	if err != nil {
		return nil, fmt.Errorf("appgit: read docs objects in %s: %w", r.workDir, err)
	}
	return parseCatFileBatch(res.Stdout, r.workDir)
}

// sniffBinary reads only the leading bytes of one object, which is all
// the §5.4 binary classification needs, and is why an oversized blob can
// be classified without being materialized.
func (r *Repo) sniffBinary(ctx context.Context, object string) (bool, error) {
	run := gitx.New(r.workDir, gitx.WithStdoutLimit(markers.BinarySniffSize))
	res, err := run.Run(ctx, "cat-file", "blob", object)
	if err != nil {
		return false, err
	}
	return markers.Scan(res.Stdout).Binary, nil
}

// objectRequestStream renders the request side of `cat-file --batch`:
// one object id per line, deduplicated so a tree holding the same blob
// at two paths is fetched once.
func objectRequestStream(targets []scanTarget) io.Reader {
	var b strings.Builder
	seen := make(map[string]bool, len(targets))
	for _, target := range targets {
		if seen[target.object] {
			continue
		}
		seen[target.object] = true
		b.WriteString(target.object)
		b.WriteByte('\n')
	}
	return strings.NewReader(b.String())
}

// parseCatFileBatch splits a `git cat-file --batch` stream into objects.
func parseCatFileBatch(stream []byte, workDir string) (map[string][]byte, error) {
	contents := make(map[string][]byte)

	for offset := 0; offset < len(stream); {
		newline := bytes.IndexByte(stream[offset:], '\n')
		if newline < 0 {
			break
		}
		header := string(stream[offset : offset+newline])
		offset += newline + 1

		fields := strings.Fields(header)
		if len(fields) == 2 && fields[1] == "missing" {
			return nil, fmt.Errorf("appgit: object %s is missing from %s", fields[0], workDir)
		}
		if len(fields) != 3 {
			return nil, fmt.Errorf("appgit: unreadable cat-file header %q in %s", header, workDir)
		}
		size, err := strconv.Atoi(fields[2])
		if err != nil || size < 0 {
			return nil, fmt.Errorf("appgit: unreadable cat-file size in %q in %s", header, workDir)
		}
		if offset+size > len(stream) {
			return nil, fmt.Errorf("appgit: truncated cat-file content for %s in %s", fields[0], workDir)
		}
		contents[fields[0]] = stream[offset : offset+size]
		// The content is followed by a single LF the size does not count.
		offset += size + 1
	}
	return contents, nil
}

// parseDiffTreeRaw reads `git diff-tree -r -z --raw` output, whose
// records are ":<srcmode> <dstmode> <srcoid> <dstoid> <status>" followed
// by the path as its own NUL-terminated field.
func parseDiffTreeRaw(stream string) ([]scanTarget, error) {
	fields := strings.Split(stream, "\x00")
	var targets []scanTarget

	for i := 0; i < len(fields); i++ {
		record := fields[i]
		if !strings.HasPrefix(record, ":") {
			continue
		}
		parts := strings.Fields(strings.TrimPrefix(record, ":"))
		if len(parts) < 5 {
			return nil, fmt.Errorf("appgit: unreadable diff-tree record %q", record)
		}
		i++
		if i >= len(fields) {
			return nil, fmt.Errorf("appgit: diff-tree record %q has no path", record)
		}
		dstMode, dstOID, path := parts[1], parts[3], fields[i]
		if path == "" || dstMode == modeSymlink || dstMode == modeGitlink || strings.Trim(dstOID, "0") == "" {
			continue
		}
		targets = append(targets, scanTarget{path: path, object: dstOID})
	}
	return targets, nil
}

// splitNULPaths reads a NUL-separated path list, dropping the empty
// trailing field git leaves behind.
func splitNULPaths(out []byte) []string {
	var paths []string
	for _, path := range strings.Split(string(out), "\x00") {
		if path != "" {
			paths = append(paths, path)
		}
	}
	return paths
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

// parseLsTreeEntry accepts both the plain three-column listing and the
// `--long` one that adds a size. The scanners no longer ask for sizes
// (one `cat-file --batch-check` answers for the whole set, F-H4c) while
// the checkout path still does, so one parser serves both.
func parseLsTreeEntry(entry string) (lsTreeEntry, error) {
	head, path, ok := strings.Cut(entry, "\t")
	if !ok {
		return lsTreeEntry{}, fmt.Errorf("malformed ls-tree entry %q", entry)
	}
	fields := strings.Fields(head)
	if len(fields) != 3 && len(fields) != 4 {
		return lsTreeEntry{}, fmt.Errorf("malformed ls-tree entry %q", entry)
	}

	parsed := lsTreeEntry{mode: fields[0], kind: fields[1], object: fields[2], path: path}
	if parsed.kind != "blob" || len(fields) == 3 {
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
