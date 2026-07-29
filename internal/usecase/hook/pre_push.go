// Package hook provides use cases for Git hook operations.
package hook

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/irootkernel/sanho/internal/domain/client"
)

// Pre-push specific errors.
var (
	// ErrPrePushConflictMarkerFound indicates conflict markers were found during pre-push.
	ErrPrePushConflictMarkerFound = errors.New("conflict markers found - push blocked")
	// ErrPrePushPendingFixExists indicates pending fix state exists during pre-push.
	ErrPrePushPendingFixExists = errors.New("pending fix state exists - push blocked")
)

// PrePushConfigLoader loads workspace configuration.
type PrePushConfigLoader interface {
	Load(workDir string) (*client.WorkspaceConfig, error)
}

// PrePushPendingFixStore checks for pending fix state.
type PrePushPendingFixStore interface {
	Exists(path string) (bool, error)
}

// PrePushConflictDetector detects conflict markers in docs.
type PrePushConflictDetector interface {
	DetectConflicts(docsDir string) ([]string, error)
}

// PrePushOutput is the output callback for user messages.
type PrePushOutput interface {
	Info(msg string)
	Warning(msg string)
	Error(msg string)
}

// PrePushUseCase handles the pre-push hook logic.
type PrePushUseCase struct {
	configLoader     PrePushConfigLoader
	pendingFixStore  PrePushPendingFixStore
	conflictDetector PrePushConflictDetector
	output           PrePushOutput
}

// NewPrePushUseCase creates a new PrePushUseCase.
func NewPrePushUseCase(
	configLoader PrePushConfigLoader,
	pendingFixStore PrePushPendingFixStore,
	conflictDetector PrePushConflictDetector,
	output PrePushOutput,
) *PrePushUseCase {
	return &PrePushUseCase{
		configLoader:     configLoader,
		pendingFixStore:  pendingFixStore,
		conflictDetector: conflictDetector,
		output:           output,
	}
}

// Execute runs the pre-push hook logic.
func (u *PrePushUseCase) Execute(ctx context.Context, workDir string) error {
	// Step 1: Load configuration
	config, err := u.configLoader.Load(workDir)
	if err != nil {
		u.output.Error(fmt.Sprintf("Failed to load configuration: %v", err))
		u.output.Error("sanho configuration is broken or missing (.sanho.json).")
		return fmt.Errorf("%w: %v", ErrConfigBroken, err)
	}
	config.ApplyDefaults()

	// Step 2: Check for conflict markers
	docsPath := filepath.Join(workDir, config.DocsDir)
	conflictFiles, err := u.conflictDetector.DetectConflicts(docsPath)
	if err != nil {
		return fmt.Errorf("failed to check for conflicts: %w", err)
	}
	if len(conflictFiles) > 0 {
		u.output.Error("Conflict markers found in docs files:")
		for _, f := range conflictFiles {
			u.output.Error(fmt.Sprintf("  - %s", f))
		}
		u.output.Error("")
		u.output.Error("Please resolve conflicts before pushing.")
		u.output.Error("Push is blocked to prevent spreading unresolved conflicts.")
		return ErrPrePushConflictMarkerFound
	}

	// Step 3: Check for pending fix state
	pendingFixPath := filepath.Join(workDir, config.PendingFixFile)
	hasPendingFix, err := u.pendingFixStore.Exists(pendingFixPath)
	if err != nil {
		return fmt.Errorf("failed to check pending fix state: %w", err)
	}
	if hasPendingFix {
		u.output.Error("Pending fix state detected (.sanho_pending_fix exists).")
		u.output.Error("")
		u.output.Error("This workspace has an incomplete docs merge from a previous commit.")
		u.output.Error("Please run 'sanho fix' to complete the merge and sync docs.")
		u.output.Error("")
		u.output.Error("Push is blocked until the pending fix is resolved.")
		return ErrPrePushPendingFixExists
	}

	// All checks passed
	u.output.Info("Docs check passed. Proceeding with push.")
	return nil
}
