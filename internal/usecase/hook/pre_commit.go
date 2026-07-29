// Package hook provides use cases for Git hook operations.
package hook

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/irootkernel/sanho/internal/domain/client"
	"github.com/irootkernel/sanho/internal/domain/docs"
	"github.com/irootkernel/sanho/internal/domain/workspace"
)

// Pre-commit specific errors.
var (
	// ErrConfigBroken indicates that the sanho configuration is missing or corrupt.
	ErrConfigBroken = errors.New("sanho configuration is broken")
	// ErrConflictMarkerFound indicates that conflict markers were found in docs.
	ErrConflictMarkerFound = errors.New("conflict markers found in docs")
	// ErrPendingFixExists indicates that a pending fix state exists.
	ErrPendingFixExists = errors.New("pending fix state exists")
	// ErrDocsRepoBusy indicates the docs repo is being updated by another workspace.
	ErrDocsRepoBusy = errors.New("docs repo is busy")
	// ErrOutdated indicates that the docs are outdated and merge was performed.
	ErrOutdated = errors.New("docs are outdated, merge performed")
	// ErrUnknownDocsCommit indicates the docs commit history was rewritten.
	ErrUnknownDocsCommit = errors.New("unknown docs commit - history may have been rewritten")
)

// PreCommitConfigLoader loads workspace configuration.
type PreCommitConfigLoader interface {
	Load(workDir string) (*client.WorkspaceConfig, error)
}

// PreCommitDocsHashStore reads and writes docs hash files.
type PreCommitDocsHashStore interface {
	Read(path string) (docs.CommitHash, error)
	Write(path string, hash docs.CommitHash) error
}

// PreCommitPendingFixStore manages pending fix state.
type PreCommitPendingFixStore interface {
	Read(path string) (client.PendingFixState, bool, error)
	Write(path string, state client.PendingFixState) error
	Remove(path string) error
}

// PreCommitConflictDetector detects conflict markers in docs.
type PreCommitConflictDetector interface {
	DetectConflicts(docsDir string) ([]string, error)
}

// PreCommitGitClient provides git operations.
type PreCommitGitClient interface {
	HasDocsChangeForCommit(ctx context.Context, repoPath, docsDir string) (bool, error)
	MergeFile(ctx context.Context, baseContent, localContent, remoteContent []byte) (MergeResult, error)
}

// FileMerger performs a three-way merge for one file.
type FileMerger interface {
	MergeFile(ctx context.Context, baseContent, localContent, remoteContent []byte) (MergeResult, error)
}

// MergeResult represents the result of a file merge.
type MergeResult struct {
	Content      []byte
	HasConflicts bool
}

// PreCommitSnapshotBuilder builds docs snapshots.
type PreCommitSnapshotBuilder interface {
	Build(sourceDir string) ([]byte, error)
}

// PreCommitSnapshotApplier applies docs snapshots.
type PreCommitSnapshotApplier interface {
	Apply(snapshot []byte, targetDir, docsDir string) error
}

// DocsPushRequest is the request for pushing docs to daemon.
type DocsPushRequest struct {
	WorkspaceID  workspace.WorkspaceID
	BaseDocsHash docs.CommitHash
	DocsSnapshot string // base64 encoded
	ActorEmail   string
}

// DocsPushResponse is the response from pushing docs to daemon.
type DocsPushResponse struct {
	Ok              bool
	Status          docs.DocsPushStatus
	NewDocsHash     docs.CommitHash
	CurrentDocsHash docs.CommitHash
	Error           string
}

// PreCommitHTTPClient communicates with sanhod.
type PreCommitHTTPClient interface {
	DocsPush(ctx context.Context, req DocsPushRequest) (DocsPushResponse, error)
	DocsSnapshot(ctx context.Context, project docs.ProjectName, commit docs.CommitHash) (docs.DocsSnapshot, docs.CommitHash, error)
}

// PreCommitOutput is the output callback for user messages.
type PreCommitOutput interface {
	Info(msg string)
	Warning(msg string)
	Error(msg string)
}

// PreCommitOutdatedHandler replaces the legacy pending-fix flow when provided.
type PreCommitOutdatedHandler func(
	ctx context.Context,
	workDir string,
	config *client.WorkspaceConfig,
	baseHash, remoteHash docs.CommitHash,
) error

// PreCommitOption customizes pre-commit behavior.
type PreCommitOption func(*PreCommitUseCase)

// WithPreCommitOutdatedHandler installs the transactional pull-commit flow.
func WithPreCommitOutdatedHandler(handler PreCommitOutdatedHandler) PreCommitOption {
	return func(usecase *PreCommitUseCase) {
		usecase.outdatedHandler = handler
	}
}

// PreCommitUseCase handles the pre-commit hook logic.
type PreCommitUseCase struct {
	configLoader     PreCommitConfigLoader
	docsHashStore    PreCommitDocsHashStore
	pendingFixStore  PreCommitPendingFixStore
	conflictDetector PreCommitConflictDetector
	gitClient        PreCommitGitClient
	snapshotBuilder  PreCommitSnapshotBuilder
	snapshotApplier  PreCommitSnapshotApplier
	httpClient       PreCommitHTTPClient
	output           PreCommitOutput
	outdatedHandler  PreCommitOutdatedHandler
}

// NewPreCommitUseCase creates a new PreCommitUseCase.
func NewPreCommitUseCase(
	configLoader PreCommitConfigLoader,
	docsHashStore PreCommitDocsHashStore,
	pendingFixStore PreCommitPendingFixStore,
	conflictDetector PreCommitConflictDetector,
	gitClient PreCommitGitClient,
	snapshotBuilder PreCommitSnapshotBuilder,
	snapshotApplier PreCommitSnapshotApplier,
	httpClient PreCommitHTTPClient,
	output PreCommitOutput,
	options ...PreCommitOption,
) *PreCommitUseCase {
	usecase := &PreCommitUseCase{
		configLoader:     configLoader,
		docsHashStore:    docsHashStore,
		pendingFixStore:  pendingFixStore,
		conflictDetector: conflictDetector,
		gitClient:        gitClient,
		snapshotBuilder:  snapshotBuilder,
		snapshotApplier:  snapshotApplier,
		httpClient:       httpClient,
		output:           output,
	}
	for _, option := range options {
		option(usecase)
	}
	return usecase
}

// Execute runs the pre-commit hook logic.
func (u *PreCommitUseCase) Execute(ctx context.Context, workDir string) error {
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

	// Step 3: Check for conflict markers
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
		u.output.Error("Please resolve conflicts before committing.")
		return ErrConflictMarkerFound
	}

	// Step 4: Check for pending fix state
	pendingFixPath := filepath.Join(workDir, config.PendingFixFile)
	_, hasPendingFix, err := u.pendingFixStore.Read(pendingFixPath)
	if err != nil {
		return fmt.Errorf("failed to check pending fix state: %w", err)
	}
	if hasPendingFix {
		u.output.Error("This workspace is in pending fix state from a previous merge.")
		u.output.Error("Please run 'sanho fix' to complete the merge and sync docs.")
		u.output.Error("Commit is blocked until pending fix is resolved.")
		return ErrPendingFixExists
	}

	// Step 5: Check for docs changes
	hasChanges, err := u.gitClient.HasDocsChangeForCommit(ctx, workDir, config.DocsDir)
	if err != nil {
		return fmt.Errorf("failed to check docs changes: %w", err)
	}
	if !hasChanges {
		u.output.Info("No docs changes detected.")
		return nil
	}

	// Step 6: Build docs snapshot
	u.output.Info("Docs changes detected. Syncing with daemon...")
	snapshot, err := u.snapshotBuilder.Build(docsPath)
	if err != nil {
		return fmt.Errorf("failed to build docs snapshot: %w", err)
	}

	// Step 7: Push to daemon
	pushReq := DocsPushRequest{
		WorkspaceID:  config.WorkspaceID,
		BaseDocsHash: baseHash,
		DocsSnapshot: base64.StdEncoding.EncodeToString(snapshot),
		ActorEmail:   config.ActorEmail,
	}

	resp, err := u.httpClient.DocsPush(ctx, pushReq)
	if err != nil {
		// Handle specific errors with appropriate messages
		if errors.Is(err, ErrUnknownDocsCommit) {
			u.output.Error("The docs repo history has been rewritten.")
			u.output.Error("Cannot automatically recover. Please manually sync docs and run 'sanho init' again.")
			return ErrUnknownDocsCommit
		}
		if errors.Is(err, ErrDocsRepoBusy) {
			u.output.Error("Another workspace is currently updating docs. Please try again shortly.")
			return ErrDocsRepoBusy
		}
		return fmt.Errorf("failed to push docs: %w", err)
	}

	// Step 8: Handle response
	if !resp.Ok {
		if resp.Error == "unknown_docs_commit" {
			u.output.Error("The docs repo history has been rewritten.")
			u.output.Error("Cannot automatically recover. Please manually sync docs and run 'sanho init' again.")
			return ErrUnknownDocsCommit
		}
		if resp.Error == "docs_repo_busy" {
			u.output.Error("Another workspace is currently updating docs. Please try again shortly.")
			return ErrDocsRepoBusy
		}
		return fmt.Errorf("daemon error: %s", resp.Error)
	}

	switch resp.Status {
	case docs.DocsPushStatusUpdated:
		// Success - update hash file
		if resp.NewDocsHash != "" {
			if err := u.docsHashStore.Write(hashFilePath, resp.NewDocsHash); err != nil {
				return fmt.Errorf("failed to update docs hash: %w", err)
			}
		}
		u.output.Info(fmt.Sprintf("Docs synced successfully. New version: %s", resp.NewDocsHash))
		return nil

	case docs.DocsPushStatusNoChange:
		// No change - ensure hash is current
		if !resp.CurrentDocsHash.IsZero() {
			if err := u.docsHashStore.Write(hashFilePath, resp.CurrentDocsHash); err != nil {
				return fmt.Errorf("failed to update docs hash: %w", err)
			}
		}
		u.output.Info("Docs are already in sync with daemon.")
		return nil

	case docs.DocsPushStatusOutdated:
		// Outdated - perform 3-way merge
		u.output.Warning("Docs are outdated. Performing 3-way merge...")
		if u.outdatedHandler != nil {
			return u.outdatedHandler(ctx, workDir, config, baseHash, resp.CurrentDocsHash)
		}
		return u.handleOutdated(ctx, workDir, config, baseHash, resp.CurrentDocsHash, docsPath, hashFilePath, pendingFixPath)

	default:
		return fmt.Errorf("unexpected response status: %s", resp.Status)
	}
}

// handleOutdated performs 3-way merge when docs are outdated.
func (u *PreCommitUseCase) handleOutdated(
	ctx context.Context,
	workDir string,
	config *client.WorkspaceConfig,
	baseHash, remoteHash docs.CommitHash,
	docsPath, hashFilePath, pendingFixPath string,
) error {
	// Download base and remote snapshots
	baseSnapshot, _, err := u.httpClient.DocsSnapshot(ctx, config.Project, baseHash)
	if err != nil {
		return fmt.Errorf("failed to download base snapshot: %w", err)
	}

	remoteSnapshot, _, err := u.httpClient.DocsSnapshot(ctx, config.Project, remoteHash)
	if err != nil {
		return fmt.Errorf("failed to download remote snapshot: %w", err)
	}

	// Create temp directories for base and remote
	tempDir, err := os.MkdirTemp("", "sanho-merge-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer func() {
		_ = os.RemoveAll(tempDir)
	}()

	baseDir := filepath.Join(tempDir, "base")
	remoteDir := filepath.Join(tempDir, "remote")

	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return fmt.Errorf("failed to create base directory: %w", err)
	}
	if err := os.MkdirAll(remoteDir, 0755); err != nil {
		return fmt.Errorf("failed to create remote directory: %w", err)
	}

	// Apply snapshots
	if err := u.snapshotApplier.Apply(baseSnapshot, baseDir, "docs"); err != nil {
		return fmt.Errorf("failed to apply base snapshot: %w", err)
	}
	if err := u.snapshotApplier.Apply(remoteSnapshot, remoteDir, "docs"); err != nil {
		return fmt.Errorf("failed to apply remote snapshot: %w", err)
	}

	// Perform 3-way merge
	baseDocs := filepath.Join(baseDir, "docs")
	remoteDocs := filepath.Join(remoteDir, "docs")

	hasConflicts, conflictFiles, err := u.mergeDirectories(ctx, baseDocs, docsPath, remoteDocs)
	if err != nil {
		return fmt.Errorf("failed to perform 3-way merge: %w", err)
	}

	// Update hash to remote
	if err := u.docsHashStore.Write(hashFilePath, remoteHash); err != nil {
		return fmt.Errorf("failed to update docs hash: %w", err)
	}

	// Create pending fix state
	pendingState := client.PendingFixState{
		BaseHash:   baseHash,
		RemoteHash: remoteHash,
		CreatedAt:  time.Now(),
	}
	if err := u.pendingFixStore.Write(pendingFixPath, pendingState); err != nil {
		return fmt.Errorf("failed to create pending fix state: %w", err)
	}

	// Output messages
	if hasConflicts {
		u.output.Error("Merge completed with conflicts:")
		for _, f := range conflictFiles {
			u.output.Error(fmt.Sprintf("  - %s", f))
		}
		u.output.Error("")
		u.output.Error("Please resolve the conflicts in the above files.")
	} else {
		u.output.Warning("Merge completed successfully (no conflicts).")
	}

	u.output.Warning("")
	u.output.Warning("After reviewing/resolving, run 'sanho fix' to complete the sync.")
	u.output.Warning("Commit is blocked until 'sanho fix' is completed.")

	return ErrOutdated
}

// mergeDirectories performs 3-way merge on all files in the directories.
// It modifies localDir in place with merged contents.
// Returns whether any conflicts occurred and the list of conflict files.
func (u *PreCommitUseCase) mergeDirectories(
	ctx context.Context,
	baseDir, localDir, remoteDir string,
) (hasConflicts bool, conflictFiles []string, err error) {
	return MergeDirectories(ctx, u.gitClient, baseDir, localDir, remoteDir)
}

// MergeDirectories performs a three-way merge on all files in the directories.
// It modifies localDir in place with merged contents.
func MergeDirectories(
	ctx context.Context,
	merger FileMerger,
	baseDir, localDir, remoteDir string,
) (hasConflicts bool, conflictFiles []string, err error) {
	// Collect all files from all three directories
	allFiles := make(map[string]struct{})

	collectFiles := func(dir string) error {
		return filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				if os.IsNotExist(err) {
					return nil
				}
				return err
			}
			if d.IsDir() {
				return nil
			}
			relPath, err := filepath.Rel(dir, path)
			if err != nil {
				return err
			}
			allFiles[relPath] = struct{}{}
			return nil
		})
	}

	if err := collectFiles(baseDir); err != nil {
		return false, nil, err
	}
	if err := collectFiles(localDir); err != nil {
		return false, nil, err
	}
	if err := collectFiles(remoteDir); err != nil {
		return false, nil, err
	}

	// Merge each file
	for relPath := range allFiles {
		basePath := filepath.Join(baseDir, relPath)
		localPath := filepath.Join(localDir, relPath)
		remotePath := filepath.Join(remoteDir, relPath)

		baseState, err := readMergeFileState(basePath)
		if err != nil {
			return false, nil, fmt.Errorf("failed to read base %s: %w", relPath, err)
		}
		localState, err := readMergeFileState(localPath)
		if err != nil {
			return false, nil, fmt.Errorf("failed to read local %s: %w", relPath, err)
		}
		remoteState, err := readMergeFileState(remotePath)
		if err != nil {
			return false, nil, fmt.Errorf("failed to read remote %s: %w", relPath, err)
		}

		switch {
		case mergeFileContentsEqual(localState, remoteState):
			if localState.exists {
				localState.mode = pickEqualContentMode(baseState, localState, remoteState)
				if err := applyMergeFileState(localPath, localState); err != nil {
					return false, nil, fmt.Errorf("failed to keep shared %s: %w", relPath, err)
				}
			}
			continue
		case mergeFileContentsEqual(localState, baseState):
			if err := applyMergeFileState(localPath, remoteState); err != nil {
				return false, nil, fmt.Errorf("failed to adopt remote %s: %w", relPath, err)
			}
			continue
		case mergeFileContentsEqual(remoteState, baseState):
			continue
		}

		// Perform merge
		result, err := merger.MergeFile(ctx, baseState.content, localState.content, remoteState.content)
		if err != nil {
			return false, nil, fmt.Errorf("failed to merge %s: %w", relPath, err)
		}

		conflictTriggered := result.HasConflicts

		// Write merged content
		targetPath := filepath.Join(localDir, relPath)
		if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
			return false, nil, fmt.Errorf("failed to create directory for %s: %w", relPath, err)
		}
		mode, err := pickFileMode(localPath, remotePath, basePath)
		if err != nil {
			return false, nil, fmt.Errorf("failed to pick file mode for %s: %w", relPath, err)
		}
		if err := os.WriteFile(targetPath, result.Content, mode); err != nil {
			return false, nil, fmt.Errorf("failed to write %s: %w", relPath, err)
		}

		// The unchanged-remote and both-deleted cases were handled by the
		// trivial fast paths. Any remaining local deletion is a conflict.
		if baseState.exists && !localState.exists && remoteState.exists &&
			!bytes.Equal(remoteState.content, baseState.content) {
			conflictTriggered = true
		}

		if conflictTriggered {
			hasConflicts = true
			conflictFiles = append(conflictFiles, relPath)
		}
	}

	return hasConflicts, conflictFiles, nil
}

type mergeFileState struct {
	content []byte
	mode    os.FileMode
	exists  bool
}

func readMergeFileState(path string) (mergeFileState, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return mergeFileState{}, nil
		}
		return mergeFileState{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return mergeFileState{}, err
	}
	return mergeFileState{
		content: content,
		mode:    info.Mode().Perm(),
		exists:  true,
	}, nil
}

func mergeFileContentsEqual(left, right mergeFileState) bool {
	if left.exists != right.exists {
		return false
	}
	if !left.exists {
		return true
	}
	return bytes.Equal(left.content, right.content)
}

func pickEqualContentMode(base, local, remote mergeFileState) os.FileMode {
	switch {
	case local.mode.Perm() == remote.mode.Perm():
		return local.mode.Perm()
	case base.exists && local.mode.Perm() == base.mode.Perm():
		return remote.mode.Perm()
	case base.exists && remote.mode.Perm() == base.mode.Perm():
		return local.mode.Perm()
	default:
		return local.mode.Perm()
	}
}

func applyMergeFileState(path string, state mergeFileState) error {
	if !state.exists {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(path, state.content, state.mode.Perm()); err != nil {
		return err
	}
	return os.Chmod(path, state.mode.Perm())
}

func pickFileMode(localPath, remotePath, basePath string) (os.FileMode, error) {
	paths := []string{localPath, remotePath, basePath}
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return 0, err
		}
		return info.Mode(), nil
	}
	return 0644, nil
}
