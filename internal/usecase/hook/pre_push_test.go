package hook

import (
	"context"
	"errors"
	"testing"

	"github.com/irootkernel/sanho/internal/domain/client"
	"github.com/irootkernel/sanho/internal/domain/workspace"
)

// --- Fake implementations for testing ---

type fakePrePushConfigLoader struct {
	config *client.WorkspaceConfig
	err    error
}

func (f *fakePrePushConfigLoader) Load(workDir string) (*client.WorkspaceConfig, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.config, nil
}

type fakePrePushPendingFixStore struct {
	exists bool
	err    error
}

func (f *fakePrePushPendingFixStore) Exists(path string) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	return f.exists, nil
}

type fakePrePushConflictDetector struct {
	conflicts []string
	err       error
}

func (f *fakePrePushConflictDetector) DetectConflicts(docsDir string) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.conflicts, nil
}

type fakePrePushOutput struct {
	infos    []string
	warnings []string
	errors   []string
}

func (f *fakePrePushOutput) Info(msg string)    { f.infos = append(f.infos, msg) }
func (f *fakePrePushOutput) Warning(msg string) { f.warnings = append(f.warnings, msg) }
func (f *fakePrePushOutput) Error(msg string)   { f.errors = append(f.errors, msg) }

// --- Helper function ---

func defaultPrePushConfig() *client.WorkspaceConfig {
	return &client.WorkspaceConfig{
		ServerURL:   "http://localhost",
		WorkspaceID: workspace.WorkspaceID("test-workspace"),
		Project:     "test-project",
		ActorEmail:  "test@example.com",
	}
}

// --- Tests ---

func TestPrePushUseCase_ConfigBroken(t *testing.T) {
	output := &fakePrePushOutput{}
	uc := NewPrePushUseCase(
		&fakePrePushConfigLoader{err: errors.New("config not found")},
		&fakePrePushPendingFixStore{},
		&fakePrePushConflictDetector{},
		output,
	)

	err := uc.Execute(context.Background(), "/fake/dir")
	if !errors.Is(err, ErrConfigBroken) {
		t.Errorf("Expected ErrConfigBroken, got: %v", err)
	}
	if len(output.errors) == 0 {
		t.Error("Expected error messages")
	}
}

func TestPrePushUseCase_ConflictMarkersFound(t *testing.T) {
	output := &fakePrePushOutput{}
	uc := NewPrePushUseCase(
		&fakePrePushConfigLoader{config: defaultPrePushConfig()},
		&fakePrePushPendingFixStore{exists: false},
		&fakePrePushConflictDetector{conflicts: []string{"readme.md", "guide.md"}},
		output,
	)

	err := uc.Execute(context.Background(), "/fake/dir")
	if !errors.Is(err, ErrPrePushConflictMarkerFound) {
		t.Errorf("Expected ErrPrePushConflictMarkerFound, got: %v", err)
	}
	if len(output.errors) < 3 {
		t.Error("Expected error messages listing conflicting files")
	}
}

func TestPrePushUseCase_PendingFixExists(t *testing.T) {
	output := &fakePrePushOutput{}
	uc := NewPrePushUseCase(
		&fakePrePushConfigLoader{config: defaultPrePushConfig()},
		&fakePrePushPendingFixStore{exists: true},
		&fakePrePushConflictDetector{},
		output,
	)

	err := uc.Execute(context.Background(), "/fake/dir")
	if !errors.Is(err, ErrPrePushPendingFixExists) {
		t.Errorf("Expected ErrPrePushPendingFixExists, got: %v", err)
	}
	if len(output.errors) == 0 {
		t.Error("Expected error messages about pending fix")
	}
}

func TestPrePushUseCase_Success(t *testing.T) {
	output := &fakePrePushOutput{}
	uc := NewPrePushUseCase(
		&fakePrePushConfigLoader{config: defaultPrePushConfig()},
		&fakePrePushPendingFixStore{exists: false},
		&fakePrePushConflictDetector{},
		output,
	)

	err := uc.Execute(context.Background(), "/fake/dir")
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if len(output.infos) == 0 {
		t.Error("Expected info message about passing check")
	}
}

func TestPrePushUseCase_ConflictDetectorError(t *testing.T) {
	output := &fakePrePushOutput{}
	uc := NewPrePushUseCase(
		&fakePrePushConfigLoader{config: defaultPrePushConfig()},
		&fakePrePushPendingFixStore{},
		&fakePrePushConflictDetector{err: errors.New("detector error")},
		output,
	)

	err := uc.Execute(context.Background(), "/fake/dir")
	if err == nil {
		t.Error("Expected error, got: nil")
	}
	if errors.Is(err, ErrPrePushConflictMarkerFound) {
		t.Error("Expected generic error, not ErrPrePushConflictMarkerFound")
	}
}

func TestPrePushUseCase_PendingFixStoreError(t *testing.T) {
	output := &fakePrePushOutput{}
	uc := NewPrePushUseCase(
		&fakePrePushConfigLoader{config: defaultPrePushConfig()},
		&fakePrePushPendingFixStore{err: errors.New("store error")},
		&fakePrePushConflictDetector{},
		output,
	)

	err := uc.Execute(context.Background(), "/fake/dir")
	if err == nil {
		t.Error("Expected error, got: nil")
	}
	if errors.Is(err, ErrPrePushPendingFixExists) {
		t.Error("Expected generic error, not ErrPrePushPendingFixExists")
	}
}
