package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/irootkernel/sanho/internal/domain/provenance"
	pubdom "github.com/irootkernel/sanho/internal/domain/publish"
	"github.com/irootkernel/sanho/internal/usecase/admin"

	"github.com/spf13/cobra"
)

type checkOptions struct {
	requireClean     bool
	requireCurrent   bool
	requirePublished bool
	asJSON           bool
}

type policyCheckJSON struct {
	Passed bool                    `json:"passed"`
	Checks []policyCheckResultJSON `json:"checks"`
}

type policyCheckResultJSON struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Reason string `json:"reason"`
}

func newCheckCmd() *cobra.Command {
	var opts checkOptions
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Evaluate explicit workspace policies",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runCheck(cmd, opts)
		},
	}
	cmd.Flags().BoolVar(&opts.requireClean, "require-clean", false, "Require the docs index and worktree to be clean")
	cmd.Flags().BoolVar(&opts.requireCurrent, "require-current", false, "Fetch and require the docs base to equal canonical head")
	cmd.Flags().BoolVar(&opts.requirePublished, "require-published", false, "Require committed docs to equal the recorded base")
	cmd.Flags().BoolVar(&opts.asJSON, "json", false, "Print machine-readable JSON")
	return cmd
}

func runCheck(cmd *cobra.Command, opts checkOptions) error {
	if !opts.requireClean && !opts.requireCurrent && !opts.requirePublished {
		err := fmt.Errorf("%w: select at least one of --require-clean, --require-current, or --require-published", errInvalidArguments)
		return finishCommand(cmd, nil, opts.asJSON, err)
	}
	ctx := cmd.Context()
	ws, err := requireV2Workspace(ctx)
	if err != nil {
		return finishCommand(cmd, nil, opts.asJSON, err)
	}
	report, err := queryCheckStatus(ctx, ws, opts)
	if err != nil {
		return finishCommand(cmd, nil, opts.asJSON, err)
	}
	document := buildCheckJSON(report, opts)
	if opts.asJSON {
		if err := writeJSON(cmd.OutOrStdout(), document); err != nil {
			return err
		}
	} else {
		renderCheck(cmd.OutOrStdout(), document)
	}
	if !document.Passed {
		return errAlreadyReported
	}
	return nil
}

func queryCheckStatus(ctx context.Context, ws *workspace, opts checkOptions) (admin.StatusReport, error) {
	var report admin.StatusReport

	if opts.requireClean {
		clean, err := ws.repo.DocsClean(ctx)
		if err != nil {
			return admin.StatusReport{}, err
		}
		report.WorkingCopyKnown, report.DocsClean = true, clean
	}

	if opts.requireCurrent || opts.requirePublished {
		base, hasBase, err := ws.statePort().LoadBase()
		if err != nil {
			return admin.StatusReport{}, err
		}
		report.Base, report.HasBase = base, hasBase
	}

	if opts.requirePublished {
		syncInProgress, err := ws.statePort().SyncInProgress()
		if err != nil {
			return admin.StatusReport{}, err
		}
		report.SyncInProgress = syncInProgress
		report.PublicationKnown, report.PublicationPending, err =
			detectCheckPublication(ctx, ws.repo, report.Base, report.HasBase, syncInProgress)
		if err != nil {
			return admin.StatusReport{}, err
		}
	}

	if opts.requireCurrent {
		store, err := ws.openCanonical()
		if err != nil {
			return admin.StatusReport{}, newCloneMissingError(ws.cloneDir())
		}
		if err := store.Fetch(ctx); err != nil {
			return admin.StatusReport{}, err
		}
		head, _, err := store.Head(ctx)
		switch {
		case err == nil:
			report.Head = head
		case errors.Is(err, pubdom.ErrEmptyBranch):
			report.CanonicalEmpty = true
		default:
			return admin.StatusReport{}, err
		}
		if report.Head != "" && report.HasBase {
			report.RelationKnown, report.Behind, report.Ahead, err =
				detectCheckRelation(ctx, store, report.Base.Commit, report.Head)
			if err != nil {
				return admin.StatusReport{}, err
			}
		}
	}

	return report, nil
}

func detectCheckPublication(
	ctx context.Context,
	local interface {
		HeadDocsTree(context.Context) (string, error)
	},
	base provenance.Base,
	hasBase, syncInProgress bool,
) (known, pending bool, err error) {
	if !hasBase || syncInProgress {
		return false, false, nil
	}
	localTree, err := local.HeadDocsTree(ctx)
	if err != nil {
		return false, false, err
	}
	return true, localTree != base.Tree, nil
}

func detectCheckRelation(
	ctx context.Context,
	canonical interface {
		ResolveCommit(context.Context, string) (bool, error)
		Distance(context.Context, string, string) (int, int, error)
	},
	base, head string,
) (known bool, behind, ahead int, err error) {
	known, err = canonical.ResolveCommit(ctx, base)
	if err != nil || !known {
		return known, 0, 0, err
	}
	behind, ahead, err = canonical.Distance(ctx, base, head)
	return known, behind, ahead, err
}

func buildCheckJSON(report admin.StatusReport, opts checkOptions) policyCheckJSON {
	document := policyCheckJSON{Passed: true, Checks: []policyCheckResultJSON{}}
	appendResult := func(result policyCheckResultJSON) {
		document.Checks = append(document.Checks, result)
		if !result.Passed {
			document.Passed = false
		}
	}
	if opts.requireClean {
		result := policyCheckResultJSON{Name: "clean", Passed: report.WorkingCopyKnown && report.DocsClean, Reason: "clean"}
		switch {
		case !report.WorkingCopyKnown:
			result.Reason = admin.ReadinessWorkingCopyUnknown
		case !report.DocsClean:
			result.Reason = admin.ReadinessDocsDirty
		}
		appendResult(result)
	}
	if opts.requireCurrent {
		result := currentCheck(report)
		appendResult(result)
	}
	if opts.requirePublished {
		result := publishedCheck(report)
		appendResult(result)
	}
	return document
}

func currentCheck(report admin.StatusReport) policyCheckResultJSON {
	result := policyCheckResultJSON{Name: "current"}
	switch {
	case report.CanonicalEmpty && !report.HasBase:
		result.Passed, result.Reason = true, "canonical_empty"
	case !report.HasBase:
		result.Reason = admin.ReadinessNoBase
	case !report.RelationKnown:
		result.Reason = "relation_unknown"
	case report.Behind == 0 && report.Ahead == 0:
		result.Passed, result.Reason = true, "current"
	case report.Behind > 0 && report.Ahead > 0:
		result.Reason = "diverged"
	case report.Behind > 0:
		result.Reason = "behind"
	default:
		result.Reason = "ahead"
	}
	return result
}

func publishedCheck(report admin.StatusReport) policyCheckResultJSON {
	result := policyCheckResultJSON{Name: "published"}
	switch {
	case report.PublicationKnown && !report.PublicationPending:
		result.Passed, result.Reason = true, "published"
	case report.PublicationKnown:
		result.Reason = "publication_pending"
	case report.SyncInProgress:
		result.Reason = admin.ReadinessSyncInProgress
	case !report.HasBase:
		result.Reason = admin.ReadinessNoBase
	default:
		result.Reason = "publication_unknown"
	}
	return result
}

func renderCheck(out io.Writer, document policyCheckJSON) {
	for _, check := range document.Checks {
		status := "pass"
		if !check.Passed {
			status = "fail"
		}
		writef(out, "%s: %s (%s)\n", check.Name, status, check.Reason)
	}
}
