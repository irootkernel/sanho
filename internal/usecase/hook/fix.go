// Package hook provides use cases for Git hook operations.
package hook

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/irootkernel/sanho/internal/domain/client"
	"github.com/irootkernel/sanho/internal/domain/docs"
)

// Fix-specific errors.
var (
	// ErrNoPendingFix indicates no pending fix state exists.
	ErrNoPendingFix = errors.New("no pending fix state")
	// ErrFixHeadChanged indicates the server HEAD changed during fix.
	ErrFixHeadChanged = errors.New("docs head changed during fix")
	// ErrActorEmailRequired indicates actor email is required but not provided.
	ErrActorEmailRequired = errors.New("actor email is required")
)

// FixConfigLoader loads workspace configuration.
type FixConfigLoader interface {
	Load(workDir string) (*client.WorkspaceConfig, error)
}

// FixDocsHashStore reads and writes docs hash files.
type FixDocsHashStore interface {
	Read(path string) (docs.CommitHash, error)
	Write(path string, hash docs.CommitHash) error
}

// FixPendingFixStore manages pending fix state.
type FixPendingFixStore interface {
	Read(path string) (exists bool, err error)
	Remove(path string) error
}

// FixConflictDetector detects conflict markers in docs.
type FixConflictDetector interface {
	DetectConflicts(docsDir string) ([]string, error)
}

// FixSnapshotBuilder builds docs snapshots.
type FixSnapshotBuilder interface {
	Build(sourceDir string) ([]byte, error)
}

// FixHTTPClient communicates with sanhod.
type FixHTTPClient interface {
	DocsHead(ctx context.Context, project docs.ProjectName) (docs.CommitHash, error)
	DocsPush(ctx context.Context, req DocsPushRequest) (DocsPushResponse, error)
}

// FixGitClient provides git operations for fix usecase.
type FixGitClient interface {
	GetUserEmail(ctx context.Context, path string) (string, error)
}

// FixOutput is the output callback for user messages.
type FixOutput interface {
	Info(msg string)
	Warning(msg string)
	Error(msg string)
}

// FixUseCase handles the sanho fix logic.
type FixUseCase struct {
	configLoader     FixConfigLoader
	docsHashStore    FixDocsHashStore
	pendingFixStore  FixPendingFixStore
	conflictDetector FixConflictDetector
	snapshotBuilder  FixSnapshotBuilder
	httpClient       FixHTTPClient
	gitClient        FixGitClient
	output           FixOutput
}

// NewFixUseCase creates a new FixUseCase.
func NewFixUseCase(
	configLoader FixConfigLoader,
	docsHashStore FixDocsHashStore,
	pendingFixStore FixPendingFixStore,
	conflictDetector FixConflictDetector,
	snapshotBuilder FixSnapshotBuilder,
	httpClient FixHTTPClient,
	gitClient FixGitClient,
	output FixOutput,
) *FixUseCase {
	return &FixUseCase{
		configLoader:     configLoader,
		docsHashStore:    docsHashStore,
		pendingFixStore:  pendingFixStore,
		conflictDetector: conflictDetector,
		snapshotBuilder:  snapshotBuilder,
		httpClient:       httpClient,
		gitClient:        gitClient,
		output:           output,
	}
}

// Execute runs the sanho fix logic.
func (u *FixUseCase) Execute(ctx context.Context, workDir string) error {
	// Step 1: Load configuration
	config, err := u.configLoader.Load(workDir)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrConfigBroken, err)
	}
	config.ApplyDefaults()

	// Step 2: Load docs hash
	hashFilePath := filepath.Join(workDir, config.DocsHashFile)
	baseHash, err := u.docsHashStore.Read(hashFilePath)
	if err != nil {
		return fmt.Errorf("%w: failed to read docs hash: %v", ErrConfigBroken, err)
	}

	// Step 3: Check for pending fix state
	pendingFixPath := filepath.Join(workDir, config.PendingFixFile)
	hasPendingFix, err := u.pendingFixStore.Read(pendingFixPath)
	if err != nil {
		return fmt.Errorf("failed to check pending fix state: %w", err)
	}
	if !hasPendingFix {
		u.output.Error("No pending fix state found (.kkachi_pending_fix does not exist).")
		u.output.Error("sanho fix should only be run after a pre-commit merge creates pending state.")
		return ErrNoPendingFix
	}

	// Step 4: Check for conflict markers
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
		u.output.Error("Please resolve all conflicts before running 'sanho fix'.")
		return ErrConflictMarkerFound
	}

	// Step 5: Get server HEAD
	u.output.Info("Checking server docs HEAD...")
	serverHead, err := u.httpClient.DocsHead(ctx, config.Project)
	if err != nil {
		return fmt.Errorf("failed to get server HEAD: %w", err)
	}

	// Step 6: Compare base hash with server HEAD
	if string(baseHash) != string(serverHead) {
		// Server HEAD changed during fix - clear pending fix and inform user
		u.output.Warning("Docs HEAD changed during fix attempt.")
		u.output.Warning(fmt.Sprintf("  local base: %s", baseHash))
		u.output.Warning(fmt.Sprintf("  server HEAD: %s", serverHead))
		u.output.Warning("")
		u.output.Warning("Clearing pending fix state. On your next commit, pre-commit will")
		u.output.Warning("perform a fresh merge against the latest HEAD.")
		u.output.Warning("")
		u.output.Warning("Consider backing up your current docs state before the next commit.")

		// Remove pending fix state
		if err := u.pendingFixStore.Remove(pendingFixPath); err != nil {
			return fmt.Errorf("failed to remove pending fix state: %w", err)
		}

		return ErrFixHeadChanged
	}

	// Step 7: Build and push docs snapshot
	u.output.Info("Building docs snapshot...")
	snapshot, err := u.snapshotBuilder.Build(docsPath)
	if err != nil {
		return fmt.Errorf("failed to build docs snapshot: %w", err)
	}

	// Determine actor email (fallback to git config if empty)
	actorEmail := strings.TrimSpace(config.ActorEmail)
	if actorEmail == "" {
		gitEmail, err := u.gitClient.GetUserEmail(ctx, workDir)
		if err != nil {
			u.output.Warning(fmt.Sprintf("Failed to read git user.email: %v", err))
		} else if gitEmail != "" {
			actorEmail = strings.TrimSpace(gitEmail)
			if actorEmail != "" {
				u.output.Info(fmt.Sprintf("Using git user.email: %s", actorEmail))
			}
		}
	}

	// Validate actor email is not empty
	if actorEmail == "" {
		u.output.Error("Actor email is required but not configured.")
		u.output.Error("Please set 'actor_email' in .kkachi.json or configure git user.email:")
		u.output.Error("  git config user.email 'your-email@example.com'")
		return ErrActorEmailRequired
	}

	u.output.Info("Pushing docs to server...")
	pushReq := DocsPushRequest{
		WorkspaceID:  config.WorkspaceID,
		BaseDocsHash: baseHash,
		DocsSnapshot: base64.StdEncoding.EncodeToString(snapshot),
		ActorEmail:   actorEmail,
	}

	resp, err := u.httpClient.DocsPush(ctx, pushReq)
	if err != nil {
		// Handle specific errors with user guidance
		if errors.Is(err, ErrUnknownDocsCommit) {
			u.output.Error("The docs repo history has been rewritten.")
			u.output.Error("Cannot automatically recover from pending fix state.")
			u.output.Error("Please manually sync docs and run 'sanho init' again.")
			return ErrUnknownDocsCommit
		}
		if errors.Is(err, ErrDocsRepoBusy) {
			u.output.Error("Another workspace is currently updating docs.")
			u.output.Error("Please try 'sanho fix' again in a few moments.")
			return ErrDocsRepoBusy
		}
		return fmt.Errorf("failed to push docs: %w", err)
	}

	// Step 8: Handle response
	if !resp.Ok {
		if resp.Error == "unknown_docs_commit" {
			u.output.Error("The docs repo history has been rewritten.")
			u.output.Error("Cannot automatically recover from pending fix state.")
			u.output.Error("Please manually sync docs and run 'sanho init' again.")
			return ErrUnknownDocsCommit
		}
		if resp.Error == "docs_repo_busy" {
			u.output.Error("Another workspace is currently updating docs. Please try again shortly.")
			return ErrDocsRepoBusy
		}
		return fmt.Errorf("server error: %s", resp.Error)
	}

	switch resp.Status {
	case docs.DocsPushStatusUpdated:
		// Success - update hash file and remove pending fix
		if resp.NewDocsHash != "" {
			if err := u.docsHashStore.Write(hashFilePath, resp.NewDocsHash); err != nil {
				return fmt.Errorf("failed to update docs hash: %w", err)
			}
		}
		if err := u.pendingFixStore.Remove(pendingFixPath); err != nil {
			return fmt.Errorf("failed to remove pending fix state: %w", err)
		}
		u.output.Info(fmt.Sprintf("Fix completed successfully. New docs version: %s", resp.NewDocsHash))
		return nil

	case docs.DocsPushStatusNoChange:
		// No change - ensure hash is current and remove pending fix
		if !resp.CurrentDocsHash.IsZero() {
			if err := u.docsHashStore.Write(hashFilePath, resp.CurrentDocsHash); err != nil {
				return fmt.Errorf("failed to update docs hash: %w", err)
			}
		}
		if err := u.pendingFixStore.Remove(pendingFixPath); err != nil {
			return fmt.Errorf("failed to remove pending fix state: %w", err)
		}
		u.output.Info("No changes detected. Pending fix state cleared.")
		return nil

	case docs.DocsPushStatusOutdated:
		// This shouldn't happen if Step 6 passed, but handle it gracefully
		u.output.Warning("Server HEAD changed. Clearing pending fix state.")
		if err := u.pendingFixStore.Remove(pendingFixPath); err != nil {
			return fmt.Errorf("failed to remove pending fix state: %w", err)
		}
		return ErrFixHeadChanged

	default:
		return fmt.Errorf("unexpected response status: %s", resp.Status)
	}
}
