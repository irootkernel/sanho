package cli

// Workspace discovery and port wiring — the one place adapters are
// built (sanho-v0.2.md §4, §5.2, §5.7).
//
// A workspace is the current directory when it carries `.sanho.json`,
// the same rule as v0.1: sanho is a per-checkout tool and the hooks
// always run from the worktree root, so walking upward would only make
// it possible to operate on a workspace by accident.
//
// The adapters below exist here rather than in a use case because the
// architecture gate forbids a usecase package from importing infra and
// an infra package from importing usecase. interface/cli may see both,
// so it is where canonical.Link, appgit.Repo, wsstate and registry are
// bound to the declared ports.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/irootkernel/sanho/internal/domain/provenance"
	"github.com/irootkernel/sanho/internal/infra/appgit"
	"github.com/irootkernel/sanho/internal/infra/canonical"
	"github.com/irootkernel/sanho/internal/infra/gitx"
	"github.com/irootkernel/sanho/internal/infra/registry"
	"github.com/irootkernel/sanho/internal/infra/wsstate"
	"github.com/irootkernel/sanho/internal/usecase/docsync"
)

// defaultHomeDirName is the sanho home directory name under the user's
// home directory when SANHO_HOME is not set.
const defaultHomeDirName = ".sanho"

// errV1Workspace is the §8 pre-migration degradation signal: the
// workspace is a v0.1 one, and only `sanho migrate` can act on it.
var errV1Workspace = errors.New(msgMigrateRequired)

// errNotWorkspace is raised when the current directory carries no
// `.sanho.json`.
var errNotWorkspace = errors.New(msgNotInWorkspace)

// workspace is one resolved managed checkout and everything built from
// it. Its zero value is never used; openWorkspace is the only
// constructor.
type workspace struct {
	// root is THIS worktree's root — the directory the hooks run in and
	// the docs live in. For a linked worktree it is the linked one.
	root string
	// configRoot is where `.sanho.json` was found. It equals root for an
	// ordinary checkout and is the MAIN worktree root for a linked one
	// (§5.2 as amended by F-H3), because `.sanho.json` is gitignored and
	// therefore never travels into `git worktree add`.
	//
	// The split matters for exactly one thing: the registry key, which
	// stays the main root so that N worktrees of one checkout are one
	// registry row rather than N. Everything worktree-shaped — the base
	// file, the sync note, the docs — stays on root.
	configRoot string
	// gitDir is `git rev-parse --git-dir` (worktree-private) and
	// commonDir is `--git-common-dir` (shared by linked worktrees). The
	// canonical clone lives under the common dir so linked worktrees
	// share it (§5.2); the sync note lives under the private dir.
	gitDir    string
	commonDir string

	config wsstate.Config
	repo   *appgit.Repo
	// homeDir is the sanho home (~/.sanho unless SANHO_HOME).
	homeDir string
}

// openWorkspace resolves the current directory as a managed workspace.
//
// A v0.1 config is not an error here — LoadConfig reports it as
// SchemaVersion 1 and this returns errV1Workspace, which each entry
// point routes according to §8: hooks degrade, commands refuse, and
// `sanho migrate` proceeds.
func openWorkspace(ctx context.Context) (*workspace, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("resolve the current directory: %w", err)
	}
	root, err := canonicalFilesystemPath(cwd)
	if err != nil {
		return nil, fmt.Errorf("resolve the current directory: %w", err)
	}

	configRoot, err := resolveConfigRoot(ctx, root)
	if err != nil {
		return nil, err
	}

	cfg, err := wsstate.LoadConfig(configRoot)
	if err != nil {
		return nil, err
	}
	cfg.ApplyDefaults()

	ws := &workspace{root: root, configRoot: configRoot, config: cfg}
	if err := ws.resolveGitDirs(ctx); err != nil {
		return nil, err
	}
	if ws.homeDir, err = resolveHome(); err != nil {
		return nil, err
	}
	// WithInheritedIndexFile is the one place sanho asks for a
	// repository-scoped git variable from its own environment, and it is
	// asked for here rather than inherited silently. The commit hooks run
	// inside a PARTIAL commit (`git commit -- docs`), where git points
	// GIT_INDEX_FILE at a temporary index holding exactly what that
	// commit will contain — which is what the §5.1 stamp and the §5.6
	// staged-marker gate must read. Every other runner in the process,
	// including every command against the private canonical clone, has it
	// scrubbed (see gitx.scrubbedEnvVars).
	hooks, err := workspaceHookConfig(cfg)
	if err != nil {
		return nil, err
	}
	ws.repo = appgit.New(root, cfg.DocsDir, gitx.New(root, gitx.WithInheritedIndexFile())).WithHooks(hooks)

	if cfg.SchemaVersion == 1 {
		return ws, errV1Workspace
	}
	return ws, nil
}

func workspaceHookConfig(cfg wsstate.Config) (appgit.HookConfig, error) {
	mode := appgit.HookMode(cfg.HookMode)
	switch mode {
	case appgit.HookModeDefault:
		if cfg.HookDir != "" {
			return appgit.HookConfig{}, fmt.Errorf("%w: hook_dir requires hook_mode", wsstate.ErrConfigCorrupt)
		}
	case appgit.HookModeCustom, appgit.HookModeHusky:
		if cfg.HookDir == "" {
			return appgit.HookConfig{}, fmt.Errorf("%w: hook_mode %q requires hook_dir", wsstate.ErrConfigCorrupt, cfg.HookMode)
		}
	default:
		return appgit.HookConfig{}, fmt.Errorf("%w: unknown hook_mode %q", wsstate.ErrConfigCorrupt, cfg.HookMode)
	}
	return appgit.HookConfig{Mode: mode, Dir: cfg.HookDir}, nil
}

// resolveConfigRoot finds the `.sanho.json` governing root.
//
// The ordinary answer is root itself. The other one is F-H3: `git
// worktree add` produces a checkout with none of the gitignored files,
// so a linked worktree never carries `.sanho.json` — and before this,
// every hook in every linked worktree found no workspace and silently
// did nothing. No marker gate, no provenance stamp, no publication, no
// message. A tool that is installed and inert is worse than one that is
// absent.
//
// So a directory with no config asks git for the MAIN worktree and uses
// the config there. A main worktree with no config is simply not a
// workspace, which is the same answer as before.
//
// The borrowing is allowed only for a directory that IS one of the
// repository's worktrees, and checking that is M6. The previous version
// took the first record of `git worktree list --porcelain` and stopped
// there, so any SUBDIRECTORY of a checkout — `docs/`, `src/internal/`,
// anywhere a user might be standing — resolved to the main worktree's
// config and was treated as a workspace root. Every path built from it
// then pointed at the wrong place: the base file, the docs pathspec, the
// checkout target. sanho is a per-checkout tool and the hooks always run
// from the top, so the honest answer for a subdirectory is "not a
// workspace" — which is what it was before linked-worktree support
// existed, and what it must stay.
//
// All records are compared rather than only the first, because the
// question is "is this one of them", and the main worktree is separately
// the first record git documents.
func resolveConfigRoot(ctx context.Context, root string) (string, error) {
	switch found, err := hasConfig(root); {
	case err != nil:
		return "", err
	case found:
		return root, nil
	}

	worktrees, err := worktreeRoots(ctx, root)
	if err != nil || len(worktrees) == 0 {
		return "", errNotWorkspace //nolint:nilerr // "not a workspace" is the answer for every failure to locate one
	}
	if !containsPath(worktrees, root) {
		// A subdirectory of a checkout, not a checkout.
		return "", errNotWorkspace
	}
	main := worktrees[0]
	if main == root {
		return "", errNotWorkspace
	}
	switch found, err := hasConfig(main); {
	case err != nil:
		return "", err
	case !found:
		return "", errNotWorkspace
	}
	return main, nil
}

// containsPath reports whether candidate is one of paths, comparing
// resolved forms so that a symlinked temp directory (every macOS
// `/var/...`) matches the `/private/var/...` git reports.
func containsPath(paths []string, candidate string) bool {
	for _, path := range paths {
		if sameFilesystemPath(path, candidate) {
			return true
		}
	}
	return false
}

// canonicalFilesystemPath returns the stable identity used for workspace
// roots. Abs is required; EvalSymlinks is best effort so callers can still
// name a path whose final component disappeared between discovery and use.
func canonicalFilesystemPath(name string) (string, error) {
	abs, err := filepath.Abs(name)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		return filepath.Clean(real), nil
	}
	return abs, nil
}

// sameFilesystemPath compares existing absolute paths by their real path.
// Registry paths are absolute by contract; refusing relative values avoids
// interpreting corrupt observational state relative to the caller's cwd.
func sameFilesystemPath(left, right string) bool {
	if !filepath.IsAbs(left) || !filepath.IsAbs(right) {
		return false
	}
	leftCanonical, leftErr := canonicalFilesystemPath(left)
	rightCanonical, rightErr := canonicalFilesystemPath(right)
	return leftErr == nil && rightErr == nil && leftCanonical == rightCanonical
}

func hasConfig(dir string) (bool, error) {
	switch _, err := os.Stat(filepath.Join(dir, wsstate.ConfigFileName)); {
	case err == nil:
		return true, nil
	case os.IsNotExist(err):
		return false, nil
	default:
		return false, fmt.Errorf("read %s: %w", wsstate.ConfigFileName, err)
	}
}

// worktreeRoots reads every record of `git worktree list --porcelain`,
// main worktree first (git's documented order).
func worktreeRoots(ctx context.Context, dir string) ([]string, error) {
	res, err := gitx.New(dir).RunExit(ctx, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	if res.ExitCode != 0 {
		detail := strings.TrimSpace(string(res.Stderr))
		if detail == "" {
			detail = fmt.Sprintf("git exited %d", res.ExitCode)
		}
		return nil, fmt.Errorf("list git worktrees: %s", detail)
	}
	var roots []string
	for _, line := range strings.Split(string(res.Stdout), "\n") {
		if path, ok := strings.CutPrefix(strings.TrimSpace(line), "worktree "); ok {
			if canonical, canonicalErr := canonicalFilesystemPath(path); canonicalErr == nil {
				path = canonical
			}
			roots = append(roots, filepath.Clean(path))
		}
	}
	return roots, nil
}

// resolveGitDirs asks git for both directory forms at once. A workspace
// that is not inside a git repository is a configuration error worth
// naming plainly: every v0.2 mechanism is built on git.
func (w *workspace) resolveGitDirs(ctx context.Context) error {
	run := gitx.New(w.root)

	gitDir, err := run.Line(ctx, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return fmt.Errorf("%s is not inside a git repository: %w", w.root, err)
	}
	if canonical, canonicalErr := canonicalFilesystemPath(gitDir); canonicalErr == nil {
		gitDir = canonical
	}
	w.gitDir = gitDir

	common, err := run.Line(ctx, "rev-parse", "--git-common-dir")
	if err != nil {
		return fmt.Errorf("resolve the git common directory of %s: %w", w.root, err)
	}
	if !filepath.IsAbs(common) {
		common = filepath.Join(w.root, common)
	}
	if canonical, canonicalErr := canonicalFilesystemPath(common); canonicalErr == nil {
		common = canonical
	}
	w.commonDir = filepath.Clean(common)
	return nil
}

// resolveHome resolves the sanho home: SANHO_HOME when set (must be an
// absolute path), else "~/.sanho". This is the one thing the CLI ever
// needed from the retired internal/config package (sanho-v0.2.md §6); it
// is inlined here now that the package's only other caller, the daemon,
// is gone.
func resolveHome() (string, error) {
	home := os.Getenv("SANHO_HOME")
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve user home: %w", err)
		}
		home = filepath.Join(userHome, defaultHomeDirName)
	}
	if !filepath.IsAbs(home) {
		return "", errors.New("SANHO_HOME must be an absolute path")
	}
	return filepath.Clean(home), nil
}

// openRegistry opens the cross-workspace registry under the sanho home.
func openRegistry() (*registry.File, error) {
	home, err := resolveHome()
	if err != nil {
		return nil, err
	}
	return registry.Open(home)
}

// cloneDir is where this workspace's private canonical clone lives.
func (w *workspace) cloneDir() string { return canonical.CloneDir(w.commonDir) }

// openCanonical opens the existing private clone. It never creates one
// and never touches the network, which is what makes it the right call
// for read paths and for the commit hook (§5.6: "canonical.Open, not
// Ensure").
func (w *workspace) openCanonical() (*canonical.Store, error) {
	return canonical.Open(w.commonDir, w.config.DocsRepoURL)
}

// ensureCanonical opens the clone, creating and fetching it when absent.
// Write paths use it; it requires the network on first call.
func (w *workspace) ensureCanonical(ctx context.Context) (*canonical.Store, error) {
	return canonical.Ensure(ctx, w.commonDir, w.config.DocsRepoURL)
}

// link binds a store to this application repository.
func (w *workspace) link(store *canonical.Store) *canonical.Link {
	return canonical.NewLink(store, w.gitDir)
}

// registryKey is the `<project>:<abs-path>` key of §5.7, which is also
// the workspace id stamped into canonical commit bodies (§5.3).
//
// It keys on configRoot, so every linked worktree of one checkout maps
// to the one registry row the user set up — the registry answers "which
// checkouts of this project exist", and five worktrees of one clone are
// one checkout by that question's own standard (F-H3).
func (w *workspace) registryKey() string { return registryKey(w.config.Project, w.configRoot) }

func registryKey(project, root string) string { return project + ":" + root }

// --- StatePort --------------------------------------------------------

// statePort adapts wsstate to the StatePort every use case declares.
// The two halves live in different places on purpose: the base file is a
// workspace-root file the user can see and a rollback can restore, while
// the sync note is `.git/`-private because it describes an in-flight
// operation, not a fact about the checkout.
// ws is what makes the base write guarded: the guard has to read the
// docs worktree, the tip's provenance and (sometimes) the canonical
// clone before it will record anything, and all three hang off the
// workspace. See basewrite.go.
type statePort struct {
	workDir, gitDir string
	ws              *workspace
}

func (s statePort) LoadBase() (provenance.Base, bool, error) { return wsstate.LoadBase(s.workDir) }

// SaveBase records a base through the §5.7 guard. Every path in the
// codebase that records one arrives here; nothing calls wsstate.SaveBase
// directly, and `internal/architecture` fails the build if anything
// starts to.
func (s statePort) SaveBase(ctx context.Context, base provenance.Base) error {
	return s.writeBase(ctx, base, nil)
}

// SaveSyncTargetBase records the base a `sanho sync --continue` adopts,
// offering the sync's entry head as the guard's evidence. See the
// warrant list in basewrite.go for why that one write needs evidence a
// tree comparison cannot supply.
func (s statePort) SaveSyncTargetBase(ctx context.Context, base provenance.Base, entryHead string) error {
	return s.writeBase(ctx, base, &syncCompletion{entryHead: entryHead})
}

func (s statePort) ClearBase() error { return wsstate.ClearBase(s.workDir) }

// LoadSyncNote adapts the wsstate note to the use-case one, and
// translates the corrupt-file case across the layer boundary.
//
// A use case may not import infra, so wsstate.ErrSyncNoteCorrupt cannot
// travel as itself; this is the one place that sees both sides, so it
// re-raises the condition as docsync.ErrSyncNoteCorrupt with the file
// detail attached. exists stays true: a note that cannot be parsed is
// still a note, and `sanho sync --abort` must be able to clear it.
func (s statePort) LoadSyncNote() (docsync.SyncNote, bool, error) {
	note, ok, err := wsstate.LoadSyncNote(s.gitDir)
	switch {
	case errors.Is(err, wsstate.ErrSyncNoteCorrupt):
		return docsync.SyncNote{}, true, fmt.Errorf("%w: %s",
			docsync.ErrSyncNoteCorrupt, errDetail(err, wsstate.ErrSyncNoteCorrupt))
	case err != nil || !ok:
		return docsync.SyncNote{}, false, err
	}
	return docsync.SyncNote{
		PrevBase:            note.PrevBase,
		Target:              note.Target,
		EntryHead:           note.EntryHead,
		EntryDocsTree:       note.EntryDocsTree,
		MergedTree:          note.MergedTree,
		Conflicts:           note.Conflicts,
		PreDatesEntryRecord: note.PreDatesEntryRecord(),
	}, true, nil
}

func (s statePort) SaveSyncNote(note docsync.SyncNote) error {
	return wsstate.SaveSyncNote(s.gitDir, wsstate.SyncNote{
		PrevBase:      note.PrevBase,
		Target:        note.Target,
		StartedAt:     time.Now().UTC(),
		EntryHead:     note.EntryHead,
		EntryDocsTree: note.EntryDocsTree,
		MergedTree:    note.MergedTree,
		Conflicts:     note.Conflicts,
	})
}

func (s statePort) ClearSyncNote() error { return wsstate.ClearSyncNote(s.gitDir) }

// SyncInProgress is publication's slice of the same state, and it
// answers on existence alone: a note that cannot be parsed still means a
// sync owns the docs worktree, so publication must still refuse. The
// reason it cannot be read is the CLI's to report, from the richer
// LoadSyncNote above.
func (s statePort) SyncInProgress() (bool, error) {
	_, ok, err := wsstate.LoadSyncNote(s.gitDir)
	if errors.Is(err, wsstate.ErrSyncNoteCorrupt) {
		return true, nil
	}
	return ok, err
}

func (w *workspace) statePort() statePort {
	return statePort{workDir: w.root, gitDir: w.gitDir, ws: w}
}

// --- AppRepoPort ------------------------------------------------------

// appPort adapts appgit.Repo to the app-repository ports.
//
// Everything but the merge is appgit's own. MergeDocs is composed here
// because it is the one operation spanning both infra packages:
// canonical.MergeTree is told to run in the *app worktree*, which is
// where §5.5 puts the sync merge and where all three trees live once
// canonical objects have been imported. Conflict paths come out of the
// merge relative to the docs root, so they are prefixed with the docs
// directory — the port contract is repository-relative paths, matching
// the marker scanners and §5.9's `docs/api.md` rendering.
type appPort struct{ *appgit.Repo }

// NewestDocsBase is the pushed tip's own account of where its docs came
// from: the newest provenance trailer reachable from it (§5.10's scan,
// applied to a tip rather than to HEAD). Publication's fast-forward gate
// is the caller.
func (a appPort) NewestDocsBase(ctx context.Context, tip string) (provenance.Base, bool, error) {
	commits, err := a.CommitTrailers(ctx, tip, deriveScanDepth)
	if err != nil {
		return provenance.Base{}, false, err
	}
	base, ok := provenance.SelectBase(commits)
	return base, ok, nil
}

func (a appPort) MergeDocs(ctx context.Context, baseTree, oursTree, theirsTree string) (string, []string, bool, error) {
	result, err := canonical.MergeTree(ctx, a.WorkDir(), baseTree, oursTree, theirsTree)
	if err != nil {
		return "", nil, false, err
	}
	return result.Tree, prefixDocsPaths(a.DocsDir(), result.Conflicts), result.Clean, nil
}

func prefixDocsPaths(docsDir string, names []string) []string {
	prefixed := make([]string, 0, len(names))
	for _, name := range names {
		prefixed = append(prefixed, path.Join(docsDir, name))
	}
	return prefixed
}

func (w *workspace) appPort() appPort { return appPort{Repo: w.repo} }
