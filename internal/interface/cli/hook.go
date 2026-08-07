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
// markers" into the more useful "finish or abort the sync". Clearing a
// finished sync comes first of all, because a resolved sync must stop
// blocking the very commit that resolves it.
//
// Only markers block. A sync that was put aside rather than resolved
// leaves no markers behind, so it is reported and the commit proceeds:
// P2 makes the commit path non-blocking for everything except the two
// §5.6 gates, and stopping every unrelated commit until a stash is dealt
// with would punish the wrong action. The state is refused where it
// matters — `pre-push`, where shared state is created.
//
// The reporting is deliberately wider than the state that names it.
// ResolutionNotCommitted is the tidy case (docs clean, HEAD unmoved),
// but the commit that made the window dangerous was made while the docs
// were *dirty* for an unrelated reason, which classifies as
// ResolutionPending and printed nothing at all. Any unfinished sync with
// no markers left in the worktree now says so, unless the commit being
// prepared is itself carrying the resolution.
func preCommitGates(ctx context.Context, cmd *cobra.Command, ws *workspace) (blocked, syncOwed bool, err error) {
	state := ws.statePort()
	use := &docsync.UseCase{App: ws.appPort(), State: state}

	resolution, err := use.CompleteIfResolved(ctx)
	if err != nil {
		// A note sanho itself cannot read must never break `git commit`
		// (P2, Critical C1's failure class): say so, and let the staged
		// marker gate below answer for the commit's own content.
		if !errors.Is(err, docsync.ErrSyncNoteCorrupt) {
			return false, false, fmt.Errorf("check the sync state: %w", err)
		}
		writeln(cmd.ErrOrStderr(), syncNoteCorruptMessage(causeLine(err)))
		syncOwed = true
	}

	switch resolution {
	case docsync.ResolutionCompleted:
		writeln(cmd.ErrOrStderr(), syncCompletedMessage())
	case docsync.ResolutionNotCommitted, docsync.ResolutionPending:
		syncOwed = true
		remaining, scanErr := ws.repo.ScanWorktreeDocsForMarkers(ctx)
		if scanErr != nil {
			return false, syncOwed, scanErr
		}
		if len(remaining) > 0 {
			writeln(cmd.ErrOrStderr(), unresolvedSyncMessage(ws.config.DocsDir, remaining))
			return true, syncOwed, nil
		}
		reportOwedSync(ctx, cmd, ws, state)
	case docsync.ResolutionNoSync:
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

// reportOwedSync prints the "still owed a resolution" notice for a sync
// whose markers are gone from the worktree, and stays quiet when the
// commit being prepared is the resolution.
//
// The staged docs tree is what separates the two. A commit that changes
// one of the paths the merge conflicted on is the resolution arriving —
// the note is cleared by the next hook that runs, once the commit
// actually exists — and telling the user their sync was "never resolved
// by a commit" at exactly that moment would be false and would name an
// abort that throws the work away. A commit that leaves every conflicted
// path alone is the state this notice exists for.
func reportOwedSync(ctx context.Context, cmd *cobra.Command, ws *workspace, state statePort) {
	note, exists, err := state.LoadSyncNote()
	if err != nil || !exists {
		// An unreadable note has already been reported by the caller, and
		// a note that vanished between the two reads owes nothing.
		return
	}
	if resolutionIsStaged(ctx, ws, note) {
		return
	}
	writeln(cmd.ErrOrStderr(), syncNotCommittedMessage(note.PrevBase.Commit, note.Target.Commit))
}

// resolutionIsStaged reports whether the index already carries a change
// to one of the sync's conflicted paths.
//
// It is CompleteIfResolved's own test moved one step earlier: that one
// asks whether HEAD settled the conflict, this one asks whether the
// commit about to be made will. Every failure is answered "no", which
// costs at most a notice the user did not need — the commit path may
// never fail for a question sanho could not answer (P2), and nothing
// here opens a network connection.
func resolutionIsStaged(ctx context.Context, ws *workspace, note docsync.SyncNote) bool {
	indexTree, err := ws.repo.IndexDocsTree(ctx)
	if err != nil {
		return false
	}
	return stagedTreeSettles(ctx, ws, note, indexTree)
}

// stagedTreeSettles is resolutionIsStaged over an already-resolved index
// tree, for the caller that has one.
func stagedTreeSettles(ctx context.Context, ws *workspace, note docsync.SyncNote, indexTree string) bool {
	staged, err := ws.repo.DocsPathsChangedBetween(ctx, note.EntryDocsTree, indexTree, note.Conflicts)
	if err != nil {
		return false
	}
	return staged
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

	preview := previewSync(ctx, ws, store, base, head, headTree)
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
		writeln(cmd.ErrOrStderr(), commitMsgStampWarning(err.Error()))
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

	base, hasBase, err := stampBase(ctx, ws, inputs.IndexDocsTree)
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
// Ordinarily that is the base file, and during an unfinished sync it
// still is. The exception is the commit that RESOLVES the sync: its docs
// carry content merged from the target, so the target is what it derives
// from, and stamping the pre-sync base would be both untrue and harmful
// — a later checkout re-derives the base from the newest stamped commit,
// which would wind it back behind content the workspace already has and
// make the next push report a conflict nobody created.
//
// The converse matters just as much. A commit made during the window
// that leaves every conflicted path alone (the unrelated-note commit
// that started this whole investigation) derives from the pre-sync base,
// and stamping the target on it would let that same re-derivation put
// the base on canonical head with pre-merge docs beneath it — the
// dangerous state, reintroduced through the trailer.
//
// It is entirely local: one state file and one `git diff-tree`. §5.1's
// no-network contract for the commit path is untouched.
//
// indexTree is the docs tree the commit will carry — the same value
// ShouldStamp has just judged, passed in rather than re-read so that the
// decision to stamp and the value stamped describe one tree.
func stampBase(ctx context.Context, ws *workspace, indexTree string) (provenance.Base, bool, error) {
	state := ws.statePort()
	note, exists, noteErr := state.LoadSyncNote()
	if noteErr == nil && exists && stagedTreeSettles(ctx, ws, note, indexTree) {
		return note.Target, true, nil
	}

	base, hasBase, err := state.LoadBase()
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

// appendTrailers appends the trailer block, separated from the message
// body by exactly one blank line so git reads it as a trailer block.
func appendTrailers(message []byte, trailers []string) []byte {
	text := strings.TrimRight(string(message), "\n")
	return []byte(text + "\n\n" + strings.Join(trailers, "\n") + "\n")
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
		// is how F-C1's clobber stayed invisible.
		for _, oid := range outcome.PublishedOIDs {
			writeln(cmd.ErrOrStderr(), pushPublishedMessage(oid, outcome.Case.String()))
		}
		recordWorkspaceState(ctx, ws)
	}
	return nil
}

// prePushSyncGate settles the sync note before the push goes anywhere
// near canonical, and reports whether it refused.
//
// Three outcomes reject, each with its own wording, because each is a
// different state with a different next step. A note still carrying
// markers is the ordinary in-progress case. A note whose sync was put
// aside without a resolution commit is the state that used to be cleared
// silently. A note that cannot be parsed is refused on its existence
// alone.
//
// All three are refusals of a half-finished reconciliation, not of a
// dangerous base: the base now stays at the pre-sync value for the whole
// window, so a push that slipped past this gate would be evaluated
// against real history rather than mistaken for a fast-forward. That is
// the point of the ordering — this gate decides what the user is told,
// and the base decides what is at stake if it is ever wrong.
func prePushSyncGate(ctx context.Context, cmd *cobra.Command, ws *workspace, state statePort) (blocked bool, err error) {
	stderr := cmd.ErrOrStderr()

	resolution, err := (&docsync.UseCase{App: ws.appPort(), State: state}).CompleteIfResolved(ctx)
	if err != nil {
		if !errors.Is(err, docsync.ErrSyncNoteCorrupt) {
			return false, fmt.Errorf("check the sync state: %w", err)
		}
		writeln(stderr, syncNoteCorruptMessage(causeLine(err)))
		writeln(stderr, msgPushRejectedTrailer)
		return true, nil
	}

	switch resolution {
	case docsync.ResolutionNotCommitted:
		note, _, noteErr := state.LoadSyncNote()
		if noteErr != nil {
			return false, fmt.Errorf("read the sync state: %w", noteErr)
		}
		writeln(stderr, syncNotCommittedMessage(note.PrevBase.Commit, note.Target.Commit))
		writeln(stderr, msgPushRejectedTrailer)
		return true, nil
	case docsync.ResolutionPending:
		writeln(stderr, msgSyncInProgressPush)
		writeln(stderr, msgPushRejectedTrailer)
		return true, nil
	case docsync.ResolutionNoSync, docsync.ResolutionCompleted:
		return false, nil
	}
	return false, nil
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

func newPostCheckoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "post-checkout [prev] [new] [branch-flag]",
		Short: "Re-derive the docs base for the new HEAD",
		Args:  cobra.ArbitraryArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return runRederiveHook(cmd) },
	}
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

// runRederiveHook is the shared body of the three HEAD-moved hooks. It
// always exits 0 and prints exactly one line — only when the base
// actually changed (§5.10).
func runRederiveHook(cmd *cobra.Command) error {
	ctx := cmd.Context()
	ws, proceed, err := hookWorkspace(ctx, cmd)
	if err != nil || !proceed {
		return nil
	}

	base, changed, err := rederiveBaseAfterHeadMoved(ctx, ws)
	if err != nil {
		debugf(cmd, "base re-derivation skipped: %v", err)
		return nil
	}
	if changed {
		writeln(cmd.ErrOrStderr(), baseRederivedMessage(base.Commit))
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
