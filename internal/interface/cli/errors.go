package cli

// Error rendering for the two flows that are not the push hook: the
// sync/pull commands (§5.5) and the machine-readable envelope every
// `--json` command owes an agent (§5.8, F-M9).
//
// The split from reportPushError is deliberate. Push renders §5.9's
// *templates*, which end in git's own "push rejected" trailer; a command
// the user typed renders guidance without one, because nothing was
// rejected but the command itself.

import (
	"errors"
	"io"
	"strings"

	"github.com/irootkernel/sanho/internal/domain/markers"
	pubdom "github.com/irootkernel/sanho/internal/domain/publish"
	"github.com/irootkernel/sanho/internal/infra/canonical"
	"github.com/irootkernel/sanho/internal/infra/fsx"
	"github.com/irootkernel/sanho/internal/infra/registry"
	"github.com/irootkernel/sanho/internal/infra/wsstate"
	"github.com/irootkernel/sanho/internal/usecase/docsync"
	"github.com/irootkernel/sanho/internal/usecase/publish"

	"github.com/spf13/cobra"
)

// reportSyncError renders the §5.9 guidance for `sanho sync` and
// `sanho pull`.
//
// Every branch names a command that succeeds in the state it is printed
// in (D3) and every one is a Catalog entry, which is what the widened
// closure gate now enforces: before F-H6 these strings lived inside
// usecase/docsync, where no test could reach them with a real workspace.
//
// An unrecognized error falls through unchanged, so a genuine internal
// failure is never dressed up as guidance.
func reportSyncError(cmd *cobra.Command, ws *workspace, err error) error {
	stderr := cmd.ErrOrStderr()

	switch {
	// The corrupt note comes before the in-progress case: both describe
	// a sync that owns the docs worktree, but only one of them can say
	// which sync, so the unreadable file has to be named first.
	case errors.Is(err, docsync.ErrSyncNoteCorrupt):
		writeln(stderr, syncNoteCorruptMessage(errDetail(err, docsync.ErrSyncNoteCorrupt)))

	case errors.Is(err, docsync.ErrSyncInProgress):
		writeln(stderr, syncInProgressMessage(errDetail(err, docsync.ErrSyncInProgress)))

	// `--continue`'s refusal for standing on history the sync never
	// began on. It comes before the two below because it describes a
	// state in which they are all satisfied: no markers, clean docs, and
	// a note — on a branch that was never part of the merge.
	case errors.Is(err, docsync.ErrContinueForeignHistory):
		writeln(stderr, syncContinueForeignHistoryMessage(errDetail(err, docsync.ErrContinueForeignHistory)))

	// `--continue`'s own two refusals. They come before the generic
	// dirty-docs reading because they describe the same worktree from a
	// different question: not "commit before reconciling" but "your
	// resolution is not recorded yet".
	case errors.Is(err, docsync.ErrMarkersRemain), errors.Is(err, docsync.ErrResolutionUncommitted):
		writeln(stderr, syncContinueBlockedMessage(causeOf(err)))

	case errors.Is(err, docsync.ErrPullNeedsSync):
		writeln(stderr, pullNeedsSyncMessage(errDetail(err, docsync.ErrPullNeedsSync)))

	case errors.Is(err, docsync.ErrUnknownBase):
		writeln(stderr, syncUnknownBaseMessage(errDetail(err, docsync.ErrUnknownBase)))

	case errors.Is(err, docsync.ErrRebaseOntoHealthy):
		base, hasBase, loadErr := ws.statePort().LoadBase()
		if loadErr != nil || !hasBase {
			return err
		}
		writeln(stderr, rebaseOntoHealthyMessage(base.Commit, errDetail(err, docsync.ErrRebaseOntoHealthy)))

	case errors.Is(err, pubdom.ErrUnreachable):
		writeln(stderr, syncUnreachableMessage(ws.config.DocsRepoURL, causeLine(err)))

	case errors.Is(err, canonical.ErrMergeFailed):
		writeln(stderr, syncMergeFailedMessage(ws.cloneDir(), causeLine(err)))

	case errors.Is(err, markers.ErrTooLarge):
		writeln(stderr, docsTooLargeMessage(userMessage(err)))

	default:
		return err
	}
	return errAlreadyReported
}

// finishCommand renders one command failure on both channels: the §5.8
// machine envelope on stdout when --json was asked for, and the §5.9
// prose on stderr.
//
// ws may be nil — for commands that have no sync guidance to give, and
// for a failure that happened before a workspace could be resolved.
func finishCommand(cmd *cobra.Command, ws *workspace, asJSON bool, err error) error {
	if err == nil {
		return nil
	}
	if asJSON {
		writeJSONError(cmd.OutOrStdout(), err)
	}
	if ws == nil {
		return err
	}
	return reportSyncError(cmd, ws, err)
}

// errDetail strips a sentinel's own text from an error wrapped as
// `fmt.Errorf("%w: <detail>", sentinel, …)`, leaving the specifics the
// message renders. An error shaped differently is quoted whole, which
// degrades to a longer line rather than to a wrong one.
func errDetail(err error, sentinel error) string {
	message := err.Error()
	if rest, ok := strings.CutPrefix(message, sentinel.Error()+": "); ok {
		return rest
	}
	return message
}

// registryError translates the two registry failures a user can act on
// (§5.7, F-M2 and F-H8a).
//
// The lock timeout names the lock file, because the only useful action
// is to wait for whatever is holding it. A legacy-schema state file is
// reported as the v0.1 layout and routed to `sanho migrate`, which is
// the one command that converts it — and, critically, nothing else
// rewrites it in the meantime: a v0.2 command that "recovered" by
// starting from an empty registry would destroy every project mapping
// the daemon recorded.
func registryError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, fsx.ErrLockTimeout):
		return &lockTimeoutError{hint: registryLockHint(errDetail(err, fsx.ErrLockTimeout))}
	case errors.Is(err, registry.ErrLegacyState):
		return errors.New(msgMigrateRequired)
	default:
		return err
	}
}

// lockTimeoutError carries the §5.9 wording without dropping the §5.8
// identity.
//
// Replacing the error with a plain errors.New(hint) reported the right
// sentence and the wrong code: nothing satisfied errors.Is(…,
// ErrLockTimeout) any more, so `--json` labelled a perfectly ordinary
// "somebody else holds the lock" as `internal` — the one code that means
// "sanho has a bug". Unwrap keeps the sentinel reachable while Error
// stays the line a user reads.
type lockTimeoutError struct{ hint string }

func (e *lockTimeoutError) Error() string { return e.hint }

func (e *lockTimeoutError) Unwrap() error { return fsx.ErrLockTimeout }

// --- machine-readable errors (§5.8, F-M9) ------------------------------

// errorJSON is the envelope a `--json` command prints on stdout when it
// fails:
//
//	{"error": {"code": "sync_required", "message": "…"}}
//
// The exit code is unchanged and the prose still goes to stderr; this
// only gives an agent something to branch on that is not a string match
// against human wording. Codes come from the §5.9 vocabulary.
type errorJSON struct {
	Error errorBodyJSON `json:"error"`
}

type errorBodyJSON struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Machine error codes (§5.9). They are stable identifiers, so they are
// spelled once, here.
const (
	codeNotInWorkspace       = "not_in_workspace"
	codeV1Workspace          = "v1_workspace"
	codeSyncInProgress       = "sync_in_progress"
	codeSyncRequired         = "sync_required"
	codeDocsDirty            = "docs_dirty"
	codeHistoryRewritten     = "history_rewritten"
	codeUnknownTarget        = "unknown_target"
	codeCanonicalUnreachable = "canonical_unreachable"
	codeRegistryLockTimeout  = "registry_lock_timeout"
	codeMarkersPresent       = "markers_present"
	codeTooLarge             = "too_large"
	// codeConfigCorrupt / codeBaseCorrupt are M4: a state file that is
	// present and unreadable is a state the user can act on (restore it,
	// re-init, let `doctor --fix` re-derive), so reporting it as
	// `internal` — the one code that means "sanho has a bug" — sent an
	// agent looking in the wrong place entirely.
	codeConfigCorrupt = "config_corrupt"
	codeBaseCorrupt   = "base_corrupt"
	// codeBaseNotCorroborated is the §5.7 guard's refusal to record a
	// base it cannot vouch for. It is a `sync_required`-family state:
	// what establishes a base the workspace can stand behind is a sync.
	codeBaseNotCorroborated = "base_not_corroborated"
	codeInternal            = "internal"
)

// machineErrorCode maps an error to its §5.9 code. The order matters
// where an error satisfies two sentinels: the more specific reading
// comes first.
func machineErrorCode(err error) string {
	switch {
	case errors.Is(err, errV1Workspace):
		return codeV1Workspace
	case errors.Is(err, errNotWorkspace):
		return codeNotInWorkspace
	case errors.Is(err, docsync.ErrSyncInProgress), errors.Is(err, docsync.ErrNoSyncInProgress),
		errors.Is(err, docsync.ErrSyncNoteCorrupt), errors.Is(err, publish.ErrSyncInProgress):
		return codeSyncInProgress
	case errors.Is(err, wsstate.ErrConfigCorrupt):
		return codeConfigCorrupt
	case errors.Is(err, wsstate.ErrBaseCorrupt), errors.Is(err, wsstate.ErrLegacyBaseEmpty):
		return codeBaseCorrupt
	case errors.Is(err, docsync.ErrBaseNotCorroborated):
		return codeBaseNotCorroborated
	case errors.Is(err, docsync.ErrContinueForeignHistory):
		return codeSyncInProgress
	case errors.Is(err, docsync.ErrDocsDirty), errors.Is(err, docsync.ErrResolutionUncommitted):
		return codeDocsDirty
	case errors.Is(err, docsync.ErrMarkersRemain):
		return codeMarkersPresent
	case errors.Is(err, docsync.ErrPullNeedsSync), errors.Is(err, publish.ErrSyncRequired),
		errors.Is(err, publish.ErrEmptyPublish):
		return codeSyncRequired
	case errors.Is(err, docsync.ErrUnknownBase), errors.Is(err, publish.ErrHistoryRewritten):
		return codeHistoryRewritten
	case errors.Is(err, docsync.ErrUnknownTarget), errors.Is(err, docsync.ErrRebaseOntoHealthy):
		return codeUnknownTarget
	case errors.Is(err, pubdom.ErrUnreachable), errors.Is(err, canonical.ErrMergeFailed):
		return codeCanonicalUnreachable
	case errors.Is(err, fsx.ErrLockTimeout):
		return codeRegistryLockTimeout
	case errors.Is(err, publish.ErrMarkersPresent):
		return codeMarkersPresent
	case errors.Is(err, markers.ErrTooLarge):
		return codeTooLarge
	default:
		return codeInternal
	}
}

// writeJSONError prints the §5.8 error envelope to stdout. The exit code
// and the stderr prose are the caller's, unchanged: this only gives an
// agent something to branch on that is not a string match against human
// wording.
//
// It is only reached when the command was invoked with --json: without
// the flag, stdout carries prose and an envelope there would be noise.
func writeJSONError(out io.Writer, err error) {
	_ = writeJSON(out, errorJSON{Error: errorBodyJSON{
		Code:    machineErrorCode(err),
		Message: userMessage(err),
	}})
}

// userMessage renders an error the way the human-facing renderer would,
// minus the "sanho: " prefix, so both channels describe the same thing.
func userMessage(err error) string {
	return stripInternalPrefixes(strings.TrimPrefix(err.Error(), errorPrefix))
}

// causeOf renders an error for a user-facing line: the whole chain, with
// infra's package tags removed.
//
// It is the interpolation to reach for instead of `%v` on an error.
// `sanho doctor` in particular reports failures it deliberately does not
// fail on, and printing them raw put `appgit: ` and `gitx: ` in front of
// sentences a user reads — package names that locate a failure for us
// and mean nothing to them (§5.9, F-M3). causeLine is the narrower
// relative: it keeps only the innermost cause, for messages that pair a
// one-line cause with an action line.
func causeOf(err error) string { return stripInternalPrefixes(err.Error()) }

// stripInternalPrefixes removes the package tags infra uses to locate
// its own failures. They are diagnostics for us, not information for the
// user, and §5.9 forbids leaking them into user-level output (F-M3).
func stripInternalPrefixes(message string) string {
	for _, tag := range []string{"appgit: ", "canonical: ", "gitx: ", "fsx: "} {
		message = strings.ReplaceAll(message, tag, "")
	}
	return message
}
