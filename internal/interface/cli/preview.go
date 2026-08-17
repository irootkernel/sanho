package cli

// `sanho preview` — the verdict a `git push` would reach, without
// reaching it.
//
// `sanho status` predicts a SYNC: how the committed docs would merge
// with canonical. Whether the PUSH itself would be accepted, and as
// which case, had no counterpart — the only way to find out was to push
// and read the rejection. The evaluation was already separable, because
// publication's contract is that it writes nothing until the whole
// multi-ref push validates, so this command is that first pass stopped
// before anything is committed or pushed.
//
// It exits 0 whenever it could reach a verdict, including a verdict that
// says the push would be rejected. That is `sanho doctor`'s principle:
// a diagnostic that fails whenever it finds a problem cannot be used to
// investigate one. `sanho check` remains the command that gates.
//
// It names no next command, and that is a boundary rather than an
// omission. Every blocked verdict here is a state the push rejection
// already words, with a recovery the closure suite proves runnable in
// it; a second copy of that guidance would be a second copy to keep
// true. Preview answers what would happen and says which documents are
// in the way — the push boundary owns what to do about it.

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/irootkernel/sanho/internal/usecase/publish"

	"github.com/spf13/cobra"
)

// Blocked verdicts. The publishing ones are pubdom.Case's own strings,
// reused rather than restated so the preview and the publication that
// follows it cannot disagree about what happened.
const (
	previewSyncInProgress   = "sync_in_progress"
	previewMarkersPresent   = "markers_present"
	previewSyncRequired     = "sync_required"
	previewHistoryRewritten = "history_rewritten"
	previewEmptyPublication = "empty_publication"
)

type previewOptions struct {
	refresh bool
	asJSON  bool
	branch  string
}

type previewJSON struct {
	Branch    string           `json:"branch"`
	Tip       string           `json:"tip"`
	Canonical previewCanonical `json:"canonical"`
	Verdict   string           `json:"verdict"`
	Publishes bool             `json:"publishes"`
	Blocked   bool             `json:"blocked"`
	Conflicts []string         `json:"conflicts"`
}

type previewCanonical struct {
	// Head is "" when nothing has been published yet, which Empty says
	// in the affirmative rather than leaving to an OID comparison.
	Head           string `json:"head"`
	Empty          bool   `json:"empty"`
	FetchedEver    bool   `json:"fetched_ever"`
	DataAgeSeconds int64  `json:"data_age_seconds"`
}

func newPreviewCmd() *cobra.Command {
	var opts previewOptions
	cmd := &cobra.Command{
		Use:   "preview",
		Short: "Show what a git push would publish",
		Long: `Report the verdict a push of one branch would reach, without pushing.

The branch is the current one unless --branch names another. Only that
branch is evaluated: a push of several branches at once is not previewed.

The verdict is decided against the last fetched canonical snapshot;
--refresh fetches first. The pre-push hook always fetches, so a preview
of a stale snapshot predicts what would happen against that snapshot,
not what a later fetch will find.

Preview writes nothing and exits 0 whenever it reached a verdict,
including one that says the push would be rejected. Use sanho check to
gate on workspace state.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireNonEmptyFlags(cmd, "branch"); err != nil {
				return finishCommand(cmd, nil, opts.asJSON, err)
			}
			return runPreview(cmd, opts)
		},
	}
	cmd.Flags().BoolVar(&opts.refresh, "refresh", false, "Fetch the canonical repository before deciding")
	cmd.Flags().StringVar(&opts.branch, "branch", "", "Preview this branch instead of the current one")
	cmd.Flags().BoolVar(&opts.asJSON, "json", false, "Print machine-readable JSON")
	return cmd
}

func runPreview(cmd *cobra.Command, opts previewOptions) error {
	ctx := cmd.Context()
	ws, err := requireV2Workspace(ctx)
	if err != nil {
		return finishCommand(cmd, nil, opts.asJSON, err)
	}

	branch, tip, err := previewTarget(ctx, ws, opts.branch)
	if err != nil {
		return finishCommand(cmd, nil, opts.asJSON, err)
	}

	store, err := ws.openCanonical()
	if err != nil {
		// A reader never creates the clone; a write path does.
		return finishCommand(cmd, nil, opts.asJSON, newCloneMissingError(ws.cloneDir()))
	}
	if opts.refresh {
		if err := store.Fetch(ctx); err != nil {
			return finishCommand(cmd, nil, opts.asJSON, fmt.Errorf("refresh canonical repository: %w", err))
		}
	}

	use := &publish.UseCase{
		Canonical:         ws.canonicalPort(store),
		App:               ws.appPort(),
		State:             ws.statePort(),
		ActorEmail:        ws.config.ActorEmail,
		WorkspaceID:       ws.config.WorkspaceID,
		AllowEmptyPublish: allowEmptyPublish(),
	}
	result, previewErr := use.Preview(ctx, []publish.RefUpdate{{
		LocalRef: "refs/heads/" + branch,
		LocalOID: tip,
	}})

	document, ok := buildPreviewJSON(branch, tip, result, previewErr)
	if !ok {
		// The evaluation itself failed — an unreachable canonical, a
		// broken clone. That is not a verdict about the push, so it is
		// reported as an error rather than dressed up as one.
		return finishCommand(cmd, nil, opts.asJSON, previewErr)
	}

	age, fetchedEver := store.Age()
	document.Canonical.FetchedEver = fetchedEver
	if fetchedEver {
		document.Canonical.DataAgeSeconds = int64(age.Seconds())
	}

	if opts.asJSON {
		return writeJSON(cmd.OutOrStdout(), document)
	}
	renderPreview(cmd.OutOrStdout(), document)
	writeln(cmd.ErrOrStderr(), dataAgeLine(age, fetchedEver))
	return nil
}

// previewTarget resolves the branch whose push is being previewed.
//
// A detached HEAD and an unborn branch are both refused rather than
// previewed: neither names a branch a push would carry, so any verdict
// about one would be a verdict about nothing.
func previewTarget(ctx context.Context, ws *workspace, requested string) (branch, tip string, err error) {
	branch = requested
	if branch == "" {
		_, current, identityErr := ws.repo.RepoIdentity(ctx)
		if identityErr != nil {
			return "", "", identityErr
		}
		// RepoIdentity reports a detached HEAD as the literal "HEAD".
		if current == "" || current == "HEAD" {
			return "", "", errors.New(previewDetachedHeadMessage())
		}
		branch = current
	}

	tip, found, err := ws.repo.BranchCommit(ctx, branch)
	if err != nil {
		return "", "", err
	}
	if !found {
		return "", "", newUnknownTargetError(previewUnknownBranchMessage(branch))
	}
	return branch, tip, nil
}

// buildPreviewJSON turns one Preview result into the document, and
// reports whether a verdict was reached at all.
//
// ok=false is the distinction the command turns on: a rejection sentinel
// IS the answer — this push would be refused, and here is why — while
// any other failure means no verdict could be decided, which is an error
// like any other.
func buildPreviewJSON(branch, tip string, result publish.Preview, err error) (previewJSON, bool) {
	document := previewJSON{
		Branch:    branch,
		Tip:       tip,
		Canonical: previewCanonical{Head: result.Head, Empty: result.Bootstrap},
		Conflicts: []string{},
	}

	if err == nil {
		document.Verdict = result.Case.String()
		document.Publishes = result.Publishes
		return document, true
	}

	document.Blocked = true
	switch {
	case errors.Is(err, publish.ErrSyncInProgress):
		document.Verdict = previewSyncInProgress
	case errors.Is(err, publish.ErrMarkersPresent):
		document.Verdict = previewMarkersPresent
		var markers *publish.MarkersPresentError
		if errors.As(err, &markers) {
			document.Conflicts = orEmpty(markers.Paths)
		}
	// The empty-publication refusal is checked before ErrSyncRequired
	// because the CLI's machine-code table folds it into that family,
	// while the two states call for entirely different actions.
	case errors.Is(err, publish.ErrEmptyPublish):
		document.Verdict = previewEmptyPublication
	case errors.Is(err, publish.ErrSyncRequired):
		document.Verdict = previewSyncRequired
		var required *publish.SyncRequiredError
		if errors.As(err, &required) {
			document.Conflicts = orEmpty(required.Conflicts)
		}
	case errors.Is(err, publish.ErrHistoryRewritten):
		document.Verdict = previewHistoryRewritten
	default:
		return previewJSON{}, false
	}
	return document, true
}

func renderPreview(out io.Writer, document previewJSON) {
	if !document.Blocked {
		writeln(out, previewOutcomeMessage(document.Branch, document.Verdict, document.Publishes))
		return
	}
	writeln(out, previewBlockedMessage(document.Branch, document.Verdict))
	for _, path := range document.Conflicts {
		writef(out, "  %s\n", path)
	}
}
