package cli

// `sanho sync` and `sanho pull` (docs/architecture.md "Synchronization"): two verbs, two
// intents. Pull consumes when there is nothing local to preserve; sync
// reconciles when there is.

import (
	"context"
	"errors"
	"strings"

	"github.com/irootkernel/sanho/internal/infra/gitx"
	"github.com/irootkernel/sanho/internal/usecase/docsync"

	"github.com/spf13/cobra"
)

// syncJSON is the stable `--json` schema for both `sanho sync` and
// `sanho pull`:
//
//	{
//	  "status":       "up_to_date" | "synced" | "conflicts"
//	                  | "completed" | "aborted",
//	  "base":         {"commit": "<oid>", "tree": "<oid>"} | null,
//	  "commit":       "<oid>",          // "" when nothing was committed
//	  "conflicts":    ["docs/api.md"],  // [] unless status is conflicts
//	  "merge_drift":  0                 // --continue only: how many docs
//	                                    // paths the completed state
//	                                    // differs from the merge result by
//	}
//
// `completed` is `--continue`'s outcome and carries the base the
// workspace has just adopted; `commit` stays empty there, because
// completing a sync creates nothing (P3).
//
// A conflicted sync is a *success*: it did what it was asked to do and
// the markers are in the worktree. It is reported as status "conflicts"
// with exit 0, so an agent reads the outcome from the document rather
// than from the exit code.
type syncJSON struct {
	Status    string    `json:"status"`
	Base      *baseJSON `json:"base"`
	Commit    string    `json:"commit"`
	Conflicts []string  `json:"conflicts"`
	// MergeDrift is how many docs paths the completed state differs from
	// the merge result by; only `--continue` ever reports a non-zero one.
	MergeDrift int `json:"merge_drift"`
}

// statusAborted and statusCompleted are the two syncJSON statuses with
// no docsync.Status counterpart: `--abort` and `--continue` are distinct
// outcomes, not kinds of sync.
const (
	statusAborted   = "aborted"
	statusCompleted = "completed"
)

func newSyncCmd() *cobra.Command {
	var (
		abort      bool
		proceed    bool
		rebaseOnto string
		asJSON     bool
	)

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Reconcile local docs with the canonical repository",
		Long: `Fetch canonical docs, three-way merge them with your local docs, and lay a
base-update commit under your work.

A clean merge produces one ordinary commit ('[SANHO] Sync docs to <oid>') authored by
you. A conflicted merge writes standard conflict markers into the docs
directory; resolve them, 'git add' and 'git commit' as you would for any merge,
then run 'sanho sync --continue' to complete the sync. 'sanho sync --abort'
restores the pre-sync state instead.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSync(cmd, syncFlags{abort: abort, proceed: proceed, rebaseOnto: rebaseOnto, asJSON: asJSON})
		},
	}
	cmd.Flags().BoolVar(&abort, "abort", false, "Undo an in-progress conflicted sync")
	cmd.Flags().BoolVar(&proceed, "continue", false, "Complete the conflicted sync you have resolved and committed")
	cmd.Flags().StringVar(&rebaseOnto, "rebase-onto", "", "Reconcile against an explicit canonical commit (rewrite recovery)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print machine-readable JSON")
	return cmd
}

// syncFlags is `sanho sync`'s flag set. The three mode flags are
// mutually exclusive, and passing them together is a mistake worth
// naming rather than resolving by precedence.
type syncFlags struct {
	abort      bool
	proceed    bool
	rebaseOnto string
	asJSON     bool
}

// mode reports which of the three exclusive modes was asked for, or an
// error naming the combination.
func (f syncFlags) mode() (abort, proceed bool, err error) {
	switch {
	case f.abort && f.proceed:
		return false, false, errors.New("--abort and --continue cannot be combined")
	case f.abort && f.rebaseOnto != "":
		return false, false, errors.New("--abort and --rebase-onto cannot be combined")
	case f.proceed && f.rebaseOnto != "":
		return false, false, errors.New("--continue and --rebase-onto cannot be combined")
	}
	return f.abort, f.proceed, nil
}

func runSync(cmd *cobra.Command, flags syncFlags) error {
	ctx := cmd.Context()
	asJSON := flags.asJSON

	// The flag-combination refusal owes the JSON contract envelope like every
	// other `--json` failure. It used to return bare, so an agent that
	// mis-combined the flags got prose on stderr, nothing on stdout, and
	// no code to branch on — from the one command whose whole point is
	// being driven by a program.
	abort, proceed, err := flags.mode()
	if err != nil {
		return finishCommand(cmd, nil, asJSON, err)
	}

	ws, err := requireV2Workspace(ctx)
	if err != nil {
		return finishCommand(cmd, nil, asJSON, err)
	}

	switch {
	case abort:
		return runSyncAbort(cmd, ws, asJSON)
	case proceed:
		return runSyncContinue(cmd, ws, asJSON)
	}
	rebaseOnto := flags.rebaseOnto

	use, err := ws.docsyncUseCase(ctx)
	if err != nil {
		return finishCommand(cmd, ws, asJSON, err)
	}
	result, err := use.Run(ctx, docsync.Options{RebaseOnto: rebaseOnto})
	if err != nil {
		return finishCommand(cmd, ws, asJSON, err)
	}

	recordWorkspaceState(ctx, ws)
	if asJSON {
		return writeJSON(cmd.OutOrStdout(), buildSyncJSON(result))
	}
	renderSyncResult(cmd, ws, result)
	return nil
}

// runSyncAbort implements the synchronization contract. Abort needs no network and no
// canonical clone: it restores the docs worktree from HEAD and settles
// the base file, which is what makes it the exit from every broken sync
// state — including one whose note is unreadable, where it clears the
// base rather than leaving behind one it cannot vouch for.
//
// The contract is that nothing about the SYNC can make it fail: no ref
// moves, no commit is created, no network is opened, and every step is
// idempotent, so an interrupted abort is simply re-run. It is not a
// promise that the process cannot fail at all — a filesystem that will
// not accept a write, or a docs checkout git itself refuses, still
// surfaces. The earlier "cannot fail" wording claimed the second thing
// while only the first is true.
func runSyncAbort(cmd *cobra.Command, ws *workspace, asJSON bool) error {
	ctx := cmd.Context()
	use := &docsync.UseCase{App: ws.appPort(), State: ws.statePort()}

	if _, err := use.Abort(ctx); err != nil {
		return finishCommand(cmd, ws, asJSON, err)
	}
	recordWorkspaceState(ctx, ws)

	if asJSON {
		return writeJSON(cmd.OutOrStdout(), syncJSON{Status: statusAborted, Conflicts: []string{}})
	}
	writeln(cmd.OutOrStdout(), syncAbortedMessage(untrackedDocs(ctx, ws)))
	return nil
}

// runSyncContinue implements the synchronization contract: the explicit completion of a
// conflicted sync.
//
// It needs no network and no canonical clone. Everything it decides is
// in two local files and the docs worktree, which is what lets it be the
// one command that finishes a sync from an offline machine — and what
// keeps it, like the commit path, unable to fail for a canonical it
// could not reach.
func runSyncContinue(cmd *cobra.Command, ws *workspace, asJSON bool) error {
	ctx := cmd.Context()
	use := &docsync.UseCase{App: ws.appPort(), State: ws.statePort()}

	result, err := use.Continue(ctx)
	if err != nil {
		return finishCommand(cmd, ws, asJSON, err)
	}
	recordWorkspaceState(ctx, ws)

	if asJSON {
		return writeJSON(cmd.OutOrStdout(), syncJSON{
			Status:     statusCompleted,
			Base:       &baseJSON{Commit: result.Base.Commit, Tree: result.Base.Tree},
			Conflicts:  []string{},
			MergeDrift: result.MergeDrift,
		})
	}
	writeln(cmd.OutOrStdout(), syncCompletedMessage(result.Base.Commit, result.MergeDrift))
	return nil
}

// untrackedDocs lists docs files git does not track, for the abort
// notice. A failure to ask is a reason to say nothing extra, never a
// reason to fail an abort that already succeeded.
func untrackedDocs(ctx context.Context, ws *workspace) []string {
	res, err := gitx.New(ws.root).RunExit(ctx,
		"ls-files", "--others", "--exclude-standard", "-z", "--", ws.config.DocsDir)
	if err != nil || res.ExitCode != 0 {
		return nil
	}
	var paths []string
	for _, path := range strings.Split(string(res.Stdout), "\x00") {
		if strings.TrimSpace(path) != "" {
			paths = append(paths, path)
		}
	}
	return paths
}

// renderSyncResult prints the guidance contract templates. A conflicted sync uses
// template 2 verbatim and still exits 0: the sync succeeded, and the
// resolution is now the user's ordinary git work.
func renderSyncResult(cmd *cobra.Command, ws *workspace, result docsync.Result) {
	out := cmd.OutOrStdout()
	switch result.Status {
	case docsync.StatusConflicts:
		writeln(out, syncConflictMessage(ws.config.DocsDir, result.Conflicts))
	case docsync.StatusSynced:
		writeln(out, syncedMessage(result.NewBase.Commit, result.CommitOID))
	default:
		writeln(out, upToDateMessage(result.NewBase.Commit))
	}
}

func buildSyncJSON(result docsync.Result) syncJSON {
	out := syncJSON{
		Status:    result.Status.String(),
		Commit:    result.CommitOID,
		Conflicts: orEmpty(result.Conflicts),
	}
	if !result.NewBase.IsZero() {
		out.Base = &baseJSON{Commit: result.NewBase.Commit, Tree: result.NewBase.Tree}
	}
	return out
}

// --- pull -------------------------------------------------------------

func newPullCmd() *cobra.Command {
	var withCommit, asJSON bool

	cmd := &cobra.Command{
		Use:   "pull",
		Short: "Fast-forward local docs to the canonical repository",
		Long: `Replace the local docs directory with canonical's, provided the local docs
have no edits relative to the recorded base. Refuses otherwise and points at
'sanho sync', which reconciles instead of overwriting.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runPull(cmd, withCommit, asJSON)
		},
	}
	cmd.Flags().BoolVar(&withCommit, "commit", false, "Record the update as a '[SANHO] Sync docs to <oid>' commit")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print machine-readable JSON")
	return cmd
}

func runPull(cmd *cobra.Command, withCommit, asJSON bool) error {
	ctx := cmd.Context()
	ws, err := requireV2Workspace(ctx)
	if err != nil {
		return finishCommand(cmd, nil, asJSON, err)
	}

	use, err := ws.docsyncUseCase(ctx)
	if err != nil {
		return finishCommand(cmd, ws, asJSON, err)
	}
	result, err := use.Pull(ctx, withCommit)
	if err != nil {
		return finishCommand(cmd, ws, asJSON, err)
	}

	recordWorkspaceState(ctx, ws)
	if asJSON {
		return writeJSON(cmd.OutOrStdout(), buildSyncJSON(result))
	}
	if result.Status == docsync.StatusUpToDate {
		writeln(cmd.OutOrStdout(), upToDateMessage(result.NewBase.Commit))
		return nil
	}
	writeln(cmd.OutOrStdout(), pulledMessage(result.NewBase.Commit, result.CommitOID))
	return nil
}

// docsyncUseCase wires sync and pull. Both are write paths, so they use
// Ensure rather than Open: a workspace whose clone is missing is
// repaired here rather than being told to re-init (the private-clone contract).
func (w *workspace) docsyncUseCase(ctx context.Context) (*docsync.UseCase, error) {
	store, err := w.ensureCanonical(ctx)
	if err != nil {
		return nil, err
	}
	return &docsync.UseCase{
		Canonical: w.canonicalPort(store),
		App:       w.appPort(),
		State:     w.statePort(),
	}, nil
}
