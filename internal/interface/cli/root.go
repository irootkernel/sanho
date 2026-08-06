// Package cli is the sanho v0.2 command-line interface: the nine
// commands and six hook entry points of sanho-v0.2.md §5.8/§5.10, and
// the one place in the codebase where the layers are wired together.
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
	"os"
	"runtime/debug"
	"strings"

	"github.com/spf13/cobra"
)

// Exit codes (sanho-v0.2.md §5.8), unchanged from v0.1:
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
// its own guidance — the multi-line §5.9 templates, which must appear
// exactly as composed rather than behind a "sanho: " prefix. The root
// renderer prints nothing more for it and exits 1.
var errAlreadyReported = errors.New("guidance already reported")

// BuildInfo carries the ldflags-injected build identity.
type BuildInfo struct {
	Version   string
	Commit    string
	BuildDate string
}

// Execute runs the CLI and terminates the process with the §5.8 exit
// code.
func Execute(info BuildInfo) { os.Exit(Run(info, os.Args[1:], os.Stdout, os.Stderr)) }

// Run executes one CLI invocation and returns its exit code, without
// terminating the process. Splitting it out of Execute is what lets the
// hooks and commands be exercised in-process.
//
// A panic anywhere below is classified as exit 2: an internal bug is the
// one failure class the user cannot act on, so it is reported as such
// instead of being mistaken for a refusal.
func Run(info BuildInfo, args []string, stdout, stderr *os.File) (code int) {
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

	if err := root.Execute(); err != nil {
		return renderError(stderr, err)
	}
	return exitSuccess
}

// errorPrefix heads every user-facing error line.
const errorPrefix = "sanho: "

// renderError applies the §5.9 rule that a user never sees a raw Go
// error chain: a command that composed its own guidance already printed
// it, anything else is prefixed once.
//
// "Once" is the operative word. Several messages are normative *whole
// lines* — §8 fixes `sanho: this workspace uses the v0.1 layout; run
// 'sanho migrate'` exactly, and hooks print it verbatim — so they carry
// the prefix themselves. Prefixing those again would produce
// `sanho: sanho: …` and break the very string the spec pins.
func renderError(stderr *os.File, err error) int {
	switch {
	case errors.Is(err, errAlreadyReported):
		return exitUser
	case errors.Is(err, errInternal):
		writef(stderr, "%sinternal error: %v\n", errorPrefix, err)
		return exitInternal
	}

	message := err.Error()
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
		newStateCmd(),
		newSyncCmd(),
		newPullCmd(),
		newCleanCmd(),
		newDoctorCmd(),
		newProjectCmd(),
		newHookCmd(),
		newMigrateCmd(),
		newVersionCmd(info),
	)
	return root
}

// debugf prints a diagnostic line under --verbose. Diagnostics go to
// stderr so that --json stdout stays machine-readable.
func debugf(cmd *cobra.Command, format string, args ...any) {
	if verbose {
		writef(cmd.ErrOrStderr(), "sanho: "+format+"\n", args...)
	}
}
