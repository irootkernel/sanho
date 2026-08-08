package cli

// `sanho doctor` (sanho-v0.2.md §5.8): report on everything a workspace
// depends on, and repair the one thing that is repairable offline.
//
// The exit-code rule is what makes doctor usable: exit 0 with warnings
// listed, exit 1 only when a *check itself* could not run. A tool whose
// diagnostic command fails whenever it finds a problem cannot be used to
// investigate a problem.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/irootkernel/sanho/internal/domain/provenance"
	"github.com/irootkernel/sanho/internal/infra/appgit"
	"github.com/irootkernel/sanho/internal/infra/canonical"
	"github.com/irootkernel/sanho/internal/infra/fsx"
	"github.com/irootkernel/sanho/internal/infra/gitx"
	"github.com/irootkernel/sanho/internal/infra/registry"
	"github.com/irootkernel/sanho/internal/infra/wsstate"
	"github.com/irootkernel/sanho/internal/usecase/admin"
	"github.com/irootkernel/sanho/internal/usecase/docsync"

	"github.com/spf13/cobra"
)

// Check severities.
//
// severityInfo is neither a pass nor a problem: it reports a state that
// looks irregular and is not. The base-derivation check is the reason it
// exists — a base that disagrees with commit history is a warning when
// re-derivation *would* have run and a plain fact when §5.10
// deliberately withheld it, and calling both "warning" would train the
// reader to ignore the one that matters.
const (
	severityOK      = "ok"
	severityInfo    = "info"
	severityWarning = "warning"
)

// doctorJSON is the stable `sanho doctor --json` schema:
//
//	{
//	  "workspace": "<abs path>",
//	  "checks": [{"name": "hooks", "severity": "ok" | "info" | "warning",
//	              "detail": "..."}],
//	  "warnings": 1
//	}
//
// There is deliberately no "error" level: it would be indistinguishable
// in practice from a warning, since doctor never stops at the first
// problem and never fails for having found one. "info" is not a third
// severity of problem — it marks a check that has something to say and
// nothing to ask for, and `warnings` counts only "warning".
type doctorJSON struct {
	Workspace string      `json:"workspace"`
	Checks    []checkJSON `json:"checks"`
	Warnings  int         `json:"warnings"`
}

type checkJSON struct {
	Name     string `json:"name"`
	Severity string `json:"severity"`
	Detail   string `json:"detail"`
}

// report accumulates checks.
type report struct{ checks []checkJSON }

func (r *report) ok(name, format string, args ...any) {
	r.checks = append(r.checks, checkJSON{Name: name, Severity: severityOK, Detail: fmt.Sprintf(format, args...)})
}

func (r *report) info(name, format string, args ...any) {
	r.checks = append(r.checks, checkJSON{Name: name, Severity: severityInfo, Detail: fmt.Sprintf(format, args...)})
}

func (r *report) warn(name, format string, args ...any) {
	r.checks = append(r.checks, checkJSON{Name: name, Severity: severityWarning, Detail: fmt.Sprintf(format, args...)})
}

func (r *report) warnings() int {
	count := 0
	for _, check := range r.checks {
		if check.Severity == severityWarning {
			count++
		}
	}
	return count
}

func newDoctorCmd() *cobra.Command {
	var fix, asJSON bool

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check this workspace's sanho installation",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return runDoctor(cmd, fix, asJSON) },
	}
	cmd.Flags().BoolVar(&fix, "fix", false, "Repair what can be repaired locally: reinstall missing hooks and re-derive the docs base")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print machine-readable JSON")
	return cmd
}

// runDoctor resolves the workspace and reports on it.
//
// A CORRUPT config does not stop it (M4). Doctor is the command a user
// reaches for when something is wrong, so the one state in which it
// refused to run was the one it was most needed in: `.sanho.json`
// parses as JSON and is neither a v0.1 config nor a v2 one, and every
// other command says so and exits — including this one. What the rest of
// the checks need is a docs directory and a git repository, both of
// which survive a damaged config, so they run against the defaults and
// the config itself is reported as the warning it is.
func runDoctor(cmd *cobra.Command, fix, asJSON bool) error {
	ctx := cmd.Context()
	ws, configErr := requireV2Workspace(ctx)
	if configErr != nil {
		var degraded *workspace
		if ws, degraded = nil, degradedWorkspace(ctx, configErr); degraded == nil {
			return finishCommand(cmd, nil, asJSON, configErr)
		}
		ws = degraded
	}

	var out report
	checkGitVersion(ctx, ws, &out)
	if configErr != nil {
		// No command is named, and that is D3 rather than an omission:
		// every route out of a corrupt config needs values the file was
		// supposed to hold (the project, the docs repository URL), so
		// anything printed here would be a shape rather than a runnable
		// command. The backup is where those values are.
		out.warn("workspace-config", "%s is unreadable and the checks below ran against the defaults: %s (a copy may be at %s%s)",
			wsstate.ConfigFileName, causeOf(configErr), wsstate.ConfigFileName, backupSuffix)
	} else {
		out.ok("workspace-config", "schema version %d, docs dir %s, project %s",
			ws.config.SchemaVersion, ws.config.DocsDir, ws.config.Project)
	}
	hooksRepaired := checkHooks(ctx, ws, fix, &out)
	checkClone(ctx, ws, &out)
	checkBase(ctx, ws, fix, &out)
	if hooksRepaired {
		checkPublicationAfterHookRepair(ctx, ws, &out)
	}
	checkRegistry(ctx, &out)
	checkSyncNote(ws, &out)
	checkDocsInventory(ws, &out)

	if asJSON {
		return writeJSON(cmd.OutOrStdout(), doctorJSON{
			Workspace: ws.root,
			Checks:    out.checks,
			Warnings:  out.warnings(),
		})
	}
	renderDoctor(cmd.OutOrStdout(), ws, out)
	return nil
}

// degradedWorkspace assembles just enough workspace for doctor's other
// checks when the config cannot be read.
//
// Only a CORRUPT config is degraded this way. "Not a workspace" and "a
// v0.1 workspace" are answers rather than damage — each has its own
// message naming the one command that works — so both keep failing the
// command outright.
func degradedWorkspace(ctx context.Context, configErr error) *workspace {
	if !errors.Is(configErr, wsstate.ErrConfigCorrupt) {
		return nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	root, err := filepath.Abs(cwd)
	if err != nil {
		return nil
	}
	config := wsstate.Config{}
	config.ApplyDefaults()

	ws := &workspace{root: root, configRoot: root, config: config}
	if err := ws.resolveGitDirs(ctx); err != nil {
		return nil
	}
	if ws.homeDir, err = resolveHome(); err != nil {
		return nil
	}
	ws.repo = appgit.New(root, config.DocsDir, gitx.New(root))
	return ws
}

// checkGitVersion reports the installed git. §5.4 decided against
// enforcing a minimum: sanho uses whatever git is installed, and an
// older one fails with git's own clear error where it matters.
func checkGitVersion(ctx context.Context, ws *workspace, out *report) {
	line, err := gitx.New(ws.root).Line(ctx, "--version")
	if err != nil {
		out.warn("git", "could not read the git version: %s", causeOf(err))
		return
	}
	out.ok("git", "%s (no minimum is enforced; merges need git 2.38 or newer)", line)
}

// checkHooks reports hook problems and, under --fix, repairs them.
//
// Repair is a reinstall, and it is safe to advise because the installer
// is exact-line and preserves every foreign line in a hook file the user
// also owns (F-H6b). The previous advice was `sanho init --force`, which
// replaces the docs directory — a destructive answer to "a hook line is
// missing", and one that refuses outright in an initialized workspace.
func checkHooks(ctx context.Context, ws *workspace, fix bool, out *report) bool {
	if ws.config.HookMode == "" {
		if _, err := ws.repo.DefaultHooksDir(ctx); err != nil {
			var custom *appgit.CustomHooksPathError
			if errors.As(err, &custom) {
				out.warn("hooks", "%s", customHooksPathMessage(custom.Path))
				return false
			}
			out.warn("hooks", "could not inspect the hooks directory: %s", causeOf(err))
			return false
		}
	}
	problems, err := hookProblems(ctx, ws)
	if err != nil {
		out.warn("hooks", "could not inspect the hooks directory: %s", causeOf(err))
		return false
	}
	if len(problems) == 0 {
		out.ok("hooks", "all %d hooks installed exactly once", len(appgit.Hooks()))
		return false
	}
	if !fix {
		out.warn("hooks", "%s", doctorHooksMessage(strings.Join(problems, "; ")))
		return false
	}

	// Remove first, then install: a duplicated or legacy line is only
	// cleared by the removal pass, and installing over it would leave
	// both (audit L3).
	if err := ws.repo.RemoveHooks(ctx); err != nil {
		out.warn("hooks-fix", "could not remove the old hook lines: %s", causeOf(err))
		return false
	}
	if err := ws.repo.InstallHooks(ctx); err != nil {
		out.warn("hooks-fix", "could not reinstall the hooks: %s", causeOf(err))
		return false
	}
	remaining, err := hookProblems(ctx, ws)
	if err != nil || len(remaining) > 0 {
		out.warn("hooks-fix", "%s", doctorHooksMessage(strings.Join(remaining, "; ")))
		return false
	}
	out.ok("hooks", "reinstalled %d hooks (%s)", len(appgit.Hooks()), strings.Join(problems, "; "))
	return true
}

func checkPublicationAfterHookRepair(ctx context.Context, ws *workspace, out *report) {
	base, hasBase, err := ws.statePort().LoadBase()
	if err != nil {
		return
	}
	syncing, err := ws.statePort().SyncInProgress()
	if err != nil {
		return
	}
	known, pending := admin.DetectPublication(ctx, ws.repo, base, hasBase, syncing)
	if known && pending {
		out.warn("publication", "%s", doctorPublicationPendingMessage())
	}
}

// hookProblems lists everything wrong with the installed hooks.
func hookProblems(ctx context.Context, ws *workspace) ([]string, error) {
	states, err := ws.repo.HooksStatus(ctx)
	if err != nil {
		return nil, err
	}

	var problems []string
	for _, state := range states {
		switch {
		case !state.Installed:
			problems = append(problems, state.Name+": missing")
		case state.Occurrences > 1:
			problems = append(problems, fmt.Sprintf("%s: installed %d times", state.Name, state.Occurrences))
		case !state.Executable:
			problems = append(problems, state.Name+": not executable")
		}
		if len(state.Legacy) > 0 {
			problems = append(problems, state.Name+": carries v0.1 lines")
		}
	}
	return problems, nil
}

func checkClone(ctx context.Context, ws *workspace, out *report) {
	store, err := ws.openCanonical()
	if err != nil {
		out.warn("clone", "%s", cloneMissingMessage(ws.cloneDir()))
		return
	}
	if store.URL() != ws.config.DocsRepoURL {
		out.warn("clone", "origin is %s but the workspace config says %s", store.URL(), ws.config.DocsRepoURL)
		return
	}

	age, fetched := store.Age()
	switch {
	case !fetched:
		out.warn("clone", "%s", neverFetchedLine)
	case age > staleDataThreshold:
		out.warn("clone", "%s", staleCanonicalLine(age))
	default:
		out.ok("clone", "%s, branch %s, fetched %s ago", store.Dir(), store.Branch(), humanizeAge(age))
	}

	// An empty publication branch is the ordinary starting state of a new
	// project; anything else is a clone that cannot answer for itself and
	// must be reported as a problem, not as "[ok] no commits yet" (F-M1).
	switch head, _, err := store.Head(ctx); {
	case err == nil:
		out.ok("canonical-head", "%s", shortOID(head))
	case errors.Is(err, canonical.ErrEmptyBranch):
		out.ok("canonical-head", "the canonical repository has no commits yet; your first push will publish docs")
	default:
		out.warn("canonical-head", "could not read the canonical head: %s", stripInternalPrefixes(causeLine(err)))
	}

	checkOriginReachable(ctx, ws, store, out)
}

// originProbeTimeout bounds the reachability probe. Doctor is a health
// check; it may not sit on a hanging network for a minute.
const originProbeTimeout = 10 * time.Second

// checkOriginReachable probes the configured docs repository (F-M1).
//
// It is warn-only and deliberately so: every read path in v0.2 works
// from the last fetch (§5.2), so an unreachable origin is a fact worth
// reporting and never a reason for `sanho doctor` to fail. `ls-remote
// --exit-code` is the cheapest question that actually opens the
// transport, and the network runner is what keeps a missing credential
// from turning into an interactive prompt inside a diagnostic.
func checkOriginReachable(ctx context.Context, ws *workspace, store *canonical.Store, out *report) {
	probeCtx, cancel := context.WithTimeout(ctx, originProbeTimeout)
	defer cancel()

	run := gitx.New(store.Dir(), gitx.WithNetwork(), gitx.WithTimeout(originProbeTimeout))
	res, err := run.RunExit(probeCtx, "ls-remote", "--exit-code", "--heads", ws.config.DocsRepoURL)
	switch {
	case err != nil:
		out.warn("origin", "%s is not reachable right now: %s", ws.config.DocsRepoURL, stripInternalPrefixes(causeLine(err)))
	case res.ExitCode == 0:
		out.ok("origin", "%s is reachable", ws.config.DocsRepoURL)
	case res.ExitCode == 2:
		// git's documented "no matching refs": reachable, and empty.
		out.ok("origin", "%s is reachable and has no branches yet", ws.config.DocsRepoURL)
	default:
		out.warn("origin", "%s is not reachable right now: %s",
			ws.config.DocsRepoURL, strings.TrimSpace(firstLineOf(string(res.Stderr))))
	}
}

// firstLineOf reduces git stderr to its first non-empty line.
func firstLineOf(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) != "" {
			return line
		}
	}
	return "git reported no diagnostics"
}

// checkBase validates the base file and, under --fix, repairs it by
// re-running the §5.10 derivation. The repair is entirely local, which
// is the whole reason a lost trailer is not a crisis (D2, audit H4).
func checkBase(ctx context.Context, ws *workspace, fix bool, out *report) {
	base, hasBase, err := ws.statePort().LoadBase()
	switch {
	case err != nil:
		out.warn("base", "the base file is unreadable: %s — %s", causeOf(err), doctorFixHint)
	case !hasBase:
		out.warn("base", "no docs base is recorded — %s", doctorFixHint)
	case !base.Valid():
		out.warn("base", "the recorded base is not a valid OID pair — %s", doctorFixHint)
	default:
		out.ok("base", "commit %s, tree %s", shortOID(base.Commit), shortOID(base.Tree))
		checkBaseDerivation(ctx, ws, base, fix, out)
		return
	}

	if fix {
		repairBase(ctx, ws, out)
	}
}

// repairBase re-runs the §5.10 derivation and writes the result,
// reporting under the `base-fix` name.
//
// It stands down while a sync is unfinished, for the same reason
// post-checkout re-derivation does (§5.10) and checkBaseDerivation
// already did: the note holds the base still until the user completes
// or undoes the sync, and a repair that wrote the newest stamped
// commit's value would be a third party moving the one file the window
// is defined by. The reading is deliberately wide — an unreadable note
// counts as a sync in progress, because "the sync state is unknown" is
// not a state to write a base in.
//
// It is reported as info rather than as a warning: the unfinished sync
// is already a warning of its own from checkSyncNote, and counting it
// twice would make `sanho doctor --fix` look like it made things worse.
func repairBase(ctx context.Context, ws *workspace, out *report) {
	if _, syncing, noteErr := ws.statePort().LoadSyncNote(); syncing || noteErr != nil {
		out.info("base-fix", "%s", syncNotePendingMessage(
			"a sync is in progress, so the docs base was not re-derived"))
		return
	}

	derived, found, deriveErr := deriveBase(ctx, ws)
	if deriveErr != nil || !found {
		out.warn("base-fix", "%s", baseNeedsSyncMessage(fmt.Sprintf(
			"no commit in the last %d carries a docs-base or docs-version trailer", deriveScanDepth)))
		return
	}
	switch saveErr := ws.statePort().SaveBase(ctx, derived); {
	case saveErr == nil:
	case errors.Is(saveErr, docsync.ErrBaseNotCorroborated):
		// The §5.7 guard refused: history names a base the documents in
		// this worktree cannot account for. Reported as info and named
		// with no command, because there is nothing for the user to run
		// — the repair IS the derivation, and it is the derivation that
		// is untrustworthy here. The base-derivation check above already
		// states the disagreement.
		out.info("base-fix", "the docs base was not re-derived: %s", causeOfBaseRefusal(saveErr))
		return
	default:
		out.warn("base-fix", "could not write the base file: %s", causeOf(saveErr))
		return
	}
	out.ok("base-fix", "re-derived the base as %s from commit history", shortOID(derived.Commit))
}

// checkBaseDerivation is the §5.10 promise this report never kept: base
// re-derivation is deliberately withheld whenever the docs worktree
// differs from HEAD's, and the comment on rederiveBaseAfterHeadMoved
// says `sanho doctor` flags the resulting inconsistency. Nothing did.
//
// The check compares the recorded base against what re-derivation would
// pick from commit history, and reads the disagreement three ways.
//
//   - The recorded base is a *descendant* of the derived one in
//     canonical history. Nothing is wrong: publication's base-advance
//     rule moves the base past the commit the trailers name, as do
//     `pull` and `sync`, so this is the state of every workspace that
//     has just published. Silence, which is what §5.6 makes the success
//     signal.
//   - The docs worktree differs from HEAD's, so §5.10 step 1 held the
//     re-derivation back on purpose. That is a fact worth stating and
//     not a problem: `[info]`.
//   - Otherwise re-derivation *would* have run and would have produced a
//     different answer, which means the file and the history disagree
//     about which canonical state these docs came from: `[warn]`, and
//     `--fix` writes the derived value.
//
// A fourth state is not read at all: an unfinished sync owns the base
// and holds it at the pre-sync value until the resolution is confirmed,
// so between a resolution commit and the hook that settles it the file
// and the newest trailer disagree by design. The sync check reports that
// state on its own terms, and `--fix` here would only race ahead to the
// value the note is about to write anyway.
func checkBaseDerivation(ctx context.Context, ws *workspace, base provenance.Base, fix bool, out *report) {
	if _, syncing, noteErr := ws.statePort().LoadSyncNote(); syncing || noteErr != nil {
		return
	}

	derived, found, err := deriveBase(ctx, ws)
	if err != nil || !found || derived.Commit == base.Commit {
		// No provenance in history is checkBase's business under --fix,
		// not a second finding here.
		return
	}
	if advanced, known := baseIsAheadOf(ctx, ws, derived.Commit, base.Commit); known && advanced {
		return
	}

	clean, cleanErr := docsMatchHead(ctx, ws)
	if cleanErr == nil && !clean {
		out.info("base-derivation", "the docs worktree differs from HEAD, so the base was not re-derived; "+
			"history's newest stamped commit names %s, the base file names %s",
			shortOID(derived.Commit), shortOID(base.Commit))
		return
	}

	out.warn("base-derivation", "history's newest stamped commit names base %s, but the base file names %s — %s",
		shortOID(derived.Commit), shortOID(base.Commit), doctorFixHint)
	if fix {
		repairBase(ctx, ws, out)
	}
}

// baseIsAheadOf reports whether the recorded base is a descendant of the
// derived one in canonical history, and whether that could be decided at
// all. Without a clone there is nothing to decide it against, and the
// caller then treats the disagreement at face value.
func baseIsAheadOf(ctx context.Context, ws *workspace, derived, recorded string) (ahead, known bool) {
	store := canonicalOrNil(ws)
	if store == nil {
		return false, false
	}
	port := ws.canonicalPort(store)
	if resolved, err := port.ResolveCommit(ctx, derived); err != nil || !resolved {
		return false, false
	}
	if resolved, err := port.ResolveCommit(ctx, recorded); err != nil || !resolved {
		return false, false
	}
	ancestor, err := port.IsAncestor(ctx, derived, recorded)
	if err != nil {
		return false, false
	}
	return ancestor, true
}

// docsMatchHead is §5.10 step 1's own test: the worktree docs hash to
// exactly what HEAD carries.
func docsMatchHead(ctx context.Context, ws *workspace) (bool, error) {
	worktree, err := ws.repo.WorktreeDocsTree(ctx)
	if err != nil {
		return false, err
	}
	head, err := ws.repo.HeadDocsTree(ctx)
	if err != nil {
		return false, err
	}
	return worktree == head, nil
}

// checkRegistry proves the registry is both readable and lockable. The
// lock probe matters because a stale holder is invisible in the file
// itself and shows up only as an unexplained hang elsewhere (§5.7).
func checkRegistry(ctx context.Context, out *report) {
	file, err := openRegistry()
	if err != nil {
		out.warn("registry", "could not open the sanho home: %s", causeOf(err))
		return
	}
	if _, err := readRegistry(ctx, file); err != nil {
		out.warn("registry", "could not read the registry: %s", causeOf(err))
		return
	}

	lockPath := filepath.Join(file.HomeDir(), registry.LockFileName)
	probeCtx, cancel := context.WithTimeout(ctx, registryProbeTimeout)
	defer cancel()

	if err := fsx.WithFlock(probeCtx, lockPath, func() error { return nil }); err != nil {
		out.warn("registry", "the registry lock is not available: %s", registryLockHint(lockPath))
		return
	}
	out.ok("registry", "%s is readable and lockable", file.HomeDir())
}

// registryProbeTimeout keeps doctor's lock probe short: it is a health
// check, not an operation waiting for its turn.
const registryProbeTimeout = 500 * time.Millisecond

// checkSyncNote reports an owed sync.
//
// A note that cannot be parsed gets the abort guidance rather than the
// resolve-or-abort one: there is no "resolve the markers and commit"
// available when nothing can say what the sync was, and `sanho sync
// --abort` is the operation that needs only the file's existence.
func checkSyncNote(ws *workspace, out *report) {
	note, exists, err := ws.statePort().LoadSyncNote()
	switch {
	case errors.Is(err, docsync.ErrSyncNoteCorrupt):
		out.warn("sync", "%s", syncNoteCorruptMessage(errDetail(err, docsync.ErrSyncNoteCorrupt)))
	case err != nil:
		out.warn("sync", "%s", syncNotePendingMessage("the sync note is unreadable: "+causeOf(err)))
	case exists:
		out.warn("sync", "%s", syncNotePendingMessage(fmt.Sprintf("a sync from %s to %s is unresolved",
			shortOID(note.PrevBase.Commit), shortOID(note.Target.Commit))))
	default:
		out.ok("sync", "no sync is in progress")
	}
}

// checkDocsInventory reports size and symlink counts. Symlinks are
// called out because they were audit H1's silent data loss under the
// retired tar-snapshot transport; v0.2 moves content as git objects, so
// this is now an inventory line rather than a hazard.
func checkDocsInventory(ws *workspace, out *report) {
	root := filepath.Join(ws.root, filepath.FromSlash(ws.config.DocsDir))
	if _, err := os.Stat(root); os.IsNotExist(err) {
		out.ok("docs", "no docs directory yet (%s)", ws.config.DocsDir)
		return
	}

	var files, symlinks int
	var bytes int64
	walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			symlinks++
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		files++
		bytes += info.Size()
		return nil
	})
	if walkErr != nil {
		out.warn("docs", "could not inventory %s: %s", root, causeOf(walkErr))
		return
	}
	out.ok("docs", "%d files, %d bytes, %d symlinks in %s", files, bytes, symlinks, ws.config.DocsDir)
}

func renderDoctor(out io.Writer, ws *workspace, result report) {
	writef(out, "sanho doctor: %s\n\n", ws.root)
	for _, check := range result.checks {
		marker := "ok  "
		switch check.Severity {
		case severityWarning:
			marker = "warn"
		case severityInfo:
			marker = "info"
		}
		writef(out, "  [%s] %-16s %s\n", marker, check.Name, check.Detail)
	}

	warnings := result.warnings()
	if warnings == 0 {
		writeln(out, "\nno problems found")
		return
	}
	writef(out, "\n%d warning(s)\n", warnings)
}
