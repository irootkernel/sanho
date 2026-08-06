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
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/irootkernel/sanho/internal/infra/fsx"
	"github.com/irootkernel/sanho/internal/infra/gitx"
	"github.com/irootkernel/sanho/internal/infra/registry"

	"github.com/spf13/cobra"
)

// Check severities.
const (
	severityOK      = "ok"
	severityWarning = "warning"
)

// doctorJSON is the stable `sanho doctor --json` schema:
//
//	{
//	  "workspace": "<abs path>",
//	  "checks": [{"name": "hooks", "severity": "ok" | "warning",
//	              "detail": "..."}],
//	  "warnings": 1
//	}
//
// Severity is a two-value vocabulary on purpose: a check either passed
// or found something the user should look at. A third "error" level
// would be indistinguishable in practice from a warning, since doctor
// never stops at the first problem.
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
	cmd.Flags().BoolVar(&fix, "fix", false, "Re-derive the docs base from commit history when it is missing or invalid")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print machine-readable JSON")
	return cmd
}

func runDoctor(cmd *cobra.Command, fix, asJSON bool) error {
	ctx := cmd.Context()
	ws, err := requireV2Workspace(ctx)
	if err != nil {
		return err
	}

	var out report
	checkGitVersion(ctx, ws, &out)
	out.ok("workspace-config", "schema version %d, docs dir %s, project %s",
		ws.config.SchemaVersion, ws.config.DocsDir, ws.config.Project)
	checkHooks(ctx, ws, &out)
	checkClone(ctx, ws, &out)
	checkBase(ctx, ws, fix, &out)
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

// checkGitVersion reports the installed git. §5.4 decided against
// enforcing a minimum: sanho uses whatever git is installed, and an
// older one fails with git's own clear error where it matters.
func checkGitVersion(ctx context.Context, ws *workspace, out *report) {
	line, err := gitx.New(ws.root).Line(ctx, "--version")
	if err != nil {
		out.warn("git", "could not read the git version: %v", err)
		return
	}
	out.ok("git", "%s (no minimum is enforced; merges need git 2.38 or newer)", line)
}

func checkHooks(ctx context.Context, ws *workspace, out *report) {
	states, err := ws.repo.HooksStatus(ctx)
	if err != nil {
		out.warn("hooks", "could not inspect the hooks directory: %v", err)
		return
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
	if len(problems) > 0 {
		out.warn("hooks", "%s — run 'sanho init --force' to reinstall", strings.Join(problems, "; "))
		return
	}
	out.ok("hooks", "all %d hooks installed exactly once", len(states))
}

func checkClone(ctx context.Context, ws *workspace, out *report) {
	store, err := ws.openCanonical()
	if err != nil {
		out.warn("clone", "the private clone is missing (%s) — run 'sanho init' in this workspace", ws.cloneDir())
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

	if _, _, err := store.Head(ctx); err != nil {
		out.ok("canonical-head", "the canonical repository has no commits yet; your first push will publish docs")
	}
}

// checkBase validates the base file and, under --fix, repairs it by
// re-running the §5.10 derivation. The repair is entirely local, which
// is the whole reason a lost trailer is not a crisis (D2, audit H4).
func checkBase(ctx context.Context, ws *workspace, fix bool, out *report) {
	base, hasBase, err := ws.statePort().LoadBase()
	switch {
	case err != nil:
		out.warn("base", "the base file is unreadable: %v — %s", err, doctorFixHint)
	case !hasBase:
		out.warn("base", "no docs base is recorded — %s", doctorFixHint)
	case !base.Valid():
		out.warn("base", "the recorded base is not a valid OID pair — %s", doctorFixHint)
	default:
		out.ok("base", "commit %s, tree %s", shortOID(base.Commit), shortOID(base.Tree))
		return
	}

	if !fix {
		return
	}
	derived, found, deriveErr := deriveBase(ctx, ws.root)
	if deriveErr != nil || !found {
		out.warn("base-fix", "no commit in the last %d carries a docs-base or docs-version trailer; run 'sanho sync' to establish a base", deriveScanDepth)
		return
	}
	if saveErr := ws.statePort().SaveBase(derived); saveErr != nil {
		out.warn("base-fix", "could not write the base file: %v", saveErr)
		return
	}
	out.ok("base-fix", "re-derived the base as %s from commit history", shortOID(derived.Commit))
}

// checkRegistry proves the registry is both readable and lockable. The
// lock probe matters because a stale holder is invisible in the file
// itself and shows up only as an unexplained hang elsewhere (§5.7).
func checkRegistry(ctx context.Context, out *report) {
	file, err := openRegistry()
	if err != nil {
		out.warn("registry", "could not open the sanho home: %v", err)
		return
	}
	if _, err := file.Read(ctx); err != nil {
		out.warn("registry", "could not read the registry: %v", err)
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

func checkSyncNote(ws *workspace, out *report) {
	prev, target, exists, err := ws.statePort().LoadSyncNote()
	switch {
	case err != nil:
		out.warn("sync", "the sync note is unreadable: %v — run 'sanho sync --abort' to clear it", err)
	case exists:
		out.warn("sync", "a sync from %s to %s is unresolved — resolve the markers and commit, or run 'sanho sync --abort'",
			shortOID(prev.Commit), shortOID(target.Commit))
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
		out.warn("docs", "could not inventory %s: %v", root, walkErr)
		return
	}
	out.ok("docs", "%d files, %d bytes, %d symlinks in %s", files, bytes, symlinks, ws.config.DocsDir)
}

func renderDoctor(out io.Writer, ws *workspace, result report) {
	writef(out, "sanho doctor: %s\n\n", ws.root)
	for _, check := range result.checks {
		marker := "ok  "
		if check.Severity == severityWarning {
			marker = "warn"
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
