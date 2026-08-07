package cli

// `sanho sync` and `sanho pull` (sanho-v0.2.md §5.5): two verbs, two
// intents. Pull consumes when there is nothing local to preserve; sync
// reconciles when there is.

import (
	"context"
	"fmt"
	"strings"

	"github.com/irootkernel/sanho/internal/infra/gitx"
	"github.com/irootkernel/sanho/internal/usecase/docsync"

	"github.com/spf13/cobra"
)

// syncJSON is the stable `--json` schema for both `sanho sync` and
// `sanho pull`:
//
//	{
//	  "status":     "up_to_date" | "synced" | "conflicts" | "aborted",
//	  "base":       {"commit": "<oid>", "tree": "<oid>"} | null,
//	  "commit":     "<oid>",            // "" when nothing was committed
//	  "conflicts":  ["docs/api.md"]     // [] unless status is conflicts
//	}
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
}

// statusAborted is the one syncJSON status with no docsync.Status
// counterpart: `--abort` is a distinct outcome, not a kind of sync.
const statusAborted = "aborted"

func newSyncCmd() *cobra.Command {
	var (
		abort      bool
		rebaseOnto string
		asJSON     bool
	)

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Reconcile local docs with the canonical repository",
		Long: `Fetch canonical docs, three-way merge them with your local docs, and lay a
base-update commit under your work.

A clean merge produces one ordinary commit ('docs: sync to <oid>') authored by
you. A conflicted merge writes standard conflict markers into the docs
directory; resolve them, 'git add', and 'git commit' as you would for any
merge. 'sanho sync --abort' restores the pre-sync state.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSync(cmd, abort, rebaseOnto, asJSON)
		},
	}
	cmd.Flags().BoolVar(&abort, "abort", false, "Undo an in-progress conflicted sync")
	cmd.Flags().StringVar(&rebaseOnto, "rebase-onto", "", "Reconcile against an explicit canonical commit (rewrite recovery)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print machine-readable JSON")
	return cmd
}

func runSync(cmd *cobra.Command, abort bool, rebaseOnto string, asJSON bool) error {
	ctx := cmd.Context()
	ws, err := requireV2Workspace(ctx)
	if err != nil {
		return finishCommand(cmd, nil, asJSON, err)
	}

	if abort {
		if rebaseOnto != "" {
			return fmt.Errorf("--abort and --rebase-onto cannot be combined")
		}
		return runSyncAbort(cmd, ws, asJSON)
	}

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

// runSyncAbort implements §5.5 step 7. Abort needs no network and no
// canonical clone: it restores the docs worktree from HEAD and puts the
// base file back, which is why it cannot fail once a note exists
// (guidance closure by construction, D3) — including when the note
// itself is unreadable, which is now lossless too: the conflicted sync
// left the base where it found it, so there is nothing an unread note
// could have told the abort to restore.
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

// renderSyncResult prints the §5.9 templates. A conflicted sync uses
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
	cmd.Flags().BoolVar(&withCommit, "commit", false, "Record the update as a 'docs: sync to <oid>' commit")
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
// repaired here rather than being told to re-init (§5.2).
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
