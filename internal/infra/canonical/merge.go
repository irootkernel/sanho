package canonical

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/irootkernel/sanho/internal/infra/fsx"
	"github.com/irootkernel/sanho/internal/infra/gitx"
)

// Merge-side constants (docs/architecture.md "Merge and marker contracts").
const (
	// labelOurs / labelUpstream are the names git writes into conflict
	// markers (`<<<<<<< sanho-ours` / `>>>>>>> sanho-upstream`) because
	// they are the names the two sides are addressed by on the
	// merge-tree command line. Audit L1 was markers labeled with temp
	// paths and raw OIDs.
	labelOurs     = "sanho-ours"
	labelUpstream = "sanho-upstream"

	// refOurs / refUpstream are where those names resolve. Refs directly
	// under refs/ are on git's ref search path, so the short label — and
	// nothing longer — is what appears in markers and in the
	// `<path>~<side>` names git invents for distinct-type conflicts.
	refOurs     = "refs/" + labelOurs
	refUpstream = "refs/" + labelUpstream

	// mergeTreeConflictExit is `git merge-tree`'s "merged with
	// conflicts" status. 0 is clean, anything else is a real failure —
	// note this is NOT git merge-file's exit contract, whose 1..127
	// encodes a conflict *count* (misreading that was audit Critical C2,
	// the merge contract).
	mergeTreeConflictExit = 1

	// mergeLockName is the file that serializes merges sharing one ref
	// store. See MergeTree for why it exists and where it lives.
	mergeLockName = "sanho-merge.lock"
)

// ErrMergeFailed marks a the merge contract merge that could not be performed at all —
// as distinct from one that ran and reported conflicts. It exists so the
// push path can render a the guidance contract-shaped message instead of leaking a raw
// git chain to the user (F-C2).
var ErrMergeFailed = errors.New("docs merge could not be performed")

// syntheticIdentity pins the author/committer of the parentless commits
// that wrap the three docs trees. Merge results must be reproducible —
// the same three trees must always yield the same result tree OID, on
// any machine and at any time — so nothing time- or user-dependent may
// leak into the synthetic commits.
var syntheticIdentity = []string{
	"GIT_AUTHOR_NAME=sanho",
	"GIT_AUTHOR_EMAIL=sanho@local",
	"GIT_AUTHOR_DATE=2000-01-01T00:00:00Z",
	"GIT_COMMITTER_NAME=sanho",
	"GIT_COMMITTER_EMAIL=sanho@local",
	"GIT_COMMITTER_DATE=2000-01-01T00:00:00Z",
}

// MergeResult is the outcome of a tree-level 3-way merge.
type MergeResult struct {
	// Tree is the result tree OID (contains conflict markers when not
	// Clean).
	Tree  string
	Clean bool
	// Conflicts lists conflicted paths relative to the docs root.
	Conflicts []string
}

// MergeTree runs `git merge-tree --write-tree` over three docs trees,
// wrapping them in synthetic parentless commits and labeling the sides
// sanho-ours / sanho-upstream (the merge contract). repoDir chooses where to run
// (clone for publication, app repo for sync); all three trees must be
// present in that repo's object database.
//
// The three trees participate as *content only*: each is wrapped in a
// parentless commit with the pinned synthetic identity above, so no app
// or canonical history takes part in the merge and the result depends on
// nothing but the trees.
//
// The temp refs are fixed names, because the merge contract fixes the marker labels.
// One ref store therefore supports one merge at a time — and, unlike
// what v0.2's first cut assumed, that is NOT guaranteed by the topology.
// Three real situations put two merges in one ref store at once:
//
//   - the pre-commit freshness preview merges in the private clone while
//     a concurrent `git push` publishes through it,
//   - two linked worktrees share both the clone and the app repo's
//     common ref store (the private-clone contract, F-H3),
//   - two `sanho sync` invocations in one checkout.
//
// Fixed ref names plus concurrency means one merge silently reading the
// other's inputs — a wrong tree published with exit 0. So the whole span
// from writing the refs to deleting them is serialized by an exclusive
// flock on <common-git-dir>/sanho-merge.lock, which is the same file for
// every worktree that shares the ref store. Recovery is part of the
// contract: a crash can leave the refs behind, so the first thing done
// under the lock is to delete any leftovers rather than fail on them.
//
// The lock lives inside the git directory, never in the worktree: a lock
// file next to the user's docs would be an untracked file sanho created
// in a repository it promises not to write to.
func MergeTree(ctx context.Context, repoDir string, baseTree, oursTree, theirsTree string) (MergeResult, error) {
	lockPath, err := mergeLockPath(ctx, repoDir)
	if err != nil {
		return MergeResult{}, err
	}

	var result MergeResult
	lockErr := fsx.WithFlock(ctx, lockPath, func() error {
		var err error
		result, err = mergeTreeLocked(ctx, repoDir, baseTree, oursTree, theirsTree)
		return err
	})
	if lockErr != nil {
		if errors.Is(lockErr, fsx.ErrLockTimeout) {
			return MergeResult{}, fmt.Errorf("%w: another sanho process holds %s", ErrMergeFailed, lockPath)
		}
		return MergeResult{}, lockErr
	}
	return result, nil
}

// mergeLockPath resolves the lock file for repoDir's ref store.
//
// `--git-common-dir` rather than `--git-dir` is what makes two linked
// worktrees contend on the same file: refs/sanho-ours is a common ref
// (git's per-worktree ref namespaces are HEAD, refs/bisect/*,
// refs/worktree/* and refs/rewritten/*), so the lock has to be common
// too. In the bare clone the two answer the same thing.
func mergeLockPath(ctx context.Context, repoDir string) (string, error) {
	common, err := gitx.New(repoDir).Line(ctx, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", fmt.Errorf("%w: resolve the git directory of %s: %v", ErrMergeFailed, repoDir, err)
	}
	if !filepath.IsAbs(common) {
		common = filepath.Join(repoDir, common)
	}
	return filepath.Join(filepath.Clean(common), mergeLockName), nil
}

// mergeTreeLocked is MergeTree's body, run while holding the lock.
func mergeTreeLocked(ctx context.Context, repoDir string, baseTree, oursTree, theirsTree string) (MergeResult, error) {
	run := gitx.New(repoDir)

	// Guarded recovery, not failure: a crashed or killed merge leaves the
	// temp refs behind, and refusing on them would wedge the workspace
	// permanently. Nothing else may hold them — the lock is exclusive and
	// these two names belong to this function alone.
	clearStaleMergeRefs(ctx, run)

	baseCommit, err := wrapTree(ctx, repoDir, baseTree)
	if err != nil {
		return MergeResult{}, err
	}
	oursCommit, err := wrapTree(ctx, repoDir, oursTree)
	if err != nil {
		return MergeResult{}, err
	}
	theirsCommit, err := wrapTree(ctx, repoDir, theirsTree)
	if err != nil {
		return MergeResult{}, err
	}

	if err := writeRef(ctx, run, refOurs, oursCommit); err != nil {
		return MergeResult{}, err
	}
	// Cleanup must outlive a cancelled context, otherwise an interrupted
	// merge leaves refs behind that a later merge would silently reuse.
	defer func() { _ = deleteRef(context.WithoutCancel(ctx), run, refOurs) }()

	if err := writeRef(ctx, run, refUpstream, theirsCommit); err != nil {
		return MergeResult{}, err
	}
	defer func() { _ = deleteRef(context.WithoutCancel(ctx), run, refUpstream) }()

	res, err := run.RunExit(ctx, "merge-tree", "-z", "--write-tree",
		"--merge-base="+baseCommit, labelOurs, labelUpstream)
	if err != nil {
		return MergeResult{}, fmt.Errorf("%w: merge-tree in %s: %v", ErrMergeFailed, repoDir, err)
	}

	switch res.ExitCode {
	case 0:
		tree, _ := parseMergeTree(res.Stdout)
		if tree == "" {
			return MergeResult{}, fmt.Errorf("%w: merge-tree in %s reported success without a tree", ErrMergeFailed, repoDir)
		}
		return MergeResult{Tree: tree, Clean: true}, nil
	case mergeTreeConflictExit:
		tree, conflicts := parseMergeTree(res.Stdout)
		if tree == "" {
			return MergeResult{}, fmt.Errorf("%w: merge-tree in %s reported conflicts without a tree", ErrMergeFailed, repoDir)
		}
		return MergeResult{Tree: tree, Clean: false, Conflicts: conflicts}, nil
	default:
		return MergeResult{}, fmt.Errorf("%w: merge-tree in %s: exit %d: %s",
			ErrMergeFailed, repoDir, res.ExitCode, firstMeaningfulLine(string(res.Stderr)))
	}
}

// clearStaleMergeRefs deletes leftovers from an interrupted merge. Both
// deletions are best-effort: an absent ref is the ordinary case, and a
// deletion that genuinely fails will resurface as a writeRef failure a
// few lines later, where it can be reported with its real context.
func clearStaleMergeRefs(ctx context.Context, run *gitx.Runner) {
	_ = deleteRef(ctx, run, refOurs)
	_ = deleteRef(ctx, run, refUpstream)
}

// wrapTree creates the parentless synthetic commit that carries tree
// into the merge.
func wrapTree(ctx context.Context, repoDir, tree string) (string, error) {
	run := gitx.New(repoDir, gitx.WithEnv(syntheticIdentity...))
	oid, err := run.Line(ctx, "commit-tree", tree, "-m", "sanho merge input")
	if err != nil {
		return "", fmt.Errorf("%w: wrap tree %s for merge in %s: %v", ErrMergeFailed, tree, repoDir, err)
	}
	return oid, nil
}

func writeRef(ctx context.Context, run *gitx.Runner, ref, oid string) error {
	if _, err := run.Run(ctx, "update-ref", ref, oid); err != nil {
		return fmt.Errorf("%w: point %s at %s: %v", ErrMergeFailed, ref, oid, err)
	}
	return nil
}

func deleteRef(ctx context.Context, run *gitx.Runner, ref string) error {
	if _, err := run.RunExit(ctx, "update-ref", "-d", ref); err != nil {
		return fmt.Errorf("canonical: delete %s: %w", ref, err)
	}
	return nil
}

// parseMergeTree reads `git merge-tree -z --write-tree` stdout, whose
// shape is (git-merge-tree(1), "OUTPUT"):
//
//	<result tree OID> NUL
//	(<mode> SP <object> SP <stage> HT <path> NUL)*   conflicted file info
//	NUL                                              section terminator
//	(<path count> NUL (<path> NUL)* <type> NUL <message> NUL)*
//
// A clean merge stops after the tree. -z is used rather than the default
// output because it removes path quoting entirely: a docs file with a
// space, a quote, or a newline in its name parses the same as any other.
//
// Conflicted paths come from the stage entries, which is the section git
// documents as machine-readable; the informational messages are only
// consulted when that section is empty, so a conflict class that reports
// itself solely in prose still names its files rather than reporting a
// conflicted merge with no paths.
func parseMergeTree(stdout []byte) (tree string, conflicts []string) {
	fields := splitNUL(stdout)
	if len(fields) == 0 {
		return "", nil
	}
	tree = strings.TrimSpace(fields[0])

	rest := fields[1:]
	end := len(rest)
	for i, field := range rest {
		if field == "" {
			end = i
			break
		}
	}

	seen := make(map[string]bool, end)
	for _, field := range rest[:end] {
		path, ok := stageEntryPath(field)
		if ok && !seen[path] {
			seen[path] = true
			conflicts = append(conflicts, path)
		}
	}
	if len(conflicts) > 0 {
		return tree, conflicts
	}

	if end < len(rest) {
		return tree, conflictPathsFromMessages(rest[end+1:])
	}
	return tree, nil
}

// stageEntryPath extracts the path from one conflicted-file-info entry,
// "<mode> <object> <stage>\t<path>".
func stageEntryPath(entry string) (string, bool) {
	_, path, ok := strings.Cut(entry, "\t")
	if !ok || path == "" {
		return "", false
	}
	return path, true
}

// conflictPathsFromMessages walks the informational-message section and
// collects the paths of CONFLICT-typed messages, preserving order and
// dropping duplicates. Anything it cannot parse ends the walk rather
// than being guessed at.
func conflictPathsFromMessages(fields []string) []string {
	var paths []string
	seen := make(map[string]bool)

	for i := 0; i < len(fields); {
		count, err := strconv.Atoi(strings.TrimSpace(fields[i]))
		if err != nil || count < 0 {
			// Trailing padding (git ends the stream with a NUL, which
			// splitting yields as an empty last field) is expected; any
			// other unparseable field means the format moved and
			// guessing would be worse than stopping.
			break
		}
		// count paths, then the message type, then the message itself.
		if i+1+count+2 > len(fields) {
			break
		}
		messagePaths := fields[i+1 : i+1+count]
		messageType := fields[i+1+count]
		i += count + 3

		if !strings.HasPrefix(messageType, "CONFLICT") {
			continue
		}
		for _, path := range messagePaths {
			if path != "" && !seen[path] {
				seen[path] = true
				paths = append(paths, path)
			}
		}
	}
	return paths
}

func splitNUL(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	return strings.Split(string(data), "\x00")
}
