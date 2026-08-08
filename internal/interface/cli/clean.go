package cli

// `sanho clean` (sanho-v0.2.md §5.8): remove sanho from this workspace.
//
// --dry-run is STRICTLY READ-ONLY, and that is the point of the split
// below rather than an aspiration. Audit M4 was a v0.1 dry-run that
// deleted state while reporting what it "would" do; here the plan is
// computed by cleanPlan, which performs no writes at all, and only
// applyCleanPlan touches anything. The regression test asserts that a
// dry-run leaves every file byte-identical — including the sync note and
// the registry.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/irootkernel/sanho/internal/infra/wsstate"

	"github.com/spf13/cobra"
)

// cleanPlan is what a clean would do. Building it is a pure read.
type cleanPlan struct {
	// files are workspace files to delete, absolute.
	files []string
	// directories are trees to delete, absolute (the canonical clone,
	// and the docs directory under --remove-docs).
	directories []string
	// registryKey is the entry to drop.
	registryKey string
	// hooks names the hook lines that will be removed, for the summary.
	hookCount int
}

func newCleanCmd() *cobra.Command {
	var dryRun, removeDocs, confirmed bool

	cmd := &cobra.Command{
		Use:   "clean",
		Short: "Remove sanho state, hooks, and the private clone from this workspace",
		Long: `Remove this workspace's sanho registration: the six hook lines, the config and
base files, the private canonical clone, and the registry entry.

--dry-run prints what would be removed and changes nothing at all.
The real run requires -y, because it is not reversible.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runClean(cmd, dryRun, removeDocs, confirmed)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print what would be removed; change nothing")
	cmd.Flags().BoolVar(&removeDocs, "remove-docs", false, "Also delete the docs directory")
	cmd.Flags().BoolVarP(&confirmed, "yes", "y", false, "Confirm the removal")
	return cmd
}

func runClean(cmd *cobra.Command, dryRun, removeDocs, confirmed bool) error {
	ctx := cmd.Context()

	// A v0.1 workspace is cleanable: removal is exactly what v0.2 can do
	// for it without interpreting any v0.1 state, and RemoveHooks knows
	// the legacy lines. So openWorkspace's v1 signal is tolerated here.
	ws, err := openWorkspace(ctx)
	if err != nil && !errors.Is(err, errV1Workspace) {
		return err
	}

	plan, err := cleanPlan{}.build(ctx, ws, removeDocs)
	if err != nil {
		return err
	}

	if dryRun {
		renderCleanPlan(cmd.OutOrStdout(), plan, true)
		return nil
	}
	if !confirmed {
		return fmt.Errorf("%s", msgCleanNeedsConfirmation)
	}

	// Refuse while a sync is owed: cleaning would strand a docs worktree
	// full of markers with no note explaining them. The named command
	// cannot fail once its precondition holds (§5.5 step 7).
	if inProgress, err := ws.statePort().SyncInProgress(); err != nil {
		return err
	} else if inProgress {
		return fmt.Errorf("%s", msgCleanSyncInProgress)
	}

	if err := applyCleanPlan(ctx, ws, plan); err != nil {
		return err
	}
	renderCleanPlan(cmd.OutOrStdout(), plan, false)
	return nil
}

// build computes the plan. Every call here is a read: Stat, and the hook
// inventory. Nothing is opened for writing, nothing is created, and the
// registry is not even locked — a --dry-run must not so much as touch
// the lock file's mtime.
func (cleanPlan) build(ctx context.Context, ws *workspace, removeDocs bool) (cleanPlan, error) {
	plan := cleanPlan{registryKey: ws.registryKey(), hookCount: 0}

	for _, name := range []string{
		wsstate.ConfigFileName,
		wsstate.BaseFileName,
		wsstate.LegacyHashFileName,
		".sanho_pending_fix",
	} {
		path := filepath.Join(ws.root, name)
		if exists, err := pathExists(path); err != nil {
			return cleanPlan{}, err
		} else if exists {
			plan.files = append(plan.files, path)
		}
	}

	if exists, err := pathExists(ws.cloneDir()); err != nil {
		return cleanPlan{}, err
	} else if exists {
		plan.directories = append(plan.directories, ws.cloneDir())
	}

	if removeDocs {
		docs := filepath.Join(ws.root, filepath.FromSlash(ws.config.DocsDir))
		if exists, err := pathExists(docs); err != nil {
			return cleanPlan{}, err
		} else if exists {
			plan.directories = append(plan.directories, docs)
		}
	}

	states, err := ws.repo.HooksStatus(ctx)
	if err != nil {
		return cleanPlan{}, err
	}
	for _, state := range states {
		if state.Installed || len(state.Legacy) > 0 {
			plan.hookCount++
		}
	}
	return plan, nil
}

// applyCleanPlan performs the removals.
//
// Hooks go first, because they are the part that keeps *acting* on the
// repository: once they are gone, an interrupted clean leaves an inert
// workspace rather than hooks pointing at state that no longer exists.
// The registry entry goes last, for the symmetric reason — it is the
// record that this workspace was managed, and dropping it while its
// files remain would hide a half-cleaned checkout from `sanho state`.
func applyCleanPlan(ctx context.Context, ws *workspace, plan cleanPlan) error {
	if err := ws.repo.RemoveHooks(ctx); err != nil {
		return err
	}
	if err := wsstate.ClearSyncNote(ws.gitDir); err != nil {
		return err
	}
	for _, path := range plan.files {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove %s: %w", path, err)
		}
	}
	for _, dir := range plan.directories {
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("remove %s: %w", dir, err)
		}
	}

	file, err := openRegistry()
	if err != nil {
		return err
	}
	return removeWorkspace(ctx, file, ws)
}

func renderCleanPlan(out io.Writer, plan cleanPlan, dryRun bool) {
	verb, tense := "removed", ""
	if dryRun {
		verb, tense = "would remove", " (dry run — nothing was changed)"
	}
	writef(out, "sanho: %s%s\n", verb, tense)

	writef(out, "  hooks         : %d hook file(s) carry sanho lines\n", plan.hookCount)
	for _, path := range plan.files {
		writef(out, "  file          : %s\n", path)
	}
	for _, dir := range plan.directories {
		writef(out, "  directory     : %s\n", dir)
	}
	writef(out, "  registry entry: %s\n", plan.registryKey)
}

func pathExists(path string) (bool, error) {
	switch _, err := os.Lstat(path); {
	case err == nil:
		return true, nil
	case os.IsNotExist(err):
		return false, nil
	default:
		return false, fmt.Errorf("inspect %s: %w", path, err)
	}
}
