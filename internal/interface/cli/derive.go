package cli

// Base re-derivation (sanho-v0.2.md §5.10) and the sync preview shared
// by the pre-commit warning and `sanho status` (§5.6 step 2).

import (
	"context"
	"strings"

	"github.com/irootkernel/sanho/internal/domain/provenance"
	"github.com/irootkernel/sanho/internal/infra/canonical"
	"github.com/irootkernel/sanho/internal/infra/gitx"
)

// deriveScanDepth bounds the history walk. The base is carried by the
// newest stamped commit, so a deep scan only matters for a workspace
// whose recent history is entirely docs-free; 500 commits is far past
// the point where an unstamped run means the trailers are simply not
// there.
const deriveScanDepth = 500

// commitRecordFormat emits, per commit, the OID, a newline, the raw
// message, and a NUL terminator. commitRecordSeparator is that
// terminator as the parser sees it.
//
// The format asks git for the NUL with its own `%x00` escape rather than
// embedding a literal one, because a literal NUL in an argv string is
// the C string terminator: exec would silently truncate the format there
// and every commit would run together with no separator at all.
const (
	commitRecordFormat    = "%H%n%B%x00"
	commitRecordSeparator = "\x00"
)

// deriveBase re-derives the base from the newest reachable stamped
// commit (§5.10): walk history, collect each commit's trailer values,
// and let domain/provenance pick.
//
// Both trailer keys are accepted. A legacy `docs-version: X` commit
// asserted that its docs tree *equaled* canonical X, which makes X a
// correct base for edits made on top of it — so mixed histories need no
// rewrite (§5.1 "legacy coexistence").
func deriveBase(ctx context.Context, workDir string) (provenance.Base, bool, error) {
	res, err := gitx.New(workDir).RunExit(ctx, "log",
		"--max-count="+itoa(deriveScanDepth),
		"--format="+commitRecordFormat, "HEAD")
	if err != nil {
		return provenance.Base{}, false, err
	}
	if res.ExitCode != 0 {
		// An unborn HEAD has no history to derive from, which is not a
		// failure — there is simply nothing to adopt.
		return provenance.Base{}, false, nil
	}
	base, ok := provenance.SelectBase(parseCommitTrailers(string(res.Stdout)))
	return base, ok, nil
}

// parseCommitTrailers turns the log output into one CommitTrailers per
// commit, newest first.
//
// The parse is deliberately simple: a trailer is a line that starts with
// one of the three keys followed by a colon. Git's own trailer rules are
// richer (block detection, folding, separators), but a stricter parser
// would reject trailers that a looser one accepts, and these values are
// a recovery source rather than a gate input (§5.1) — reading one that
// git would not call a trailer is harmless, while missing one that git
// would costs a recoverable base.
func parseCommitTrailers(out string) []provenance.CommitTrailers {
	var commits []provenance.CommitTrailers

	for _, record := range strings.Split(out, commitRecordSeparator) {
		record = strings.TrimLeft(record, "\n")
		if strings.TrimSpace(record) == "" {
			continue
		}
		header, body, _ := strings.Cut(record, "\n")

		commit := provenance.CommitTrailers{
			Commit: strings.TrimSpace(header),
			Values: map[string][]string{},
		}
		for _, line := range strings.Split(body, "\n") {
			line = strings.TrimRight(line, "\r")
			for _, key := range trailerKeys {
				value, ok := strings.CutPrefix(line, key+":")
				if !ok {
					continue
				}
				commit.Values[key] = append(commit.Values[key], strings.TrimSpace(value))
				break
			}
		}
		commits = append(commits, commit)
	}
	return commits
}

// trailerKeys are the keys the scan accepts, in priority order.
var trailerKeys = []string{
	provenance.TrailerBase,
	provenance.TrailerBaseTree,
	provenance.LegacyTrailerVersion,
}

// rederiveBaseAfterHeadMoved is the post-checkout / post-merge /
// post-rewrite body (§5.10). It reports the adopted base only when the
// base file actually moved, so the hooks stay silent in the ordinary
// case.
//
// Two states leave the base untouched, and both are spec rules rather
// than caution. History with no stamped commit has nothing to adopt.
// And a docs worktree that differs from HEAD's docs carries uncommitted
// edits that survived the checkout — the base answers "which canonical
// state do the *worktree* docs derive from" (§5.7 invariant), so a base
// derived from HEAD's history would describe content the worktree does
// not have. `sanho doctor` flags any resulting inconsistency.
func rederiveBaseAfterHeadMoved(ctx context.Context, ws *workspace) (provenance.Base, bool, error) {
	worktreeTree, err := ws.repo.WorktreeDocsTree(ctx)
	if err != nil {
		return provenance.Base{}, false, err
	}
	headTree, err := ws.repo.HeadDocsTree(ctx)
	if err != nil {
		return provenance.Base{}, false, err
	}
	if worktreeTree != headTree {
		return provenance.Base{}, false, nil
	}

	derived, found, err := deriveBase(ctx, ws.root)
	if err != nil || !found {
		return provenance.Base{}, false, err
	}

	state := ws.statePort()
	current, hasCurrent, err := state.LoadBase()
	if err != nil {
		// A corrupt base file is exactly what re-derivation repairs, so
		// an unreadable one is a reason to write, not to stop.
		hasCurrent = false
	}
	if hasCurrent && current.Commit == derived.Commit {
		// Same commit: keep what is recorded. A legacy `docs-version`
		// adoption carries no tree, and overwriting a recorded tree with
		// nothing would discard the rewrite anchor (D2) for no gain.
		return current, false, nil
	}
	if err := state.SaveBase(derived); err != nil {
		return provenance.Base{}, false, err
	}
	return derived, true, nil
}

// syncPreview is the clean/conflict prediction of §5.6 step 2, shared by
// the commit warning and `sanho status`.
type syncPreview struct {
	// Known is false when the prediction could not be computed; callers
	// then degrade to a behind-count-only message (§11 open question 3
	// sanctions exactly that shape).
	Known     bool
	Clean     bool
	Conflicts []string
}

// previewSync predicts what `sanho sync` would do, without touching the
// network, the app worktree, the index, or any app ref.
//
// It runs clone-side rather than app-side, which is the significant
// choice. The merge needs three trees in one object database; app-side
// would mean fetching canonical objects *into the application
// repository* on every commit, writing FETCH_HEAD and growing the user's
// object store as a side effect of a read-only check. Importing the app
// tip into sanho's own private clone instead keeps the churn entirely
// inside `.git/sanho/canonical`, which sanho owns and `sanho clean`
// deletes.
func previewSync(ctx context.Context, ws *workspace, store *canonical.Store, base provenance.Base, head, headTree string) syncPreview {
	oursTree, err := ws.repo.HeadDocsTree(ctx)
	if err != nil {
		return syncPreview{}
	}
	if oursTree == headTree {
		// Nothing to merge: the local docs already are canonical's.
		return syncPreview{Known: true, Clean: true}
	}

	port := ws.canonicalPort(store)
	if err := port.FetchFromApp(ctx, "HEAD"); err != nil {
		return syncPreview{}
	}
	_, conflicts, clean, err := port.MergeDocs(ctx, base.Commit, oursTree, head)
	if err != nil {
		return syncPreview{}
	}
	return syncPreview{Known: true, Clean: clean, Conflicts: conflicts}
}
