package cli

// `sanho init` (sanho-v0.2.md §5.8): register the project and this
// workspace, write the v2 files, clone canonical, install the six hooks,
// and establish a base.
//
// Three head states, decided in this order (see establishBase):
//
//	fresh     canonical has content and this workspace has no docs of its
//	          own — check canonical's docs out and adopt its head.
//	bootstrap canonical is empty — record no base and say so; the first
//	          push publishes (§5.3).
//	reuse     local docs already exist — derive the base from the docs
//	          provenance already in this repository's history, and never
//	          overwrite the user's files.
//
// Reuse refusing is the important case. Docs with no provenance could be
// anything, and adopting canonical's head as their base would assert an
// ancestry that is not true — the next push would then "merge" unrelated
// content. Refusing, and naming --force for the destructive alternative,
// is the fail-closed reading.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/irootkernel/sanho/internal/domain/provenance"
	"github.com/irootkernel/sanho/internal/infra/appgit"
	"github.com/irootkernel/sanho/internal/infra/canonical"
	"github.com/irootkernel/sanho/internal/infra/fsx"
	"github.com/irootkernel/sanho/internal/infra/gitx"
	"github.com/irootkernel/sanho/internal/infra/registry"
	"github.com/irootkernel/sanho/internal/infra/wsstate"

	"github.com/spf13/cobra"
)

// gitignoreEntries are added on init. The legacy v0.1 names stay listed
// so that a workspace which still carries them (or is rolled back to
// v0.1) never accidentally commits them (§8).
var gitignoreEntries = []string{
	wsstate.ConfigFileName,
	wsstate.ConfigFileName + ".bak",
	wsstate.BaseFileName,
	wsstate.LegacyHashFileName,
	wsstate.LegacyHashFileName + backupSuffix,
	".sanho_pending_fix",
	// fsx.WriteFileAtomic's temp files, which live beside their target
	// for the moment between creation and rename (F-L12). A crash there
	// leaves one behind, and an untracked `.  .sanho.json.tmp-1234` in
	// `git status` is noise the user cannot explain. The patterns are
	// deliberately narrow — `.*.tmp-*` would swallow half the world —
	// and cover the two sanho files that live at the repository root.
	".sanho*.tmp-*",
	"..sanho*.tmp-*",
}

type initOptions struct {
	project     string
	docsRepoURL string
	docsDir     string
	actorEmail  string
	force       bool
	confirmed   bool
	manageHooks bool
}

func newInitCmd() *cobra.Command {
	var opts initOptions

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Register this repository as a sanho workspace",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return runInit(cmd, opts) },
	}
	cmd.Flags().StringVar(&opts.project, "project", "", "Project name (required)")
	cmd.Flags().StringVar(&opts.docsRepoURL, "docs-repo-url", "", "Canonical docs repository URL (required)")
	cmd.Flags().StringVar(&opts.docsDir, "docs-dir", appgit.DefaultDocsDir, "Docs directory, relative to the repository root")
	cmd.Flags().StringVar(&opts.actorEmail, "actor-email", "", "Email recorded on canonical commits (default: git config user.email)")
	cmd.Flags().BoolVar(&opts.force, "force", false, "Replace an existing docs directory with canonical content")
	cmd.Flags().BoolVarP(&opts.confirmed, "yes", "y", false, "Confirm destructive operations without prompting")
	cmd.Flags().BoolVar(&opts.manageHooks, "manage-custom-hooks", false, "Manage a repository-local custom core.hooksPath or recognized Husky 9 hooks")
	return cmd
}

func runInit(cmd *cobra.Command, opts initOptions) error {
	ctx := cmd.Context()

	root, err := requireGitWorktreeRoot(ctx)
	if err != nil {
		return err
	}
	if opts.project == "" || opts.docsRepoURL == "" {
		return fmt.Errorf("--project and --docs-repo-url are required")
	}
	if opts.docsDir, err = normalizeDocsDir(opts.docsDir); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(root, wsstate.ConfigFileName)); err == nil && !opts.force {
		return fmt.Errorf("%s already exists in %s; rerun with --force to reinitialize", wsstate.ConfigFileName, root)
	}
	repo := appgit.New(root, opts.docsDir, gitx.New(root))
	hooks, err := detectHookConfig(ctx, repo, opts.manageHooks)
	if err != nil {
		return err
	}
	// The sync note is consulted before the dirty-docs check, and the
	// order is the same one `sanho sync` uses: an unfinished sync makes
	// the docs dirty by construction, so asking about the docs first
	// would answer a state that is really about the sync with advice that
	// does not end it.
	if err := refuseInitWhileSyncing(ctx, root); err != nil {
		return err
	}
	if opts.force {
		if err := refuseForceOnDirtyDocs(ctx, root, opts.docsDir); err != nil {
			return err
		}
	}

	if opts.actorEmail == "" {
		if opts.actorEmail, err = gitUserEmail(ctx, root); err != nil {
			return err
		}
	}

	config := wsstate.Config{
		WorkspaceID: registryKey(opts.project, root),
		Project:     opts.project,
		DocsRepoURL: opts.docsRepoURL,
		ActorEmail:  opts.actorEmail,
		DocsDir:     opts.docsDir,
		HookMode:    string(hooks.Mode),
		HookDir:     hooks.Dir,
	}

	// The registry first: a project whose name is already bound to a
	// different docs repository must stop the whole operation before any
	// file in the workspace changes.
	file, err := openRegistry()
	if err != nil {
		return err
	}
	if err := updateRegistry(ctx, file, func(state *registry.State) error {
		return upsertProject(state, opts.project, opts.docsRepoURL)
	}); err != nil {
		return err
	}

	// Everything from here on can fail, and a half-initialized workspace
	// is worse than none: its config makes every later command believe
	// sanho is installed while the clone, the base or the hooks are
	// missing. rollback undoes the two records this function wrote —
	// nothing else has happened yet (F-M5).
	existingConfig := configExists(root)
	rollback := func(cause error) error {
		if existingConfig {
			// A --force re-init over a working workspace: leave what was
			// already there rather than deleting a config we did not
			// create.
			return cause
		}
		_ = os.Remove(filepath.Join(root, wsstate.ConfigFileName))
		_ = updateRegistry(ctx, file, func(state *registry.State) error {
			delete(state.Workspaces, registryKey(opts.project, root))
			return nil
		})
		return cause
	}

	if err := wsstate.SaveConfig(root, config); err != nil {
		return err
	}
	ws, err := openWorkspace(ctx)
	if err != nil {
		return rollback(err)
	}

	store, err := canonical.Ensure(ctx, ws.commonDir, opts.docsRepoURL)
	if err != nil {
		return rollback(initGitError("create the canonical clone", err))
	}
	// A clone Ensure just built has already fetched; only an existing one
	// needs refreshing here.
	if !store.Fresh() {
		if err := store.Fetch(ctx); err != nil {
			return rollback(initGitError("fetch the canonical repository", err))
		}
	}

	base, hasBase, staged, err := establishBase(ctx, cmd, ws, store, opts)
	if err != nil {
		return rollback(err)
	}
	if hasBase {
		// Through the §5.7 guard like every other base write. Fresh mode
		// has just checked canonical's docs out, so the worktree IS the
		// base's tree; reuse mode derived the value from this history's
		// own trailer. Both are warrants the guard checks for itself.
		if err := ws.statePort().SaveBase(ctx, base); err != nil {
			return rollback(err)
		}
	}

	if err := ws.repo.InstallHooks(ctx); err != nil {
		return rollback(initGitError("install the git hooks", err))
	}
	if err := ensureGitignoreEntries(root); err != nil {
		return rollback(err)
	}
	if err := upsertWorkspace(ctx, file, ws, base); err != nil {
		return rollback(err)
	}

	renderInitSummary(cmd, ws, store, base, hasBase, staged)
	return nil
}

// detectHookConfig keeps lifecycle commands out of custom hook directories
// before mutation unless the caller explicitly opts into a repository-local
// target. The infra resolver distinguishes direct hooks from Husky shims.
func detectHookConfig(ctx context.Context, repo *appgit.Repo, manageCustom bool) (appgit.HookConfig, error) {
	config, err := repo.DetectHookConfig(ctx, manageCustom)
	if err == nil {
		return config, nil
	}
	var custom *appgit.CustomHooksPathError
	if errors.As(err, &custom) {
		return appgit.HookConfig{}, errors.New(customHooksPathMessage(custom.Path))
	}
	return appgit.HookConfig{}, initGitError("resolve the git hooks directory", err)
}

func configExists(root string) bool {
	_, err := os.Stat(filepath.Join(root, wsstate.ConfigFileName))
	return err == nil
}

// initGitError wraps a git-layer failure in the operation that was being
// attempted, so the user reads "could not create the canonical clone:
// …" rather than a bare transport message (F-M5).
func initGitError(operation string, err error) error {
	return fmt.Errorf("could not %s: %s", operation, stripInternalPrefixes(causeLine(err)))
}

// normalizeDocsDir validates --docs-dir up front (F-M5).
//
// The value is used as a git pathspec, as a filesystem path, and as a
// prefix on every conflict path sanho prints, so a malformed one fails
// deep inside a later git call with an error about something else
// entirely. filepath.IsLocal is exactly the property wanted: inside the
// repository, no `..`, not absolute, not a reserved device name.
func normalizeDocsDir(docsDir string) (string, error) {
	if docsDir == "" {
		return appgit.DefaultDocsDir, nil
	}
	cleaned := path.Clean(filepath.ToSlash(strings.TrimRight(docsDir, "/")))
	if cleaned == "" || cleaned == "." {
		return "", fmt.Errorf("--docs-dir %q is the repository root; name a subdirectory", docsDir)
	}
	if !filepath.IsLocal(filepath.FromSlash(cleaned)) {
		return "", fmt.Errorf("--docs-dir %q must be a path inside the repository (no leading '/', no '..')", docsDir)
	}
	return cleaned, nil
}

// refuseInitWhileSyncing refuses to (re-)initialize a workspace whose
// sync is unfinished.
//
// `sanho init --force` replaces the docs directory and records a base
// from canonical head. Both halves are wrong inside the window: the
// docs it would replace are a reconciliation in progress, and the base
// it would record describes content `sanho sync --abort` then takes back
// — leaving a base ahead of the worktree, which is the one state every
// path in v0.2 is arranged to prevent.
//
// A directory that is not a git repository, or one with no note, is not
// this function's business: it answers only for the note's existence,
// and an unreadable one counts, because "the sync state is unknown" is
// not a state to re-initialize over.
func refuseInitWhileSyncing(ctx context.Context, root string) error {
	gitDir, err := gitx.New(root).Line(ctx, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return nil //nolint:nilerr // requireGitWorktreeRoot has already vouched for the directory
	}
	if _, exists, noteErr := wsstate.LoadSyncNote(gitDir); exists || noteErr != nil {
		return errors.New(msgCleanSyncInProgress)
	}
	return nil
}

// refuseForceOnDirtyDocs is F-H7.
//
// `--force` replaces the docs directory with canonical's content. Doing
// that over uncommitted work destroys it with no undo — the files are
// not in any commit, so `git checkout` cannot bring them back — and
// nothing about re-initializing requires it. The check is `DocsClean`,
// the same one `sanho sync` uses, so worktree edits, staged edits and
// untracked docs files all count.
func refuseForceOnDirtyDocs(ctx context.Context, root, docsDir string) error {
	repo := appgit.New(root, docsDir, gitx.New(root))
	clean, err := repo.DocsClean(ctx)
	if err != nil {
		// Before the first commit there is nothing for `git status` to
		// compare against and nothing committed to lose.
		return nil //nolint:nilerr // an unreadable status is not evidence of dirty docs
	}
	if clean {
		return nil
	}
	return fmt.Errorf("--force replaces %s with canonical content, and it has uncommitted changes; commit or stash your docs changes first", docsDir)
}

// requireGitWorktreeRoot resolves the current directory and insists it
// is the top of a git worktree. Every v0.2 mechanism — hooks, trees,
// provenance — is built on git, and the hooks always run from the top,
// so initializing anywhere else would produce a workspace whose own
// hooks could not find it.
func requireGitWorktreeRoot(ctx context.Context) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve the current directory: %w", err)
	}
	root, err := canonicalFilesystemPath(cwd)
	if err != nil {
		return "", fmt.Errorf("resolve the current directory: %w", err)
	}

	top, err := gitx.New(root).Line(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("%s is not inside a git repository; run 'git init' first", root)
	}
	if canonical, canonicalErr := canonicalFilesystemPath(top); canonicalErr == nil {
		top = canonical
	}
	if top != root {
		return "", fmt.Errorf("a sanho workspace must be initialized at the repository root (%s), not at %s", top, root)
	}
	return root, nil
}

func gitUserEmail(ctx context.Context, root string) (string, error) {
	res, err := gitx.New(root).RunExit(ctx, "config", "--get", "user.email")
	if err != nil {
		return "", fmt.Errorf("read git user.email: %w", err)
	}
	email := strings.TrimSpace(string(res.Stdout))
	if res.ExitCode != 0 || email == "" {
		return "", fmt.Errorf("no git user.email is configured; set it or pass --actor-email")
	}
	return email, nil
}

// establishBase implements the three head states described at the top of
// this file. staged reports whether canonical docs were written into the
// worktree and index, which is the one outcome that leaves the user a
// commit to make.
func establishBase(ctx context.Context, cmd *cobra.Command, ws *workspace, store *canonical.Store, opts initOptions) (base provenance.Base, hasBase, staged bool, err error) {
	// An unfinished sync owns the base, and `sanho init --force` over one
	// would both replace the docs it is holding and record a base for
	// content the abort would then take back — leaving the base ahead of
	// the worktree, which is the one state every path in v0.2 is arranged
	// to prevent. Refusing is the honest answer, and both commands it
	// names work here.
	if _, syncing, noteErr := ws.statePort().LoadSyncNote(); syncing || noteErr != nil {
		return provenance.Base{}, false, false, errors.New(msgCleanSyncInProgress)
	}

	head, headTree, headErr := store.Head(ctx)
	canonicalEmpty := headErr != nil

	docsExist, err := docsDirHasContent(ws)
	if err != nil {
		return provenance.Base{}, false, false, err
	}

	switch {
	case canonicalEmpty:
		// Nothing to adopt and nothing to check out; the first push
		// bootstraps canonical (§5.3).
		writeln(cmd.ErrOrStderr(), msgInitCanonicalEmpty)
		return provenance.Base{}, false, false, nil

	case docsExist && !opts.force:
		reused, ok, reuseErr := reuseExistingDocs(ctx, cmd, ws, store)
		return reused, ok, false, reuseErr

	case docsExist && opts.force:
		if !opts.confirmed {
			return provenance.Base{}, false, false, fmt.Errorf("%s", msgInitForceNeedsConfirmation)
		}
		fallthrough

	default:
		// Fresh mode: canonical's docs become this workspace's docs. The
		// objects have to come across first — the tree lives in the
		// private clone, and a checkout can only write what the app
		// repository's own object database holds (§5.2 object exchange).
		if _, err := ws.link(store).FetchIntoApp(ctx); err != nil {
			return provenance.Base{}, false, false, err
		}
		if err := ws.repo.CheckoutDocsTree(ctx, headTree); err != nil {
			return provenance.Base{}, false, false, err
		}
		return provenance.Base{Commit: head, Tree: headTree}, true, true, nil
	}
}

// reuseExistingDocs derives the base from provenance already present in
// this repository's history (§5.10 derivation, §5.1 legacy coexistence),
// and never touches the user's files.
//
// A derived base that canonical does not recognize is kept with a
// warning rather than rejected: the trailer is evidence of what these
// docs were built from, and canonical history may simply have been
// rewritten since — which is exactly the state docs-base-tree exists to
// recover from (D2). Discarding it would throw away the anchor.
func reuseExistingDocs(ctx context.Context, cmd *cobra.Command, ws *workspace, store *canonical.Store) (provenance.Base, bool, error) {
	derived, found, err := deriveBase(ctx, ws)
	if err != nil {
		return provenance.Base{}, false, err
	}
	if !found {
		return provenance.Base{}, false, fmt.Errorf("%s", msgInitNoProvenance)
	}

	known, err := store.ResolveCommit(ctx, derived.Commit)
	if err != nil {
		return provenance.Base{}, false, err
	}
	if !known {
		writeln(cmd.ErrOrStderr(), baseUnknownToCanonicalMessage(derived.Commit))
	}
	return derived, true, nil
}

// docsDirHasContent reports whether the docs directory exists and holds
// anything. An empty directory is treated as absent: it constrains
// nothing and refusing on it would be a surprise.
func docsDirHasContent(ws *workspace) (bool, error) {
	path := filepath.Join(ws.root, filepath.FromSlash(ws.config.DocsDir))
	entries, err := os.ReadDir(path)
	switch {
	case err == nil:
		return len(entries) > 0, nil
	case os.IsNotExist(err):
		return false, nil
	default:
		return false, fmt.Errorf("inspect the docs directory %s: %w", path, err)
	}
}

// ensureGitignoreEntries appends the sanho state files to .gitignore,
// skipping any line already present. Exact-line matching again: a
// `.sanho.json` entry must not be mistaken for `.sanho_base.json`.
func ensureGitignoreEntries(root string) error {
	gitignorePath := filepath.Join(root, ".gitignore")

	existing, err := os.ReadFile(gitignorePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", gitignorePath, err)
	}

	present := map[string]bool{}
	for _, line := range strings.Split(string(existing), "\n") {
		present[strings.TrimSpace(line)] = true
	}

	var added []string
	for _, entry := range gitignoreEntries {
		if !present[entry] {
			added = append(added, entry)
			present[entry] = true
		}
	}
	if len(added) == 0 {
		return nil
	}

	content := string(existing)
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	content += strings.Join(added, "\n") + "\n"
	// Atomic: an interrupted append must not leave a truncated
	// .gitignore, which would un-ignore the state files it names (F-L4).
	return fsx.WriteFileAtomic(gitignorePath, []byte(content), 0644)
}

func renderInitSummary(cmd *cobra.Command, ws *workspace, store *canonical.Store, base provenance.Base, hasBase, staged bool) {
	out := cmd.OutOrStdout()
	writeln(out, "sanho: workspace initialized")
	writef(out, "  workspace  : %s\n", ws.root)
	writef(out, "  project    : %s\n", ws.config.Project)
	writef(out, "  docs dir   : %s\n", ws.config.DocsDir)
	writef(out, "  docs repo  : %s (branch %s)\n", store.URL(), store.Branch())
	writef(out, "  clone      : %s\n", store.Dir())
	if hasBase {
		writef(out, "  docs base  : %s\n", shortOID(base.Commit))
	} else {
		writeln(out, "  docs base  : (none yet)")
	}
	writef(out, "  hooks      : %d installed\n", len(appgit.Hooks()))
	// Fresh mode stages canonical's docs into the index and init always
	// appends the ignore entries; both are the user's to commit (P3: the
	// tool never authors commits).
	writeln(out, initNextStepsMessage(staged))
}
