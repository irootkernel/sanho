package cli

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/irootkernel/sanho/internal/domain/provenance"
	"github.com/irootkernel/sanho/internal/infra/wsstate"
	"github.com/irootkernel/sanho/internal/usecase/docsync"
	"github.com/spf13/cobra"
)

type inspectionJSON struct {
	State        string                    `json:"state"`
	Head         *string                   `json:"head"`
	Note         *inspectionNoteJSON       `json:"note"`
	Checks       []inspectionCheckJSON     `json:"checks"`
	Comparison   *inspectionComparisonJSON `json:"comparison"`
	Continuation inspectionContinuation    `json:"continuation"`
}

type inspectionBaseJSON struct {
	Commit *string `json:"commit"`
	Tree   *string `json:"tree"`
}

type inspectionNoteJSON struct {
	PreviousBase  *inspectionBaseJSON `json:"previous_base"`
	Target        inspectionBaseJSON  `json:"target"`
	EntryHead     *string             `json:"entry_head"`
	EntryDocsTree *string             `json:"entry_docs_tree"`
	MergedTree    *string             `json:"merged_tree"`
	Conflicts     []string            `json:"conflicts"`
}

type inspectionCheckJSON struct {
	Name   string                       `json:"name"`
	State  docsync.CompletionCheckState `json:"state"`
	Reason *string                      `json:"reason"`
	Paths  []string                     `json:"paths"`
}

type inspectionComparisonJSON struct {
	ExpectedTree      string   `json:"expected_tree"`
	ActualTree        string   `json:"actual_tree"`
	AllowedChanges    []string `json:"allowed_changes"`
	UnexpectedChanges []string `json:"unexpected_changes"`
}

type inspectionContinuation struct {
	Ready      bool     `json:"ready"`
	Reason     *string  `json:"reason"`
	Paths      []string `json:"paths"`
	RecoveryID *string  `json:"recovery_id"`
}

type observedSyncNote struct{ wsstate.SyncNoteObservation }

func (n observedSyncNote) LoadSyncNote() (docsync.SyncNote, bool, error) {
	note, exists, err := n.Load()
	return adaptSyncNote(note, exists, err)
}

func runSyncInspect(cmd *cobra.Command, asJSON bool) error {
	ws, err := openInspectionWorkspace(cmd.Context())
	if err != nil {
		return finishCommand(cmd, nil, asJSON, inspectionError(err))
	}
	result, err := inspectSync(cmd.Context(), ws.repo, ws.gitDir)
	if err != nil {
		return finishCommand(cmd, nil, asJSON, inspectionError(err))
	}
	if asJSON {
		return writeJSON(cmd.OutOrStdout(), result)
	}
	renderSyncInspection(cmd.OutOrStdout(), result)
	return nil
}

func inspectSync(ctx context.Context, app docsync.CompletionReader, gitDir string) (inspectionJSON, error) {
	headBefore, err := app.HeadCommit(ctx)
	if err != nil {
		return inspectionJSON{}, fmt.Errorf("read HEAD before inspection: %w", err)
	}
	noteBefore, err := wsstate.ObserveSyncNote(gitDir)
	if err != nil {
		return inspectionJSON{}, err
	}
	assessment, err := docsync.AssessCompletion(ctx, app, observedSyncNote{noteBefore})
	if err != nil {
		return inspectionJSON{}, err
	}
	headAfter, err := app.HeadCommit(ctx)
	if err != nil {
		return inspectionJSON{}, fmt.Errorf("read HEAD after inspection: %w", err)
	}
	noteAfter, err := wsstate.ObserveSyncNote(gitDir)
	if err != nil {
		return inspectionJSON{}, err
	}
	result := buildInspectionJSON(headBefore, assessment)
	headAssessed := false
	for _, check := range assessment.Checks {
		if check.Name == "entry_history" {
			headAssessed = check.State != docsync.CheckNotEvaluated
			break
		}
	}
	if headBefore != headAfter || !noteBefore.Same(noteAfter) || (headAssessed && assessment.Head != headBefore) {
		result.State, result.Head, result.Note, result.Comparison = "changed", nil, nil, nil
		for i := range result.Checks {
			result.Checks[i].State = docsync.CheckNotEvaluated
			result.Checks[i].Reason, result.Checks[i].Paths = nil, []string{}
		}
		result.Continuation = inspectionContinuation{Reason: inspectionString("observation_changed"), Paths: []string{}}
	}
	return result, nil
}

func buildInspectionJSON(head string, a docsync.CompletionAssessment) inspectionJSON {
	out := inspectionJSON{State: "active", Head: inspectionString(head),
		Checks: []inspectionCheckJSON{}, Continuation: inspectionContinuation{Ready: a.Blocker == nil, Paths: []string{}}}
	var details *SyncErrorDetails
	if a.Blocker != nil {
		details = syncDetails(a.Blocker)
		out.Continuation = inspectionContinuation{Reason: inspectionString(details.Reason), Paths: details.Paths, RecoveryID: details.RecoveryID}
		switch a.Blocker.Kind {
		case docsync.BlockerNoSync:
			out.State = "none"
		case docsync.BlockerCorruptNote, docsync.BlockerInvalidTarget:
			out.State = "corrupt"
		}
	}
	for _, check := range a.Checks {
		c := inspectionCheckJSON{Name: check.Name, State: check.State, Paths: []string{}}
		switch check.State {
		case docsync.CheckBlocked:
			c.Reason, c.Paths = inspectionString(details.Reason), details.Paths
		case docsync.CheckNotApplicable:
			if check.Name == "entry_history" {
				c.Reason = inspectionString("entry_history_unrecorded")
			}
		}
		out.Checks = append(out.Checks, c)
	}
	if a.Note != nil {
		n := a.Note
		out.Note = &inspectionNoteJSON{Target: inspectionBase(n.Target), EntryHead: inspectionString(n.EntryHead),
			EntryDocsTree: inspectionString(n.EntryDocsTree), MergedTree: inspectionString(n.MergedTree), Conflicts: orEmpty(n.Conflicts)}
		if !n.PrevBase.IsZero() {
			previous := inspectionBase(n.PrevBase)
			out.Note.PreviousBase = &previous
		}
	}
	if a.Comparison != nil {
		c := a.Comparison
		out.Comparison = &inspectionComparisonJSON{ExpectedTree: c.ExpectedTree, ActualTree: c.ActualTree,
			AllowedChanges: orEmpty(c.AllowedChanges), UnexpectedChanges: orEmpty(c.UnexpectedChanges)}
	}
	return out
}

func inspectionString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func inspectionBase(base provenance.Base) inspectionBaseJSON {
	return inspectionBaseJSON{Commit: inspectionString(base.Commit), Tree: inspectionString(base.Tree)}
}

func renderSyncInspection(out io.Writer, result inspectionJSON) {
	value := func(s *string) string {
		if s == nil {
			return "null"
		}
		return strconv.Quote(*s)
	}
	paths := func(p []string) string {
		quoted := make([]string, len(p))
		for i, name := range p {
			quoted[i] = strconv.Quote(name)
		}
		return "[" + strings.Join(quoted, ", ") + "]"
	}
	fmt.Fprintf(out, "Sync state: %s\nHEAD: %s\n", result.State, value(result.Head))
	if n := result.Note; n != nil {
		if n.PreviousBase == nil {
			fmt.Fprintln(out, "Previous base: null")
		} else {
			fmt.Fprintf(out, "Previous base: commit=%s tree=%s\n", value(n.PreviousBase.Commit), value(n.PreviousBase.Tree))
		}
		fmt.Fprintf(out, "Target: commit=%s tree=%s\nEntry HEAD: %s\nEntry docs tree: %s\nMerged tree: %s\nRecorded conflicts: %s\n",
			value(n.Target.Commit), value(n.Target.Tree), value(n.EntryHead), value(n.EntryDocsTree), value(n.MergedTree), paths(n.Conflicts))
	} else {
		fmt.Fprintln(out, "Sync note: null")
	}
	for _, c := range result.Checks {
		fmt.Fprintf(out, "%s: %s reason=%s paths=%s\n", c.Name, c.State, value(c.Reason), paths(c.Paths))
	}
	if c := result.Comparison; c != nil {
		fmt.Fprintf(out, "Comparison: expected=%s actual=%s\nAllowed changes: %s\nUnexpected changes: %s\n",
			c.ExpectedTree, c.ActualTree, paths(c.AllowedChanges), paths(c.UnexpectedChanges))
	} else {
		fmt.Fprintln(out, "Comparison: null")
	}
	c := result.Continuation
	fmt.Fprintf(out, "Local completion checks ready: %t\nReason: %s\nPaths: %s\nRecovery ID: %s\n",
		c.Ready, value(c.Reason), paths(c.Paths), value(c.RecoveryID))
}
