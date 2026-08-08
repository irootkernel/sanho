package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/irootkernel/sanho/internal/infra/registry"
)

func TestCanonicalFilesystemPathResolvesSymlinkedWorkspaceRoot(t *testing.T) {
	realRoot := filepath.Join(t.TempDir(), "workspace")
	if err := os.Mkdir(realRoot, 0755); err != nil {
		t.Fatal(err)
	}
	aliasRoot := filepath.Join(t.TempDir(), "workspace-alias")
	if err := os.Symlink(realRoot, aliasRoot); err != nil {
		t.Fatal(err)
	}

	got, err := canonicalFilesystemPath(aliasRoot)
	if err != nil {
		t.Fatal(err)
	}
	want, err := canonicalFilesystemPath(realRoot)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("canonical path = %q, want %q", got, want)
	}
	if !sameFilesystemPath(realRoot, aliasRoot) {
		t.Fatal("real path and symlink alias were not treated as the same workspace")
	}
}

func TestPruneWorkspaceAliasesKeepsCanonicalRowAndOtherWorkspace(t *testing.T) {
	realRoot := filepath.Join(t.TempDir(), "workspace")
	if err := os.Mkdir(realRoot, 0755); err != nil {
		t.Fatal(err)
	}
	aliasRoot := filepath.Join(t.TempDir(), "workspace-alias")
	if err := os.Symlink(realRoot, aliasRoot); err != nil {
		t.Fatal(err)
	}
	otherRoot := t.TempDir()
	keep := registryKey("product", realRoot)
	alias := registryKey("product", aliasRoot)
	other := registryKey("product", otherRoot)
	state := registry.State{Workspaces: map[string]registry.Workspace{
		keep:  {Project: "product", LocalPath: realRoot},
		alias: {Project: "product", LocalPath: aliasRoot},
		other: {Project: "product", LocalPath: otherRoot},
	}}

	pruneWorkspaceAliases(&state, "product", realRoot, keep)

	if _, ok := state.Workspaces[alias]; ok {
		t.Fatal("symlink alias survived registry pruning")
	}
	if _, ok := state.Workspaces[keep]; !ok {
		t.Fatal("canonical workspace row was pruned")
	}
	if _, ok := state.Workspaces[other]; !ok {
		t.Fatal("different workspace row was pruned")
	}
}
