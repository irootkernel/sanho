// Package docs provides use cases for docs operations.
package docs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/irootkernel/sanho/internal/domain/client"
	"github.com/irootkernel/sanho/internal/domain/docs"
)

// Pull-specific errors.
var (
	// ErrPullConfigBroken indicates sanho configuration is broken.
	ErrPullConfigBroken = errors.New("sanho configuration is broken")
	// ErrPullPendingFix indicates a pending fix state exists that must be resolved first.
	ErrPullPendingFix = errors.New("pending fix state exists")
	// ErrPullLocalChanges indicates uncommitted local changes exist.
	ErrPullLocalChanges = errors.New("local changes exist in docs")
	// ErrPullAlreadyUpToDate indicates docs are already up to date.
	ErrPullAlreadyUpToDate = errors.New("already up to date")
	// ErrPullUnknownProject indicates the project is not registered on the daemon.
	ErrPullUnknownProject = errors.New("project not registered on daemon")
	// ErrPullUnknownWorkspace indicates the workspace is not registered on the daemon.
	ErrPullUnknownWorkspace = errors.New("workspace not registered on daemon")
)

// PullConfigLoader loads workspace configuration.
type PullConfigLoader interface {
	Load(workDir string) (*client.WorkspaceConfig, error)
}

// PullDocsHashStore reads and writes docs hash files.
type PullDocsHashStore interface {
	Read(path string) (docs.CommitHash, error)
	Write(path string, hash docs.CommitHash) error
}

// PullPendingFixStore checks pending fix state.
type PullPendingFixStore interface {
	Exists(path string) (bool, error)
}

// PullHTTPClient communicates with sanhod.
type PullHTTPClient interface {
	DocsHead(ctx context.Context, project docs.ProjectName) (docs.CommitHash, error)
	DocsSnapshot(ctx context.Context, project docs.ProjectName, commit docs.CommitHash) (docs.DocsSnapshot, docs.CommitHash, error)
}

// PullSnapshotApplier applies docs snapshots.
type PullSnapshotApplier interface {
	Apply(snapshot []byte, targetDir, docsDir string) error
}

// PullGitClient provides git operations for pull usecase.
type PullGitClient interface {
	HasLocalDocsChanges(ctx context.Context, repoPath, docsDir string) (bool, error)
}

// PullOutput is the output callback for user messages.
type PullOutput interface {
	Info(msg string)
	Warning(msg string)
	Error(msg string)
}

// PullInput holds the input parameters for pull operation.
type PullInput struct {
	WorkDir string
	Force   bool
}

// PullUseCase handles the sanho pull logic.
type PullUseCase struct {
	configLoader    PullConfigLoader
	docsHashStore   PullDocsHashStore
	pendingFixStore PullPendingFixStore
	httpClient      PullHTTPClient
	snapshotApplier PullSnapshotApplier
	gitClient       PullGitClient
	output          PullOutput
}

// NewPullUseCase creates a new PullUseCase.
func NewPullUseCase(
	configLoader PullConfigLoader,
	docsHashStore PullDocsHashStore,
	pendingFixStore PullPendingFixStore,
	httpClient PullHTTPClient,
	snapshotApplier PullSnapshotApplier,
	gitClient PullGitClient,
	output PullOutput,
) *PullUseCase {
	return &PullUseCase{
		configLoader:    configLoader,
		docsHashStore:   docsHashStore,
		pendingFixStore: pendingFixStore,
		httpClient:      httpClient,
		snapshotApplier: snapshotApplier,
		gitClient:       gitClient,
		output:          output,
	}
}

// Execute runs the sanho pull logic.
func (u *PullUseCase) Execute(ctx context.Context, input PullInput) error {
	// Step 1: Load configuration
	config, err := u.configLoader.Load(input.WorkDir)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrPullConfigBroken, err)
	}
	config.ApplyDefaults()

	// Step 2: Load docs hash
	hashFilePath := filepath.Join(input.WorkDir, config.DocsHashFile)
	localHash, err := u.docsHashStore.Read(hashFilePath)
	if err != nil {
		return fmt.Errorf("%w: failed to read docs hash: %v", ErrPullConfigBroken, err)
	}

	// Step 3: Check for pending fix state
	pendingFixPath := filepath.Join(input.WorkDir, config.PendingFixFile)
	hasPendingFix, err := u.pendingFixStore.Exists(pendingFixPath)
	if err != nil {
		return fmt.Errorf("failed to check pending fix state: %w", err)
	}
	if hasPendingFix {
		u.output.Error("Cannot pull while in pending fix state.")
		u.output.Error("Please complete 'sanho fix' first to resolve the pending merge.")
		return ErrPullPendingFix
	}

	// Step 4: Get daemon HEAD
	u.output.Info("Checking daemon docs HEAD...")
	daemonHead, err := u.httpClient.DocsHead(ctx, config.Project)
	if err != nil {
		return fmt.Errorf("failed to get daemon HEAD: %w", err)
	}

	// Step 5: Compare hashes
	if string(localHash) == string(daemonHead) {
		u.output.Info("Already up to date.")
		return nil
	}

	// Step 6: Check for local changes (unless --force)
	if !input.Force {
		docsPath := filepath.Join(input.WorkDir, config.DocsDir)
		hasChanges, err := u.gitClient.HasLocalDocsChanges(ctx, input.WorkDir, docsPath)
		if err != nil {
			return fmt.Errorf("failed to check for local changes: %w", err)
		}
		if hasChanges {
			u.output.Error("Local docs have uncommitted changes.")
			u.output.Error("Use 'sanho pull --force' to overwrite local changes,")
			u.output.Error("or commit/stash your changes first.")
			return ErrPullLocalChanges
		}
	}

	// Step 7: Download snapshot
	u.output.Info("Downloading docs snapshot...")
	snapshot, actualCommit, err := u.httpClient.DocsSnapshot(ctx, config.Project, daemonHead)
	if err != nil {
		return fmt.Errorf("failed to download snapshot: %w", err)
	}

	// Step 8: Apply snapshot to a temporary location to avoid data loss on failure.
	tempDir, err := os.MkdirTemp(input.WorkDir, ".sanho_pull_tmp_")
	if err != nil {
		return fmt.Errorf("failed to create temp dir for snapshot: %w", err)
	}
	defer os.RemoveAll(tempDir)

	u.output.Info("Applying snapshot...")
	if err := u.snapshotApplier.Apply(snapshot, tempDir, config.DocsDir); err != nil {
		return fmt.Errorf("failed to apply snapshot: %w", err)
	}

	// Ensure the applied docs exist (defensive for empty snapshots)
	newDocsPath := filepath.Join(tempDir, config.DocsDir)
	if _, err := os.Stat(newDocsPath); os.IsNotExist(err) {
		if err := os.MkdirAll(newDocsPath, 0755); err != nil {
			return fmt.Errorf("failed to prepare applied docs directory: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("failed to access applied docs directory: %w", err)
	}

	// Swap in the new docs atomically with a backup to allow rollback if rename fails.
	targetDocsPath := filepath.Join(input.WorkDir, config.DocsDir)
	backupPath := targetDocsPath + ".sanho_pull_backup"
	_ = os.RemoveAll(backupPath) // clean stale backup

	if _, err := os.Stat(targetDocsPath); err == nil {
		if err := os.Rename(targetDocsPath, backupPath); err != nil {
			return fmt.Errorf("failed to backup existing docs: %w", err)
		}
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to access existing docs: %w", err)
	}

	if err := os.Rename(newDocsPath, targetDocsPath); err != nil {
		// Attempt rollback
		_ = os.Rename(backupPath, targetDocsPath)
		return fmt.Errorf("failed to replace docs directory: %w", err)
	}

	_ = os.RemoveAll(backupPath)

	// Step 10: Update docs hash
	if err := u.docsHashStore.Write(hashFilePath, actualCommit); err != nil {
		return fmt.Errorf("failed to update docs hash: %w", err)
	}

	u.output.Info("Pull completed successfully.")
	u.output.Info(fmt.Sprintf("  pulled from: %s", localHash))
	u.output.Info(fmt.Sprintf("  new version: %s", actualCommit))

	return nil
}
