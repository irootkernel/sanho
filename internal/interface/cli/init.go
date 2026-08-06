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
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/irootkernel/sanho/internal/domain/provenance"
	"github.com/irootkernel/sanho/internal/infra/appgit"
	"github.com/irootkernel/sanho/internal/infra/canonical"
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
	wsstate.BaseFileName,
	wsstate.LegacyHashFileName,
	".sanho_pending_fix",
}

type initOptions struct {
	project     string
	docsRepoURL string
	docsDir     string
	actorEmail  string
	force       bool
	confirmed   bool
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
	if opts.docsDir == "" {
		opts.docsDir = appgit.DefaultDocsDir
	}
	if _, err := os.Stat(filepath.Join(root, wsstate.ConfigFileName)); err == nil && !opts.force {
		return fmt.Errorf("%s already exists in %s; rerun with --force to reinitialize", wsstate.ConfigFileName, root)
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
	}

	// The registry first: a project whose name is already bound to a
	// different docs repository must stop the whole operation before any
	// file in the workspace changes.
	file, err := openRegistry()
	if err != nil {
		return err
	}
	if err := file.Update(ctx, func(state *registry.State) error {
		return upsertProject(state, opts.project, opts.docsRepoURL)
	}); err != nil {
		return err
	}

	if err := wsstate.SaveConfig(root, config); err != nil {
		return err
	}
	ws, err := openWorkspace(ctx)
	if err != nil {
		return err
	}

	store, err := canonical.Ensure(ctx, ws.commonDir, opts.docsRepoURL)
	if err != nil {
		return err
	}
	if err := store.Fetch(ctx); err != nil {
		return err
	}

	base, hasBase, staged, err := establishBase(ctx, cmd, ws, store, opts)
	if err != nil {
		return err
	}
	if hasBase {
		if err := ws.statePort().SaveBase(base); err != nil {
			return err
		}
	}

	if err := ws.repo.InstallHooks(ctx); err != nil {
		return err
	}
	if err := ensureGitignoreEntries(root); err != nil {
		return err
	}
	if err := upsertWorkspace(ctx, file, ws, base); err != nil {
		return err
	}

	renderInitSummary(cmd, ws, store, base, hasBase, staged)
	return nil
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
	root, err := filepath.Abs(cwd)
	if err != nil {
		return "", fmt.Errorf("resolve the current directory: %w", err)
	}

	top, err := gitx.New(root).Line(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("%s is not inside a git repository; run 'git init' first", root)
	}
	if resolved, err := filepath.EvalSymlinks(top); err == nil {
		top = resolved
	}
	if here, err := filepath.EvalSymlinks(root); err == nil {
		root = here
	}
	if top != root {
		return "", fmt.Errorf("run 'sanho init' at the repository root (%s)", top)
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
	derived, found, err := deriveBase(ctx, ws.root)
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
		writef(cmd.ErrOrStderr(),
			"sanho: the derived docs base %s is not in the canonical repository; canonical history may have been rewritten. Run 'sanho status' to check, and 'sanho sync' to reconcile.\n",
			shortOID(derived.Commit))
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
	path := filepath.Join(root, ".gitignore")

	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", path, err)
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
	return os.WriteFile(path, []byte(content), 0644)
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
	if staged {
		// Fresh mode stages canonical's docs into the index; the commit
		// is the user's to make (P3: the tool never authors commits).
		writeln(out, "\nCanonical docs are staged. Commit them:  git commit -m 'docs: adopt canonical docs'")
	}
}
