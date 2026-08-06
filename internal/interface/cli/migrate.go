package cli

// `sanho migrate` (sanho-v0.2.md §8): convert one v0.1 workspace to the
// v0.2 layout, in place, reversibly, without rewriting history.
//
// Three properties define the command.
//
// It is idempotent: a workspace that is already v2 is reported as
// migrated and exits 0, so re-running is safe and scripting it is safe.
//
// It is reversible: every file it rewrites or removes gets a `.bak`
// sibling first, and the legacy hash file is left in place as a
// read-only input. Rollback is "reinstall v0.1, restore the .bak files,
// restart the daemon" (§8 step 6) — which only works if nothing v0.1
// needs was consumed.
//
// It refuses rather than guesses: a live v0.1 transaction is the one
// piece of state v0.2 has no vocabulary for, so it is a precondition
// failure with the v0.1 binary named as the fix, not something to
// interpret (§8 step 1).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/irootkernel/sanho/internal/domain/provenance"
	"github.com/irootkernel/sanho/internal/infra/appgit"
	"github.com/irootkernel/sanho/internal/infra/canonical"
	"github.com/irootkernel/sanho/internal/infra/gitx"
	"github.com/irootkernel/sanho/internal/infra/registry"
	"github.com/irootkernel/sanho/internal/infra/wsstate"

	"github.com/spf13/cobra"
)

// backupSuffix marks the rollback copies §8 step 6 requires.
const backupSuffix = ".bak"

// legacyPendingFixFile and legacyTransactionPath are the two v0.1 states
// migration refuses on.
const (
	legacyPendingFixFile  = ".sanho_pending_fix"
	legacyTransactionPath = "sanho/pull-commit"
)

// v1Config is the subset of the v0.1 `.sanho.json` migration reads.
// Only these fields carry forward; socket_path is dropped outright,
// which is the schema change in one line.
type v1Config struct {
	SocketPath  string `json:"socket_path"`
	WorkspaceID string `json:"workspace_id"`
	Project     string `json:"project"`
	ActorEmail  string `json:"actor_email"`
	DocsDir     string `json:"docs_dir"`
}

// legacyDaemonState is the v0.1 daemon's `~/.sanho/state.json` schema
// (infra/state), read only for the project→URL mapping that the v0.2
// workspace config now has to carry itself.
type legacyDaemonState struct {
	DocsRepos map[string]struct {
		ID      string
		Path    string
		RepoURL string
	} `json:"docs_repos"`
	ProjectToDocsRepo map[string]string `json:"project_to_docs_repo"`
}

func newMigrateCmd() *cobra.Command {
	var docsRepoURL string

	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Convert a v0.1 workspace to the v0.2 layout",
		Long: `Convert this workspace from the v0.1 daemon layout to v0.2: write the v2
config and base file, create the private canonical clone, swap the seven v0.1
hook lines for the six v0.2 ones, and register the workspace.

Every rewritten file gets a .bak sibling. The command is idempotent and never
stops or reconfigures the v0.1 daemon; it prints the command to do that.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return runMigrate(cmd, docsRepoURL) },
	}
	cmd.Flags().StringVar(&docsRepoURL, "docs-repo-url", "",
		"Canonical docs repository URL (required when the legacy state does not record one)")
	return cmd
}

func runMigrate(cmd *cobra.Command, docsRepoURLFlag string) error {
	ctx := cmd.Context()

	root, err := requireGitWorktreeRoot(ctx)
	if err != nil {
		return err
	}

	config, err := wsstate.LoadConfig(root)
	if err != nil {
		return err
	}
	if config.SchemaVersion != 1 {
		// Already migrated: idempotent success (§8).
		writeln(cmd.OutOrStdout(), msgAlreadyMigrated)
		return nil
	}

	if err := refuseOnLiveV1State(ctx, root); err != nil {
		return err
	}

	legacy, err := readV1Config(root)
	if err != nil {
		return err
	}
	docsRepoURL, err := resolveDocsRepoURL(legacy.Project, docsRepoURLFlag)
	if err != nil {
		return err
	}

	if err := writeV2Config(root, legacy, docsRepoURL); err != nil {
		return err
	}
	ws, err := openWorkspace(ctx)
	if err != nil {
		return err
	}

	store, err := canonical.Ensure(ctx, ws.commonDir, docsRepoURL)
	if err != nil {
		return err
	}
	if err := store.Fetch(ctx); err != nil {
		return err
	}

	base, hasBase, err := migrateBase(ctx, cmd, ws, store)
	if err != nil {
		return err
	}

	if err := swapHooks(ctx, ws); err != nil {
		return err
	}
	if err := ensureGitignoreEntries(root); err != nil {
		return err
	}

	preserved, err := preserveLegacyDaemonState()
	if err != nil {
		return err
	}
	if preserved != "" {
		writeln(cmd.OutOrStdout(), "sanho: preserved the v0.1 daemon state at "+preserved)
	}

	file, err := openRegistry()
	if err != nil {
		return err
	}
	if err := file.Update(ctx, func(state *registry.State) error {
		return upsertProject(state, ws.config.Project, docsRepoURL)
	}); err != nil {
		return err
	}
	if err := upsertWorkspace(ctx, file, ws, base); err != nil {
		return err
	}

	renderMigrateSummary(cmd, ws, store, base, hasBase)
	return nil
}

// refuseOnLiveV1State implements §8 step 1. Both states are things the
// v0.1 binary knows how to finish and v0.2 does not know how to read; a
// half-interpreted transaction is exactly the wedge class the audit's
// C3 documented, so the answer is to stop.
func refuseOnLiveV1State(ctx context.Context, root string) error {
	if exists, err := pathExists(filepath.Join(root, legacyPendingFixFile)); err != nil {
		return err
	} else if exists {
		return errors.New(msgMigrateBlockedByTransaction)
	}

	path, err := gitx.New(root).Line(ctx, "rev-parse", "--git-path", legacyTransactionPath)
	if err != nil {
		return fmt.Errorf("resolve the v0.1 transaction directory: %w", err)
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	if exists, err := pathExists(path); err != nil {
		return err
	} else if exists {
		return errors.New(msgMigrateBlockedByTransaction)
	}
	return nil
}

func readV1Config(root string) (v1Config, error) {
	data, err := os.ReadFile(filepath.Join(root, wsstate.ConfigFileName))
	if err != nil {
		return v1Config{}, fmt.Errorf("read the v0.1 workspace config: %w", err)
	}
	var config v1Config
	if err := json.Unmarshal(data, &config); err != nil {
		return v1Config{}, fmt.Errorf("parse the v0.1 workspace config: %w", err)
	}
	if config.Project == "" {
		return v1Config{}, errors.New("the v0.1 workspace config records no project name")
	}
	if config.DocsDir == "" {
		config.DocsDir = appgit.DefaultDocsDir
	}
	return config, nil
}

// resolveDocsRepUrlFromLegacyState is where the daemon's last useful
// contribution is collected: v0.1 kept the docs repository URL only in
// the daemon's state file, indexed project → docs-repo id → URL, because
// the CLI never needed it. v0.2 has no daemon to ask, so the URL moves
// into the workspace config here.
func resolveDocsRepoURL(project, override string) (string, error) {
	if override != "" {
		return override, nil
	}

	home, err := resolveHome()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join(home, registry.StateFileName))
	if err != nil {
		return "", errors.New(msgMigrateNeedsURL)
	}

	var state legacyDaemonState
	if err := json.Unmarshal(data, &state); err != nil {
		return "", errors.New(msgMigrateNeedsURL)
	}
	repoID, ok := state.ProjectToDocsRepo[project]
	if !ok {
		return "", errors.New(msgMigrateNeedsURL)
	}
	repo, ok := state.DocsRepos[repoID]
	if !ok || repo.RepoURL == "" {
		return "", errors.New(msgMigrateNeedsURL)
	}
	return repo.RepoURL, nil
}

// writeV2Config backs the v0.1 config up and replaces it.
func writeV2Config(root string, legacy v1Config, docsRepoURL string) error {
	configPath := filepath.Join(root, wsstate.ConfigFileName)
	if err := backupFile(configPath); err != nil {
		return err
	}

	workspaceID := legacy.WorkspaceID
	if workspaceID == "" {
		workspaceID = registryKey(legacy.Project, root)
	}
	return wsstate.SaveConfig(root, wsstate.Config{
		WorkspaceID: workspaceID,
		Project:     legacy.Project,
		DocsRepoURL: docsRepoURL,
		ActorEmail:  legacy.ActorEmail,
		DocsDir:     legacy.DocsDir,
	})
}

// migrateBase carries the v0.1 base forward (§8 step 4).
//
// The legacy hash file holds a bare commit OID under *identity*
// semantics — "my docs tree equals canonical X" — which makes X a
// correct *ancestry* base for edits made on top of it (§5.1 legacy
// coexistence). Its tree is resolved from the freshly fetched canonical
// when the commit still exists; when it does not, the base is recorded
// with an empty tree and the rewrite is called out, because a wrong tree
// would be worse than none (it is the re-anchoring key, D2).
//
// The legacy file itself is copied, never consumed: LoadBase prefers the
// v2 file, so leaving it intact costs nothing and keeps rollback whole.
func migrateBase(ctx context.Context, cmd *cobra.Command, ws *workspace, store *canonical.Store) (provenance.Base, bool, error) {
	legacyPath := filepath.Join(ws.root, wsstate.LegacyHashFileName)
	if exists, err := pathExists(legacyPath); err != nil {
		return provenance.Base{}, false, err
	} else if exists {
		if err := backupFile(legacyPath); err != nil {
			return provenance.Base{}, false, err
		}
	}

	base, hasBase, err := wsstate.LoadBase(ws.root)
	if err != nil || !hasBase {
		// No usable v0.1 base. Deriving one from history is exactly what
		// the post-checkout hook and `sanho doctor --fix` do, and both
		// succeed later, so migration does not stop here.
		writeln(cmd.ErrOrStderr(),
			"sanho: no v0.1 docs base was found; run 'sanho sync' to establish one")
		return provenance.Base{}, false, nil
	}

	known, err := store.ResolveCommit(ctx, base.Commit)
	if err != nil {
		return provenance.Base{}, false, err
	}
	if !known {
		writef(cmd.ErrOrStderr(),
			"sanho: the recorded docs base %s is no longer in the canonical repository; canonical history may have been rewritten. Run 'sanho sync' to reconcile.\n",
			shortOID(base.Commit))
		return base, true, wsstate.SaveBase(ws.root, base)
	}

	if tree, treeErr := commitTreeInClone(ctx, store, base.Commit); treeErr == nil {
		base.Tree = tree
	}
	return base, true, wsstate.SaveBase(ws.root, base)
}

// commitTreeInClone resolves a canonical commit's tree. The canonical
// repository is docs-only, so its root tree is the docs tree.
func commitTreeInClone(ctx context.Context, store *canonical.Store, commit string) (string, error) {
	return gitx.New(store.Dir()).Line(ctx, "rev-parse", "--verify", commit+"^{tree}")
}

// swapHooks removes the seven v0.1 lines and installs the six v0.2 ones.
// Removal first, so a `pre-push` file carrying both forms ends up with
// exactly one line rather than two (audit L3).
func swapHooks(ctx context.Context, ws *workspace) error {
	if err := ws.repo.RemoveHooks(ctx); err != nil {
		return err
	}
	return ws.repo.InstallHooks(ctx)
}

// legacyStateBackupName is where preserveLegacyDaemonState keeps the
// v0.1 daemon's state file.
const legacyStateBackupName = "state.json.v1.bak"

// preserveLegacyDaemonState copies the v0.1 daemon's ~/.sanho/state.json
// to state.json.v1.bak before the registry conversion rewrites the file
// — and its ordinary .bak — with the v2 schema in place. Without this
// copy, migrate's own conversion would destroy the only rollback source
// for the daemon state. Idempotent: a state.json that is absent or
// already v2 leaves nothing to preserve, and an existing v1 backup is
// never overwritten (a second run sees v2 and does not reach that
// check). Returns the backup path when a copy was made.
func preserveLegacyDaemonState() (string, error) {
	home, err := resolveHome()
	if err != nil {
		return "", err
	}
	statePath := filepath.Join(home, registry.StateFileName)
	data, err := os.ReadFile(statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read %s for the v0.1 backup: %w", statePath, err)
	}
	var versioned struct {
		Version int `json:"version"`
	}
	// The v0.1 daemon schema has no version field; the v2 registry always
	// writes version 2. Anything unparsable is preserved too — better a
	// spurious backup than a destroyed one.
	if json.Unmarshal(data, &versioned) == nil && versioned.Version == 2 {
		return "", nil
	}
	backup := filepath.Join(home, legacyStateBackupName)
	if _, err := os.Stat(backup); err == nil {
		return "", nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("inspect %s: %w", backup, err)
	}
	if err := os.WriteFile(backup, data, 0600); err != nil {
		return "", fmt.Errorf("write %s: %w", backup, err)
	}
	return backup, nil
}

// backupFile copies path to path.bak, preserving permissions. An absent
// source is not an error: there is nothing to roll back to.
func backupFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read %s for backup: %w", path, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("inspect %s for backup: %w", path, err)
	}
	backup := path + backupSuffix
	if err := os.WriteFile(backup, data, info.Mode().Perm()); err != nil {
		return fmt.Errorf("write %s: %w", backup, err)
	}
	return nil
}

func renderMigrateSummary(cmd *cobra.Command, ws *workspace, store *canonical.Store, base provenance.Base, hasBase bool) {
	out := cmd.OutOrStdout()
	writeln(out, "sanho: migrated this workspace to the v0.2 layout")
	writef(out, "  workspace : %s\n", ws.root)
	writef(out, "  project   : %s\n", ws.config.Project)
	writef(out, "  docs repo : %s (branch %s)\n", store.URL(), store.Branch())
	writef(out, "  clone     : %s\n", store.Dir())
	if hasBase {
		writef(out, "  docs base : %s\n", shortOID(base.Commit))
	} else {
		writeln(out, "  docs base : (none yet)")
	}
	writef(out, "  hooks     : %d installed, v0.1 lines removed\n", len(appgit.Hooks()))
	writef(out, "  backups   : %s%s, %s%s\n",
		wsstate.ConfigFileName, backupSuffix, wsstate.LegacyHashFileName, backupSuffix)

	writeln(out)
	for _, line := range daemonStopInstructions {
		writeln(out, line)
	}
}
