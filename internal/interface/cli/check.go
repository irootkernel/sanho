package cli

import (
	"fmt"
	"io"

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
	store, err := ws.openCanonical()
	if err != nil {
		return finishCommand(cmd, nil, opts.asJSON, err)
	}
	report, err := queryStatus(ctx, ws, store, opts.requireCurrent)
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
