package cli

// The six hook entry points (sanho-v0.2.md §5.10).
//
// Two rules shape every one of them.
//
// Fail-open on the commit path (P2). `pre-commit`, `commit-msg` and the
// three `post-*` hooks exit 0 for everything except the two gates §5.6
// makes blocking (staged markers, an unresolved sync). A commit that
// fails because sanho had an internal problem is exactly Critical C1's
// failure class, and v0.2 does not have it.
//
// Fail-closed at the push boundary. `pre-push` is the only networked
// hook and the only one that rejects, because push is where shared state
// is created.

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/irootkernel/sanho/internal/domain/provenance"
	pubdom "github.com/irootkernel/sanho/internal/domain/publish"
	"github.com/irootkernel/sanho/internal/infra/appgit"
	"github.com/irootkernel/sanho/internal/infra/canonical"
	"github.com/irootkernel/sanho/internal/infra/fsx"
	"github.com/irootkernel/sanho/internal/usecase/docsync"
	"github.com/irootkernel/sanho/internal/usecase/publish"

	"github.com/spf13/cobra"
)

func newHookCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hook",
		Short: "Git hook entry points (invoked by git, not by hand)",
	}
	cmd.AddCommand(
		newPreCommitCmd(),
		newCommitMsgCmd(),
		newPrePushCmd(),
		newPostCheckoutCmd(),
		newPostMergeCmd(),
		newPostRewriteCmd(),
		newPostCommitCmd(),
	)
	return cmd
}

// hookWorkspace resolves the workspace for a hook.
//
// The three outcomes are the §8 degradation contract. A directory that
// is not a workspace means the hook lines outlived their configuration,
// which is the user's business and never git's: skip silently. A v0.1
// workspace prints the migrate hint once and skips. Anything else is a
// real workspace.
func hookWorkspace(ctx context.Context, cmd *cobra.Command) (ws *workspace, proceed bool, err error) {
	ws, err = openWorkspace(ctx)
	switch {
	case err == nil:
		return ws, true, nil
	case errors.Is(err, errNotWorkspace):
		return nil, false, nil
	case errors.Is(err, errV1Workspace):
		writeln(cmd.ErrOrStderr(), msgMigrateRequired)
		return ws, false, nil
	default:
		return nil, false, err
	}
}

// --- pre-commit (§5.6) ------------------------------------------------

func newPreCommitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pre-commit",
		Short: "Local marker gate and freshness warning",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return runPreCommit(cmd) },
	}
}

// runPreCommit implements §5.6. It never opens a network connection and
// never blocks on canonical availability; exit is 0 for every freshness
// state.
func runPreCommit(cmd *cobra.Command) error {
	ctx := cmd.Context()
	ws, proceed, err := hookWorkspace(ctx, cmd)
	if err != nil || !proceed {
		// A broken workspace must not break `git commit` (P2): report the
		// reason and let the commit through.
		if err != nil {
			writef(cmd.ErrOrStderr(), "sanho: skipped the docs check (%v)\n", err)
		}
		return nil
	}

	blocked, syncOwed, err := preCommitGates(ctx, cmd, ws)
	switch {
	case err != nil:
		return err
	case blocked:
		return errAlreadyReported
	case syncOwed:
		// The freshness warning is suppressed for the duration of an
		// unfinished sync, and only there. The base deliberately stays at
		// the pre-sync value until the resolution is confirmed, so the
		// workspace *is* behind canonical for the whole window and would
		// say so on every commit — under a notice that already describes
		// the same fact more usefully and names the way out. Two lines
		// about one state, one of them advising a `sanho sync` that
		// refuses while a note exists, is worse than one.
		return nil
	}

	warnStaleBase(ctx, cmd, ws)
	return nil
}

// preCommitGates runs the two conditions §5.6 lets block a commit, in
// the order that makes the state describe itself correctly, and reports
// whether a sync is still owed at the end of it.
//
// The sync note comes first: an unresolved sync leaves markers in the
// worktree by construction, so checking it first turns "you have
// markers" into the more useful "finish the sync or undo it".
//
// **Nothing here writes.** The hook reads the sync state and prints; the
// note is deleted by `sanho sync --continue` and `sanho sync --abort`
// and by nothing else. It used to be cleared right here whenever the
// state looked like a resolution — which is how a stash followed by more
// work on the same file came to be read as a finished sync, and how a
// read path came to mutate the one file the whole window is defined by.
//
// Only markers block. A sync that was put aside, or resolved but not yet
// completed, leaves no markers behind, so it is reported and the commit
// proceeds: P2 makes the commit path non-blocking for everything except
// the two §5.6 gates, and stopping every unrelated commit until a stash
// is dealt with would punish the wrong action. The state is refused
// where it matters — `pre-push`, where shared state is created.
//
// Every unfinished sync says so, on every commit. The previous version
// stayed quiet when the commit being prepared looked like the
// resolution, and that silence is the first half of the reproduction
// this wave closes: the user got no signal at the one moment they were
// most likely to believe the sync had ended.
func preCommitGates(ctx context.Context, cmd *cobra.Command, ws *workspace) (blocked, syncOwed bool, err error) {
	state := ws.statePort()
	use := &docsync.UseCase{App: ws.appPort(), State: state}

	resolution, err := use.ResolutionState(ctx)
	if err != nil {
		// A sync state sanho itself cannot read must never break
		// `git commit` (P2, Critical C1's failure class): say so, and let
		// the staged marker gate below answer for the commit's own
		// content.
		if errors.Is(err, docsync.ErrSyncNoteCorrupt) {
			writeln(cmd.ErrOrStderr(), syncNoteCorruptMessage(causeLine(err)))
			syncOwed = true
		} else {
			writef(cmd.ErrOrStderr(), "sanho: skipped the sync-state check (%v)\n", err)
		}
	}

	if resolution != docsync.ResolutionNoSync {
		syncOwed = true
		remaining, scanErr := ws.repo.ScanWorktreeDocsForMarkers(ctx)
		if scanErr != nil {
			return false, syncOwed, scanErr
		}
		if len(remaining) > 0 {
			writeln(cmd.ErrOrStderr(), unresolvedSyncMessage(ws.config.DocsDir, remaining))
			return true, syncOwed, nil
		}
		reportUnfinishedSync(cmd, state, resolution)
	}

	staged, err := ws.repo.ScanStagedDocsForMarkers(ctx)
	if err != nil {
		return false, syncOwed, err
	}
	if len(staged) > 0 {
		writeln(cmd.ErrOrStderr(), stagedMarkersMessage(staged))
		return true, syncOwed, nil
	}
	return false, syncOwed, nil
}

// reportUnfinishedSync prints the one line that describes an unfinished
// sync whose markers are gone from the worktree.
//
// Three states, three sentences, because they leave the user in three
// different places: mid-resolution, resolved but not recorded, and put
// aside without a resolution at all. All three name the same two ways
// out — `sanho sync --continue` and `sanho sync --abort` — because under
// the explicit-completion contract those are the only two there are.
//
// It is shared by `pre-commit`, which blocks on none of them, and
// `pre-push`, which refuses all three.
func reportUnfinishedSync(cmd *cobra.Command, state statePort, resolution docsync.Resolution) {
	note, exists, err := state.LoadSyncNote()
	if err != nil || !exists {
		// An unreadable note has already been reported by the caller, and
		// a note that vanished between the two reads owes nothing.
		return
	}
	writeln(cmd.ErrOrStderr(), unfinishedSyncMessage(resolution, note.PrevBase.Commit, note.Target.Commit))
}

// unfinishedSyncMessage picks the wording for one unfinished state.
//
// ResolutionUnknown shares the "nothing has been committed yet" wording
// rather than the "put aside" one, and the difference is the point: a
// note that never recorded what the merge conflicted on cannot support
// the sentence "no commit has changed the files it conflicted on". That
// false explanation was the legacy-note defect this wave closes.
func unfinishedSyncMessage(resolution docsync.Resolution, prev, target string) string {
	switch resolution {
	case docsync.ResolutionNotCommitted:
		return syncNotCommittedMessage(prev, target)
	case docsync.ResolutionResolved:
		return syncNeedsContinueMessage(prev, target, true)
	default:
		return syncNeedsContinueMessage(prev, target, false)
	}
}

// warnStaleBase is §5.6 step 2: a single informational line when the
// base is behind, silence when it is current, and never an error.
//
// Every failure path here is a silent pass. A missing clone, an
// unreadable base, a canonical branch with no commits — none of them
// says anything about *this* commit, and P2 makes the commit path
// unconditionally non-blocking. Canonical head comes from the last
// fetch; nothing here fetches.
func warnStaleBase(ctx context.Context, cmd *cobra.Command, ws *workspace) {
	store, err := ws.openCanonical()
	if err != nil {
		return
	}
	base, hasBase, err := ws.statePort().LoadBase()
	if err != nil || !hasBase {
		return
	}
	head, headTree, err := store.Head(ctx)
	if err != nil {
		return
	}

	behind, _, err := store.Distance(ctx, base.Commit, head)
	if err != nil || behind == 0 {
		return
	}

	// The prediction is about the COMMIT BEING MADE, so its local side is
	// the index rather than HEAD (M7). An unreadable index degrades to
	// HEAD, which is what the warning used to use unconditionally.
	oursTree, err := ws.repo.IndexDocsTree(ctx)
	if err != nil {
		if oursTree, err = ws.repo.HeadDocsTree(ctx); err != nil {
			return
		}
	}
	preview := previewSync(ctx, ws, store, base, head, headTree, oursTree)
	var line string
	switch {
	case !preview.Known:
		line = commitBehindUnknown(behind)
	case preview.Clean:
		line = commitBehindClean(behind)
	default:
		line = commitBehindConflicts(behind, preview.Conflicts)
	}
	if age, ok := store.Age(); ok && age > staleDataThreshold {
		line += staleDataSuffix(age)
	}
	writeln(cmd.ErrOrStderr(), line)
}

// --- commit-msg (§5.1) ------------------------------------------------

func newCommitMsgCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "commit-msg [message-file]",
		Short: "Stamp the docs-base provenance trailers",
		Args:  cobra.MaximumNArgs(1),
		RunE:  func(cmd *cobra.Command, args []string) error { return runCommitMsg(cmd, args) },
	}
}

// runCommitMsg implements §5.1. It is purely local — no network, no
// clone access — and it NEVER exits non-zero: trailers are a durable
// record and a recovery source, never a gate input, so a commit is worth
// more than a stamp.
func runCommitMsg(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}
	ctx := cmd.Context()

	ws, proceed, err := hookWorkspace(ctx, cmd)
	if err != nil || !proceed {
		return nil
	}

	if err := stampCommitMessage(ctx, ws, args[0]); err != nil {
		// One line, the innermost cause, no package tags: §5.9 forbids a
		// raw Go chain at user level, and this one is printed inside
		// `git commit` where it competes with git's own output.
		writeln(cmd.ErrOrStderr(), commitMsgStampWarning(stripInternalPrefixes(causeLine(err))))
	}
	return nil
}

func stampCommitMessage(ctx context.Context, ws *workspace, messagePath string) error {
	message, err := os.ReadFile(messagePath)
	if err != nil {
		return fmt.Errorf("read the commit message: %w", err)
	}

	inputs, err := stampInputs(ctx, ws, string(message))
	if err != nil {
		return err
	}
	if !provenance.ShouldStamp(inputs) {
		return nil
	}

	base, hasBase, err := stampBase(ws)
	if err != nil {
		return err
	}
	if !hasBase {
		return errors.New("no docs base is recorded")
	}
	if !base.Valid() {
		return errors.New("the recorded docs base is not a valid OID pair")
	}

	// Atomic: a truncated COMMIT_EDITMSG is a commit message the user
	// loses, and this runs inside the commit git is about to make (F-L4).
	return fsx.WriteFileAtomic(messagePath, appendTrailers(message, base.Trailers()), 0644)
}

// stampBase answers which canonical state the commit being made derives
// its docs from — the value the `docs-base` trailer records.
//
// It is the base file. Always, including inside a conflicted sync's
// window, and that flatness is the fix for the second reproduction of
// the third review.
//
// The previous version made an exception for the commit that resolved
// the sync: its docs carry merged content, so the merge TARGET looked
// like the truthful answer. But a trailer outlives the sync that
// produced it. `sanho sync --abort` — the command the tool itself
// advises — leaves that stamped commit in history and removes the note,
// and the very next branch switch lets base re-derivation adopt the
// target with pre-merge documents beneath it: the fast-forward-over-
// upstream state, arrived at through the trailer rather than through the
// sync. The stand-down guard could not help, because it only holds while
// a note exists.
//
// Stamping the base file instead is never a lie and never dangerous. The
// base file is a real ancestor of the committed docs — during the window
// it is the state the worktree derived from when the merge began — so a
// later re-derivation that adopts it lands on a base that is at worst
// too OLD, which publication reconciles as an ordinary divergence. The
// merge target reaches the base file through `sanho sync --continue`,
// and through nothing else.
//
// It is entirely local: one state file. §5.1's no-network contract for
// the commit path is untouched, and there is no longer a tree comparison
// to run.
func stampBase(ws *workspace) (provenance.Base, bool, error) {
	base, hasBase, err := ws.statePort().LoadBase()
	if err != nil {
		return provenance.Base{}, false, fmt.Errorf("read the base file: %w", err)
	}
	return base, hasBase, nil
}

// stampInputs gathers the three trees §5.1's stamping rule compares.
//
// The HEAD~ term degrades to the empty tree whenever it cannot be
// resolved — an unborn HEAD, a root commit, a shallow clone. §5.1 says
// to treat a missing HEAD~ as having empty docs, and using the empty
// *tree* rather than the empty *string* is what makes a docs-free commit
// on a root commit compare equal instead of spuriously stamping.
func stampInputs(ctx context.Context, ws *workspace, message string) (provenance.StampInputs, error) {
	indexTree, err := ws.repo.IndexDocsTree(ctx)
	if err != nil {
		if errors.Is(err, appgit.ErrUnmergedIndex) {
			// git will refuse this commit itself; stamping it is moot.
			return provenance.StampInputs{}, err
		}
		return provenance.StampInputs{}, fmt.Errorf("read the staged docs tree: %w", err)
	}
	headTree, err := ws.repo.HeadDocsTree(ctx)
	if err != nil {
		return provenance.StampInputs{}, fmt.Errorf("read HEAD's docs tree: %w", err)
	}
	emptyTree, err := ws.repo.EmptyTree(ctx)
	if err != nil {
		return provenance.StampInputs{}, fmt.Errorf("resolve the empty tree: %w", err)
	}

	parentTree := emptyTree
	if tree, parentErr := ws.repo.DocsTreeOf(ctx, "HEAD~"); parentErr == nil {
		parentTree = tree
	}

	return provenance.StampInputs{
		MessageHasBase:     messageHasBaseTrailer(message),
		IndexDocsTree:      indexTree,
		HeadDocsTree:       headTree,
		HeadParentDocsTree: parentTree,
	}, nil
}

// messageHasBaseTrailer reports an existing docs-base trailer.
//
// The key must start the line, at column zero, exactly as git's own
// trailer rules require and exactly as appendTrailers writes it (F-L6).
// The previous version trimmed first, which made an INDENTED
// `docs-base:` count — and indented trailers are what a squash-merge
// body is full of, since git indents every squashed commit message. A
// squash would then suppress its own stamp while parsing nothing, so the
// commit ended up with neither a trailer nor a re-derivable base. Not
// trimming keeps the two halves coherent: an indented line neither
// suppresses stamping nor is read as a trailer.
func messageHasBaseTrailer(message string) bool {
	for _, line := range strings.Split(message, "\n") {
		if strings.HasPrefix(line, provenance.TrailerBase+":") {
			return true
		}
	}
	return false
}

// appendTrailers adds the provenance lines to a commit message.
//
// The blank line is the whole subtlety (M5). Git reads a trailer block
// as the LAST paragraph of the message, so a message that already ends
// in one — `Signed-off-by:`, `Co-authored-by:`, a squash's collected
// trailers — must be extended rather than followed by a new paragraph.
// Always inserting a blank line demoted the existing block into ordinary
// body text: `git interpret-trailers --parse` then reported only
// sanho's own lines, and `Signed-off-by` silently stopped being a
// trailer on every commit sanho stamped. Where the message does NOT end
// in a trailer block, the blank line is exactly what makes one.
//
// The test for "is a trailer block" is git's own shape — `Key: value`
// with a token key, or a folded continuation line — applied to the last
// paragraph. It is a local string decision, so §5.1's no-network
// contract for the commit path is untouched.
func appendTrailers(message []byte, trailers []string) []byte {
	text := strings.TrimRight(string(message), "\n")
	separator := "\n\n"
	if endsWithTrailerBlock(text) {
		separator = "\n"
	}
	return []byte(text + separator + strings.Join(trailers, "\n") + "\n")
}

// endsWithTrailerBlock reports whether the message's last paragraph is
// entirely trailer lines.
//
// "Entirely" is deliberate and matches git: one non-trailer line in the
// final paragraph makes the whole paragraph body text, so appending
// there would produce a trailer git does not parse. An empty message has
// no paragraph and takes the blank-line form.
func endsWithTrailerBlock(text string) bool {
	lines := strings.Split(text, "\n")

	start := len(lines)
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) == "" {
			break
		}
		start = i
	}
	if start >= len(lines) {
		return false
	}
	// A message that is nothing but its subject line is not a trailer
	// block, however the subject happens to be punctuated.
	if start == 0 {
		return false
	}
	for _, line := range lines[start:] {
		if !isTrailerLine(line) {
			return false
		}
	}
	return true
}

// isTrailerLine applies git's trailer shape: `<token>: <value>` where
// the token is letters, digits and hyphens, or a folded continuation
// (a line beginning with whitespace).
func isTrailerLine(line string) bool {
	if line == "" {
		return false
	}
	if line[0] == ' ' || line[0] == '\t' {
		return true
	}
	key, _, ok := strings.Cut(line, ":")
	if !ok || key == "" {
		return false
	}
	for _, r := range key {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
		default:
			return false
		}
	}
	return true
}

// --- pre-push (§5.3) --------------------------------------------------

func newPrePushCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pre-push [remote] [url]",
		Short: "Publish docs to the canonical repository",
		Args:  cobra.MaximumNArgs(2),
		RunE:  func(cmd *cobra.Command, args []string) error { return runPrePush(cmd, args) },
	}
}

// runPrePush implements §5.3. It is the only hook that fails closed:
// push is the boundary at which local work becomes shared, so a state
// that must not be shared is stopped here.
func runPrePush(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()

	ws, err := openWorkspace(ctx)
	switch {
	case errors.Is(err, errNotWorkspace):
		return nil
	case errors.Is(err, errV1Workspace):
		// The push boundary is the natural migration prompt (§8): every
		// other entry point degrades quietly, this one stops.
		writeln(cmd.ErrOrStderr(), msgMigrateRequired)
		writeln(cmd.ErrOrStderr(), msgPushRejectedTrailer)
		return errAlreadyReported
	case err != nil:
		return err
	}

	updates, err := readRefUpdates(cmd.InOrStdin())
	if err != nil {
		return err
	}
	if updates, err = resolveSymbolicRefs(ctx, ws, updates); err != nil {
		return err
	}
	// M1: the §5.3 step-1 filter, applied at the boundary rather than
	// inside the use case. A push carrying only tags, or only branch
	// deletions, publishes nothing — so it must not be refused by the
	// sync gate for a window it has no part in, and must not be made to
	// wait on `ensureCanonical`, which creates and fetches a clone. Both
	// used to happen: `git push --tags` from an unfinished sync was
	// rejected, and the same push offline failed on a network call it
	// never needed. Symbolic refs are resolved first, because `HEAD` is
	// not yet recognizable as a branch update when it arrives.
	updates = publish.Publishable(updates)
	if len(updates) == 0 {
		return nil
	}

	state := ws.statePort()
	// The sync gate, before anything opens the clone.
	//
	// A sync that has already been resolved must not reject the push
	// that carries the resolution — and one that has NOT been resolved
	// must be refused here rather than three steps later, because this
	// refusal needs nothing but a local file. ensureCanonical creates and
	// fetches a clone when there is none, so leaving the cheapest
	// rejection behind it made an already-doomed push pay for a network
	// round trip first (§5.3's ordering principle).
	if blocked, err := prePushSyncGate(ctx, cmd, ws, state); err != nil {
		return err
	} else if blocked {
		return errAlreadyReported
	}

	store, err := ws.ensureCanonical(ctx)
	if err != nil {
		return reportPushError(cmd, ws, err)
	}
	use := &publish.UseCase{
		Canonical:         ws.canonicalPort(store),
		App:               ws.appPort(),
		State:             state,
		ActorEmail:        ws.config.ActorEmail,
		WorkspaceID:       ws.config.WorkspaceID,
		AllowEmptyPublish: allowEmptyPublish(),
	}

	outcome, err := use.Run(ctx, updates)
	if err != nil {
		if len(outcome.PublishedOIDs) > 0 {
			// Evaluate-then-publish makes this rare, but when part of a
			// multi-ref push has already landed the rejection template's
			// "no remote ref was changed" must not stand alone.
			writeln(cmd.ErrOrStderr(), pushPartialPublicationLine(outcome.PublishedOIDs))
		}
		return reportPushError(cmd, ws, err)
	}
	if len(outcome.PublishedOIDs) > 0 {
		// One line per publication: a multi-ref push writes one canonical
		// commit per distinct docs tree, and reporting only the last one
		// is how F-C1's clobber stayed invisible. Each line carries ITS
		// OWN case — reusing the last tip's label for every line reported
		// a fast-forward as a merge, or worse the reverse.
		for i, oid := range outcome.PublishedOIDs {
			decided := outcome.Case
			if i < len(outcome.PublishedCases) {
				decided = outcome.PublishedCases[i]
			}
			writeln(cmd.ErrOrStderr(), pushPublishedMessage(oid, decided.String()))
		}
		if outcome.BaseAdvanceError != nil {
			// The push landed and the local pointer did not move (M2).
			// Saying so is the whole of it: this is not a rejection, and
			// the two commands that repair it run on the user's schedule.
			writeln(cmd.ErrOrStderr(), baseNotAdvancedMessage(causeOfBaseRefusal(outcome.BaseAdvanceError)))
		}
		if outcome.MaintenanceError != nil {
			reportCloneMaintenance(cmd, outcome.MaintenanceError)
		}
		recordWorkspaceState(ctx, ws)
	}
	return nil
}

func reportCloneMaintenance(cmd *cobra.Command, err error) {
	debugf(cmd, "private canonical clone maintenance skipped: %s", causeLine(err))
}

// prePushSyncGate refuses a push while a sync note exists, and reports
// what it refused.
//
// The gate is the note's existence. Every classification below only
// chooses the sentence: markers still in the worktree is the ordinary
// in-progress case; a sync put aside without a resolution commit, one
// resolved but never completed, and one whose note cannot say are three
// different things to tell a user, and all four are the same refusal.
//
// A resolved sync is refused too, and that is the contract rather than
// an oversight. Completion is an explicit act (`sanho sync --continue`),
// so a workspace that has resolved and committed but not completed is
// still a workspace whose base does not describe its docs — publishing
// from there would evaluate the push against the pre-sync base and, at
// best, merge something the user has already merged.
//
// It writes nothing. This gate used to call the completion routine,
// which cleared the note when the state looked resolved — a read path
// mutating the window's own record, on evidence a stash could forge.
func prePushSyncGate(ctx context.Context, cmd *cobra.Command, ws *workspace, state statePort) (blocked bool, err error) {
	stderr := cmd.ErrOrStderr()

	resolution, err := (&docsync.UseCase{App: ws.appPort(), State: state}).ResolutionState(ctx)
	if err != nil {
		if !errors.Is(err, docsync.ErrSyncNoteCorrupt) {
			return false, fmt.Errorf("check the sync state: %w", err)
		}
		writeln(stderr, syncNoteCorruptMessage(causeLine(err)))
		writeln(stderr, msgPushRejectedTrailer)
		return true, nil
	}

	switch resolution {
	case docsync.ResolutionNoSync:
		return false, nil
	case docsync.ResolutionPending:
		remaining, scanErr := ws.repo.ScanWorktreeDocsForMarkers(ctx)
		if scanErr == nil && len(remaining) == 0 {
			// Pending without markers is a resolution being made, not one
			// waiting to be resolved: say what is missing.
			break
		}
		writeln(stderr, msgSyncInProgressPush)
		writeln(stderr, msgPushRejectedTrailer)
		return true, nil
	}

	note, exists, noteErr := state.LoadSyncNote()
	if noteErr != nil {
		return false, fmt.Errorf("read the sync state: %w", noteErr)
	}
	if !exists {
		// The note vanished between the two reads; nothing is owed.
		return false, nil
	}
	writeln(stderr, unfinishedSyncMessage(resolution, note.PrevBase.Commit, note.Target.Commit))
	writeln(stderr, msgPushRejectedTrailer)
	return true, nil
}

// envAllowDocsDeletion is the F-H2 escape hatch: publishing a docs-free
// branch over a canonical that has documents deletes all of them, so it
// is refused unless the user says, for that one push, that they mean it.
const envAllowDocsDeletion = "SANHO_ALLOW_DOCS_DELETION"

func allowEmptyPublish() bool { return os.Getenv(envAllowDocsDeletion) == "1" }

// readRefUpdates parses the pre-push hook's stdin, one
// "<local ref> <local oid> <remote ref> <remote oid>" line per update.
// Malformed lines are skipped rather than fatal: git owns this format,
// and a line this parser does not understand is not a reason to block a
// push it has nothing to say about.
func readRefUpdates(stdin io.Reader) ([]publish.RefUpdate, error) {
	var updates []publish.RefUpdate

	scanner := bufio.NewScanner(stdin)
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 4 {
			continue
		}
		updates = append(updates, publish.RefUpdate{
			LocalRef:  fields[0],
			LocalOID:  fields[1],
			RemoteRef: fields[2],
			RemoteOID: fields[3],
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read the pushed refs: %w", err)
	}
	return updates, nil
}

// resolveSymbolicRefs rewrites a local ref of "HEAD" to the branch it
// points at.
//
// Git reports the *refspec source* as the local ref, so `git push origin
// HEAD` and `git push <url> HEAD:refs/heads/x` both arrive as "HEAD"
// rather than as a branch name. Publication considers only
// refs/heads/* updates (§5.3 step 1), so without this a perfectly
// ordinary push would silently publish nothing. A detached HEAD has no
// branch to name and is left as it is — it is then filtered out, which
// is the right answer for a push that is not from a branch.
func resolveSymbolicRefs(ctx context.Context, ws *workspace, updates []publish.RefUpdate) ([]publish.RefUpdate, error) {
	needed := false
	for _, update := range updates {
		if update.LocalRef == "HEAD" {
			needed = true
			break
		}
	}
	if !needed {
		return updates, nil
	}

	_, branch, err := ws.repo.RepoIdentity(ctx)
	if err != nil {
		return nil, err
	}
	if branch == "" || branch == "HEAD" {
		return updates, nil
	}
	for i := range updates {
		if updates[i].LocalRef == "HEAD" {
			updates[i].LocalRef = "refs/heads/" + branch
		}
	}
	return updates, nil
}

// reportPushError renders the §5.9 rejection templates. Every branch
// names a command that succeeds in the state it is printed in (D3), and
// every one ends with git's own "push rejected" line so the user sees a
// familiar verdict rather than a tool-specific one.
func reportPushError(cmd *cobra.Command, ws *workspace, err error) error {
	stderr := cmd.ErrOrStderr()

	var markersErr *publish.MarkersPresentError
	var syncErr *publish.SyncRequiredError
	var emptyErr *publish.EmptyPublishError
	switch {
	case errors.Is(err, publish.ErrSyncInProgress):
		writeln(stderr, msgSyncInProgressPush)
		writeln(stderr, msgPushRejectedTrailer)

	case errors.As(err, &markersErr):
		writeln(stderr, pushMarkersMessage(markersErr.Paths))

	case errors.As(err, &emptyErr):
		writeln(stderr, pushEmptyDocsMessage(emptyErr.Branch, emptyErr.Head, emptyErr.DocsCount))

	case errors.Is(err, canonical.ErrMergeFailed):
		// A merge that could not run at all (a contended ref store, a
		// broken clone) is not a conflict, and printing the raw chain
		// would be exactly the §5.9 violation F-C2 found.
		writeln(stderr, pushMergeFailedMessage(ws.cloneDir(), causeLine(err)))

	case errors.As(err, &syncErr):
		if syncErr.Reason == publish.ReasonConflicts {
			writeln(stderr, pushConflictMessage(syncErr.Base, syncErr.Head, syncErr.Conflicts))
			break
		}
		writeln(stderr, pushSyncRequiredMessage(syncErr.Reason, syncErr.Base, syncErr.Head))

	case errors.Is(err, publish.ErrHistoryRewritten):
		writeln(stderr, rewrittenGuidance(cmd.Context(), ws))

	case errors.Is(err, pubdom.ErrUnreachable):
		writeln(stderr, pushUnreachableMessage(ws.config.DocsRepoURL, causeLine(err)))

	default:
		return err
	}
	return errAlreadyReported
}

// rewrittenGuidance builds the §5.3 case ④ message. It re-runs the
// docs-base-tree search the use case already attempted, for one reason:
// a runnable command must name a real commit, and only the search can
// say whether one exists. When it does not, the message says so rather
// than printing a command that would fail (D3, audit H5).
func rewrittenGuidance(ctx context.Context, ws *workspace) string {
	// The publication branch names the ref the listing command reads.
	// It comes from the clone whenever there is one — and there always
	// is when this message is reached, since publication could not have
	// got as far as case ④ otherwise.
	branch := canonical.DefaultBranch
	store, openErr := ws.openCanonical()
	if openErr == nil {
		branch = store.Branch()
	}

	base, hasBase, err := ws.statePort().LoadBase()
	if err != nil || !hasBase {
		return pushRewrittenMessage("", "", ws.cloneDir(), branch)
	}

	anchor := ""
	if openErr == nil {
		if found, ok, searchErr := store.FindCommitByDocsTree(ctx, base.Tree); searchErr == nil && ok {
			anchor = found
		}
	}
	return pushRewrittenMessage(base.Commit, anchor, ws.cloneDir(), branch)
}

// causeLine reduces an error chain to the one line worth showing, per
// §5.9's "never print raw Go error chains at user level".
func causeLine(err error) string {
	message := err.Error()
	if index := strings.LastIndex(message, ": "); index >= 0 {
		message = message[index+2:]
	}
	return strings.TrimSpace(message)
}

// --- post-checkout / post-merge / post-rewrite (§5.10) ----------------

// newPostCheckoutCmd is the one HEAD-moved hook that receives arguments
// worth reading. Git calls `post-checkout <prev> <new> <branch-flag>`,
// and the flag is 1 for a branch checkout and **0 for a file checkout**
// — `git checkout -- docs/api.md`, which moves no ref at all.
//
// Re-deriving there is wrong twice over (M8). HEAD did not move, so
// there is nothing to re-derive from; and a file checkout is precisely
// how a user restores one document, which changes the docs worktree
// without changing history — the state §5.10 step 1 already stands down
// for, reached by a route that used to skip the test.
func newPostCheckoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "post-checkout [prev] [new] [branch-flag]",
		Short: "Re-derive the docs base for the new HEAD",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if isFileCheckout(args) {
				return nil
			}
			return runRederiveHook(cmd)
		},
	}
}

// isFileCheckout reads git's third post-checkout argument. Anything
// else — too few arguments, an unrecognized value — is treated as a
// branch checkout, which is the answer that keeps the hook doing its
// job when git's contract is not what this expects.
func isFileCheckout(args []string) bool {
	return len(args) >= 3 && strings.TrimSpace(args[2]) == "0"
}

func newPostMergeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "post-merge [squash-flag]",
		Short: "Re-derive the docs base for the new HEAD",
		Args:  cobra.ArbitraryArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return runRederiveHook(cmd) },
	}
}

func newPostRewriteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "post-rewrite [command]",
		Short: "Re-derive the docs base for the rewritten HEAD",
		Args:  cobra.ArbitraryArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return runRederiveHook(cmd) },
	}
}

// newPostCommitCmd is a compatibility entry point, not a hook v0.2
// installs: commits do not move the base (§5.10), so there is nothing
// for it to do. It exists because a v0.1 hook file carries a
// `sanho hook post-commit` line, and until `sanho migrate` rewrites the
// hooks that line reaches this binary on every commit. The §8
// degradation contract covers it: a v1 workspace gets the migrate hint,
// anything else gets silence, and the exit is always 0.
func newPostCommitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "post-commit",
		Short: "Removed in v0.2; exits cleanly so v0.1 hook lines stay inert",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, _, _ = hookWorkspace(cmd.Context(), cmd)
			return nil
		},
	}
}

// runRederiveHook is the shared body of the three HEAD-moved hooks. It
// always exits 0 and prints exactly one line — only when the base
// actually changed (§5.10).
func runRederiveHook(cmd *cobra.Command) error {
	ctx := cmd.Context()
	ws, proceed, err := hookWorkspace(ctx, cmd)
	if err != nil || !proceed {
		return nil
	}

	base, outcome, err := rederiveBaseAfterHeadMoved(ctx, ws)
	if err != nil {
		debugf(cmd, "base re-derivation skipped: %v", err)
		return nil
	}
	switch outcome {
	case rederivedAdopted:
		writeln(cmd.ErrOrStderr(), baseRederivedMessage(base.Commit))
	case rederivedCleared:
		writeln(cmd.ErrOrStderr(), baseClearedMessage())
	}
	return nil
}

// recordWorkspaceState refreshes this workspace's registry entry after a
// state-changing operation (§5.7). The registry is observational —
// publication correctness never depends on it — so a failure to update
// it is reported under --verbose and never fails the operation that
// succeeded.
func recordWorkspaceState(ctx context.Context, ws *workspace) {
	base, hasBase, err := ws.statePort().LoadBase()
	if err != nil || !hasBase {
		return
	}
	file, err := openRegistry()
	if err != nil {
		return
	}
	_ = upsertWorkspace(ctx, file, ws, base)
}

// canonicalOrNil opens the clone for read paths that tolerate its
// absence.
func canonicalOrNil(ws *workspace) *canonical.Store {
	store, err := ws.openCanonical()
	if err != nil {
		return nil
	}
	return store
}
