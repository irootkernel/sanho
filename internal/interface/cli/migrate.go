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
	"sort"
	"strings"
	"time"

	"github.com/irootkernel/sanho/internal/domain/provenance"
	"github.com/irootkernel/sanho/internal/infra/appgit"
	"github.com/irootkernel/sanho/internal/infra/canonical"
	"github.com/irootkernel/sanho/internal/infra/fsx"
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
	var manageHooks bool

	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Convert a v0.1 workspace to the v0.2 layout",
		Long: `Convert this workspace from the v0.1 daemon layout to v0.2: write the v2
config and base file, create the private canonical clone, swap the seven v0.1
hook lines for the six v0.2 ones, and register the workspace.

Every rewritten file gets a .bak sibling. The command is idempotent and never
stops or reconfigures the v0.1 daemon; it prints the command to do that.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return runMigrate(cmd, docsRepoURL, manageHooks) },
	}
	cmd.Flags().StringVar(&docsRepoURL, "docs-repo-url", "",
		"Canonical docs repository URL (required when the legacy state does not record one)")
	cmd.Flags().BoolVar(&manageHooks, "manage-custom-hooks", false,
		"Manage a repository-local custom core.hooksPath or recognized Husky 9 hooks")
	return cmd
}

func runMigrate(cmd *cobra.Command, docsRepoURLFlag string, manageHooks bool) error {
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
		// Idempotence is a claim about the whole migration, not about one
		// file (F-H8b). A run interrupted after the config write used to
		// report "already migrated" while the clone or the hooks were
		// still missing — a workspace that looks migrated and behaves like
		// nothing at all. Re-enter unless every step actually landed.
		complete, checkErr := migrationComplete(ctx, root, config)
		if checkErr != nil {
			return checkErr
		}
		if complete {
			writeln(cmd.OutOrStdout(), msgAlreadyMigrated)
			return nil
		}
	}
	repo := appgit.New(root, config.DocsDir, gitx.New(root))
	hooks, err := detectHookConfig(ctx, repo, manageHooks)
	if err != nil {
		return err
	}

	if err := refuseOnLiveV1State(ctx, root); err != nil {
		return err
	}

	legacy, err := readV1Config(root)
	if err != nil {
		return err
	}
	docsRepoURL, err := resolveDocsRepoURL(legacy.Project, docsRepoURLFlag, config.DocsRepoURL)
	if err != nil {
		return err
	}

	// The order below is the resumability order (F-H8b): the v2 config
	// is written LAST, so an interruption anywhere leaves a workspace
	// that is still recognizably v0.1 and that re-running completes.
	// Writing it first — the previous order — made every failure produce
	// a workspace v0.2 refused to migrate and v0.1 could no longer read.
	site, err := openMigrationSite(ctx, root, legacy.DocsDir, docsRepoURL, hooks)
	if err != nil {
		return err
	}
	preserved, err := preserveLegacyDaemonState()
	if err != nil {
		return err
	}
	store, err := canonical.Ensure(ctx, site.commonDir, docsRepoURL)
	if err != nil {
		return err
	}
	if !store.Fresh() {
		if err := store.Fetch(ctx); err != nil {
			return err
		}
	}

	// Defence in depth: a v0.1 workspace cannot have a v0.2 sync note, so
	// finding one means the layout is further along than the config says
	// — a partially rolled-back upgrade, or a config restored from a
	// backup. Migration rewrites the base, which is the one file an
	// unfinished sync is holding still, so it refuses for exactly the
	// reason `sanho init` does and names the same two exits.
	if _, syncing, noteErr := wsstate.LoadSyncNote(site.gitDir); syncing || noteErr != nil {
		return errors.New(msgCleanSyncInProgress)
	}

	base, hasBase, err := migrateBase(ctx, cmd, root, site.statePort(), store)
	if err != nil {
		return err
	}

	file, err := openRegistry()
	if err != nil {
		return err
	}
	convertedRegistry, err := file.ConvertLegacy(ctx)
	if err != nil {
		return err
	}
	if err := updateRegistry(ctx, file, func(state *registry.State) error {
		if err := upsertProject(state, legacy.Project, docsRepoURL); err != nil {
			return err
		}
		pruneWorkspaceAliases(state, legacy.Project, root, registryKey(legacy.Project, root))
		state.Workspaces[registryKey(legacy.Project, root)] = registry.Workspace{
			Project:       legacy.Project,
			LocalPath:     root,
			BaseCommit:    base.Commit,
			BaseTree:      base.Tree,
			ActorEmail:    legacy.ActorEmail,
			LastUpdatedAt: time.Now().UTC(),
		}
		return nil
	}); err != nil {
		return err
	}

	hookBackups, err := swapHooks(ctx, site.repo)
	if err != nil {
		return err
	}
	if err := ensureGitignoreEntries(root); err != nil {
		return err
	}
	if err := writeV2Config(root, legacy, docsRepoURL, hooks); err != nil {
		return err
	}

	if preserved != "" {
		writeln(cmd.OutOrStdout(), "sanho: preserved the v0.1 daemon state at "+preserved)
	}
	if convertedRegistry {
		writeln(cmd.OutOrStdout(), "sanho: converted the v0.1 registry to the v0.2 schema")
	}
	renderMigrateSummary(cmd, root, legacy.Project, store, base, hasBase, hookBackups)
	return nil
}

// migrationSite is the handful of facts migrate needs about a workspace
// before it has a v2 config to open one from.
type migrationSite struct {
	commonDir string
	gitDir    string
	repo      *appgit.Repo
	// workspace is the same facts in the shape the rest of the package
	// takes, so that migrate's base write goes through the ordinary
	// guarded port rather than around it. A v0.1 workspace cannot be
	// resolved by openWorkspace — that is what migration is for — so it
	// is assembled here from what has already been read.
	workspace *workspace
}

// statePort is migrate's guarded state adapter.
func (m migrationSite) statePort() statePort { return m.workspace.statePort() }

func openMigrationSite(ctx context.Context, root, docsDir, docsRepoURL string, hooks appgit.HookConfig) (migrationSite, error) {
	run := gitx.New(root)
	common, err := run.Line(ctx, "rev-parse", "--git-common-dir")
	if err != nil {
		return migrationSite{}, fmt.Errorf("resolve the git common directory of %s: %w", root, err)
	}
	if !filepath.IsAbs(common) {
		common = filepath.Join(root, common)
	}
	gitDir, err := run.Line(ctx, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return migrationSite{}, fmt.Errorf("%s is not inside a git repository: %w", root, err)
	}

	repo := appgit.New(root, docsDir, run).WithHooks(hooks)
	site := migrationSite{
		commonDir: filepath.Clean(common),
		gitDir:    gitDir,
		repo:      repo,
	}
	site.workspace = &workspace{
		root:       root,
		configRoot: root,
		gitDir:     gitDir,
		commonDir:  site.commonDir,
		config: wsstate.Config{
			DocsRepoURL: docsRepoURL,
			DocsDir:     repo.DocsDir(),
			HookMode:    string(hooks.Mode),
			HookDir:     hooks.Dir,
		},
		repo: repo,
	}
	return site, nil
}

// migrationComplete reports whether a v2 workspace really finished
// migrating: config, clone, and all six hooks (F-H8b).
func migrationComplete(ctx context.Context, root string, config wsstate.Config) (bool, error) {
	hooks, err := workspaceHookConfig(config)
	if err != nil {
		return false, err
	}
	site, err := openMigrationSite(ctx, root, config.DocsDir, config.DocsRepoURL, hooks)
	if err != nil {
		return false, err
	}
	if _, err := canonical.Open(site.commonDir, config.DocsRepoURL); err != nil {
		return false, nil //nolint:nilerr // an absent clone means "not finished", not a failure
	}
	states, err := site.repo.HooksStatus(ctx)
	if err != nil {
		return false, err
	}
	for _, state := range states {
		if !state.Installed {
			return false, nil
		}
	}
	return true, nil
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
func resolveDocsRepoURL(project, override, configured string) (string, error) {
	if override != "" {
		return override, nil
	}
	// A re-entered migration already carries the URL in its own v2
	// config; asking for --docs-repo-url again would be a demand for
	// something the workspace has already recorded (F-H8b).
	if configured != "" {
		return configured, nil
	}

	home, err := resolveHome()
	if err != nil {
		return "", err
	}
	// state.json first, then the backup this command itself makes. After
	// the first workspace migrates, state.json is v2 and no longer holds
	// the mapping — but state.json.v1.bak does, which is what lets the
	// SECOND project migrate without --docs-repo-url (F-H8c, R1's lab3).
	for _, name := range []string{registry.StateFileName, legacyStateBackupName} {
		if url := docsRepoURLFrom(filepath.Join(home, name), project); url != "" {
			return url, nil
		}
	}
	return "", errors.New(msgMigrateNeedsURL)
}

// docsRepoURLFrom reads one project's docs repository URL out of a v0.1
// daemon state file. Anything unreadable yields "", so the caller simply
// tries the next candidate.
func docsRepoURLFrom(path, project string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var state legacyDaemonState
	if err := json.Unmarshal(data, &state); err != nil {
		return ""
	}
	repoID, ok := state.ProjectToDocsRepo[project]
	if !ok {
		return ""
	}
	return state.DocsRepos[repoID].RepoURL
}

// writeV2Config backs the v0.1 config up and replaces it.
func writeV2Config(root string, legacy v1Config, docsRepoURL string, hooks appgit.HookConfig) error {
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
		HookMode:    string(hooks.Mode),
		HookDir:     hooks.Dir,
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
// It writes through the §5.7 guard like every other base write, and the
// warrant it holds is the plainest one: LoadBase already reads the
// legacy file, so the commit being written is the commit already
// recorded and only the tree annotation is new. A guard refusal here
// would mean the workspace's own v0.1 pointer disagrees with its
// documents, which migration is not the place to adjudicate — it is
// reported and the workspace continues without a base, exactly as the
// no-base branch above does.
func migrateBase(ctx context.Context, cmd *cobra.Command, root string, state statePort, store *canonical.Store) (provenance.Base, bool, error) {
	legacyPath := filepath.Join(root, wsstate.LegacyHashFileName)
	if exists, err := pathExists(legacyPath); err != nil {
		return provenance.Base{}, false, err
	} else if exists {
		if err := backupFile(legacyPath); err != nil {
			return provenance.Base{}, false, err
		}
	}

	base, hasBase, err := wsstate.LoadBase(root)
	if err != nil || !hasBase {
		// No usable v0.1 base. Deriving one from history is exactly what
		// the post-checkout hook and `sanho doctor --fix` do, and both
		// succeed later, so migration does not stop here.
		writef(cmd.ErrOrStderr(), "sanho: %s\n", baseNeedsSyncMessage("no v0.1 docs base was found"))
		return provenance.Base{}, false, nil
	}

	known, err := store.ResolveCommit(ctx, base.Commit)
	if err != nil {
		return provenance.Base{}, false, err
	}
	if !known {
		writeln(cmd.ErrOrStderr(), baseUnknownToCanonicalMessage(base.Commit))
		return base, true, state.SaveBase(ctx, base)
	}

	if tree, treeErr := commitTreeInClone(ctx, store, base.Commit); treeErr == nil {
		base.Tree = tree
	}
	return base, true, state.SaveBase(ctx, base)
}

// commitTreeInClone resolves a canonical commit's tree. The canonical
// repository is docs-only, so its root tree is the docs tree.
func commitTreeInClone(ctx context.Context, store *canonical.Store, commit string) (string, error) {
	return gitx.New(store.Dir()).Line(ctx, "rev-parse", "--verify", commit+"^{tree}")
}

// swapHooks removes the seven v0.1 lines and installs the six v0.2 ones.
// Removal first, so a `pre-push` file carrying both forms ends up with
// exactly one line rather than two (audit L3).
//
// Every hook file that exists is copied to `<name>.bak` beforehand
// (F-H8d). §8 step 6 makes migration reversible by promising a .bak for
// everything it rewrites, and hook files were the one thing it rewrote
// without one — including a hook file the user wrote themselves, whose
// foreign lines the installer preserves but whose shape it changes.
func swapHooks(ctx context.Context, repo *appgit.Repo) ([]string, error) {
	states, err := repo.HooksStatus(ctx)
	if err != nil {
		return nil, err
	}

	var backups []string
	for _, state := range states {
		if exists, err := pathExists(state.Path); err != nil {
			return nil, err
		} else if !exists {
			continue
		}
		if err := backupFile(state.Path); err != nil {
			return nil, err
		}
		backups = append(backups, filepath.Base(state.Path)+backupSuffix)
	}
	sort.Strings(backups)

	if err := repo.RemoveHooks(ctx); err != nil {
		return nil, err
	}
	return backups, repo.InstallHooks(ctx)
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
	if err := fsx.WriteFileAtomic(backup, data, 0600); err != nil {
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
	// Atomic: a .bak is a rollback source, and a truncated one is worse
	// than none because it looks like a rollback that exists (F-L4).
	if err := fsx.WriteFileAtomic(backup, data, info.Mode().Perm()); err != nil {
		return fmt.Errorf("write %s: %w", backup, err)
	}
	return nil
}

func renderMigrateSummary(cmd *cobra.Command, root, project string, store *canonical.Store, base provenance.Base, hasBase bool, hookBackups []string) {
	out := cmd.OutOrStdout()
	writeln(out, "sanho: migrated this workspace to the v0.2 layout")
	writef(out, "  workspace : %s\n", root)
	writef(out, "  project   : %s\n", project)
	writef(out, "  docs repo : %s (branch %s)\n", store.URL(), store.Branch())
	writef(out, "  clone     : %s\n", store.Dir())
	if hasBase {
		writef(out, "  docs base : %s\n", shortOID(base.Commit))
	} else {
		writeln(out, "  docs base : (none yet)")
	}
	writef(out, "  hooks     : %d installed, v0.1 lines removed\n", len(appgit.Hooks()))
	backups := append([]string{
		wsstate.ConfigFileName + backupSuffix,
		wsstate.LegacyHashFileName + backupSuffix,
	}, hookBackups...)
	writef(out, "  backups   : %s\n", strings.Join(backups, ", "))

	writeln(out)
	for _, line := range daemonStopInstructions {
		writeln(out, line)
	}
}
