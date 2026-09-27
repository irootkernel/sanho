package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/irootkernel/sanho/internal/infra/appgit"
	"github.com/irootkernel/sanho/internal/infra/gitx"
	"github.com/irootkernel/sanho/internal/usecase/docsync"
	"github.com/irootkernel/sanho/internal/usecase/publish"
	"github.com/spf13/cobra"
)

func TestSyncDiagnosticsPreserveCompatibilityAndTypedEvidence(t *testing.T) {
	special := []string{"docs/한글.md", "docs/a, b.md", "docs/[x]*\n.md", "docs/a, b.md"}
	wantPaths := []string{"docs/[x]*\n.md", "docs/a, b.md", "docs/한글.md"}
	for _, tc := range []struct {
		kind                   docsync.BlockerKind
		cause                  error
		reason, code, recovery string
		paths                  bool
	}{
		{docsync.BlockerNoSync, docsync.ErrNoSyncInProgress, "no_sync", codeSyncInProgress, "", false},
		{docsync.BlockerCorruptNote, docsync.ErrSyncNoteCorrupt, "sync_note_corrupt", codeSyncInProgress, "sync_review_corrupt_note", false},
		{docsync.BlockerInvalidTarget, docsync.ErrSyncNoteCorrupt, "invalid_sync_target", codeSyncInProgress, "sync_review_corrupt_note", false},
		{docsync.BlockerMarkers, docsync.ErrMarkersRemain, "markers_remaining", codeMarkersPresent, "sync_finish_resolution", true},
		{docsync.BlockerUncommitted, docsync.ErrResolutionUncommitted, "resolution_uncommitted", codeDocsDirty, "sync_finish_resolution", false},
		{docsync.BlockerForeignHistory, docsync.ErrContinueForeignHistory, "foreign_entry_history", codeSyncInProgress, "sync_return_to_entry_history", false},
		{docsync.BlockerMissingMergeTree, docsync.ErrResolutionUnverifiable, "missing_merge_tree", codeSyncInProgress, "sync_review_unverified_resolution", false},
		{docsync.BlockerNonConflictChanges, docsync.ErrResolutionChangedNonConflicts, "non_conflict_paths_changed", codeSyncInProgress, "sync_review_unverified_resolution", true},
	} {
		t.Run(tc.reason, func(t *testing.T) {
			blocker := &docsync.CompletionBlocker{Kind: tc.kind, Cause: tc.cause}
			if tc.paths {
				blocker.Paths = append([]string{}, special...)
			}
			err := fmt.Errorf("wrapped: %w", blocker)
			if !errors.Is(err, tc.cause) {
				t.Fatal("sentinel identity lost")
			}
			var out, stderr bytes.Buffer
			cmd := &cobra.Command{}
			cmd.SetOut(&out)
			cmd.SetErr(&stderr)
			if exit := renderError(&stderr, finishCommand(cmd, &workspace{}, true, err)); exit != 1 {
				t.Fatalf("exit=%d", exit)
			}
			if stderr.Len() == 0 {
				t.Fatal("missing human guidance")
			}
			var old struct {
				Error struct{ Code, Message string }
			}
			if err := json.Unmarshal(out.Bytes(), &old); err != nil {
				t.Fatal(err)
			}
			if old.Error.Code != tc.code || old.Error.Message != userMessage(err) {
				t.Fatalf("old consumer=%+v", old)
			}
			var modern errorJSON
			decoder := json.NewDecoder(&out)
			if err := decoder.Decode(&modern); err != nil {
				t.Fatal(err)
			}
			if err := decoder.Decode(new(any)); err != io.EOF {
				t.Fatalf("extra stdout=%v", err)
			}
			details := modern.Error.SyncErrorDetails
			if details == nil || details.Reason != tc.reason {
				t.Fatalf("diagnosis=%+v", details)
			}
			want := []string{}
			if tc.paths {
				want = wantPaths
			}
			if !reflect.DeepEqual(details.Paths, want) {
				t.Fatalf("paths=%q, want %q", details.Paths, want)
			}
			if tc.recovery == "" {
				if details.RecoveryID != nil {
					t.Fatal("unexpected recovery")
				}
			} else if details.RecoveryID == nil || *details.RecoveryID != tc.recovery {
				t.Fatalf("recovery=%v", details.RecoveryID)
			}
			if tc.paths && !reflect.DeepEqual(blocker.Paths, special) {
				t.Fatal("rendering mutated path evidence")
			}
		})
	}
}

func TestSyncAdmissionDiagnosticsAndUnrelatedFallback(t *testing.T) {
	for _, tc := range []struct {
		err    error
		reason string
	}{
		{fmt.Errorf("%w: active target", docsync.ErrSyncInProgress), "active_sync"},
		{fmt.Errorf("%w: /private/state: invalid JSON", docsync.ErrSyncNoteCorrupt), "sync_note_corrupt"},
		{docsync.ErrDocsDirty, ""},
		{publish.ErrSyncInProgress, ""},
		{publish.ErrMarkersPresent, ""},
		{errors.New("the resolution has not been committed"), ""},
	} {
		var out bytes.Buffer
		writeJSONError(&out, tc.err)
		var fields struct{ Error map[string]json.RawMessage }
		if err := json.Unmarshal(out.Bytes(), &fields); err != nil {
			t.Fatal(err)
		}
		var modern errorJSON
		if err := json.Unmarshal(out.Bytes(), &modern); err != nil {
			t.Fatal(err)
		}
		if tc.reason == "" {
			if len(fields.Error) != 2 || modern.Error.SyncErrorDetails != nil {
				t.Fatalf("unrelated error extended: %s", out.String())
			}
		} else {
			if len(fields.Error) != 5 || modern.Error.Reason != tc.reason || string(fields.Error["paths"]) != "[]" {
				t.Fatalf("details=%s", out.String())
			}
		}
	}
}

func TestInspectionAvailabilityIsExplicitAndSanitized(t *testing.T) {
	for _, tc := range []struct {
		cause           error
		reason, message string
	}{
		{appgit.ErrExternalFilterConfigured, "external_filter_configured", msgInspectionFilterUnavailable},
		{gitx.ErrInspectionPolicyUnavailable, "execution_policy_unavailable", msgInspectionPolicyUnavailable},
	} {
		raw := fmt.Errorf("%w: /private/path secret configured command", tc.cause)
		if machineErrorCode(raw) == codeInspectionUnavailable {
			t.Fatal("ordinary error changed classification")
		}
		err := inspectionError(raw)
		if !errors.Is(err, tc.cause) {
			t.Fatal("lost typed cause")
		}
		var out, stderr bytes.Buffer
		writeJSONError(&out, fmt.Errorf("private context: %w", err))
		if exit := renderError(&stderr, err); exit != 1 {
			t.Fatalf("exit=%d", exit)
		}
		var envelope errorJSON
		if decodeErr := json.Unmarshal(out.Bytes(), &envelope); decodeErr != nil {
			t.Fatal(decodeErr)
		}
		body := envelope.Error
		if body.Code != codeInspectionUnavailable || body.Message != tc.message || body.Reason != tc.reason || body.Paths == nil || len(body.Paths) != 0 || body.RecoveryID != nil {
			t.Fatalf("availability=%+v", body)
		}
		if strings.Contains(out.String()+stderr.String(), "private") || strings.Contains(out.String()+stderr.String(), "secret") {
			t.Fatal("availability leaked configuration detail")
		}
	}
	err := errors.New("ordinary I/O failure")
	if inspectionError(err) != err || inspectionError(nil) != nil {
		t.Fatal("operational error reclassified")
	}
}

func TestRecoveryIDsBelongToExistingCatalogEntries(t *testing.T) {
	want := map[string]string{
		"sync_in_progress_command": "sync_inspect_active", "sync_note_corrupt": "sync_review_corrupt_note",
		"sync_continue_blocked": "sync_finish_resolution", "sync_continue_foreign_history": "sync_return_to_entry_history",
		"sync_continue_unverified": "sync_review_unverified_resolution",
	}
	seen := map[string]bool{}
	for _, entry := range Catalog {
		if entry.RecoveryID != want[entry.ID] {
			t.Errorf("%s recovery=%q, want %q", entry.ID, entry.RecoveryID, want[entry.ID])
		}
		if entry.RecoveryID == "" {
			continue
		}
		if seen[entry.RecoveryID] {
			t.Errorf("duplicate recovery ID %s", entry.RecoveryID)
		}
		seen[entry.RecoveryID] = true
		if entry.Source == "" || entry.Scenario == "" || len(entry.NextCommands) == 0 {
			t.Errorf("recovery without guidance: %+v", entry)
		}
		requireCoherentPrerequisites(t, entry)
	}
	if len(seen) != len(want) {
		t.Fatalf("recovery entries=%d", len(seen))
	}
}
