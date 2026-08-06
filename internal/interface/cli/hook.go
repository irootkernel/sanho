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

	if blocked, err := preCommitGates(ctx, cmd, ws); err != nil {
		return err
	} else if blocked {
		return errAlreadyReported
	}

	warnStaleBase(ctx, cmd, ws)
	return nil
}

// preCommitGates runs the two conditions §5.6 lets block a commit, in
// the order that makes the state describe itself correctly.
//
// The sync note comes first: an unresolved sync leaves markers in the
// worktree by construction, so checking it first turns "you have
// markers" into the more useful "finish or abort the sync". Clearing a
// finished sync comes first of all, because a resolved sync must stop
// blocking the very commit that resolves it.
func preCommitGates(ctx context.Context, cmd *cobra.Command, ws *workspace) (blocked bool, err error) {
	state := ws.statePort()
	use := &docsync.UseCase{App: ws.appPort(), State: state}

	completed, err := use.CompleteIfResolved(ctx)
	if err != nil {
		return false, fmt.Errorf("check the sync state: %w", err)
	}
	if completed {
		writeln(cmd.ErrOrStderr(), syncCompletedMessage())
	}

	_, _, noteExists, err := state.LoadSyncNote()
	if err != nil {
		return false, fmt.Errorf("read the sync state: %w", err)
	}
	if noteExists {
		remaining, err := ws.repo.ScanWorktreeDocsForMarkers(ctx)
		if err != nil {
			return false, err
		}
		if len(remaining) > 0 {
			writeln(cmd.ErrOrStderr(), unresolvedSyncMessage(ws.config.DocsDir, remaining))
			return true, nil
		}
	}

	staged, err := ws.repo.ScanStagedDocsForMarkers(ctx)
	if err != nil {
		return false, err
	}
	if len(staged) > 0 {
		writeln(cmd.ErrOrStderr(), stagedMarkersMessage(staged))
		return true, nil
	}
	return false, nil
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

	base, hasBase, err := ws.statePort().LoadBase()
	if err != nil {
		return fmt.Errorf("read the base file: %w", err)
	}
	if !hasBase {
		return errors.New("no docs base is recorded")
	}
	if !base.Valid() {
		return errors.New("the recorded docs base is not a valid OID pair")
	}

	return os.WriteFile(messagePath, appendTrailers(message, base.Trailers()), 0644)
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

// messageHasBaseTrailer reports an existing docs-base trailer. Any line
// beginning with the key counts, so a message written by hand or carried
// through a cherry-pick is not stamped twice.
func messageHasBaseTrailer(message string) bool {
	for _, line := range strings.Split(message, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), provenance.TrailerBase+":") {
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
	// A sync that has already been resolved must not reject the push
	// that carries the resolution.
	if _, err := (&docsync.UseCase{App: ws.appPort(), State: state}).CompleteIfResolved(ctx); err != nil {
		return fmt.Errorf("check the sync state: %w", err)
	}

	store, err := ws.ensureCanonical(ctx)
	if err != nil {
		return reportPushError(cmd, ws, err)
	}
	use := &publish.UseCase{
		Canonical:   ws.canonicalPort(store),
		App:         ws.appPort(),
		State:       state,
		ActorEmail:  ws.config.ActorEmail,
		WorkspaceID: ws.config.WorkspaceID,
	}

	outcome, err := use.Run(ctx, updates)
	if err != nil {
		return reportPushError(cmd, ws, err)
	}
	if outcome.Published != "" {
		writeln(cmd.ErrOrStderr(), pushPublishedMessage(outcome.Published, outcome.Case.String()))
		recordWorkspaceState(ctx, ws)
	}
	return nil
}

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
	switch {
	case errors.Is(err, publish.ErrSyncInProgress):
		writeln(stderr, msgSyncInProgressPush)
		writeln(stderr, msgPushRejectedTrailer)

	case errors.As(err, &markersErr):
		writeln(stderr, pushMarkersMessage(markersErr.Paths))

	case errors.As(err, &syncErr):
		if syncErr.Reason == publish.ReasonConflicts {
			writeln(stderr, pushConflictMessage(syncErr.Base, syncErr.Head))
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
	base, hasBase, err := ws.statePort().LoadBase()
	if err != nil || !hasBase {
		return pushRewrittenMessage("", "", ws.cloneDir())
	}

	anchor := ""
	if store, openErr := ws.openCanonical(); openErr == nil {
		if found, ok, searchErr := store.FindCommitByDocsTree(ctx, base.Tree); searchErr == nil && ok {
			anchor = found
		}
	}
	return pushRewrittenMessage(base.Commit, anchor, ws.cloneDir())
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
