package state_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/SeventeenthEarth/kkachi/internal/config"
	"github.com/SeventeenthEarth/kkachi/internal/infra/state"
)

func TestFileStateRepositoryRecoversCorruptPrimaryFromBackup(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	repository, err := state.NewFileStateRepository(statePath)
	if err != nil {
		t.Fatal(err)
	}
	repoConfig := config.DocsRepoConfig{ID: "docs", Path: "/tmp/docs", RepoURL: "example"}
	if err := repository.AddDocsRepo(repoConfig); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, []byte("{broken"), 0644); err != nil {
		t.Fatal(err)
	}

	recovered, err := state.NewFileStateRepository(statePath)
	if err != nil {
		t.Fatalf("NewFileStateRepository() error = %v", err)
	}
	if got, ok := recovered.GetDocsRepo("docs"); !ok || got != repoConfig {
		t.Fatalf("recovered repo = %#v, %v; want %#v, true", got, ok, repoConfig)
	}
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(data) {
		t.Fatal("primary state file was not restored with valid JSON")
	}
}

func TestFileStateRepositoryRejectsCorruptPrimaryAndBackup(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(statePath, []byte("{broken"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath+".bak", []byte("{also-broken"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := state.NewFileStateRepository(statePath)
	if !errors.Is(err, state.ErrStateCorrupt) {
		t.Fatalf("NewFileStateRepository() error = %v, want ErrStateCorrupt", err)
	}
}

func TestFileStateRepositoryRollsBackMemoryWhenSaveFails(t *testing.T) {
	root := t.TempDir()
	blocker := filepath.Join(root, "not-a-directory")
	statePath := filepath.Join(blocker, "state.json")
	repository, err := state.NewFileStateRepository(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(blocker, []byte("block"), 0644); err != nil {
		t.Fatal(err)
	}

	err = repository.AddDocsRepo(config.DocsRepoConfig{ID: "docs"})
	if err == nil {
		t.Fatal("AddDocsRepo() error = nil, want persistence failure")
	}
	if _, ok := repository.GetDocsRepo("docs"); ok {
		t.Fatal("failed mutation remained visible in memory")
	}
}

func TestFileStateRepositoryLeavesNoTemporaryFiles(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")
	repository, err := state.NewFileStateRepository(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.AddProject("project", "docs"); err != nil {
		t.Fatal(err)
	}

	matches, err := filepath.Glob(filepath.Join(root, ".*.tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary state files remain: %v", matches)
	}
}
