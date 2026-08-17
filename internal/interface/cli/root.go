// Package cli is the sanho v0.2 command-line interface: the commands of
// docs/cli-json.md and the six hook entry points of docs/architecture.md
// "Git hooks", and the one place in the codebase where the layers are
// wired together.
//
// The commands are deliberately not counted here. The number was, and
// went stale as soon as one was added; the documents named above are
// where the surface is enumerated, and the hook count stays because six
// is a contract rather than a tally.
//
// That wiring role is deliberate and is the reason several adapters live
// here rather than in a use case. The architecture gate forbids a
// usecase package from importing infra and an infra package from
// importing usecase; interface/cli may see both, so it is where
// canonical.Link, appgit.Repo, wsstate and registry are bound to the
// ports that usecase/publish, usecase/docsync and usecase/admin declare
// (see ports.go).
//
// It is also where *lifecycle glue* lives — init, clean, migrate,
// doctor. See the placement note in usecase/admin's package comment for
// why those are commands rather than use cases.
package cli

import (
	"errors"
	"io"
	"os"
	"runtime/debug"
	"strings"

	"github.com/spf13/cobra"
)

// Exit codes (docs/cli-json.md), unchanged from v0.1:
//
//	0  success
//	1  user-actionable — a state the user can fix, named in the message
//	2  internal bug — a panic or an explicitly classified defect
const (
	exitSuccess  = 0
	exitUser     = 1
	exitInternal = 2
)

// errInternal marks a failure as a bug in sanho rather than a state the
// user can act on, routing it to exit code 2.
var errInternal = errors.New("internal error")

// errAlreadyReported is returned by a command that has already written
// its own guidance — the multi-line the guidance contract templates, which must appear
// exactly as composed rather than behind a "sanho: " prefix. The root
// renderer prints nothing more for it and exits 1.
var errAlreadyReported = errors.New("guidance already reported")

var errInvalidArguments = errors.New("invalid arguments")

// BuildInfo carries the resolved release version.
type BuildInfo struct {
	Version string
}

// Execute runs the CLI and terminates the process with the JSON contract exit
// code.
func Execute(info BuildInfo) { os.Exit(Run(info, os.Args[1:], os.Stdout, os.Stderr)) }

// Run executes one CLI invocation and returns its exit code, without
// terminating the process. Splitting it out of Execute is what lets the
// hooks and commands be exercised in-process.
//
// A panic anywhere below is classified as exit 2: an internal bug is the
// one failure class the user cannot act on, so it is reported as such
// instead of being mistaken for a refusal.
func Run(info BuildInfo, args []string, stdout, stderr io.Writer) (code int) {
	defer func() {
		if recovered := recover(); recovered != nil {
			writef(stderr, "sanho: internal error: %v\n", recovered)
			writef(stderr, "%s\n", debug.Stack())
			code = exitInternal
		}
	}()

	root := newRootCmd(info)
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)

	// ExecuteC rather than Execute for the command it resolved: an
	// argument cobra rejects never reaches RunE, so this is the only
	// place that still knows both which command was meant and what it
	// was handed.
	cmd, err := root.ExecuteC()
	if err == nil {
		return exitSuccess
	}
	// The JSON contract owes an envelope for *every* failure of a --json
	// command, and a malformed invocation is the one an agent is most
	// likely to hit first. Writing it here keeps the promise for the
	// failures the command itself never sees.
	var argErr *argumentError
	if errors.As(err, &argErr) && jsonRequested(cmd, args) {
		writeJSONError(cmd.OutOrStdout(), err)
	}
	return renderError(stderr, err)
}

// errorPrefix heads every user-facing error line.
const errorPrefix = "sanho: "

// renderError applies the guidance contract rule that a user never sees a raw Go
// error chain: a command that composed its own guidance already printed
// it, anything else is prefixed once.
//
// "Once" is the operative word. Several messages are normative *whole
// lines* — the legacy-workspace contract fixes `sanho: this workspace uses the v0.1 layout; run
// 'sanho migrate'` exactly, and hooks print it verbatim — so they carry
// the prefix themselves. Prefixing those again would produce
// `sanho: sanho: …` and break the very string the spec pins.
func renderError(stderr io.Writer, err error) int {
	switch {
	case errors.Is(err, errAlreadyReported):
		return exitUser
	case errors.Is(err, errInternal):
		writef(stderr, "%sinternal error: %v\n", errorPrefix, err)
		return exitInternal
	}

	// the guidance contract: no raw Go error chain at user level. The infra packages tag
	// their failures with a package name to locate them; that is a
	// diagnostic for us, not information for the user (F-M3).
	message := stripInternalPrefixes(err.Error())
	if !strings.HasPrefix(message, errorPrefix) {
		message = errorPrefix + message
	}
	writeln(stderr, message)
	return exitUser
}

// verbose is the global --verbose flag. It only ever adds detail; no
// behavior depends on it.
var verbose bool

func newRootCmd(info BuildInfo) *cobra.Command {
	verbose = false

	root := &cobra.Command{
		Use:   "sanho",
		Short: "Keep docs/ synchronized with a canonical docs repository",
		Long: `Sanho keeps the docs/ directory of an application repository synchronized
with one canonical docs repository.

Publication happens at 'git push'; the commit path only performs a local,
network-free freshness check. Reconciling is an explicit command
('sanho sync') that runs between your own commits — sanho never creates
commits in your repository.`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Print additional diagnostic detail")

	root.AddCommand(
		newInitCmd(),
		newStatusCmd(),
		newDiffCmd(),
		newLogCmd(),
		newShowCmd(),
		newPreviewCmd(),
		newCheckCmd(),
		newStateCmd(),
		newSyncCmd(),
		newPullCmd(),
		newCleanCmd(),
		newDoctorCmd(),
		newProjectCmd(),
		newWorkspaceCmd(),
		newHookCmd(),
		newMigrateCmd(),
		newVersionCmd(info),
	)

	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return newArgumentError(err)
	})
	classifyArgumentErrors(root)
	return root
}

// classifyArgumentErrors gives every declared Args rule the machine
// identity its rejection owes an agent, which is what lets Run write the
// `invalid_arguments` envelope for a command that never got to run.
//
// A command whose Args is still nil is skipped, and that is the point
// rather than an omission: cobra's Find only consults legacyArgs — the
// unknown-command check, suggestions included — while Args is nil, so
// installing a wrapper on the root would turn `sanho statu` from a
// refusal into a help page at exit 0.
//
// The skip costs no coverage because every command with a JSON mode
// declares an Args rule of its own. The rule need not be cobra.NoArgs
// and once was not asserted to be: `sanho show` declares
// cobra.ExactArgs(1), and keying on the declaration rather than on one
// particular rule is what let it owe the same envelope without any
// change here.
func classifyArgumentErrors(cmd *cobra.Command) {
	for _, child := range cmd.Commands() {
		classifyArgumentErrors(child)
	}
	declared := cmd.Args
	if declared == nil {
		return
	}
	cmd.Args = func(c *cobra.Command, args []string) error {
		if err := declared(c, args); err != nil {
			return newArgumentError(err)
		}
		return nil
	}
}

// runGroup is every command group's RunE: list the subcommands when the
// group was named alone, and refuse a word that resolves to none of
// them.
//
// A group needs a RunE at all because cobra returns flag.ErrHelp for a
// command it considers unrunnable *before* it validates arguments — so
// an Args rule on a group is unreachable, and `sanho project ad` printed
// help and exited 0. Leaving Args nil is equally deliberate: it keeps
// the group out of classifyArgumentErrors, so the refusal reads exactly
// like the root's instead of gaining a prefix the root does not have.
func runGroup(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return cmd.Help()
	}
	return unknownSubcommandError(cmd, args[0])
}

// debugf prints a diagnostic line under --verbose. Diagnostics go to
// stderr so that --json stdout stays machine-readable.
func debugf(cmd *cobra.Command, format string, args ...any) {
	if verbose {
		writef(cmd.ErrOrStderr(), "sanho: "+format+"\n", args...)
	}
}
