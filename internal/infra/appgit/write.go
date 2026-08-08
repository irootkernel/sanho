package appgit

// Write side of the application-repository adapter: everything
// `sanho sync` and `sanho pull` need in order to move the docs worktree
// and index (docs/architecture.md "Synchronization"). The read side in appgit.go keeps its
// stricter contract — pre-push publication still touches nothing —
// because worktree inviolability (the publication contract) belongs to the push path, not
// to sync.
//
// One rule governs every operation here: **only the docs pathspec is
// ever written**. Sync runs between the user's own commits, on their
// schedule (D3), so their non-docs work — including changes they have
// already staged — must come out of it byte for byte unchanged. That is
// why nothing below uses a whole-index command (`git checkout`,
// `git read-tree`, `git reset`, `git stash`): each invocation is handed
// the exact list of docs paths it may touch, computed from the target
// tree and the current index.
//
// The mechanism CheckoutDocsTree uses is therefore three plumbing
// passes over explicit path lists, in this order:
//
//  1. `git rm --force --ignore-unmatch` for every docs path the index
//     holds and the target tree does not. This is the deletion case,
//     which a plain `git checkout <tree> -- docs/` silently skips; it
//     removes the file from the index and the worktree in one step and
//     prunes directories the removal empties.
//  2. `git update-index --add --replace --cacheinfo <mode>,<oid>,<path>`
//     for every entry of the target tree. Repeating --cacheinfo lets one
//     invocation set many entries with no stdin (gitx is argv-only), and
//     it writes nothing but the named paths.
//  3. `git checkout-index --force --index` for those same paths, which
//     materializes the index content into the worktree (creating leading
//     directories, restoring file modes and symlinks) and refreshes the
//     index stat data so `git status` does not report the result as
//     modified.
//
// Deletions run first so that a file→directory or directory→file swap
// at the same path finds the way clear.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/irootkernel/sanho/internal/domain/markers"
)

// pathBatchSize bounds how many paths one git invocation receives. Docs
// directories are small in practice, but the operating system argument
// limit is a hard edge rather than a soft one, so path lists are chunked
// instead of assumed to fit.
const pathBatchSize = 200

// literalPathspec turns one exact path into a pathspec that cannot
// glob. Docs file names come out of a tree listing verbatim, so a file
// literally named `a*.md` has to match itself and nothing else.
func literalPathspec(path string) string { return ":(literal)" + path }

// DocsClean reports whether the docs paths are clean in the worktree
// and the index relative to HEAD (the synchronization contract). Paths outside the docs
// directory are not consulted: the user's non-docs work in progress is
// none of sync's business.
//
// `git status --porcelain` restricted to the docs pathspec answers all
// three questions at once — staged changes, unstaged changes, untracked
// files — and untracked files count as dirty deliberately. An untracked
// file under docs/ is docs work that a sync commit would not carry,
// while the base advances past it; refusing is the fail-closed reading
// of "commit or stash your docs changes first", and `git status` is the
// very signal the user is being asked to clear.
func (r *Repo) DocsClean(ctx context.Context) (bool, error) {
	res, err := r.git.Run(ctx, "status", "--porcelain", "--", r.docsDir)
	if err != nil {
		return false, fmt.Errorf("appgit: read docs status in %s: %w", r.workDir, err)
	}
	return strings.TrimSpace(string(res.Stdout)) == "", nil
}

// HeadDocsTree returns HEAD's docs tree OID, the "ours" side of the
// the synchronization contract merge. An unborn HEAD and a HEAD without a docs directory both
// yield the empty tree, which is the same stand-in DocsTreeOf uses:
// "this commit contributes no docs content" is one state, however it
// came about.
func (r *Repo) HeadDocsTree(ctx context.Context) (string, error) {
	res, err := r.git.RunExit(ctx, "rev-parse", "--verify", "--quiet", "HEAD^{commit}")
	if err != nil {
		return "", fmt.Errorf("appgit: resolve HEAD in %s: %w", r.workDir, err)
	}
	if res.ExitCode != 0 {
		return r.EmptyTree(ctx)
	}
	return r.DocsTreeOf(ctx, "HEAD")
}

// CommitTree returns the root tree OID of any commit present in this
// repository's object database.
//
// Sync uses it for canonical commits, not app commits: the canonical
// repository is docs-only, so once `FetchIntoApp` has imported a
// canonical commit its root tree *is* the docs tree the merge takes as
// base or theirs. Resolving it here rather than clone-side is not an
// accident — the merge runs in the app repository, so this call
// doubles as proof that the object the merge needs is actually present
// where the merge will look for it.
func (r *Repo) CommitTree(ctx context.Context, commit string) (string, error) {
	tree, err := r.git.Line(ctx, "rev-parse", "--verify", commit+"^{tree}")
	if err != nil {
		return "", fmt.Errorf("appgit: resolve tree of %s in %s: %w", commit, r.workDir, err)
	}
	return tree, nil
}

// DocsPathsChangedBetween reports whether any of paths differs between
// two docs trees.
//
// It is the question "has this sync been resolved?" reduced to something
// git can answer: the sync note records which paths the merge conflicted
// on, and a resolution is a commit that changed at least one of them.
// Asking only whether the docs tree moved at all is passed by any docs
// commit, which is how an unrelated note file came to stand in for a
// resolution.
//
// Both arguments are *docs* trees, so the recorded repository-relative
// paths (`docs/api.md`) are reduced to the tree-relative ones git wants
// (`api.md`) here rather than by every caller. A path is matched
// literally: a docs file called `a*.md` has to match itself and nothing
// else. The empty answers — no paths, an unrecorded tree, two identical
// trees — are all "nothing changed", which is the reading that keeps a
// caller from mistaking an absent fact for a resolution.
func (r *Repo) DocsPathsChangedBetween(ctx context.Context, fromTree, toTree string, paths []string) (bool, error) {
	if fromTree == "" || toTree == "" || len(paths) == 0 {
		return false, nil
	}
	if fromTree == toTree {
		return false, nil
	}

	prefix := r.docsDir + "/"
	relative := make([]string, 0, len(paths))
	for _, path := range paths {
		name := strings.TrimPrefix(path, prefix)
		if name == "" {
			continue
		}
		relative = append(relative, name)
	}

	for batch := range batches(relative) {
		args := []string{"diff-tree", "-r", "-z", "--name-only", fromTree, toTree, "--"}
		for _, name := range batch {
			args = append(args, literalPathspec(name))
		}
		res, err := r.git.Run(ctx, args...)
		if err != nil {
			return false, fmt.Errorf("appgit: diff docs trees %s..%s in %s: %w",
				fromTree, toTree, r.workDir, err)
		}
		if len(splitNULPaths(res.Stdout)) > 0 {
			return true, nil
		}
	}
	return false, nil
}

// DocsTreeDifferences counts the paths that differ between two docs
// trees, whatever they are.
//
// It answers a reporting question rather than a gate one: `sanho sync
// --continue` completes a sync whose merge result the worktree may no
// longer resemble, and the user is told how far it drifted rather than
// left to assume the merge is what was adopted. Two identical trees, or
// an unrecorded one, are "no difference" — the reading that keeps a
// missing fact from being reported as a change.
func (r *Repo) DocsTreeDifferences(ctx context.Context, fromTree, toTree string) (int, error) {
	if fromTree == "" || toTree == "" || fromTree == toTree {
		return 0, nil
	}
	res, err := r.git.Run(ctx, "diff-tree", "-r", "-z", "--name-only", fromTree, toTree)
	if err != nil {
		return 0, fmt.Errorf("appgit: diff docs trees %s..%s in %s: %w", fromTree, toTree, r.workDir, err)
	}
	return len(splitNULPaths(res.Stdout)), nil
}

// CheckoutDocsTree materializes tree — a *docs* tree, whose entries are
// relative to the docs directory — into the worktree and the index,
// touching no path outside the docs directory. Files the index holds
// and tree does not are removed from both; everything tree holds is
// written to both. See the package-level note above for the mechanism
// and why it is built out of explicit path lists.
func (r *Repo) CheckoutDocsTree(ctx context.Context, tree string) error {
	entries, err := r.docsTreeEntries(ctx, tree)
	if err != nil {
		return err
	}
	indexed, err := r.indexDocsPaths(ctx)
	if err != nil {
		return err
	}

	wanted := make(map[string]bool, len(entries))
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		wanted[entry.path] = true
		paths = append(paths, entry.path)
	}
	var obsolete []string
	for _, path := range indexed {
		if !wanted[path] {
			obsolete = append(obsolete, path)
		}
	}

	// Validate EVERY path before deleting anything (F-M7). The three
	// passes below are ordered deletion-first, so a target rejected
	// half-way through would leave the docs directory emptied of the
	// files the deletion pass already removed and refilled with nothing
	// — the one shape of data loss this file exists to prevent. A tree
	// carrying `..`, an absolute path, or a `.git` segment is a hostile
	// or corrupt tree; refusing it whole costs nothing.
	for _, path := range append(append([]string{}, paths...), obsolete...) {
		if err := validateDocsPath(path); err != nil {
			return err
		}
	}

	if err := r.removeDocsPaths(ctx, obsolete); err != nil {
		return checkoutRecoveryError(r.docsDir, err)
	}
	if err := r.stageDocsEntries(ctx, entries); err != nil {
		return checkoutRecoveryError(r.docsDir, err)
	}
	if err := r.materializeDocsPaths(ctx, paths); err != nil {
		return checkoutRecoveryError(r.docsDir, err)
	}
	return nil
}

// validateDocsPath rejects a tree entry that would address anything but
// a file inside the docs directory.
func validateDocsPath(path string) error {
	switch {
	case path == "":
		return fmt.Errorf("appgit: refusing an empty path in the docs tree")
	case filepath.IsAbs(path) || strings.HasPrefix(path, "/"):
		return fmt.Errorf("appgit: refusing the absolute docs path %q", path)
	}
	for _, segment := range strings.Split(path, "/") {
		if segment == ".." || segment == ".git" {
			return fmt.Errorf("appgit: refusing the docs path %q (it contains a %q segment)", path, segment)
		}
	}
	return nil
}

// checkoutRecoveryError names the way back.
//
// A checkout that fails between its passes leaves the docs directory
// partially written, and the user needs one command that restores it.
// `git checkout HEAD -- <docsDir>` is that command, it is scoped to the
// docs pathspec exactly as everything here is, and it works whatever
// pass failed.
func checkoutRecoveryError(docsDir string, err error) error {
	return fmt.Errorf("%w (the docs directory may be partially written; restore it with: git checkout HEAD -- %s)",
		err, docsDir)
}

// RestoreDocsFromHead resets the docs worktree and index to HEAD
// (`sanho sync --abort`, the synchronization contract). It is CheckoutDocsTree of HEAD's
// docs tree, which is what makes it correct for the state an aborted
// sync leaves behind: files the conflicted merge introduced are absent
// from HEAD's tree, so the deletion pass removes them from the worktree
// and the index instead of leaving them stranded.
func (r *Repo) RestoreDocsFromHead(ctx context.Context) error {
	tree, err := r.HeadDocsTree(ctx)
	if err != nil {
		return err
	}
	return r.CheckoutDocsTree(ctx, tree)
}

// CommitDocs creates one ordinary commit of the current worktree state
// of the docs pathspec (`git commit -m <message> -- <docsDir>`) and
// returns the new HEAD.
//
// Two properties are the point of doing it this way. Changes the user
// has staged for other paths stay staged and uncommitted, because a
// pathspec commit records only the listed paths. And the commit is the
// *user's* — no identity is injected, so git resolves author and
// committer from the repository's own configuration, which is what D3
// means by "the tool never creates commits" in the sense that matters:
// nothing here is attributed to sanho. Hooks run normally; this is a
// real commit and is never made with --no-verify.
func (r *Repo) CommitDocs(ctx context.Context, message string) (string, error) {
	if _, err := r.git.Run(ctx, "commit", "--quiet", "-m", message, "--", r.docsDir); err != nil {
		return "", fmt.Errorf("appgit: commit docs in %s: %w", r.workDir, err)
	}
	oid, err := r.git.Line(ctx, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return "", fmt.Errorf("appgit: resolve the docs commit in %s: %w", r.workDir, err)
	}
	return oid, nil
}

// ScanWorktreeDocsForMarkers applies the merge contract detector to the docs
// *worktree* — the sync-in-progress half of the detector's contract,
// where ScanDocsBlobsForMarkers covers committed content. It returns
// the repository-relative paths that still carry conflict markers.
//
// It walks the directory rather than the index so that a file the user
// created while resolving is scanned too, and so that a file they
// deleted while resolving simply is not there. The gate is fail-closed
// in the same two ways as the blob scanner (audit H2): a file larger
// than markers.MaxScanSize is an error naming the file, never a silent
// pass, and any read failure propagates. Symlinks and other irregular
// entries are skipped — their content is a path, not mergeable text.
func (r *Repo) ScanWorktreeDocsForMarkers(ctx context.Context) ([]string, error) {
	root := filepath.Join(r.workDir, filepath.FromSlash(r.docsDir))
	switch _, err := os.Stat(root); {
	case err == nil:
	case os.IsNotExist(err):
		return nil, nil
	default:
		return nil, fmt.Errorf("appgit: inspect docs directory %s: %w", root, err)
	}

	var conflicted []string
	walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("appgit: walk docs directory %s: %w", path, err)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() || !entry.Type().IsRegular() {
			return nil
		}

		relative, err := filepath.Rel(r.workDir, path)
		if err != nil {
			return fmt.Errorf("appgit: locate %s under %s: %w", path, r.workDir, err)
		}
		name := filepath.ToSlash(relative)

		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("appgit: stat %s: %w", name, err)
		}
		if info.Size() > markers.MaxScanSize {
			// the merge contract's order, as corrected by F-M8: classify first, then
			// apply the size rule. A large *binary* file — an image, a
			// PDF, a recording under docs/ — cannot carry a conflict
			// marker, so refusing it would block the commit path over
			// content the detector has nothing to say about. Only
			// oversized TEXT is the fail-closed error (audit H2).
			binary, sniffErr := sniffBinaryFile(path)
			if sniffErr != nil {
				return fmt.Errorf("appgit: read %s: %w", name, sniffErr)
			}
			if binary {
				return nil
			}
			return fmt.Errorf("appgit: %s is %d bytes: %w", name, info.Size(), markers.ErrTooLarge)
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("appgit: read %s: %w", name, err)
		}
		if markers.Scan(content).HasMarkers {
			conflicted = append(conflicted, name)
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	return conflicted, nil
}

// sniffBinaryFile reads only the leading bytes the merge contract's binary rule looks
// at, which is what makes classifying an oversized file cheap enough to
// do before refusing it.
func sniffBinaryFile(path string) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer func() { _ = file.Close() }()

	head := make([]byte, markers.BinarySniffSize)
	n, err := io.ReadFull(file, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return false, err
	}
	return markers.Scan(head[:n]).Binary, nil
}

// docsTreeEntry is one file of a docs tree, already addressed by the
// path it occupies in the application repository.
type docsTreeEntry struct {
	path   string
	mode   string
	object string
}

// docsTreeEntries lists tree recursively. --full-tree makes the listing
// independent of the process working directory, -z removes path
// quoting, and --long is what parseLsTreeEntry (shared with the blob
// scanner) expects.
func (r *Repo) docsTreeEntries(ctx context.Context, tree string) ([]docsTreeEntry, error) {
	res, err := r.git.Run(ctx, "ls-tree", "-r", "-z", "--long", "--full-tree", tree)
	if err != nil {
		return nil, fmt.Errorf("appgit: list docs tree %s in %s: %w", tree, r.workDir, err)
	}

	var entries []docsTreeEntry
	for _, record := range strings.Split(string(res.Stdout), "\x00") {
		if strings.TrimSpace(record) == "" {
			continue
		}
		parsed, err := parseLsTreeEntry(record)
		if err != nil {
			return nil, fmt.Errorf("appgit: read listing of docs tree %s: %w", tree, err)
		}
		entries = append(entries, docsTreeEntry{
			path:   r.docsDir + "/" + parsed.path,
			mode:   parsed.mode,
			object: parsed.object,
		})
	}
	return entries, nil
}

// indexDocsPaths lists the docs paths the index currently holds.
// Unmerged entries appear once per stage, so paths are deduplicated.
func (r *Repo) indexDocsPaths(ctx context.Context) ([]string, error) {
	res, err := r.git.Run(ctx, "ls-files", "-z", "--", r.docsDir)
	if err != nil {
		return nil, fmt.Errorf("appgit: list indexed docs paths in %s: %w", r.workDir, err)
	}

	var paths []string
	seen := make(map[string]bool)
	for _, path := range strings.Split(string(res.Stdout), "\x00") {
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		paths = append(paths, path)
	}
	return paths, nil
}

// removeDocsPaths drops paths from the index and the worktree. --force
// is required and intended: an aborted sync removes files whose
// worktree content the merge itself wrote, which git would otherwise
// refuse to discard. --ignore-unmatch keeps the operation idempotent,
// so re-running an interrupted abort is not an error.
func (r *Repo) removeDocsPaths(ctx context.Context, paths []string) error {
	for batch := range batches(paths) {
		args := []string{"rm", "--force", "-r", "--quiet", "--ignore-unmatch", "--"}
		for _, path := range batch {
			args = append(args, literalPathspec(path))
		}
		if _, err := r.git.Run(ctx, args...); err != nil {
			return fmt.Errorf("appgit: remove docs paths in %s: %w", r.workDir, err)
		}
	}
	return nil
}

// stageDocsEntries writes the target tree's entries into the index.
// --add covers paths that are new, --replace covers a path whose kind
// changed, and neither can reach an entry that was not named.
func (r *Repo) stageDocsEntries(ctx context.Context, entries []docsTreeEntry) error {
	for batch := range batches(entries) {
		args := []string{"update-index", "--add", "--replace"}
		for _, entry := range batch {
			args = append(args, "--cacheinfo", entry.mode+","+entry.object+","+entry.path)
		}
		if _, err := r.git.Run(ctx, args...); err != nil {
			return fmt.Errorf("appgit: stage docs entries in %s: %w", r.workDir, err)
		}
	}
	return nil
}

// materializeDocsPaths writes the staged content out to the worktree.
// checkout-index matches its arguments against index entries by exact
// name (no globbing), which is why these are raw paths rather than
// pathspecs.
func (r *Repo) materializeDocsPaths(ctx context.Context, paths []string) error {
	for batch := range batches(paths) {
		args := append([]string{"checkout-index", "--force", "--index", "--"}, batch...)
		if _, err := r.git.Run(ctx, args...); err != nil {
			return fmt.Errorf("appgit: write docs files in %s: %w", r.workDir, err)
		}
	}
	return nil
}

// batches yields consecutive slices of at most pathBatchSize items.
func batches[T any](items []T) func(func([]T) bool) {
	return func(yield func([]T) bool) {
		for start := 0; start < len(items); start += pathBatchSize {
			end := min(start+pathBatchSize, len(items))
			if !yield(items[start:end]) {
				return
			}
		}
	}
}
