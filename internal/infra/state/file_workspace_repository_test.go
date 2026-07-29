package state_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/irootkernel/sanho/internal/domain/docs"
	"github.com/irootkernel/sanho/internal/infra/state"
)

// TestFileWorkspaceRepository_List tests that List returns all workspaces from state file.
func TestFileWorkspaceRepository_List(t *testing.T) {
	// Create temp directory for state file
	tempDir, err := os.MkdirTemp("", "sanho-state-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	statePath := filepath.Join(tempDir, "state.json")

	// Create state repository
	stateRepo, err := state.NewFileStateRepository(statePath)
	if err != nil {
		t.Fatal(err)
	}

	// Create workspace repository
	wsRepo := state.NewFileWorkspaceRepository(stateRepo)

	// Test 1: Empty list
	t.Run("Empty list", func(t *testing.T) {
		workspaces, err := wsRepo.List(context.Background())
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}
		if len(workspaces) != 0 {
			t.Errorf("Expected 0 workspaces, got %d", len(workspaces))
		}
	})

	// Add workspaces
	ws1 := state.WorkspaceState{
		ID:             "proj1:/tmp/ws1",
		Project:        "proj1",
		DocsRepoID:     "repo1",
		LocalPath:      "/tmp/ws1",
		RepoURL:        "git@github.com:org/repo.git",
		DocsHash:       "hash1",
		OwnerEmail:     "dev1@example.com",
		LastActorEmail: "dev1@example.com",
	}
	ws2 := state.WorkspaceState{
		ID:             "proj2:/tmp/ws2",
		Project:        "proj2",
		DocsRepoID:     "repo2",
		LocalPath:      "/tmp/ws2",
		RepoURL:        "git@github.com:org/repo2.git",
		DocsHash:       "hash2",
		OwnerEmail:     "dev2@example.com",
		LastActorEmail: "dev2@example.com",
	}
	ws3 := state.WorkspaceState{
		ID:             "proj1:/tmp/ws3",
		Project:        "proj1",
		DocsRepoID:     "repo1",
		LocalPath:      "/tmp/ws3",
		RepoURL:        "git@github.com:org/repo.git",
		DocsHash:       "hash1",
		OwnerEmail:     "dev3@example.com",
		LastActorEmail: "dev3@example.com",
	}

	if err := stateRepo.AddWorkspace(ws1); err != nil {
		t.Fatalf("AddWorkspace failed: %v", err)
	}
	if err := stateRepo.AddWorkspace(ws2); err != nil {
		t.Fatalf("AddWorkspace failed: %v", err)
	}
	if err := stateRepo.AddWorkspace(ws3); err != nil {
		t.Fatalf("AddWorkspace failed: %v", err)
	}

	// Test 2: List returns all workspaces
	t.Run("List returns all workspaces", func(t *testing.T) {
		workspaces, err := wsRepo.List(context.Background())
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}
		if len(workspaces) != 3 {
			t.Errorf("Expected 3 workspaces, got %d", len(workspaces))
		}

		// Verify data integrity
		wsMap := make(map[string]bool)
		for _, ws := range workspaces {
			wsMap[string(ws.ID)] = true
		}

		if !wsMap["proj1:/tmp/ws1"] {
			t.Error("Expected ws1 to be in list")
		}
		if !wsMap["proj2:/tmp/ws2"] {
			t.Error("Expected ws2 to be in list")
		}
		if !wsMap["proj1:/tmp/ws3"] {
			t.Error("Expected ws3 to be in list")
		}
	})

	// Test 3: Verify workspace data is correctly converted
	t.Run("Workspace data is correctly converted", func(t *testing.T) {
		workspaces, err := wsRepo.List(context.Background())
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}

		for _, ws := range workspaces {
			if ws.ID == "proj1:/tmp/ws1" {
				if ws.Project != docs.ProjectName("proj1") {
					t.Errorf("Expected Project 'proj1', got '%s'", ws.Project)
				}
				if ws.DocsRepoID != docs.DocsRepoID("repo1") {
					t.Errorf("Expected DocsRepoID 'repo1', got '%s'", ws.DocsRepoID)
				}
				if ws.LocalPath != "/tmp/ws1" {
					t.Errorf("Expected LocalPath '/tmp/ws1', got '%s'", ws.LocalPath)
				}
				if ws.DocsHash != docs.CommitHash("hash1") {
					t.Errorf("Expected DocsHash 'hash1', got '%s'", ws.DocsHash)
				}
				if ws.OwnerEmail != "dev1@example.com" {
					t.Errorf("Expected OwnerEmail 'dev1@example.com', got '%s'", ws.OwnerEmail)
				}
			}
		}
	})

	// Test 4: Persistence - reload and verify
	t.Run("Persistence - reload and verify", func(t *testing.T) {
		// Create new repository pointing to same file
		stateRepo2, err := state.NewFileStateRepository(statePath)
		if err != nil {
			t.Fatal(err)
		}
		wsRepo2 := state.NewFileWorkspaceRepository(stateRepo2)

		workspaces, err := wsRepo2.List(context.Background())
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}
		if len(workspaces) != 3 {
			t.Errorf("Expected 3 workspaces after reload, got %d", len(workspaces))
		}
	})
}
