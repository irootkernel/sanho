package cli

// The user-facing message catalog (sanho-v0.2.md §5.9).
//
// Every string sanho prints that names a next command lives here, as a
// constant or a single renderer. Nothing is assembled inline at a call
// site. That is a deliberate seed for P5's **guidance-closure suite**:
// D3 makes it normative that *every advised command must succeed in the
// state where it is advised*, and §9 rule 2 requires a table that
// enumerates every (state, message) pair, parses the advised command out
// of the message, runs it, and asserts success — with an unlisted
// message failing the build. That test can only be written against a
// catalog it can enumerate, so new guidance must be added here rather
// than at the point of use.
//
// Hygiene rules this file enforces by construction (§5.9):
//
//   - English only (audit L4). The only non-ASCII characters are the em
//     dash and right arrow that appear verbatim in the §5.9 normative
//     templates.
//   - OIDs are shortened to 12 characters, via shortOID.
//   - Never a raw Go error chain at user level: causes are wrapped with
//     a cause line plus an action line.
//   - Degraded-mode lines always carry the data age.
//
// The three numbered templates below are reproduced from §5.9 verbatim,
// with two documented substitutions: the docs directory is the
// workspace's configured one (the template's `docs/` is the default),
// and OIDs render at the §5.9 hygiene width of 12 rather than the
// template's illustrative 7.

import (
	"fmt"
	"strings"
	"time"
)

// shortOIDWidth is §5.9's "OIDs shortened to 12 chars".
const shortOIDWidth = 12

// shortOID renders an OID at the §5.9 width. An absent OID is named
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
	// msgMigrateRequired is the §8 pre-migration degradation line. It is
	// the single hint a v1 workspace gets from every entry point, and the
	// only command that succeeds in that state.
	msgMigrateRequired = "sanho: this workspace uses the v0.1 layout; run 'sanho migrate'"

	// msgNotInWorkspace is printed when the current directory is not a
	// managed workspace. `sanho init` is what makes it one.
	msgNotInWorkspace = "sanho: not a sanho workspace (no .sanho.json here); run 'sanho init' to create one"

	// msgSyncInProgressPush rejects a push while a conflicted sync owns
	// the docs worktree (§5.3 step 2).
	msgSyncInProgressPush = "sanho: finish the sync first: resolve conflicts, then 'git add' and 'git commit' (or 'sanho sync --abort')"

	// msgMarkersBeforePush follows the marker list on a rejected push.
	msgMarkersBeforePush = "resolve the markers before pushing"

	// msgCanonicalUnreachableAction is the action line paired with the
	// canonical-unreachable cause line (§5.9).
	msgCanonicalUnreachableAction = "Check network access to the docs repository, then push again."

	// msgPushRejectedTrailer is git's own idiom, and the last line of the
	// §5.9 push-rejection template.
	msgPushRejectedTrailer = "error: push rejected — no remote ref was changed"

	// msgCleanNeedsConfirmation guards the destructive path of
	// `sanho clean`; --dry-run is the read-only alternative (audit M4).
	msgCleanNeedsConfirmation = "sanho: 'sanho clean' removes this workspace's sanho state; rerun with -y to confirm, or 'sanho clean --dry-run' to preview"

	// msgCleanSyncInProgress refuses to clean while a sync is owed. The
	// named command cannot fail once its precondition holds (§5.5 step 7).
	msgCleanSyncInProgress = "sanho: a conflicted sync is in progress; finish it, or run 'sanho sync --abort' first"

	// msgInitNoProvenance refuses reuse mode when existing docs carry no
	// provenance to derive a base from (audit L4: English only).
	msgInitNoProvenance = "existing docs directory has no docs-base/docs-version commits; " +
		"commit docs through sanho first or rerun with --force to replace the directory"

	// msgInitForceNeedsConfirmation guards --force, which discards the
	// existing docs directory.
	msgInitForceNeedsConfirmation = "sanho: --force replaces the existing docs directory with canonical content; rerun with -y to confirm"

	// msgInitCanonicalEmpty is the bootstrap notice: nothing has been
	// published yet, so there is no base to record (§5.3).
	msgInitCanonicalEmpty = "sanho: canonical repository is empty; your first push will publish docs"

	// msgAlreadyMigrated is `sanho migrate` on a workspace that is
	// already v2 — an idempotent success, not a failure (§8).
	msgAlreadyMigrated = "sanho: already migrated"

	// msgMigrateBlockedByTransaction refuses migration while v0.1 state
	// exists that v0.2 will not interpret (§8 step 1).
	msgMigrateBlockedByTransaction = "sanho: a v0.1 pull-commit transaction or pending-fix state is still present; " +
		"finish or abort it with the v0.1 binary, then run 'sanho migrate' again"

	// msgMigrateNeedsURL is raised when the legacy daemon state carries
	// no docs repository URL for this project.
	msgMigrateNeedsURL = "sanho: the docs repository URL is not recorded in the legacy state; rerun with --docs-repo-url <url>"
)

// daemonStopInstructions is §8 step 2, printed verbatim and never
// executed: service ownership was explicitly the user's in v0.1, so
// migrate names the command and stops there.
var daemonStopInstructions = []string{
	"sanho: the v0.1 daemon is no longer used. Stop and unload it yourself:",
	"  macOS (launchd):  launchctl bootout gui/$(id -u)/xyz.rootkernel.sanho",
	"  Linux (systemd):  systemctl --user disable --now sanhod",
	"The 'sanhod' binary can be deleted at your leisure; nothing references it.",
}

// --- Template 1: commit warning (§5.9, §5.6) --------------------------

// commitBehindClean is the §5.9 template-1 line, verbatim.
func commitBehindClean(behind int) string {
	return fmt.Sprintf("sanho: docs base is %d commits behind — 'sanho sync' will merge cleanly", behind)
}

// commitBehindConflicts is the §5.6 conflict-prediction variant. It says
// what happens if the warning is ignored, which §5.9 requires whenever
// there is something to say.
func commitBehindConflicts(behind int, files []string) string {
	return fmt.Sprintf("sanho: docs base is %d commits behind — 'sanho sync' will report conflicts in %s; syncing sooner keeps them small",
		behind, strings.Join(files, ", "))
}

// commitBehindUnknown degrades to the behind count alone when the
// clean/conflict prediction could not be computed (§11 open question 3
// sanctions this shape). It still names the command that fixes the
// state, so guidance closure holds.
func commitBehindUnknown(behind int) string {
	return fmt.Sprintf("sanho: docs base is %d commits behind — run 'sanho sync' to reconcile", behind)
}

// staleDataSuffix is appended to a freshness line when the last fetch is
// older than staleDataThreshold: degraded-mode lines always include the
// data age (§5.9).
func staleDataSuffix(age time.Duration) string {
	return fmt.Sprintf(" (canonical last checked %s ago)", humanizeAge(age))
}

// staleDataThreshold is §5.6's 24-hour rule.
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

// --- Template 2: sync conflict (§5.9) ---------------------------------

// syncConflictMessage renders §5.9 template 2 verbatim. docsDir is the
// workspace's configured docs directory, which for the default "docs"
// reproduces the template character for character.
func syncConflictMessage(docsDir string, conflicts []string) string {
	var b strings.Builder
	writef(&b, "sanho: merged docs with upstream — %d files have conflicts:\n", len(conflicts))
	for _, path := range conflicts {
		writef(&b, "  %s\n", path)
	}
	writef(&b, "Resolve the markers, then:  git add %s/ && git commit\n", docsDir)
	b.WriteString("To undo this sync:          sanho sync --abort")
	return b.String()
}

// --- Template 3: push rejection (§5.9) --------------------------------

// pushConflictMessage renders §5.9 template 3 verbatim for the case
// ③-conflict rejection.
func pushConflictMessage(base, head string) string {
	return fmt.Sprintf("sanho: your docs changes conflict with upstream (base %s → %s)\n"+
		"Run 'sanho sync', resolve, commit, then push again.\n"+
		"%s", shortOID(base), shortOID(head), msgPushRejectedTrailer)
}

// pushSyncRequiredMessage covers the other rejections that route to
// `sanho sync`: no recorded base, and an exhausted CAS retry budget.
// Both are states in which `sanho sync` succeeds, which is what keeps
// the advice closed (D3).
func pushSyncRequiredMessage(reason, base, head string) string {
	return fmt.Sprintf("sanho: docs must be reconciled before publishing (%s; base %s → %s)\n"+
		"Run 'sanho sync', resolve if needed, commit, then push again.\n"+
		"%s", reason, shortOID(base), shortOID(head), msgPushRejectedTrailer)
}

// pushMarkersMessage rejects a push whose docs carry committed conflict
// markers (§5.3 step 3).
func pushMarkersMessage(paths []string) string {
	var b strings.Builder
	b.WriteString("sanho: pushed docs still contain conflict markers:\n")
	for _, path := range paths {
		writef(&b, "  %s\n", path)
	}
	writef(&b, "%s\n%s", msgMarkersBeforePush, msgPushRejectedTrailer)
	return b.String()
}

// pushUnreachableMessage is the §5.9 canonical-unreachable pair: a cause
// line naming the URL and the failure, then an action line. The raw git
// error never reaches the user unwrapped.
func pushUnreachableMessage(url, cause string) string {
	return fmt.Sprintf("sanho: canonical repository unreachable (%s): %s\n%s\n%s",
		url, cause, msgCanonicalUnreachableAction, msgPushRejectedTrailer)
}

// pushRewrittenMessage is §5.3 case ④. When a commit carrying the
// recorded docs-base-tree still exists in canonical, it is named, so the
// advised command is runnable as printed. When none does, no command can
// succeed, so the message says "manual intervention required" and gives
// the diagnostics needed to choose a target — never a command that will
// fail (D3).
func pushRewrittenMessage(base, anchor, cloneDir string) string {
	if anchor != "" {
		return fmt.Sprintf("sanho: canonical history was rewritten; base %s is no longer reachable\n"+
			"Run 'sanho sync --rebase-onto %s', resolve if needed, commit, then push again.\n"+
			"%s", shortOID(base), anchor, msgPushRejectedTrailer)
	}
	return fmt.Sprintf("sanho: canonical history was rewritten; base %s is no longer reachable\n"+
		"manual intervention required: no canonical commit carries this workspace's docs base tree.\n"+
		"List the candidates with:  git -C %s log --oneline refs/remotes/origin/HEAD\n"+
		"Then run:                  sanho sync --rebase-onto <commit>\n"+
		"%s", shortOID(base), cloneDir, msgPushRejectedTrailer)
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

// syncAbortedMessage reports a completed `sanho sync --abort`.
func syncAbortedMessage() string { return "sanho: sync aborted; docs restored to HEAD" }

// baseRederivedMessage is the one line a post-checkout/merge/rewrite
// hook prints, and only when the base actually moved (§5.10).
func baseRederivedMessage(base string) string {
	return fmt.Sprintf("sanho: docs base re-derived as %s after HEAD moved", shortOID(base))
}

// syncCompletedMessage reports that a conflicted sync has been resolved
// and its note cleared.
func syncCompletedMessage() string { return "sanho: sync resolved; the sync note has been cleared" }

// stagedMarkersMessage blocks a commit whose staged docs still carry
// conflict markers (§5.6 step 1).
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
// unresolved, in the shape of template 2: the same two next steps, for
// the same state.
func unresolvedSyncMessage(docsDir string, paths []string) string {
	var b strings.Builder
	writef(&b, "sanho: a sync is in progress — %d files still have conflicts:\n", len(paths))
	for _, path := range paths {
		writef(&b, "  %s\n", path)
	}
	writef(&b, "Resolve the markers, then:  git add %s/ && git commit\n", docsDir)
	b.WriteString("To undo this sync:          sanho sync --abort")
	return b.String()
}

// commitMsgStampWarning is the one-line, never-blocking warning
// `commit-msg` prints when it cannot stamp (§5.1).
func commitMsgStampWarning(cause string) string {
	return fmt.Sprintf("sanho: docs provenance not stamped (%s); run 'sanho doctor --fix' to restore it", cause)
}

// --- Status and doctor ------------------------------------------------

// staleCanonicalLine is the §5.2 degraded-read line: cached results
// always say how old they are and how to refresh.
func staleCanonicalLine(age time.Duration) string {
	return fmt.Sprintf("canonical data is %s old — run 'sanho status --refresh'", humanizeAge(age))
}

// neverFetchedLine covers a clone that has never fetched.
const neverFetchedLine = "canonical has never been fetched — run 'sanho status --refresh'"

// doctorFixHint names the repair for a base file doctor found wanting.
const doctorFixHint = "run 'sanho doctor --fix' to re-derive the base from commit history"

// registryLockHint names the lock path in a timeout message (§5.7).
func registryLockHint(lockPath string) string {
	return fmt.Sprintf("another sanho process holds %s; retry in a moment", lockPath)
}
