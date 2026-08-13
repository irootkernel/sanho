package cli

// The user-facing message catalog (docs/architecture.md "User guidance and exit codes").
//
// Every string sanho prints that names a next command lives here, as a
// constant or a single renderer. Nothing is assembled inline at a call
// site. That is a deliberate seed for P5's **guidance-closure suite**:
// D3 makes it normative that *every advised command must succeed in the
// state where it is advised*, and the testing contract requires a table that
// enumerates every (state, message) pair, parses the advised command out
// of the message, runs it, and asserts success — with an unlisted
// message failing the build. That test can only be written against a
// catalog it can enumerate, so new guidance must be added here rather
// than at the point of use.
//
// Hygiene rules this file enforces by construction (the guidance contract):
//
//   - English only (audit L4). The only non-ASCII characters are the em
//     dash and right arrow that appear verbatim in the guidance contract normative
//     templates.
//   - OIDs are shortened to 12 characters, via shortOID.
//   - Never a raw Go error chain at user level: causes are wrapped with
//     a cause line plus an action line.
//   - Degraded-mode lines always carry the data age.
//
// The three numbered templates below are reproduced from the guidance contract verbatim,
// with two documented substitutions: the docs directory is the
// workspace's configured one (the template's `docs/` is the default),
// and OIDs render at the guidance contract hygiene width of 12 rather than the
// template's illustrative 7.

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/irootkernel/sanho/internal/usecase/publish"
)

// shortOIDWidth is the guidance contract's "OIDs shortened to 12 chars".
const shortOIDWidth = 12

// shortOID renders an OID at the guidance contract width. An absent OID is named
// rather than printed as an empty gap.
func shortOID(oid string) string {
	if oid == "" {
		return "(none)"
	}
	if len(oid) <= shortOIDWidth {
		return oid
	}
	return oid[:shortOIDWidth]
}

// --- Fixed strings ----------------------------------------------------

const (
	// msgMigrateRequired is the legacy-workspace contract pre-migration degradation line. It is
	// the single hint a v1 workspace gets from every entry point, and the
	// only command that succeeds in that state.
	msgMigrateRequired = "sanho: this workspace uses the v0.1 layout; run 'sanho migrate'"

	// msgNotInWorkspace is printed when the current directory is not a
	// managed workspace. `sanho init` is what makes it one.
	msgNotInWorkspace = "sanho: not a sanho workspace (no .sanho.json here); run 'sanho init' to create one"

	// msgSyncInProgressPush rejects a push while a conflicted sync still
	// has markers in the docs worktree (the publication contract step 2). It names the whole
	// sequence, because the commit is no longer the last step: a sync
	// ends when 'sanho sync --continue' records it.
	msgSyncInProgressPush = "sanho: finish the sync first: resolve the conflicts, 'git add' and 'git commit', " +
		"then 'sanho sync --continue' (or 'sanho sync --abort' to undo it)"

	// msgMarkersBeforePush follows the marker list on a rejected push.
	msgMarkersBeforePush = "resolve the markers before pushing"

	// msgCanonicalUnreachableAction is the action line paired with the
	// canonical-unreachable cause line (the guidance contract).
	msgCanonicalUnreachableAction = "Check network access to the docs repository, then push again."

	// msgPushRejectedTrailer is git's own idiom, and the last line of the
	// the guidance contract push-rejection template.
	msgPushRejectedTrailer = "error: push rejected — no remote ref was changed"

	// msgCleanNeedsConfirmation guards the destructive path of
	// `sanho clean`; --dry-run is the read-only alternative (audit M4).
	msgCleanNeedsConfirmation = "sanho: 'sanho clean' removes this workspace's sanho state; rerun with -y to confirm, or 'sanho clean --dry-run' to preview"

	// msgCleanSyncInProgress refuses to clean while a sync is owed, and
	// is reused by `sanho init` for the same state (a re-init would write
	// a base the unfinished sync is holding still). Both named commands
	// cannot fail once their preconditions hold (the synchronization contract and 7).
	msgCleanSyncInProgress = "sanho: a conflicted sync is in progress; complete it with 'sanho sync --continue', " +
		"or undo it with 'sanho sync --abort' first"

	// msgInitNoProvenance refuses reuse mode when existing docs carry no
	// provenance to derive a base from (audit L4: English only).
	msgInitNoProvenance = "existing docs directory has no docs-base/docs-version commits; " +
		"commit docs through sanho first or rerun with --force to replace the directory"

	// msgInitForceNeedsConfirmation guards --force, which discards the
	// existing docs directory.
	msgInitForceNeedsConfirmation = "sanho: --force replaces the existing docs directory with canonical content; rerun with -y to confirm"

	// msgInitCanonicalEmpty is the bootstrap notice: nothing has been
	// published yet, so there is no base to record (the publication contract).
	msgInitCanonicalEmpty = "sanho: canonical repository is empty; your first push will publish docs"

	// msgAlreadyMigrated is `sanho migrate` on a workspace that is
	// already v2 — an idempotent success, not a failure (the legacy-workspace contract).
	msgAlreadyMigrated = "sanho: already migrated"

	// msgMigrateBlockedByTransaction refuses migration while v0.1 state
	// exists that v0.2 will not interpret (the legacy-workspace contract step 1).
	msgMigrateBlockedByTransaction = "sanho: a v0.1 pull-commit transaction or pending-fix state is still present; " +
		"finish or abort it with the v0.1 binary, then run 'sanho migrate' again"

	// msgMigrateNeedsURL is raised when the legacy daemon state carries
	// no docs repository URL for this project.
	msgMigrateNeedsURL = "sanho: the docs repository URL is not recorded in the legacy state; rerun with --docs-repo-url <url>"
)

// daemonStopInstructions is the legacy-workspace contract step 2, printed verbatim and never
// executed: service ownership was explicitly the user's in v0.1, so
// migrate names the command and stops there.
var daemonStopInstructions = []string{
	"sanho: the v0.1 daemon is no longer used. Stop and unload it yourself:",
	"  macOS (launchd):  launchctl bootout gui/$(id -u)/xyz.rootkernel.sanho",
	"  Linux (systemd):  systemctl --user disable --now sanhod",
	"The 'sanhod' binary can be deleted at your leisure; nothing references it.",
}

// --- Template 1: commit warning (the guidance contract, the commit-hook contract) --------------------------

// commitBehindClean is the guidance contract line, verbatim.
func commitBehindClean(behind int) string {
	return fmt.Sprintf("sanho: docs base is %d commits behind — 'sanho sync' will merge cleanly", behind)
}

// commitBehindConflicts is the commit-hook contract conflict-prediction variant. It says
// what happens if the warning is ignored, which the guidance contract requires whenever
// there is something to say.
func commitBehindConflicts(behind int, files []string) string {
	return fmt.Sprintf("sanho: docs base is %d commits behind — 'sanho sync' will report conflicts in %s; syncing sooner keeps them small",
		behind, strings.Join(files, ", "))
}

// commitBehindUnknown degrades to the behind count alone when the
// clean/conflict prediction could not be computed; the guidance contract
// permits this shape. It still names the command that fixes the
// state, so guidance closure holds.
func commitBehindUnknown(behind int) string {
	return fmt.Sprintf("sanho: docs base is %d commits behind — run 'sanho sync' to reconcile", behind)
}

// staleDataSuffix is appended to a freshness line when the last fetch is
// older than staleDataThreshold: degraded-mode lines always include the
// data age (the guidance contract).
func staleDataSuffix(age time.Duration) string {
	return fmt.Sprintf(" (canonical last checked %s ago)", humanizeAge(age))
}

// staleDataThreshold is the commit-hook contract's 24-hour rule.
const staleDataThreshold = 24 * time.Hour

// humanizeAge renders a fetch age at one significant unit — the
// precision a human acts on. Anything under a minute is "less than a
// minute", because "0 minutes" reads as an error rather than as fresh.
func humanizeAge(age time.Duration) string {
	switch {
	case age < time.Minute:
		return "less than a minute"
	case age < time.Hour:
		return plural(int(age.Minutes()), "minute")
	case age < 24*time.Hour:
		return plural(int(age.Hours()), "hour")
	default:
		return plural(int(age.Hours()/24), "day")
	}
}

func plural(n int, unit string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, unit)
	}
	return fmt.Sprintf("%d %ss", n, unit)
}

// --- Template 2: sync conflict (the guidance contract) ---------------------------------

// syncConflictMessage renders the guidance contract template 2. docsDir is the
// workspace's configured docs directory, which for the default "docs"
// reproduces the template's paths character for character.
//
// The template gains one line, and it is the whole of this wave at user
// level: resolving is ordinary git work, and *completing* the sync is an
// act of its own. The two commands are ordered — the commit first, then
// the completion — and the closure fixture runs them in that order.
// The three next-step lines are spelled out here and again in
// unresolvedSyncMessage rather than shared through a helper. The catalog
// gate reads this file's declarations and requires an entry for each one
// that names a command; guidance hidden behind a helper is guidance no
// closure fixture is forced to exist for. Two renderers, two entries,
// two fixtures — and the duplication is what the messages_test.go
// comparison of the two renderings keeps honest.
func syncConflictMessage(docsDir string, conflicts []string) string {
	var b strings.Builder
	writef(&b, "sanho: merged docs with upstream — %d files have conflicts:\n", len(conflicts))
	for _, path := range conflicts {
		writef(&b, "  %s\n", path)
	}
	writef(&b, "Resolve the markers, then:  git add %s/ && git commit\n", docsDir)
	b.WriteString("Then complete the sync:     sanho sync --continue\n")
	b.WriteString("To undo this sync:          sanho sync --abort")
	return b.String()
}

// --- Template 3: push rejection (the guidance contract) --------------------------------

// pushConflictMessage renders the guidance contract template 3 for the case -conflict
// rejection, with the conflicted files listed between the state line and
// the action line.
//
// The list is the template's one addition, and it is information the
// rejection already had: the merge that failed reported exactly which
// paths conflicted, and dropping them left the user to rediscover by
// running the very `sanho sync` the next line advises. The indentation
// matches templates 2 and the marker listing, so all three rejections
// read the same way. A rejection with no file-level detail (the CAS
// budget, say) renders the template unchanged.
func pushConflictMessage(base, head string, files []string) string {
	var b strings.Builder
	writef(&b, "sanho: your docs changes conflict with upstream (base %s → %s)\n",
		shortOID(base), shortOID(head))
	for _, path := range files {
		writef(&b, "  %s\n", path)
	}
	writef(&b, "Run 'sanho sync', resolve, commit, then push again.\n%s", msgPushRejectedTrailer)
	return b.String()
}

// pushSyncRequiredMessage covers the remaining sync-required reasons.
// Most route to `sanho sync`; the uncorroborated base==head state routes
// to a provenance restamp because sync is already a no-op there. Each
// branch names an action that succeeds in its printed state (D3).
func pushSyncRequiredMessage(reason, base, head string) string {
	if reason == publish.ReasonUncorroboratedBase && base != "" && base == head {
		return fmt.Sprintf("sanho: docs provenance does not corroborate canonical head %s\n"+
			"Make a docs change, then run 'git add docs/ && git commit' to restamp provenance, followed by 'git push'.\n"+
			"%s", shortOID(head), msgPushRejectedTrailer)
	}
	// A workspace with no base has nothing to render on the left of the
	// arrow, and "base (none) → 9a41f2c0e1d2" reads as a transition from
	// a state rather than as the absence of one. Name the head instead.
	state := fmt.Sprintf("base %s → %s", shortOID(base), shortOID(head))
	if base == "" {
		state = fmt.Sprintf("canonical head %s", shortOID(head))
	}
	return fmt.Sprintf("sanho: docs must be reconciled before publishing (%s; %s)\n"+
		"Run 'sanho sync', resolve if needed, commit, then push again.\n"+
		"%s", reason, state, msgPushRejectedTrailer)
}

// pushMarkersMessage rejects a push whose docs carry committed conflict
// markers (the publication contract step 3).
func pushMarkersMessage(paths []string) string {
	var b strings.Builder
	b.WriteString("sanho: pushed docs still contain conflict markers:\n")
	for _, path := range paths {
		writef(&b, "  %s\n", path)
	}
	writef(&b, "%s\n%s", msgMarkersBeforePush, msgPushRejectedTrailer)
	return b.String()
}

// pushUnreachableMessage is the guidance contract canonical-unreachable pair: a cause
// line naming the URL and the failure, then an action line. The raw git
// error never reaches the user unwrapped.
func pushUnreachableMessage(url, cause string) string {
	return fmt.Sprintf("sanho: canonical repository unreachable (%s): %s\n%s\n%s",
		url, cause, msgCanonicalUnreachableAction, msgPushRejectedTrailer)
}

// pushRewrittenMessage is the publication contract. When a commit carrying the
// recorded docs-base-tree still exists in canonical, it is named, so the
// advised command is runnable as printed. When none does, no command can
// succeed, so the message says "manual intervention required" and gives
// the diagnostics needed to choose a target — never a command that will
// fail (D3).
//
// branch is the publication branch, and naming it is load-bearing. The
// private clone is `git init --bare` plus a fetch (the private-clone contract), so it holds
// `refs/remotes/origin/<branch>` and no `refs/remotes/origin/HEAD` at
// all: a listing command naming HEAD exits 128 in the very state that
// prints it, which is exactly the closure failure D3 forbids.
func pushRewrittenMessage(base, anchor, cloneDir, branch string) string {
	if anchor != "" {
		return fmt.Sprintf("sanho: canonical history was rewritten; base %s is no longer reachable\n"+
			"Run 'sanho sync --rebase-onto %s', resolve if needed, commit, then push again.\n"+
			"%s", shortOID(base), anchor, msgPushRejectedTrailer)
	}
	return fmt.Sprintf("sanho: canonical history was rewritten; base %s is no longer reachable\n"+
		"manual intervention required: no canonical commit carries this workspace's docs base tree.\n"+
		"List the candidates with:  git -C %s log --oneline refs/remotes/origin/%s\n"+
		"Then run:                  sanho sync --rebase-onto <commit>\n"+
		"%s", shortOID(base), cloneDir, branch, msgPushRejectedTrailer)
}

// pushEmptyDocsMessage is the publication contract empty-publication refusal (F-H2).
//
// The state is genuinely ambiguous — a branch created before the docs
// directory existed and a branch where `git rm -r docs` was the point
// look identical from the push boundary — so the message states what
// would happen, names the branch, and offers both readings.
//
// The escape hatch is shown as a ONE-COMMAND PREFIX rather than as a
// variable to set. It reads from the process environment, so an
// `export` disarms the refusal for every push in that shell until the
// user remembers to unset it — which is not "for that one push", the
// promise the previous wording made. The prefix form is genuinely
// single-use, and it is the shape the sentence can keep.
func pushEmptyDocsMessage(branch, head string, docsCount int) string {
	scope := "every canonical document"
	if docsCount > 0 {
		scope = fmt.Sprintf("all %d canonical documents", docsCount)
	}
	return fmt.Sprintf("sanho: branch %s carries no docs; publishing it would delete %s (canonical head %s)\n"+
		"If that is not what you meant, push a docs-bearing branch, or run 'sanho sync' on this one first.\n"+
		"If it is, prefix the one push:  SANHO_ALLOW_DOCS_DELETION=1 git push\n"+
		"%s", branch, scope, shortOID(head), msgPushRejectedTrailer)
}

// pushMergeFailedMessage covers a the merge contract merge that could not run at all
// — a locked ref store, a broken clone — as opposed to one that ran and
// found conflicts (F-C2).
//
// It names no command deliberately. Every merge failure this can reach
// is environmental, and D3 forbids printing a command that would fail
// where it is printed; the clone directory is the fact the user needs.
func pushMergeFailedMessage(cloneDir, cause string) string {
	return fmt.Sprintf("sanho: could not merge docs with the canonical repository: %s\n"+
		"Inspect the canonical clone at %s, then push again.\n"+
		"%s", cause, cloneDir, msgPushRejectedTrailer)
}

// pushPartialPublicationLine precedes a rejection template when part of
// a multi-ref push already landed.
//
// Evaluate-then-publish (F-C1) makes this rare — every tip is validated
// before the first write — but a transport failure between two
// publications of one push is still possible, and the template's "no
// remote ref was changed" must not be the only thing the user reads
// when canonical did move.
func pushPartialPublicationLine(oids []string) string {
	short := make([]string, 0, len(oids))
	for _, oid := range oids {
		short = append(short, shortOID(oid))
	}
	return fmt.Sprintf("sanho: %d of this push's publications already landed in canonical (%s)",
		len(oids), strings.Join(short, ", "))
}

// pushPublishedMessage reports a successful publication.
func pushPublishedMessage(newHead, decidedCase string) string {
	return fmt.Sprintf("sanho: published docs %s (%s)", shortOID(newHead), decidedCase)
}

// --- Sync, pull, and base movement ------------------------------------

// syncedMessage reports a clean reconcile.
func syncedMessage(target, commit string) string {
	if commit == "" {
		return fmt.Sprintf("sanho: docs base advanced to %s (docs unchanged)", shortOID(target))
	}
	return fmt.Sprintf("sanho: synced docs to %s (commit %s)", shortOID(target), shortOID(commit))
}

// upToDateMessage reports that there was nothing to do. An empty target
// is the empty-canonical state, which has no OID to name.
func upToDateMessage(target string) string {
	if target == "" {
		return "sanho: canonical repository has no commits yet; nothing to sync"
	}
	return fmt.Sprintf("sanho: docs are up to date with %s", shortOID(target))
}

// pulledMessage reports a fast-forward consume.
func pulledMessage(target, commit string) string {
	if commit == "" {
		return fmt.Sprintf("sanho: pulled docs to %s", shortOID(target))
	}
	return fmt.Sprintf("sanho: pulled docs to %s (commit %s)", shortOID(target), shortOID(commit))
}

// syncNotCommittedMessage covers a conflicted sync whose markers are
// gone from the worktree without any commit having gone near the paths
// it conflicted on — `git stash push -- docs`, `git checkout HEAD --
// docs`, a revert, or a commit that changed some other document.
//
// The state looks finished and is not, which is why it gets its own
// wording rather than the in-progress one: there are no markers left to
// "resolve and commit", so template 2's advice would name a `git commit`
// with nothing to commit.
//
// It states the criterion it actually applied, and only where that
// criterion is knowable: a note that never recorded what the merge
// conflicted on gets syncNeedsContinueMessage instead, which asserts
// nothing about commits. Saying "no commit has changed the files it
// conflicted on" about a note that does not list those files was a
// reason nothing knew to be true.
//
// The recommended route is ordered — abort first (the stash is untouched
// by it), then sync again to lay the same conflicts out — and the second
// sentence names the other legitimate reading: a resolution can be
// "keep every one of my lines", which leaves no trace whatsoever, and
// `sanho sync --continue` is how a user declares it.
//
// It is printed by `pre-commit`, which does NOT block on it, and by
// `pre-push`, which does. The split follows P2 and the commit-hook contract: the commit path
// blocks only for markers, and a sync put aside is no reason to stop
// unrelated work — while the push boundary is where local work becomes
// shared.
func syncNotCommittedMessage(prev, target string) string {
	return fmt.Sprintf("sanho: the sync from %s to %s is not completed; no commit has changed the files it conflicted on\n"+
		"Run 'sanho sync --abort' to undo it — anything you stashed stays in your stash — then 'sanho sync' to lay the conflicts out again.\n"+
		"If the docs already read the way you want them, run 'sanho sync --continue' instead to complete the sync as it stands.",
		shortOID(prev), shortOID(target))
}

// syncNeedsContinueMessage covers the two unfinished states in which
// nothing is wrong: the resolution is being made, or it is made and
// committed and simply not recorded yet.
//
// Both renderings say "is not completed", which is the fact, and both
// name the same two exits. What differs is the step in between: a
// committed resolution needs only the completion, while an uncommitted
// one needs the commit first — so the second rendering names it, and
// says what the abort would do to work that is not committed. That
// second clause is the one an earlier version left unsaid, and abort's
// `git checkout HEAD -- docs` is not obviously reversible to a reader.
// The first rendering is equally careful in the other direction: once
// the resolution is committed, abort restores docs from a HEAD that
// already contains it, so what abort takes back is the sync's base
// record, not the user's commits — saying "undo the whole sync" there
// promised a revert abort does not perform.
func syncNeedsContinueMessage(prev, target string, committed bool) string {
	if committed {
		return fmt.Sprintf("sanho: the sync from %s to %s is not completed — the resolution is committed, and only 'sanho sync --continue' records it\n"+
			"Run 'sanho sync --continue' now, or 'sanho sync --abort' to forget the sync — your commits stay; only the recorded base returns to its pre-sync value.",
			shortOID(prev), shortOID(target))
	}
	return fmt.Sprintf("sanho: the sync from %s to %s is not completed — no resolution has been committed yet\n"+
		"Commit your resolution, then run 'sanho sync --continue'.\n"+
		"Or run 'sanho sync --abort' to undo the sync, which restores docs/ from HEAD and discards anything you have not committed.",
		shortOID(prev), shortOID(target))
}

// syncContinueBlockedMessage refuses `sanho sync --continue` when the
// sync is not in a state that can be completed: markers are still in the
// worktree, or the resolution has been edited and not committed.
//
// One renderer for both, because the remedy is one sequence and the
// detail says which part of it is outstanding. The abort is named too:
// a user who reaches this line may have decided the reconciliation is
// not worth finishing.
func syncContinueBlockedMessage(detail string) string {
	return fmt.Sprintf("sanho: the sync is not ready to be completed (%s)\n"+
		"Finish the resolution with 'git add docs/ && git commit', then run 'sanho sync --continue' again.\n"+
		"Or run 'sanho sync --abort' to undo the sync.", detail)
}

// syncNoteCorruptMessage covers a sync note that is present and
// unreadable.
//
// It names the abort and nothing else, because the abort is the one
// operation that needs only the note's *existence*: it restores the docs
// from HEAD, clears the base it can no longer vouch for, and deletes the
// file. The second line says so, because a workspace with no recorded
// base is a state the user will meet at the next push — where the
// rejection names `sanho sync`, which establishes one.
func syncNoteCorruptMessage(detail string) string {
	return fmt.Sprintf("sanho: the record of the sync in progress is unreadable (%s)\n"+
		"Run 'sanho sync --abort' to restore the docs from HEAD, forget the docs base it cannot vouch for, and clear it.", detail)
}

// syncAbortedMessage reports a completed `sanho sync --abort`.
//
// Untracked docs files are named when any remain (F-L8). Abort restores
// the docs worktree to HEAD, which cannot remove a file HEAD never had
// — one the user created while resolving. Left unsaid, the next
// `sanho sync` refuses with ErrDocsDirty for a reason the user has no
// way to connect to the abort they just ran.
func syncAbortedMessage(untracked []string) string {
	line := "sanho: sync aborted; docs restored to HEAD"
	if len(untracked) == 0 {
		return line
	}
	return line + fmt.Sprintf("\nuntracked files you created remain: %s", strings.Join(untracked, ", "))
}

// baseRederivedMessage is the one line a post-checkout/merge/rewrite
// hook prints, and only when the base actually moved (the hook contract).
func baseRederivedMessage(base string) string {
	return fmt.Sprintf("sanho: docs base re-derived as %s after HEAD moved", shortOID(base))
}

// baseClearedMessage is the line a HEAD-moved hook prints when it
// removed a base the new HEAD cannot account for.
//
// It has to be said out loud. Losing the base is not a failure — the
// next `sanho sync` establishes one, merging against the empty tree —
// but it changes what the next push does, and a workspace that silently
// stopped having a base would meet that at the push boundary with no
// idea when it happened. The alternative was worse and is what this
// wave closes: carrying a base belonging to another branch across a
// checkout, and publishing this branch's documents over canonical as a
// fast-forward.
func baseClearedMessage() string {
	return "sanho: this branch carries no docs provenance, so the docs base was cleared — run 'sanho sync' to establish one"
}

// baseNotAdvancedMessage follows a SUCCESSFUL publication whose local
// base pointer could not be moved (the publication contract step 6, M2).
//
// It names no command, for the same reason pushMergeFailedMessage names
// none: everything that can reach it is environmental — an unreadable
// docs directory, a workspace root that cannot be written — and D3
// forbids printing a command that would fail where it is printed. Every
// command that repairs a stale base needs the same filesystem the write
// just failed on.
//
// Nor does the state need guidance. The publication stands; what did not
// move is a local pointer, and the next `sanho sync` records it as a
// matter of course (the worktree already holds what was published, so
// the merge is clean and only the base moves). `sanho status` and
// `sanho doctor` report the staleness with their own guidance until then.
func baseNotAdvancedMessage(cause string) string {
	return fmt.Sprintf("sanho: published, but the docs base was not advanced (%s)", cause)
}

// syncCompletedMessage reports a completed `sanho sync --continue`: the
// note is gone and the base names the state the docs now derive from.
//
// drift is how many docs paths differ between the merge result and what
// was actually completed. It is reported, never refused: completing a
// sync whose clean half was reverted along with its conflicts is the
// legitimate "keep my own lines" reading, and it also silently drops
// upstream content the user was never shown a conflict for. Saying so is
// the difference between a decision and an accident.
func syncCompletedMessage(base string, drift int) string {
	line := fmt.Sprintf("sanho: sync completed; docs base is now %s", shortOID(base))
	if drift == 0 {
		return line
	}
	return line + fmt.Sprintf("\n%s differ from the merge result and were completed as they stand.",
		plural(drift, "file"))
}

// syncContinueForeignHistoryMessage refuses `sanho sync --continue` from
// history the sync never stood on (C1).
//
// The abort is the one route out, and it works from anywhere: it
// restores docs from HEAD, puts the base back where the sync found it,
// and deletes the note — none of which depends on which branch is
// checked out. Naming the branch switch explicitly is the point, because
// from inside the state nothing looks wrong: no markers, clean docs, a
// note that still names a target.
func syncContinueForeignHistoryMessage(detail string) string {
	return fmt.Sprintf("sanho: this sync cannot be completed here (%s)\n"+
		"Completing it would record a docs base for documents that never took part in the merge.\n"+
		"Run 'sanho sync --abort' to undo the sync, then run 'sanho sync' again where you want to reconcile.", detail)
}

// stagedMarkersMessage blocks a commit whose staged docs still carry
// conflict markers (the commit-hook contract step 1).
func stagedMarkersMessage(paths []string) string {
	var b strings.Builder
	b.WriteString("sanho: staged docs contain conflict markers:\n")
	for _, path := range paths {
		writef(&b, "  %s\n", path)
	}
	b.WriteString("Resolve them, then 'git add' the files and commit again.")
	return b.String()
}

// unresolvedSyncMessage blocks a commit made while a sync is still
// unresolved, in the shape of template 2: the same next steps, for the
// same state.
func unresolvedSyncMessage(docsDir string, paths []string) string {
	var b strings.Builder
	writef(&b, "sanho: a sync is in progress — %d files still have conflicts:\n", len(paths))
	for _, path := range paths {
		writef(&b, "  %s\n", path)
	}
	writef(&b, "Resolve the markers, then:  git add %s/ && git commit\n", docsDir)
	b.WriteString("Then complete the sync:     sanho sync --continue\n")
	b.WriteString("To undo this sync:          sanho sync --abort")
	return b.String()
}

// commitMsgStampWarning is the one-line, never-blocking warning
// `commit-msg` prints when it cannot stamp (the provenance contract).
func commitMsgStampWarning(cause string) string {
	return fmt.Sprintf("sanho: docs provenance not stamped (%s); run 'sanho doctor --fix' to restore it", cause)
}

// --- Sync and pull refusals (the synchronization contract, the guidance contract) ------------------------------
//
// The use cases raise command-free sentinels; the guidance is composed
// here, once, so the closure gate can enumerate it. Before F-H6 these
// strings lived in usecase/docsync, where nothing could prove they named
// a command that works.

// syncInProgressMessage refuses a sync or pull while an earlier one is
// still unfinished. The next steps are the ones the guidance contract template 2 named
// when the conflict was created, in the same order.
func syncInProgressMessage(detail string) string {
	return fmt.Sprintf("sanho: a conflicted sync is in progress (%s)\n"+
		"Resolve the markers and commit, then run 'sanho sync --continue' — or 'sanho sync --abort' to undo it.", detail)
}

// pullNeedsSyncMessage refuses a fast-forward-only pull.
//
// The advice is deliberately two-step. `sanho sync` requires clean docs
// itself, so "run 'sanho sync'" alone would name a command that fails in
// the very state it is printed in; committing or stashing first is what
// makes it succeed (D3, and the closure fixture performs both halves).
func pullNeedsSyncMessage(detail string) string {
	return fmt.Sprintf("sanho: 'pull' can only fast-forward, and these docs have moved on (%s)\n"+
		"Commit or stash your docs changes, then run 'sanho sync' to reconcile them.", detail)
}

// syncUnknownBaseMessage covers a recorded base canonical no longer
// knows, with no docs-base-tree anchor to recover it (the synchronization contract).
func syncUnknownBaseMessage(detail string) string {
	return fmt.Sprintf("sanho: the recorded docs base is not in canonical history (%s)\n"+
		"Pick a canonical commit and run 'sanho sync --rebase-onto <commit>'.", detail)
}

// syncUnreachableMessage is the guidance contract cause/action pair for sync and pull
// (F-M3): the same two-line shape the push path uses, so an offline
// workspace reads the same whichever verb hit the network.
func syncUnreachableMessage(url, cause string) string {
	return fmt.Sprintf("sanho: canonical repository unreachable (%s): %s\n"+
		"Check network access to the docs repository, then run 'sanho sync' again.", url, cause)
}

// rebaseOntoHealthyMessage refuses `--rebase-onto` against an ancestor
// of a perfectly reachable base (F-M4).
//
// The flag exists for rewritten history. Pointing it at an older commit
// on healthy history asks sanho to adopt a past state as the base, which
// would make the next push "merge" content the user never reverted —
// so it is refused and the ordinary route is described in prose (edit,
// commit) rather than named as a command, because no single command does
// it.
func rebaseOntoHealthyMessage(base, target string) string {
	return fmt.Sprintf("sanho: --rebase-onto is for recovering from rewritten history, and base %s is healthy: %s already precedes it\n"+
		"To adopt an older state, edit the docs and commit the change. Run 'sanho status' to see where the base stands.",
		shortOID(base), shortOID(target))
}

// syncMergeFailedMessage is pushMergeFailedMessage without the push
// trailer: the same environmental failure, met from a command the user
// typed rather than from a hook (F-C2).
func syncMergeFailedMessage(cloneDir, cause string) string {
	return fmt.Sprintf("sanho: could not merge docs with the canonical repository: %s\n"+
		"Inspect the canonical clone at %s and try again.", cause, cloneDir)
}

// docsTooLargeMessage renders the merge contract scan-size refusal at user level.
//
// The detector reads whole files, so a file past markers.MaxScanSize is
// refused rather than silently skipped — but the raw error is an infra
// diagnostic ("appgit: docs/big.md is 11534336 bytes: content too
// large…"), and the guidance contract does not let that reach a user (F-M3). No command
// is named because none fixes it: the file has to move or shrink.
func docsTooLargeMessage(cause string) string {
	return fmt.Sprintf("sanho: %s\n"+
		"Conflict-marker scanning has a size limit; move the file out of the docs directory, or split it, then try again.", cause)
}

// --- Status and doctor ------------------------------------------------

// cloneMissingMessage covers the one repair status, check, and doctor
// need to name: the workspace-private clone is gone.
//
// It advises `sanho sync`, not `sanho init` (F-H6b). Sync is a write
// path and therefore opens the clone with Ensure, which recreates and
// fetches it; `sanho init` refuses outright in an initialized workspace,
// so the old advice named a command that could not work where it was
// printed.
func cloneMissingMessage(cloneDir string) string {
	return fmt.Sprintf("the canonical clone is missing (%s) — run 'sanho sync' to recreate it", cloneDir)
}

// doctorHooksMessage reports hook problems and names the repair.
//
// `sanho doctor --fix` reinstalls, which is non-destructive: the
// installer matches whole lines and preserves every foreign one, so a
// hook file the user also owns comes out with their content intact
// (F-H6b). The old advice, `sanho init --force`, replaced the docs
// directory — a destructive answer to a cosmetic problem.
func doctorHooksMessage(problems string) string {
	return fmt.Sprintf("%s — run 'sanho doctor --fix' to reinstall them", problems)
}

func doctorPublicationPendingMessage() string {
	return "committed docs differ from the publication base after hook repair; make another docs-changing commit, then run 'git push' to publish them"
}

// customHooksPathMessage explains why lifecycle and repair commands do
// not write hooks into a path whose ownership may be shared or tracked.
// It deliberately gives a condition rather than a command: the right
// Git configuration scope is a user decision.
func customHooksPathMessage(path string) string {
	return fmt.Sprintf("custom core.hooksPath %q requires explicit management; repository-local paths and recognized Husky 9 layouts can be initialized or migrated with --manage-custom-hooks", path)
}

// syncNotePendingMessage is the unfinished-sync line `sanho status` and
// `sanho doctor` both print, and the one `doctor --fix` prints when it
// declines to re-derive a base a sync is holding still.
//
// It names the two commands that end a sync and no third one. The
// previous wording ("resolve the markers and commit") described a step
// rather than an exit: doing it leaves the sync exactly as unfinished as
// before, which is the state this line is reporting.
func syncNotePendingMessage(detail string) string {
	return fmt.Sprintf("%s — complete it with 'sanho sync --continue', or undo it with 'sanho sync --abort'", detail)
}

// baseNeedsSyncMessage covers every state in which no base can be
// derived locally: doctor's failed --fix, and a migrated workspace whose
// v0.1 layout recorded none. `sanho sync` establishes one from canonical
// and succeeds in both.
func baseNeedsSyncMessage(detail string) string {
	return fmt.Sprintf("%s — run 'sanho sync' to establish one from canonical", detail)
}

// baseUnknownToCanonicalMessage covers a base that exists locally but
// which canonical does not recognize, as `sanho init` (reuse mode) and
// `sanho migrate` both find it.
//
// It names `sanho status` and nothing more. `sanho sync` is the tempting
// advice and the wrong one: with the base unreachable and no canonical
// commit carrying its docs tree, sync refuses and points at
// `--rebase-onto <commit>`, so advising it here would hand the user a
// command that fails. Status reports the state in every case.
func baseUnknownToCanonicalMessage(base string) string {
	return fmt.Sprintf("sanho: the docs base %s is not in the canonical repository; canonical history may have been rewritten. Run 'sanho status' to see where things stand.",
		shortOID(base))
}

// statusBehindLine is the `sanho status` sync row when the base is
// behind. The three renderings mirror the three commit-warning variants
// of the guidance contract template 1, and all of them quote 'sanho sync'.
func statusBehindLine(behind int, known, clean bool, conflicts []string) string {
	switch {
	case !known:
		return fmt.Sprintf("%d behind — run 'sanho sync' to reconcile", behind)
	case clean:
		return fmt.Sprintf("%d behind — 'sanho sync' will merge cleanly", behind)
	default:
		return fmt.Sprintf("%d behind — 'sanho sync' will report conflicts in %s",
			behind, strings.Join(conflicts, ", "))
	}
}

// --- Lifecycle --------------------------------------------------------

// initNextStepsMessage is the last thing `sanho init` prints (F-L5).
//
// Init leaves two things for the user to commit: the ignore entries it
// appended, and — in fresh mode — canonical's docs staged in the index.
// One command lands both.
func initNextStepsMessage(staged bool) string {
	if staged {
		return "\nCanonical docs are staged, and the sanho state files were added to .gitignore.\n" +
			"Commit both:  git add .gitignore && git commit"
	}
	return "\nThe sanho state files were added to .gitignore.\n" +
		"Commit it:  git add .gitignore && git commit"
}

// projectHasWorkspacesMessage refuses `sanho project delete` while
// checkouts still reference the project (the JSON contract).
func projectHasWorkspacesMessage(project string, count int, example string) string {
	return fmt.Sprintf("project %q still has %d registered workspace(s) (%s); run 'sanho clean' in them, or rerun with --force",
		project, count, example)
}

// staleCanonicalLine is the private-clone contract degraded-read line: cached results
// always say how old they are and how to refresh.
func staleCanonicalLine(age time.Duration) string {
	return fmt.Sprintf("canonical data is %s old — run 'sanho status --refresh'", humanizeAge(age))
}

// neverFetchedLine covers a clone that has never fetched.
const neverFetchedLine = "canonical has never been fetched — run 'sanho status --refresh'"

// doctorFixHint names the repair for a base file doctor found wanting.
const doctorFixHint = "run 'sanho doctor --fix' to re-derive the base from commit history"

// registryLockHint names the lock path in a timeout message (the state contract).
func registryLockHint(lockPath string) string {
	return fmt.Sprintf("another sanho process holds %s; retry in a moment", lockPath)
}

// --- The guidance catalog (the guidance contract closure, the testing contract) --------------------
//
// D3 makes it normative that *every advised command must succeed in the
// state where it is advised*. A test can only enforce that against a
// catalog it can enumerate, so every message above that names a next
// command appears below exactly once per rendering that reaches a user.
//
// Two tests hold the two halves of the contract shut:
//
//   - catalog_test.go parses THIS FILE and requires a Catalog entry for
//     every constant or renderer whose literals name a `sanho …` or
//     `git …` command. Adding guidance here without a catalog entry
//     fails the build.
//   - test/cli/e2e's closure suite requires one fixture per Scenario in
//     ClosureScenarios(), reaches that state with the real binary,
//     asserts Match appears, and runs NextCommands. Adding a catalog
//     entry with a new scenario and no fixture fails the build too.
//
// Nothing here changes what sanho prints: Sample is produced by calling
// the very renderer it describes.

// CatalogEntry is one user-facing message that names a next command.
type CatalogEntry struct {
	// ID is this message's stable identity, independent of its wording.
	ID string
	// Source is the constant or renderer in messages.go that produces
	// it. It is what ties the catalog to the file the scan reads.
	Source string
	// Scenario is the closure-scenario ID: the state the e2e suite
	// builds in order to make this message appear and then run its
	// NextCommands. Scenarios are one-to-one with entries.
	Scenario string
	// Sample is the message rendered with representative arguments —
	// the exact bytes the renderer produces, not a paraphrase.
	Sample string
	// Match is a literal substring present in *every* rendering, which
	// is what the closure suite asserts against real output.
	Match string
	// NextCommands are the commands this message advises, written the
	// way the closure suite runs them. An angle-bracketed token is a
	// placeholder the fixture substitutes (`<clone-dir>`, `<commit>`).
	//
	// Two entries name a command the message text does not quote:
	// push_markers and push_unreachable. Both describe a state the user
	// leaves by *retrying the push* — resolving markers, or restoring
	// network access — so `git push` is the advised action even though
	// the action line spells it in prose. The closure suite treats them
	// exactly like the rest: reach the state, clear its cause, run the
	// command, require success.
	NextCommands []string
	// Prerequisites makes an advised command's *place in a sequence*
	// part of the contract: for the command it keys, the listed commands
	// run first, in order, in the same workspace, and each must succeed.
	//
	// It exists because guidance stopped being one command per state.
	// The conflict template now names `git add docs/ && git commit`
	// followed by `sanho sync --continue`, and a suite that ran the
	// second one in a world where the first had not happened would be
	// proving something else entirely. Declaring the order here rather
	// than hiding it in a fixture keeps the catalog readable as what it
	// claims to be: the enumerable form of the guidance contract.
	//
	// Every key must also appear in NextCommands (catalog_test.go), and
	// the entries listed are run verbatim through /bin/sh exactly as
	// NextCommands are.
	Prerequisites map[string][]string
}

// Sample OIDs for the catalog renderings. They are syntactically real
// (40 hex characters) so that shortOID renders them as it would in
// production.
const (
	sampleBaseOID = "67c4bbfeada37f5dda8fb79aa43216ef062cd8df"
	sampleHeadOID = "9a41f2c0e1d2c3b4a5968778695a4b3c2d1e0f9a"
)

// samplePlaceholderClone and samplePlaceholderBranch are the
// placeholders the rewrite-recovery entry carries, so its Sample and its
// NextCommands agree character for character before substitution.
const (
	samplePlaceholderClone  = "<clone-dir>"
	samplePlaceholderBranch = "<branch>"
)

// Catalog is the enumerable form of the guidance contract.
var Catalog = []CatalogEntry{
	{
		ID:           "migrate_required",
		Source:       "msgMigrateRequired",
		Scenario:     "v1_layout",
		Sample:       msgMigrateRequired,
		Match:        "this workspace uses the v0.1 layout",
		NextCommands: []string{"sanho migrate"},
	},
	{
		ID:           "not_in_workspace",
		Source:       "msgNotInWorkspace",
		Scenario:     "not_a_workspace",
		Sample:       msgNotInWorkspace,
		Match:        "not a sanho workspace",
		NextCommands: []string{"sanho init"},
	},
	{
		ID:       "push_sync_in_progress",
		Source:   "msgSyncInProgressPush",
		Scenario: "push_sync_in_progress",
		Sample:   msgSyncInProgressPush,
		Match:    "finish the sync first",
		// The resolution sequence and the undo, both proven: the fixture
		// resolves the markers, the prerequisite commits them, and
		// `--continue` is what actually ends the sync.
		NextCommands:  []string{"sanho sync --continue", "sanho sync --abort"},
		Prerequisites: map[string][]string{"sanho sync --continue": {"git add docs/ && git commit"}},
	},
	{
		ID:           "clean_needs_confirmation",
		Source:       "msgCleanNeedsConfirmation",
		Scenario:     "clean_unconfirmed",
		Sample:       msgCleanNeedsConfirmation,
		Match:        "rerun with -y to confirm",
		NextCommands: []string{"sanho clean --dry-run"},
	},
	{
		ID:            "clean_sync_in_progress",
		Source:        "msgCleanSyncInProgress",
		Scenario:      "clean_sync_in_progress",
		Sample:        msgCleanSyncInProgress,
		Match:         "a conflicted sync is in progress",
		NextCommands:  []string{"sanho sync --continue", "sanho sync --abort"},
		Prerequisites: map[string][]string{"sanho sync --continue": {"git add docs/ && git commit"}},
	},
	{
		ID:           "migrate_blocked",
		Source:       "msgMigrateBlockedByTransaction",
		Scenario:     "migrate_blocked",
		Sample:       msgMigrateBlockedByTransaction,
		Match:        "pull-commit transaction or pending-fix state",
		NextCommands: []string{"sanho migrate"},
	},
	{
		ID:           "commit_behind_clean",
		Source:       "commitBehindClean",
		Scenario:     "behind_clean",
		Sample:       commitBehindClean(2),
		Match:        "'sanho sync' will merge cleanly",
		NextCommands: []string{"sanho sync"},
	},
	{
		ID:           "commit_behind_conflicts",
		Source:       "commitBehindConflicts",
		Scenario:     "behind_conflicts",
		Sample:       commitBehindConflicts(1, []string{"docs/api.md"}),
		Match:        "'sanho sync' will report conflicts in",
		NextCommands: []string{"sanho sync"},
	},
	{
		ID:           "commit_behind_unknown",
		Source:       "commitBehindUnknown",
		Scenario:     "behind_unknown",
		Sample:       commitBehindUnknown(1),
		Match:        "run 'sanho sync' to reconcile",
		NextCommands: []string{"sanho sync"},
	},
	{
		ID:       "sync_conflict",
		Source:   "syncConflictMessage",
		Scenario: "sync_conflict",
		Sample:   syncConflictMessage("docs", []string{"docs/api.md"}),
		Match:    "merged docs with upstream",
		// Two branches out of one state: resolve-commit-complete, or
		// undo. The first is a sequence, and Prerequisites is what makes
		// the suite run it as one.
		NextCommands:  []string{"git add docs/ && git commit", "sanho sync --continue", "sanho sync --abort"},
		Prerequisites: map[string][]string{"sanho sync --continue": {"git add docs/ && git commit"}},
	},
	{
		ID:            "unresolved_sync",
		Source:        "unresolvedSyncMessage",
		Scenario:      "unresolved_sync",
		Sample:        unresolvedSyncMessage("docs", []string{"docs/api.md"}),
		Match:         "a sync is in progress",
		NextCommands:  []string{"git add docs/ && git commit", "sanho sync --continue", "sanho sync --abort"},
		Prerequisites: map[string][]string{"sanho sync --continue": {"git add docs/ && git commit"}},
	},
	{
		ID:           "staged_markers",
		Source:       "stagedMarkersMessage",
		Scenario:     "staged_markers",
		Sample:       stagedMarkersMessage([]string{"docs/api.md"}),
		Match:        "staged docs contain conflict markers",
		NextCommands: []string{"git add docs/ && git commit"},
	},
	{
		ID:           "push_conflict",
		Source:       "pushConflictMessage",
		Scenario:     "push_conflict",
		Sample:       pushConflictMessage(sampleBaseOID, sampleHeadOID, []string{"docs/api.md"}),
		Match:        "your docs changes conflict with upstream",
		NextCommands: []string{"sanho sync"},
	},
	{
		ID:       "sync_not_committed",
		Source:   "syncNotCommittedMessage",
		Scenario: "sync_not_committed",
		Sample:   syncNotCommittedMessage(sampleBaseOID, sampleHeadOID),
		Match:    "no commit has changed the files it conflicted on",
		// Two routes out, and the recommended one is a sequence: the
		// abort is what makes `sanho sync` possible, since sync refuses
		// while a note exists. `--continue` is the other reading — "my
		// docs are already what I want" — and it works from this state
		// without anything preceding it.
		NextCommands:  []string{"sanho sync --abort", "sanho sync", "sanho sync --continue"},
		Prerequisites: map[string][]string{"sanho sync": {"sanho sync --abort"}},
	},
	{
		ID:       "sync_needs_continue",
		Source:   "syncNeedsContinueMessage",
		Scenario: "sync_needs_continue",
		Sample:   syncNeedsContinueMessage(sampleBaseOID, sampleHeadOID, true),
		// The em dash is load-bearing: syncNotCommittedMessage also says
		// "is not completed", and the two states must not be able to
		// satisfy each other's fixture.
		Match:        "is not completed — ",
		NextCommands: []string{"sanho sync --continue", "sanho sync --abort"},
	},
	{
		ID:            "sync_continue_blocked",
		Source:        "syncContinueBlockedMessage",
		Scenario:      "sync_continue_blocked",
		Sample:        syncContinueBlockedMessage("the docs worktree still contains conflict markers: docs/api.md"),
		Match:         "the sync is not ready to be completed",
		NextCommands:  []string{"git add docs/ && git commit", "sanho sync --continue", "sanho sync --abort"},
		Prerequisites: map[string][]string{"sanho sync --continue": {"git add docs/ && git commit"}},
	},
	{
		ID:           "sync_note_corrupt",
		Source:       "syncNoteCorruptMessage",
		Scenario:     "sync_note_corrupt",
		Sample:       syncNoteCorruptMessage("/repo/.git/sanho/sync.json: unexpected end of JSON input"),
		Match:        "the record of the sync in progress is unreadable",
		NextCommands: []string{"sanho sync --abort"},
	},
	{
		ID:           "push_sync_required",
		Source:       "pushSyncRequiredMessage",
		Scenario:     "push_sync_required",
		Sample:       pushSyncRequiredMessage("no_base", "", sampleHeadOID),
		Match:        "docs must be reconciled before publishing",
		NextCommands: []string{"sanho sync"},
	},
	{
		ID:           "push_provenance_uncorroborated",
		Source:       "pushSyncRequiredMessage",
		Scenario:     "push_provenance_uncorroborated",
		Sample:       pushSyncRequiredMessage(publish.ReasonUncorroboratedBase, sampleHeadOID, sampleHeadOID),
		Match:        "docs provenance does not corroborate canonical head",
		NextCommands: []string{"git add docs/ && git commit", "git push"},
		Prerequisites: map[string][]string{
			"git push": {"git add docs/ && git commit"},
		},
	},
	{
		ID:           "push_markers",
		Source:       "pushMarkersMessage",
		Scenario:     "push_markers",
		Sample:       pushMarkersMessage([]string{"docs/api.md"}),
		Match:        "pushed docs still contain conflict markers",
		NextCommands: []string{"git push"},
	},
	{
		ID:           "push_unreachable",
		Source:       "pushUnreachableMessage",
		Scenario:     "canonical_unreachable",
		Sample:       pushUnreachableMessage("git@host:docs.git", "connection refused"),
		Match:        "canonical repository unreachable",
		NextCommands: []string{"git push"},
	},
	{
		ID:       "push_rewritten",
		Source:   "pushRewrittenMessage",
		Scenario: "history_rewritten",
		// The anchor-naming rendering of the same function is a
		// defensive branch: pre-push only raises ErrHistoryRewritten
		// when the docs-base-tree search found nothing, so the message a
		// user actually meets is this one. The anchor rendering stays
		// pinned by messages_test.go rather than by a fixture that
		// cannot exist.
		Sample: pushRewrittenMessage(sampleBaseOID, "", samplePlaceholderClone, samplePlaceholderBranch),
		Match:  "canonical history was rewritten",
		NextCommands: []string{
			"git -C " + samplePlaceholderClone + " log --oneline refs/remotes/origin/" + samplePlaceholderBranch,
			"sanho sync --rebase-onto <commit>",
		},
	},
	{
		ID:           "stamp_warning",
		Source:       "commitMsgStampWarning",
		Scenario:     "stamp_warning",
		Sample:       commitMsgStampWarning("no docs base is recorded"),
		Match:        "docs provenance not stamped",
		NextCommands: []string{"sanho doctor --fix"},
	},
	{
		ID:           "doctor_fix_hint",
		Source:       "doctorFixHint",
		Scenario:     "doctor_fix_hint",
		Sample:       doctorFixHint,
		Match:        "re-derive the base from commit history",
		NextCommands: []string{"sanho doctor --fix"},
	},
	{
		ID:           "stale_canonical",
		Source:       "staleCanonicalLine",
		Scenario:     "stale_data",
		Sample:       staleCanonicalLine(50 * time.Hour),
		Match:        "old — run 'sanho status --refresh'",
		NextCommands: []string{"sanho status --refresh"},
	},
	{
		ID:           "never_fetched",
		Source:       "neverFetchedLine",
		Scenario:     "never_fetched",
		Sample:       neverFetchedLine,
		Match:        "canonical has never been fetched",
		NextCommands: []string{"sanho status --refresh"},
	},
	{
		ID:           "push_empty_docs",
		Source:       "pushEmptyDocsMessage",
		Scenario:     "push_empty_docs",
		Sample:       pushEmptyDocsMessage("legacy", sampleHeadOID, 3),
		Match:        "publishing it would delete",
		NextCommands: []string{"sanho sync"},
	},
	{
		ID:           "clone_missing",
		Source:       "cloneMissingMessage",
		Scenario:     "clone_missing",
		Sample:       cloneMissingMessage(samplePlaceholderClone),
		Match:        "the canonical clone is missing",
		NextCommands: []string{"sanho sync"},
	},
	{
		ID:           "doctor_hooks",
		Source:       "doctorHooksMessage",
		Scenario:     "doctor_hooks",
		Sample:       doctorHooksMessage("pre-commit: missing"),
		Match:        "to reinstall them",
		NextCommands: []string{"sanho doctor --fix"},
	},
	{
		ID:           "doctor_publication_pending",
		Source:       "doctorPublicationPendingMessage",
		Scenario:     "doctor_publication_pending",
		Sample:       doctorPublicationPendingMessage(),
		Match:        "committed docs differ from the publication base after hook repair",
		NextCommands: []string{"git push"},
	},
	{
		ID:            "sync_note_pending",
		Source:        "syncNotePendingMessage",
		Scenario:      "sync_note_pending",
		Sample:        syncNotePendingMessage("a sync from " + sampleBaseOID[:12] + " is unresolved"),
		Match:         "complete it with 'sanho sync --continue'",
		NextCommands:  []string{"sanho sync --continue", "sanho sync --abort"},
		Prerequisites: map[string][]string{"sanho sync --continue": {"git add docs/ && git commit"}},
	},
	{
		ID:           "base_needs_sync",
		Source:       "baseNeedsSyncMessage",
		Scenario:     "base_needs_sync",
		Sample:       baseNeedsSyncMessage("no docs base is recorded"),
		Match:        "to establish one from canonical",
		NextCommands: []string{"sanho sync"},
	},
	{
		ID:           "base_cleared",
		Source:       "baseClearedMessage",
		Scenario:     "base_cleared",
		Sample:       baseClearedMessage(),
		Match:        "the docs base was cleared",
		NextCommands: []string{"sanho sync"},
	},
	{
		ID:       "sync_continue_foreign_history",
		Source:   "syncContinueForeignHistoryMessage",
		Scenario: "sync_continue_foreign_history",
		Sample: syncContinueForeignHistoryMessage(
			"it began at " + sampleBaseOID[:12] + ", and HEAD is " + sampleHeadOID[:12]),
		Match: "this sync cannot be completed here",
		// One route out, and it is the whole of it: the abort needs
		// nothing from the branch it is standing on. `sanho sync` after
		// it is the way back to the reconciliation, so the sequence is
		// declared rather than left implied.
		NextCommands:  []string{"sanho sync --abort", "sanho sync"},
		Prerequisites: map[string][]string{"sanho sync": {"sanho sync --abort"}},
	},
	{
		ID:           "base_unknown_to_canonical",
		Source:       "baseUnknownToCanonicalMessage",
		Scenario:     "base_unknown_to_canonical",
		Sample:       baseUnknownToCanonicalMessage(sampleBaseOID),
		Match:        "is not in the canonical repository",
		NextCommands: []string{"sanho status"},
	},
	{
		ID:           "status_behind",
		Source:       "statusBehindLine",
		Scenario:     "status_behind",
		Sample:       statusBehindLine(2, true, true, nil),
		Match:        "'sanho sync'",
		NextCommands: []string{"sanho sync"},
	},
	{
		ID:            "sync_in_progress_command",
		Source:        "syncInProgressMessage",
		Scenario:      "sync_in_progress_command",
		Sample:        syncInProgressMessage("syncing " + sampleBaseOID[:12] + " to " + sampleHeadOID[:12]),
		Match:         "a conflicted sync is in progress",
		NextCommands:  []string{"sanho sync --continue", "sanho sync --abort"},
		Prerequisites: map[string][]string{"sanho sync --continue": {"git add docs/ && git commit"}},
	},
	{
		ID:           "pull_needs_sync",
		Source:       "pullNeedsSyncMessage",
		Scenario:     "pull_needs_sync",
		Sample:       pullNeedsSyncMessage("docs have uncommitted changes"),
		Match:        "can only fast-forward",
		NextCommands: []string{"sanho sync"},
	},
	{
		ID:       "sync_unknown_base",
		Source:   "syncUnknownBaseMessage",
		Scenario: "sync_unknown_base",
		Sample:   syncUnknownBaseMessage("neither " + sampleBaseOID[:12] + " nor its docs tree is in canonical history"),
		Match:    "the recorded docs base is not in canonical history",
		// The placeholder is the same one the rewrite-recovery entry
		// carries: the fixture substitutes a real canonical commit.
		NextCommands: []string{"sanho sync --rebase-onto <commit>"},
	},
	{
		ID:           "sync_unreachable",
		Source:       "syncUnreachableMessage",
		Scenario:     "sync_unreachable",
		Sample:       syncUnreachableMessage("git@host:docs.git", "connection refused"),
		Match:        "canonical repository unreachable",
		NextCommands: []string{"sanho sync"},
	},
	{
		ID:           "rebase_onto_healthy",
		Source:       "rebaseOntoHealthyMessage",
		Scenario:     "rebase_onto_healthy",
		Sample:       rebaseOntoHealthyMessage(sampleHeadOID, sampleBaseOID),
		Match:        "--rebase-onto is for recovering from rewritten history",
		NextCommands: []string{"sanho status"},
	},
	{
		ID:           "init_next_steps",
		Source:       "initNextStepsMessage",
		Scenario:     "init_next_steps",
		Sample:       initNextStepsMessage(true),
		Match:        "git add .gitignore && git commit",
		NextCommands: []string{"git add .gitignore && git commit"},
	},
	{
		ID:           "project_has_workspaces",
		Source:       "projectHasWorkspacesMessage",
		Scenario:     "project_has_workspaces",
		Sample:       projectHasWorkspacesMessage("product", 1, "/home/u/product"),
		Match:        "run 'sanho clean' in them",
		NextCommands: []string{"sanho clean"},
	},
}

// ClosureScenarios is the manifest the e2e closure suite matches its
// fixture table against, sorted so the two sides compare as sequences.
func ClosureScenarios() []string {
	scenarios := make([]string, 0, len(Catalog))
	for _, entry := range Catalog {
		scenarios = append(scenarios, entry.Scenario)
	}
	sort.Strings(scenarios)
	return scenarios
}
