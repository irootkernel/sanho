package hook

import (
	"context"
	"errors"
	"testing"

	"github.com/SeventeenthEarth/kkachi/internal/domain/client"
	"github.com/SeventeenthEarth/kkachi/internal/domain/docs"
	"github.com/SeventeenthEarth/kkachi/internal/domain/workspace"
)

// --- Fake implementations for testing ---

type fakeFixConfigLoader struct {
	config *client.WorkspaceConfig
	err    error
}

func (f *fakeFixConfigLoader) Load(workDir string) (*client.WorkspaceConfig, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.config, nil
}

type fakeFixDocsHashStore struct {
	hash     docs.CommitHash
	readErr  error
	writeErr error
	written  docs.CommitHash
}

func (f *fakeFixDocsHashStore) Read(path string) (docs.CommitHash, error) {
	if f.readErr != nil {
		return "", f.readErr
	}
	return f.hash, nil
}

func (f *fakeFixDocsHashStore) Write(path string, hash docs.CommitHash) error {
	if f.writeErr != nil {
		return f.writeErr
	}
	f.written = hash
	return nil
}

type fakeFixPendingFixStore struct {
	exists    bool
	readErr   error
	removeErr error
	removed   bool
}

func (f *fakeFixPendingFixStore) Read(path string) (bool, error) {
	if f.readErr != nil {
		return false, f.readErr
	}
	return f.exists, nil
}

func (f *fakeFixPendingFixStore) Remove(path string) error {
	if f.removeErr != nil {
		return f.removeErr
	}
	f.removed = true
	return nil
}

type fakeFixConflictDetector struct {
	conflicts []string
	err       error
}

func (f *fakeFixConflictDetector) DetectConflicts(docsDir string) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.conflicts, nil
}

type fakeFixSnapshotBuilder struct {
	snapshot []byte
	err      error
}

func (f *fakeFixSnapshotBuilder) Build(sourceDir string) ([]byte, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.snapshot, nil
}

type fakeFixHTTPClient struct {
	docsHead     docs.CommitHash
	docsHeadErr  error
	pushResponse DocsPushResponse
	pushErr      error
	lastReq      DocsPushRequest
}

func (f *fakeFixHTTPClient) DocsHead(ctx context.Context, project docs.ProjectName) (docs.CommitHash, error) {
	if f.docsHeadErr != nil {
		return "", f.docsHeadErr
	}
	return f.docsHead, nil
}

func (f *fakeFixHTTPClient) DocsPush(ctx context.Context, req DocsPushRequest) (DocsPushResponse, error) {
	f.lastReq = req
	if f.pushErr != nil {
		return DocsPushResponse{}, f.pushErr
	}
	return f.pushResponse, nil
}

type fakeFixGitClient struct {
	email string
	err   error
}

func (f *fakeFixGitClient) GetUserEmail(ctx context.Context, path string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.email, nil
}

type fakeFixOutput struct {
	infos    []string
	warnings []string
	errors   []string
}

func (f *fakeFixOutput) Info(msg string)    { f.infos = append(f.infos, msg) }
func (f *fakeFixOutput) Warning(msg string) { f.warnings = append(f.warnings, msg) }
func (f *fakeFixOutput) Error(msg string)   { f.errors = append(f.errors, msg) }

// --- Helper function ---

func defaultFixConfig() *client.WorkspaceConfig {
	return &client.WorkspaceConfig{
		ServerURL:   "http://localhost",
		WorkspaceID: workspace.WorkspaceID("test-workspace"),
		Project:     "test-project",
		ActorEmail:  "test@example.com",
	}
}

// --- Tests ---

func TestFixUseCase_ConfigBroken(t *testing.T) {
	uc := NewFixUseCase(
		&fakeFixConfigLoader{err: errors.New("config not found")},
		&fakeFixDocsHashStore{},
		&fakeFixPendingFixStore{},
		&fakeFixConflictDetector{},
		&fakeFixSnapshotBuilder{},
		&fakeFixHTTPClient{},
		&fakeFixGitClient{},
		&fakeFixOutput{},
	)

	err := uc.Execute(context.Background(), "/fake/dir")
	if !errors.Is(err, ErrConfigBroken) {
		t.Errorf("Expected ErrConfigBroken, got: %v", err)
	}
}

func TestFixUseCase_HashFileBroken(t *testing.T) {
	uc := NewFixUseCase(
		&fakeFixConfigLoader{config: defaultFixConfig()},
		&fakeFixDocsHashStore{readErr: errors.New("hash file not found")},
		&fakeFixPendingFixStore{},
		&fakeFixConflictDetector{},
		&fakeFixSnapshotBuilder{},
		&fakeFixHTTPClient{},
		&fakeFixGitClient{},
		&fakeFixOutput{},
	)

	err := uc.Execute(context.Background(), "/fake/dir")
	if !errors.Is(err, ErrConfigBroken) {
		t.Errorf("Expected ErrConfigBroken, got: %v", err)
	}
}

func TestFixUseCase_NoPendingFix(t *testing.T) {
	output := &fakeFixOutput{}
	uc := NewFixUseCase(
		&fakeFixConfigLoader{config: defaultFixConfig()},
		&fakeFixDocsHashStore{hash: "abc123"},
		&fakeFixPendingFixStore{exists: false},
		&fakeFixConflictDetector{},
		&fakeFixSnapshotBuilder{},
		&fakeFixHTTPClient{},
		&fakeFixGitClient{},
		output,
	)

	err := uc.Execute(context.Background(), "/fake/dir")
	if !errors.Is(err, ErrNoPendingFix) {
		t.Errorf("Expected ErrNoPendingFix, got: %v", err)
	}
	if len(output.errors) == 0 {
		t.Error("Expected error message about no pending fix")
	}
}

func TestFixUseCase_ConflictMarkersFound(t *testing.T) {
	output := &fakeFixOutput{}
	uc := NewFixUseCase(
		&fakeFixConfigLoader{config: defaultFixConfig()},
		&fakeFixDocsHashStore{hash: "abc123"},
		&fakeFixPendingFixStore{exists: true},
		&fakeFixConflictDetector{conflicts: []string{"readme.md", "guide.md"}},
		&fakeFixSnapshotBuilder{},
		&fakeFixHTTPClient{},
		&fakeFixGitClient{},
		output,
	)

	err := uc.Execute(context.Background(), "/fake/dir")
	if !errors.Is(err, ErrConflictMarkerFound) {
		t.Errorf("Expected ErrConflictMarkerFound, got: %v", err)
	}
	if len(output.errors) < 2 {
		t.Error("Expected error messages listing conflicting files")
	}
}

func TestFixUseCase_HeadChanged(t *testing.T) {
	output := &fakeFixOutput{}
	pendingStore := &fakeFixPendingFixStore{exists: true}
	uc := NewFixUseCase(
		&fakeFixConfigLoader{config: defaultFixConfig()},
		&fakeFixDocsHashStore{hash: "abc123"},
		pendingStore,
		&fakeFixConflictDetector{},
		&fakeFixSnapshotBuilder{},
		&fakeFixHTTPClient{docsHead: "def456"}, // Different from base
		&fakeFixGitClient{},
		output,
	)

	err := uc.Execute(context.Background(), "/fake/dir")
	if !errors.Is(err, ErrFixHeadChanged) {
		t.Errorf("Expected ErrFixHeadChanged, got: %v", err)
	}
	if !pendingStore.removed {
		t.Error("Expected pending fix state to be removed")
	}
	if len(output.warnings) == 0 {
		t.Error("Expected warning messages about HEAD change")
	}
}

func TestFixUseCase_SuccessUpdated(t *testing.T) {
	output := &fakeFixOutput{}
	pendingStore := &fakeFixPendingFixStore{exists: true}
	hashStore := &fakeFixDocsHashStore{hash: "abc123"}
	uc := NewFixUseCase(
		&fakeFixConfigLoader{config: defaultFixConfig()},
		hashStore,
		pendingStore,
		&fakeFixConflictDetector{},
		&fakeFixSnapshotBuilder{snapshot: []byte("snapshot data")},
		&fakeFixHTTPClient{
			docsHead: "abc123", // Same as base
			pushResponse: DocsPushResponse{
				Ok:          true,
				Status:      docs.DocsPushStatusUpdated,
				NewDocsHash: "xyz789",
			},
		},
		&fakeFixGitClient{},
		output,
	)

	err := uc.Execute(context.Background(), "/fake/dir")
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if hashStore.written != "xyz789" {
		t.Errorf("Expected hash to be updated to 'xyz789', got: %s", hashStore.written)
	}
	if !pendingStore.removed {
		t.Error("Expected pending fix state to be removed")
	}
}

func TestFixUseCase_SuccessNoChange(t *testing.T) {
	output := &fakeFixOutput{}
	pendingStore := &fakeFixPendingFixStore{exists: true}
	hashStore := &fakeFixDocsHashStore{hash: "abc123"}
	uc := NewFixUseCase(
		&fakeFixConfigLoader{config: defaultFixConfig()},
		hashStore,
		pendingStore,
		&fakeFixConflictDetector{},
		&fakeFixSnapshotBuilder{snapshot: []byte("snapshot data")},
		&fakeFixHTTPClient{
			docsHead: "abc123",
			pushResponse: DocsPushResponse{
				Ok:              true,
				Status:          docs.DocsPushStatusNoChange,
				CurrentDocsHash: "abc123",
			},
		},
		&fakeFixGitClient{},
		output,
	)

	err := uc.Execute(context.Background(), "/fake/dir")
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if !pendingStore.removed {
		t.Error("Expected pending fix state to be removed")
	}
}

func TestFixUseCase_UnknownDocsCommit(t *testing.T) {
	output := &fakeFixOutput{}
	uc := NewFixUseCase(
		&fakeFixConfigLoader{config: defaultFixConfig()},
		&fakeFixDocsHashStore{hash: "abc123"},
		&fakeFixPendingFixStore{exists: true},
		&fakeFixConflictDetector{},
		&fakeFixSnapshotBuilder{snapshot: []byte("snapshot")},
		&fakeFixHTTPClient{
			docsHead: "abc123",
			pushResponse: DocsPushResponse{
				Ok:    false,
				Error: "unknown_docs_commit",
			},
		},
		&fakeFixGitClient{},
		output,
	)

	err := uc.Execute(context.Background(), "/fake/dir")
	if !errors.Is(err, ErrUnknownDocsCommit) {
		t.Errorf("Expected ErrUnknownDocsCommit, got: %v", err)
	}
}

func TestFixUseCase_DocsRepoBusy(t *testing.T) {
	output := &fakeFixOutput{}
	uc := NewFixUseCase(
		&fakeFixConfigLoader{config: defaultFixConfig()},
		&fakeFixDocsHashStore{hash: "abc123"},
		&fakeFixPendingFixStore{exists: true},
		&fakeFixConflictDetector{},
		&fakeFixSnapshotBuilder{snapshot: []byte("snapshot")},
		&fakeFixHTTPClient{
			docsHead: "abc123",
			pushResponse: DocsPushResponse{
				Ok:    false,
				Error: "docs_repo_busy",
			},
		},
		&fakeFixGitClient{},
		output,
	)

	err := uc.Execute(context.Background(), "/fake/dir")
	if !errors.Is(err, ErrDocsRepoBusy) {
		t.Errorf("Expected ErrDocsRepoBusy, got: %v", err)
	}
}

func TestFixUseCase_ActorEmailFallbackToGit(t *testing.T) {
	output := &fakeFixOutput{}
	httpClient := &fakeFixHTTPClient{
		docsHead: "abc123",
		pushResponse: DocsPushResponse{
			Ok:          true,
			Status:      docs.DocsPushStatusUpdated,
			NewDocsHash: "new-hash",
		},
	}

	uc := NewFixUseCase(
		&fakeFixConfigLoader{config: &client.WorkspaceConfig{
			ServerURL:   "http://localhost",
			WorkspaceID: workspace.WorkspaceID("test-workspace"),
			Project:     "test-project",
			ActorEmail:  "",
		}},
		&fakeFixDocsHashStore{hash: "abc123"},
		&fakeFixPendingFixStore{exists: true},
		&fakeFixConflictDetector{},
		&fakeFixSnapshotBuilder{snapshot: []byte("snapshot")},
		httpClient,
		&fakeFixGitClient{email: "git@example.com"},
		output,
	)

	err := uc.Execute(context.Background(), "/fake/dir")
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if httpClient.lastReq.ActorEmail != "git@example.com" {
		t.Errorf("Expected actor email to fallback to git email, got: %s", httpClient.lastReq.ActorEmail)
	}
	if len(output.infos) == 0 {
		t.Error("Expected info message about using git user.email")
	}
}

func TestFixUseCase_ActorEmailMissing(t *testing.T) {
	output := &fakeFixOutput{}
	uc := NewFixUseCase(
		&fakeFixConfigLoader{config: &client.WorkspaceConfig{
			ServerURL:   "http://localhost",
			WorkspaceID: workspace.WorkspaceID("test-workspace"),
			Project:     "test-project",
			ActorEmail:  "",
		}},
		&fakeFixDocsHashStore{hash: "abc123"},
		&fakeFixPendingFixStore{exists: true},
		&fakeFixConflictDetector{},
		&fakeFixSnapshotBuilder{snapshot: []byte("snapshot")},
		&fakeFixHTTPClient{docsHead: "abc123"},
		&fakeFixGitClient{email: ""},
		output,
	)

	err := uc.Execute(context.Background(), "/fake/dir")
	if !errors.Is(err, ErrActorEmailRequired) {
		t.Errorf("Expected ErrActorEmailRequired, got: %v", err)
	}
	if len(output.errors) == 0 {
		t.Error("Expected error messages about missing actor email")
	}
}

func TestFixUseCase_ActorEmailRequired_EmptyConfigAndGit(t *testing.T) {
	output := &fakeFixOutput{}
	// Config with empty actor email, git also returns empty
	emptyEmailConfig := &client.WorkspaceConfig{
		ServerURL:   "http://localhost",
		WorkspaceID: workspace.WorkspaceID("test-workspace"),
		Project:     "test-project",
		ActorEmail:  "", // Empty!
	}
	uc := NewFixUseCase(
		&fakeFixConfigLoader{config: emptyEmailConfig},
		&fakeFixDocsHashStore{hash: "abc123"},
		&fakeFixPendingFixStore{exists: true},
		&fakeFixConflictDetector{},
		&fakeFixSnapshotBuilder{snapshot: []byte("snapshot data")},
		&fakeFixHTTPClient{docsHead: "abc123"},
		&fakeFixGitClient{email: ""}, // Git also returns empty
		output,
	)

	err := uc.Execute(context.Background(), "/fake/dir")
	if !errors.Is(err, ErrActorEmailRequired) {
		t.Errorf("Expected ErrActorEmailRequired, got: %v", err)
	}
	if len(output.errors) == 0 {
		t.Error("Expected error message about actor email being required")
	}
}

func TestFixUseCase_ActorEmailFallbackToGit(t *testing.T) {
	output := &fakeFixOutput{}
	pendingStore := &fakeFixPendingFixStore{exists: true}
	hashStore := &fakeFixDocsHashStore{hash: "abc123"}
	// Config with empty actor email, but git returns valid email
	emptyEmailConfig := &client.WorkspaceConfig{
		ServerURL:   "http://localhost",
		WorkspaceID: workspace.WorkspaceID("test-workspace"),
		Project:     "test-project",
		ActorEmail:  "", // Empty - will fallback to git
	}
	uc := NewFixUseCase(
		&fakeFixConfigLoader{config: emptyEmailConfig},
		hashStore,
		pendingStore,
		&fakeFixConflictDetector{},
		&fakeFixSnapshotBuilder{snapshot: []byte("snapshot data")},
		&fakeFixHTTPClient{
			docsHead: "abc123",
			pushResponse: DocsPushResponse{
				Ok:          true,
				Status:      docs.DocsPushStatusUpdated,
				NewDocsHash: "xyz789",
			},
		},
		&fakeFixGitClient{email: "git-fallback@example.com"}, // Git returns email
		output,
	)

	err := uc.Execute(context.Background(), "/fake/dir")
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	// Should have info message about using git email
	foundGitEmailMsg := false
	for _, msg := range output.infos {
		if contains(msg, "git user.email") && contains(msg, "git-fallback@example.com") {
			foundGitEmailMsg = true
			break
		}
	}
	if !foundGitEmailMsg {
		t.Error("Expected info message about using git email fallback")
	}
}

func TestFixUseCase_ActorEmailGitReadError(t *testing.T) {
	output := &fakeFixOutput{}
	// Config with empty actor email, git returns error
	emptyEmailConfig := &client.WorkspaceConfig{
		ServerURL:   "http://localhost",
		WorkspaceID: workspace.WorkspaceID("test-workspace"),
		Project:     "test-project",
		ActorEmail:  "", // Empty!
	}
	uc := NewFixUseCase(
		&fakeFixConfigLoader{config: emptyEmailConfig},
		&fakeFixDocsHashStore{hash: "abc123"},
		&fakeFixPendingFixStore{exists: true},
		&fakeFixConflictDetector{},
		&fakeFixSnapshotBuilder{snapshot: []byte("snapshot data")},
		&fakeFixHTTPClient{docsHead: "abc123"},
		&fakeFixGitClient{email: "", err: errors.New("git error")}, // Git returns error
		output,
	)

	err := uc.Execute(context.Background(), "/fake/dir")
	// Should still fail because email is required
	if !errors.Is(err, ErrActorEmailRequired) {
		t.Errorf("Expected ErrActorEmailRequired, got: %v", err)
	}
	// Should have warning about git error
	if len(output.warnings) == 0 {
		t.Error("Expected warning about git email read failure")
	}
}

// Helper function for string containment check
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || (len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
