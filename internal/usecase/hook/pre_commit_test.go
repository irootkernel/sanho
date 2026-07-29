package hook

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/irootkernel/sanho/internal/domain/client"
	"github.com/irootkernel/sanho/internal/domain/docs"
	"github.com/irootkernel/sanho/internal/domain/workspace"
)

// --- Fake implementations for testing ---

type fakeConfigLoader struct {
	config *client.WorkspaceConfig
	err    error
}

func (f *fakeConfigLoader) Load(workDir string) (*client.WorkspaceConfig, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.config, nil
}

type fakeDocsHashStore struct {
	hash     docs.CommitHash
	readErr  error
	writeErr error
	written  docs.CommitHash
}

func (f *fakeDocsHashStore) Read(path string) (docs.CommitHash, error) {
	if f.readErr != nil {
		return "", f.readErr
	}
	return f.hash, nil
}

func (f *fakeDocsHashStore) Write(path string, hash docs.CommitHash) error {
	if f.writeErr != nil {
		return f.writeErr
	}
	f.written = hash
	return nil
}

type fakePendingFixStore struct {
	state     client.PendingFixState
	exists    bool
	readErr   error
	writeErr  error
	removeErr error
	written   bool
	removed   bool
}

func (f *fakePendingFixStore) Read(path string) (client.PendingFixState, bool, error) {
	if f.readErr != nil {
		return client.PendingFixState{}, false, f.readErr
	}
	return f.state, f.exists, nil
}

func (f *fakePendingFixStore) Write(path string, state client.PendingFixState) error {
	if f.writeErr != nil {
		return f.writeErr
	}
	f.written = true
	f.state = state
	return nil
}

func (f *fakePendingFixStore) Remove(path string) error {
	if f.removeErr != nil {
		return f.removeErr
	}
	f.removed = true
	return nil
}

type fakeConflictDetector struct {
	conflicts []string
	err       error
}

func (f *fakeConflictDetector) DetectConflicts(docsDir string) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.conflicts, nil
}

type fakeGitClient struct {
	hasChanges    bool
	hasChangesErr error
	mergeResult   MergeResult
	mergeErr      error
}

func (f *fakeGitClient) HasDocsChangeForCommit(ctx context.Context, repoPath, docsDir string) (bool, error) {
	if f.hasChangesErr != nil {
		return false, f.hasChangesErr
	}
	return f.hasChanges, nil
}

func (f *fakeGitClient) MergeFile(ctx context.Context, baseContent, localContent, remoteContent []byte) (MergeResult, error) {
	if f.mergeErr != nil {
		return MergeResult{}, f.mergeErr
	}
	return f.mergeResult, nil
}

type fakeSnapshotBuilder struct {
	snapshot []byte
	err      error
}

func (f *fakeSnapshotBuilder) Build(sourceDir string) ([]byte, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.snapshot, nil
}

type fakeSnapshotApplier struct {
	err error
}

func (f *fakeSnapshotApplier) Apply(snapshot []byte, targetDir, docsDir string) error {
	return f.err
}

type fakeHTTPClient struct {
	pushResponse DocsPushResponse
	pushErr      error
	snapshotData docs.DocsSnapshot
	snapshotHash docs.CommitHash
	snapshotErr  error
}

func (f *fakeHTTPClient) DocsPush(ctx context.Context, req DocsPushRequest) (DocsPushResponse, error) {
	if f.pushErr != nil {
		return DocsPushResponse{}, f.pushErr
	}
	return f.pushResponse, nil
}

func (f *fakeHTTPClient) DocsSnapshot(ctx context.Context, project docs.ProjectName, commit docs.CommitHash) (docs.DocsSnapshot, docs.CommitHash, error) {
	if f.snapshotErr != nil {
		return nil, "", f.snapshotErr
	}
	return f.snapshotData, f.snapshotHash, nil
}

type fakeOutput struct {
	infos    []string
	warnings []string
	errors   []string
}

func (f *fakeOutput) Info(msg string)    { f.infos = append(f.infos, msg) }
func (f *fakeOutput) Warning(msg string) { f.warnings = append(f.warnings, msg) }
func (f *fakeOutput) Error(msg string)   { f.errors = append(f.errors, msg) }

// --- Tests ---

func TestPreCommitUseCase_ConfigBroken(t *testing.T) {
	uc := NewPreCommitUseCase(
		&fakeConfigLoader{err: errors.New("config not found")},
		&fakeDocsHashStore{},
		&fakePendingFixStore{},
		&fakeConflictDetector{},
		&fakeGitClient{},
		&fakeSnapshotBuilder{},
		&fakeSnapshotApplier{},
		&fakeHTTPClient{},
		&fakeOutput{},
	)

	err := uc.Execute(context.Background(), "/fake/dir")
	if !errors.Is(err, ErrConfigBroken) {
		t.Errorf("Expected ErrConfigBroken, got: %v", err)
	}
}

func TestPreCommitUseCase_HashFileBroken(t *testing.T) {
	uc := NewPreCommitUseCase(
		&fakeConfigLoader{config: &client.WorkspaceConfig{
			SocketPath:  "http://localhost",
			WorkspaceID: "test",
			Project:     "test",
		}},
		&fakeDocsHashStore{readErr: errors.New("hash file not found")},
		&fakePendingFixStore{},
		&fakeConflictDetector{},
		&fakeGitClient{},
		&fakeSnapshotBuilder{},
		&fakeSnapshotApplier{},
		&fakeHTTPClient{},
		&fakeOutput{},
	)

	err := uc.Execute(context.Background(), "/fake/dir")
	if !errors.Is(err, ErrConfigBroken) {
		t.Errorf("Expected ErrConfigBroken, got: %v", err)
	}
}

func TestPreCommitUseCase_ConflictMarkersFound(t *testing.T) {
	output := &fakeOutput{}
	uc := NewPreCommitUseCase(
		&fakeConfigLoader{config: &client.WorkspaceConfig{
			SocketPath:  "http://localhost",
			WorkspaceID: "test",
			Project:     "test",
		}},
		&fakeDocsHashStore{hash: "abc123"},
		&fakePendingFixStore{},
		&fakeConflictDetector{conflicts: []string{"readme.md", "guide.md"}},
		&fakeGitClient{},
		&fakeSnapshotBuilder{},
		&fakeSnapshotApplier{},
		&fakeHTTPClient{},
		output,
	)

	err := uc.Execute(context.Background(), "/fake/dir")
	if !errors.Is(err, ErrConflictMarkerFound) {
		t.Errorf("Expected ErrConflictMarkerFound, got: %v", err)
	}
	if len(output.errors) == 0 {
		t.Error("Expected error messages to be output")
	}
}

func TestPreCommitUseCase_PendingFixExists(t *testing.T) {
	output := &fakeOutput{}
	uc := NewPreCommitUseCase(
		&fakeConfigLoader{config: &client.WorkspaceConfig{
			SocketPath:  "http://localhost",
			WorkspaceID: "test",
			Project:     "test",
		}},
		&fakeDocsHashStore{hash: "abc123"},
		&fakePendingFixStore{exists: true, state: client.PendingFixState{
			BaseHash:   "abc123",
			RemoteHash: "def456",
			CreatedAt:  time.Now(),
		}},
		&fakeConflictDetector{},
		&fakeGitClient{},
		&fakeSnapshotBuilder{},
		&fakeSnapshotApplier{},
		&fakeHTTPClient{},
		output,
	)

	err := uc.Execute(context.Background(), "/fake/dir")
	if !errors.Is(err, ErrPendingFixExists) {
		t.Errorf("Expected ErrPendingFixExists, got: %v", err)
	}
}

func TestPreCommitUseCase_NoDocsChanges(t *testing.T) {
	output := &fakeOutput{}
	uc := NewPreCommitUseCase(
		&fakeConfigLoader{config: &client.WorkspaceConfig{
			SocketPath:  "http://localhost",
			WorkspaceID: "test",
			Project:     "test",
		}},
		&fakeDocsHashStore{hash: "abc123"},
		&fakePendingFixStore{},
		&fakeConflictDetector{},
		&fakeGitClient{hasChanges: false},
		&fakeSnapshotBuilder{},
		&fakeSnapshotApplier{},
		&fakeHTTPClient{},
		output,
	)

	err := uc.Execute(context.Background(), "/fake/dir")
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if len(output.infos) == 0 {
		t.Error("Expected info message about no changes")
	}
}

func TestPreCommitUseCase_PushUpdated(t *testing.T) {
	hashStore := &fakeDocsHashStore{hash: "abc123"}
	output := &fakeOutput{}
	uc := NewPreCommitUseCase(
		&fakeConfigLoader{config: &client.WorkspaceConfig{
			SocketPath:  "http://localhost",
			WorkspaceID: workspace.WorkspaceID("test"),
			Project:     "test",
		}},
		hashStore,
		&fakePendingFixStore{},
		&fakeConflictDetector{},
		&fakeGitClient{hasChanges: true},
		&fakeSnapshotBuilder{snapshot: []byte("snapshot")},
		&fakeSnapshotApplier{},
		&fakeHTTPClient{pushResponse: DocsPushResponse{
			Ok:          true,
			Status:      docs.DocsPushStatusUpdated,
			NewDocsHash: "def456",
		}},
		output,
	)

	err := uc.Execute(context.Background(), "/fake/dir")
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if hashStore.written != "def456" {
		t.Errorf("Expected hash to be updated to 'def456', got: %s", hashStore.written)
	}
}

func TestPreCommitUseCase_PushNoChange(t *testing.T) {
	hashStore := &fakeDocsHashStore{hash: "abc123"}
	output := &fakeOutput{}
	uc := NewPreCommitUseCase(
		&fakeConfigLoader{config: &client.WorkspaceConfig{
			SocketPath:  "http://localhost",
			WorkspaceID: workspace.WorkspaceID("test"),
			Project:     "test",
		}},
		hashStore,
		&fakePendingFixStore{},
		&fakeConflictDetector{},
		&fakeGitClient{hasChanges: true},
		&fakeSnapshotBuilder{snapshot: []byte("snapshot")},
		&fakeSnapshotApplier{},
		&fakeHTTPClient{pushResponse: DocsPushResponse{
			Ok:              true,
			Status:          docs.DocsPushStatusNoChange,
			CurrentDocsHash: "abc123",
		}},
		output,
	)

	err := uc.Execute(context.Background(), "/fake/dir")
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
}

func TestPreCommitUseCase_UnknownDocsCommit(t *testing.T) {
	output := &fakeOutput{}
	uc := NewPreCommitUseCase(
		&fakeConfigLoader{config: &client.WorkspaceConfig{
			SocketPath:  "http://localhost",
			WorkspaceID: workspace.WorkspaceID("test"),
			Project:     "test",
		}},
		&fakeDocsHashStore{hash: "abc123"},
		&fakePendingFixStore{},
		&fakeConflictDetector{},
		&fakeGitClient{hasChanges: true},
		&fakeSnapshotBuilder{snapshot: []byte("snapshot")},
		&fakeSnapshotApplier{},
		&fakeHTTPClient{pushResponse: DocsPushResponse{
			Ok:    false,
			Error: "unknown_docs_commit",
		}},
		output,
	)

	err := uc.Execute(context.Background(), "/fake/dir")
	if !errors.Is(err, ErrUnknownDocsCommit) {
		t.Errorf("Expected ErrUnknownDocsCommit, got: %v", err)
	}
}

func TestPreCommitUseCase_DocsRepoBusy(t *testing.T) {
	output := &fakeOutput{}
	uc := NewPreCommitUseCase(
		&fakeConfigLoader{config: &client.WorkspaceConfig{
			SocketPath:  "http://localhost",
			WorkspaceID: workspace.WorkspaceID("test"),
			Project:     "test",
		}},
		&fakeDocsHashStore{hash: "abc123"},
		&fakePendingFixStore{},
		&fakeConflictDetector{},
		&fakeGitClient{hasChanges: true},
		&fakeSnapshotBuilder{snapshot: []byte("snapshot")},
		&fakeSnapshotApplier{},
		&fakeHTTPClient{pushResponse: DocsPushResponse{
			Ok:    false,
			Error: "docs_repo_busy",
		}},
		output,
	)

	err := uc.Execute(context.Background(), "/fake/dir")
	if !errors.Is(err, ErrDocsRepoBusy) {
		t.Errorf("Expected ErrDocsRepoBusy, got: %v", err)
	}
}

func TestMergeDirectories_RemoteDeletionRemovesUnchangedFile(t *testing.T) {
	baseDir := t.TempDir()
	localDir := t.TempDir()
	remoteDir := t.TempDir()

	relPath := filepath.Join("docs", "guide.md")

	basePath := filepath.Join(baseDir, relPath)
	localPath := filepath.Join(localDir, relPath)

	if err := os.MkdirAll(filepath.Dir(basePath), 0755); err != nil {
		t.Fatalf("failed to create base dir: %v", err)
	}
	if err := os.WriteFile(basePath, []byte("content"), 0644); err != nil {
		t.Fatalf("failed to write base file: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
		t.Fatalf("failed to create local dir: %v", err)
	}
	if err := os.WriteFile(localPath, []byte("content"), 0644); err != nil {
		t.Fatalf("failed to write local file: %v", err)
	}

	uc := &PreCommitUseCase{
		gitClient: &fakeGitClient{},
	}

	hasConflicts, conflictFiles, err := uc.mergeDirectories(context.Background(), baseDir, localDir, remoteDir)
	if err != nil {
		t.Fatalf("mergeDirectories returned error: %v", err)
	}
	if hasConflicts {
		t.Fatalf("expected no conflicts, got hasConflicts=true")
	}
	if len(conflictFiles) != 0 {
		t.Fatalf("expected no conflict files, got %v", conflictFiles)
	}
	if _, err := os.Stat(localPath); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be removed, got err=%v", localPath, err)
	}
}

func TestMergeDirectories_RemoteDeletionConflictsWithLocalChanges(t *testing.T) {
	baseDir := t.TempDir()
	localDir := t.TempDir()
	remoteDir := t.TempDir()

	relPath := filepath.Join("docs", "guide.md")

	basePath := filepath.Join(baseDir, relPath)
	localPath := filepath.Join(localDir, relPath)

	if err := os.MkdirAll(filepath.Dir(basePath), 0755); err != nil {
		t.Fatalf("failed to create base dir: %v", err)
	}
	if err := os.WriteFile(basePath, []byte("base"), 0644); err != nil {
		t.Fatalf("failed to write base file: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
		t.Fatalf("failed to create local dir: %v", err)
	}
	if err := os.WriteFile(localPath, []byte("base\nlocal change"), 0644); err != nil {
		t.Fatalf("failed to write local file: %v", err)
	}

	gitClient := &fakeGitClient{
		mergeResult: MergeResult{
			Content:      []byte("conflict markers"),
			HasConflicts: true,
		},
	}

	uc := &PreCommitUseCase{
		gitClient: gitClient,
	}

	hasConflicts, conflictFiles, err := uc.mergeDirectories(context.Background(), baseDir, localDir, remoteDir)
	if err != nil {
		t.Fatalf("mergeDirectories returned error: %v", err)
	}
	if !hasConflicts {
		t.Fatalf("expected conflicts, got hasConflicts=false")
	}
	if len(conflictFiles) != 1 || conflictFiles[0] != relPath {
		t.Fatalf("expected conflict file %s, got %v", relPath, conflictFiles)
	}

	mergedContent, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatalf("failed to read merged file: %v", err)
	}
	if string(mergedContent) != "conflict markers" {
		t.Fatalf("unexpected merged content: %s", string(mergedContent))
	}
}

func TestMergeDirectories_LocalDeletionKeepsRemovalWhenRemoteUnchanged(t *testing.T) {
	baseDir := t.TempDir()
	localDir := t.TempDir()
	remoteDir := t.TempDir()

	relPath := filepath.Join("docs", "guide.md")

	basePath := filepath.Join(baseDir, relPath)
	remotePath := filepath.Join(remoteDir, relPath)

	if err := os.MkdirAll(filepath.Dir(basePath), 0755); err != nil {
		t.Fatalf("failed to create base dir: %v", err)
	}
	if err := os.WriteFile(basePath, []byte("content"), 0644); err != nil {
		t.Fatalf("failed to write base file: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(remotePath), 0755); err != nil {
		t.Fatalf("failed to create remote dir: %v", err)
	}
	if err := os.WriteFile(remotePath, []byte("content"), 0644); err != nil {
		t.Fatalf("failed to write remote file: %v", err)
	}

	uc := &PreCommitUseCase{
		gitClient: &fakeGitClient{
			mergeResult: MergeResult{
				Content:      []byte("content"),
				HasConflicts: false,
			},
		},
	}

	hasConflicts, conflictFiles, err := uc.mergeDirectories(context.Background(), baseDir, localDir, remoteDir)
	if err != nil {
		t.Fatalf("mergeDirectories returned error: %v", err)
	}
	if hasConflicts {
		t.Fatalf("expected no conflicts, got hasConflicts=true")
	}
	if len(conflictFiles) != 0 {
		t.Fatalf("expected no conflict files, got %v", conflictFiles)
	}
	if _, err := os.Stat(filepath.Join(localDir, relPath)); !os.IsNotExist(err) {
		t.Fatalf("expected %s to remain deleted, got err=%v", relPath, err)
	}
}

func TestMergeDirectories_LocalDeletionConflictsWhenRemoteModified(t *testing.T) {
	baseDir := t.TempDir()
	localDir := t.TempDir()
	remoteDir := t.TempDir()

	relPath := filepath.Join("docs", "guide.md")

	basePath := filepath.Join(baseDir, relPath)
	remotePath := filepath.Join(remoteDir, relPath)

	if err := os.MkdirAll(filepath.Dir(basePath), 0755); err != nil {
		t.Fatalf("failed to create base dir: %v", err)
	}
	if err := os.WriteFile(basePath, []byte("base"), 0644); err != nil {
		t.Fatalf("failed to write base file: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(remotePath), 0755); err != nil {
		t.Fatalf("failed to create remote dir: %v", err)
	}
	if err := os.WriteFile(remotePath, []byte("remote change"), 0644); err != nil {
		t.Fatalf("failed to write remote file: %v", err)
	}

	gitClient := &fakeGitClient{
		mergeResult: MergeResult{
			Content:      []byte("merged"),
			HasConflicts: false,
		},
	}

	uc := &PreCommitUseCase{
		gitClient: gitClient,
	}

	hasConflicts, conflictFiles, err := uc.mergeDirectories(context.Background(), baseDir, localDir, remoteDir)
	if err != nil {
		t.Fatalf("mergeDirectories returned error: %v", err)
	}
	if !hasConflicts {
		t.Fatalf("expected conflicts, got hasConflicts=false")
	}
	if len(conflictFiles) != 1 || conflictFiles[0] != relPath {
		t.Fatalf("expected conflict file %s, got %v", relPath, conflictFiles)
	}

	mergedContent, err := os.ReadFile(filepath.Join(localDir, relPath))
	if err != nil {
		t.Fatalf("failed to read merged file: %v", err)
	}
	if string(mergedContent) != "merged" {
		t.Fatalf("unexpected merged content: %s", string(mergedContent))
	}
}

func TestMergeDirectories_PreservesLocalFileMode(t *testing.T) {
	baseDir := t.TempDir()
	localDir := t.TempDir()
	remoteDir := t.TempDir()

	relPath := filepath.Join("docs", "run.sh")
	basePath := filepath.Join(baseDir, relPath)
	localPath := filepath.Join(localDir, relPath)
	remotePath := filepath.Join(remoteDir, relPath)

	for _, p := range []string{basePath, localPath, remotePath} {
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatalf("failed to create dir for %s: %v", p, err)
		}
		if err := os.WriteFile(p, []byte("echo hi"), 0644); err != nil {
			t.Fatalf("failed to write %s: %v", p, err)
		}
	}
	// Make local executable to ensure mode is preserved.
	if err := os.Chmod(localPath, 0755); err != nil {
		t.Fatalf("failed to chmod local: %v", err)
	}

	uc := &PreCommitUseCase{
		gitClient: &fakeGitClient{
			mergeResult: MergeResult{
				Content:      []byte("echo hi"),
				HasConflicts: false,
			},
		},
	}

	if _, _, err := uc.mergeDirectories(context.Background(), baseDir, localDir, remoteDir); err != nil {
		t.Fatalf("mergeDirectories returned error: %v", err)
	}

	info, err := os.Stat(localPath)
	if err != nil {
		t.Fatalf("failed to stat merged file: %v", err)
	}
	if info.Mode().Perm() != 0755 {
		t.Fatalf("expected mode 0755, got %v", info.Mode().Perm())
	}
}

func TestMergeDirectories_UsesRemoteModeWhenCreatingFile(t *testing.T) {
	baseDir := t.TempDir()
	localDir := t.TempDir()
	remoteDir := t.TempDir()

	relPath := filepath.Join("docs", "run.sh")
	basePath := filepath.Join(baseDir, relPath)
	remotePath := filepath.Join(remoteDir, relPath)

	for _, p := range []string{basePath, remotePath} {
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatalf("failed to create dir for %s: %v", p, err)
		}
		if err := os.WriteFile(p, []byte("base"), 0644); err != nil {
			t.Fatalf("failed to write %s: %v", p, err)
		}
	}
	// Remote has changed content and executable bit.
	if err := os.WriteFile(remotePath, []byte("remote change"), 0644); err != nil {
		t.Fatalf("failed to write remote change: %v", err)
	}
	if err := os.Chmod(remotePath, 0755); err != nil {
		t.Fatalf("failed to chmod remote: %v", err)
	}

	uc := &PreCommitUseCase{
		gitClient: &fakeGitClient{
			mergeResult: MergeResult{
				Content:      []byte("merged"),
				HasConflicts: false,
			},
		},
	}

	hasConflicts, conflictFiles, err := uc.mergeDirectories(context.Background(), baseDir, localDir, remoteDir)
	if err != nil {
		t.Fatalf("mergeDirectories returned error: %v", err)
	}
	if !hasConflicts || len(conflictFiles) != 1 || conflictFiles[0] != relPath {
		t.Fatalf("expected conflict for %s, got hasConflicts=%v, files=%v", relPath, hasConflicts, conflictFiles)
	}

	info, err := os.Stat(filepath.Join(localDir, relPath))
	if err != nil {
		t.Fatalf("failed to stat merged file: %v", err)
	}
	if info.Mode().Perm() != 0755 {
		t.Fatalf("expected mode 0755 from remote, got %v", info.Mode().Perm())
	}
}
