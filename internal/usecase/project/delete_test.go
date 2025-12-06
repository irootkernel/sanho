package project_test

import (
	"context"
	"testing"

	"github.com/SeventeenthEarth/kkachi/internal/config"
	"github.com/SeventeenthEarth/kkachi/internal/usecase/project"
)

// Mock implementations for unit testing

type mockStateRepository struct {
	docsRepoID           string
	hasDocsRepoID        bool
	hasWorkspaces        bool
	deleteProjectCalled  bool
	deleteDocsRepoCalled bool
	repoUsage            map[string]int
	docsRepoConfig       config.DocsRepoConfig
	hasDocsRepoConfig    bool
}

func (m *mockStateRepository) GetDocsRepoID(projectName string) (string, bool) {
	return m.docsRepoID, m.hasDocsRepoID
}

func (m *mockStateRepository) DeleteProject(projectName string) error {
	m.deleteProjectCalled = true
	return nil
}

func (m *mockStateRepository) GetRepoUsage() map[string]int {
	if m.repoUsage == nil {
		return map[string]int{}
	}
	return m.repoUsage
}

func (m *mockStateRepository) GetDocsRepo(id string) (config.DocsRepoConfig, bool) {
	return m.docsRepoConfig, m.hasDocsRepoConfig
}

func (m *mockStateRepository) DeleteDocsRepo(id string) error {
	m.deleteDocsRepoCalled = true
	return nil
}

func (m *mockStateRepository) HasWorkspacesForProject(projectName string) bool {
	return m.hasWorkspaces
}

type mockGitManager struct {
	deleteRepoCalled bool
}

func (m *mockGitManager) Sync(_ context.Context, _ []config.DocsRepoConfig) error {
	return nil
}

func (m *mockGitManager) DeleteRepo(path string) error {
	m.deleteRepoCalled = true
	return nil
}

func TestDeleteProjectUseCase_UnknownProject(t *testing.T) {
	stateRepo := &mockStateRepository{
		hasDocsRepoID: false,
	}
	gitManager := &mockGitManager{}
	uc := project.NewDeleteProjectUseCase(stateRepo, gitManager)

	err := uc.Execute("unknown-project", false)

	if err != project.ErrUnknownProject {
		t.Errorf("Expected ErrUnknownProject, got %v", err)
	}
	if stateRepo.deleteProjectCalled {
		t.Error("DeleteProject should not be called for unknown project")
	}
}

func TestDeleteProjectUseCase_HasWorkspacesWithoutForce(t *testing.T) {
	stateRepo := &mockStateRepository{
		docsRepoID:    "test-repo",
		hasDocsRepoID: true,
		hasWorkspaces: true,
	}
	gitManager := &mockGitManager{}
	uc := project.NewDeleteProjectUseCase(stateRepo, gitManager)

	err := uc.Execute("test-project", false)

	if err != project.ErrProjectHasWorkspaces {
		t.Errorf("Expected ErrProjectHasWorkspaces, got %v", err)
	}
	if stateRepo.deleteProjectCalled {
		t.Error("DeleteProject should not be called when workspaces exist without force")
	}
}

func TestDeleteProjectUseCase_HasWorkspacesWithForce(t *testing.T) {
	stateRepo := &mockStateRepository{
		docsRepoID:        "test-repo",
		hasDocsRepoID:     true,
		hasWorkspaces:     true,
		repoUsage:         map[string]int{"test-repo": 1}, // Still used by another project
		docsRepoConfig:    config.DocsRepoConfig{ID: "test-repo", Path: "/tmp/repo"},
		hasDocsRepoConfig: true,
	}
	gitManager := &mockGitManager{}
	uc := project.NewDeleteProjectUseCase(stateRepo, gitManager)

	err := uc.Execute("test-project", true)

	if err != nil {
		t.Errorf("Expected no error with force=true, got %v", err)
	}
	if !stateRepo.deleteProjectCalled {
		t.Error("DeleteProject should be called with force=true")
	}
}

func TestDeleteProjectUseCase_NoWorkspaces(t *testing.T) {
	stateRepo := &mockStateRepository{
		docsRepoID:        "test-repo",
		hasDocsRepoID:     true,
		hasWorkspaces:     false,
		repoUsage:         map[string]int{}, // Not used anymore
		docsRepoConfig:    config.DocsRepoConfig{ID: "test-repo", Path: "/tmp/repo"},
		hasDocsRepoConfig: true,
	}
	gitManager := &mockGitManager{}
	uc := project.NewDeleteProjectUseCase(stateRepo, gitManager)

	err := uc.Execute("test-project", false)

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if !stateRepo.deleteProjectCalled {
		t.Error("DeleteProject should be called")
	}
	// Repo should be deleted since usage is 0
	if !gitManager.deleteRepoCalled {
		t.Error("DeleteRepo should be called when usage is 0")
	}
	if !stateRepo.deleteDocsRepoCalled {
		t.Error("DeleteDocsRepo should be called when usage is 0")
	}
}

func TestDeleteProjectUseCase_RepoStillUsed(t *testing.T) {
	stateRepo := &mockStateRepository{
		docsRepoID:        "shared-repo",
		hasDocsRepoID:     true,
		hasWorkspaces:     false,
		repoUsage:         map[string]int{"shared-repo": 2}, // Still used by other projects
		docsRepoConfig:    config.DocsRepoConfig{ID: "shared-repo", Path: "/tmp/repo"},
		hasDocsRepoConfig: true,
	}
	gitManager := &mockGitManager{}
	uc := project.NewDeleteProjectUseCase(stateRepo, gitManager)

	err := uc.Execute("test-project", false)

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if !stateRepo.deleteProjectCalled {
		t.Error("DeleteProject should be called")
	}
	// Repo should NOT be deleted since usage > 0
	if gitManager.deleteRepoCalled {
		t.Error("DeleteRepo should NOT be called when repo is still used")
	}
	if stateRepo.deleteDocsRepoCalled {
		t.Error("DeleteDocsRepo should NOT be called when repo is still used")
	}
}
