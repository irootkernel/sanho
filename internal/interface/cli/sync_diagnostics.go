package cli

import (
	"errors"
	"slices"

	"github.com/irootkernel/sanho/internal/infra/appgit"
	"github.com/irootkernel/sanho/internal/infra/gitx"
	"github.com/irootkernel/sanho/internal/usecase/docsync"
)

// SyncErrorDetails carries one typed diagnosis at the CLI boundary.
// The pointer embedding in errorBodyJSON keeps these three fields together:
// an unrelated error has none, while a diagnosis always has paths and recovery.
type SyncErrorDetails struct {
	Reason     string   `json:"reason"`
	Paths      []string `json:"paths"`
	RecoveryID *string  `json:"recovery_id"`
}

func syncDetails(err error) *SyncErrorDetails {
	var unavailable *inspectionUnavailableError
	if errors.As(err, &unavailable) {
		return &SyncErrorDetails{Reason: unavailable.reason, Paths: []string{}}
	}
	var blocker *docsync.CompletionBlocker
	var reason, catalogID string
	paths := []string{}
	if errors.As(err, &blocker) {
		switch blocker.Kind {
		case docsync.BlockerNoSync:
			reason = "no_sync"
		case docsync.BlockerCorruptNote:
			reason, catalogID = "sync_note_corrupt", "sync_note_corrupt"
		case docsync.BlockerInvalidTarget:
			reason, catalogID = "invalid_sync_target", "sync_note_corrupt"
		case docsync.BlockerMarkers:
			reason, catalogID = "markers_remaining", "sync_continue_blocked"
			paths = append(paths, blocker.Paths...)
		case docsync.BlockerUncommitted:
			reason, catalogID = "resolution_uncommitted", "sync_continue_blocked"
		case docsync.BlockerForeignHistory:
			reason, catalogID = "foreign_entry_history", "sync_continue_foreign_history"
		case docsync.BlockerMissingMergeTree:
			reason, catalogID = "missing_merge_tree", "sync_continue_unverified"
		case docsync.BlockerNonConflictChanges:
			reason, catalogID = "non_conflict_paths_changed", "sync_continue_unverified"
			paths = append(paths, blocker.Paths...)
		}
	} else {
		// These sentinels identify the sync/pull admission refusal itself.
		// Generic machine codes and human error strings are not diagnoses.
		switch {
		case errors.Is(err, docsync.ErrSyncNoteCorrupt):
			reason, catalogID = "sync_note_corrupt", "sync_note_corrupt"
		case errors.Is(err, docsync.ErrSyncInProgress):
			reason, catalogID = "active_sync", "sync_in_progress_command"
		}
	}
	if reason == "" {
		return nil
	}
	slices.Sort(paths)
	return &SyncErrorDetails{Reason: reason, Paths: slices.Compact(paths), RecoveryID: catalogRecoveryID(catalogID)}
}

func catalogRecoveryID(id string) *string {
	for _, entry := range Catalog {
		if entry.ID == id && entry.RecoveryID != "" {
			return &entry.RecoveryID
		}
	}
	return nil
}

// Only inspection binds this translation. Ordinary commands keep their
// existing classifications, and ordinary Git/I/O failures pass through.
func inspectionError(err error) error {
	switch {
	case errors.Is(err, appgit.ErrExternalFilterConfigured):
		return &inspectionUnavailableError{"external_filter_configured", msgInspectionFilterUnavailable, err}
	case errors.Is(err, gitx.ErrInspectionPolicyUnavailable):
		return &inspectionUnavailableError{"execution_policy_unavailable", msgInspectionPolicyUnavailable, err}
	default:
		return err
	}
}

type inspectionUnavailableError struct {
	reason, message string
	cause           error
}

func (e *inspectionUnavailableError) Error() string { return e.message }
func (e *inspectionUnavailableError) Unwrap() error { return e.cause }
