package architecture_test

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const modulePath = "github.com/irootkernel/sanho"

type listedPackage struct {
	ImportPath   string
	Imports      []string
	TestImports  []string
	XTestImports []string
}

func TestRepositoryArchitecture(t *testing.T) {
	packages := listInternalPackages(t)
	for _, pkg := range packages {
		imports := append([]string{}, pkg.Imports...)
		imports = append(imports, pkg.TestImports...)
		imports = append(imports, pkg.XTestImports...)
		for _, imported := range imports {
			if err := validateImport(pkg.ImportPath, imported); err != nil {
				t.Error(err)
			}
		}
	}
}

func TestLayerRulesRejectOutwardDependencies(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		imported string
	}{
		{
			name:     "domain to infra",
			source:   modulePath + "/internal/domain/docs",
			imported: modulePath + "/internal/infra/git",
		},
		{
			name:     "usecase to config",
			source:   modulePath + "/internal/usecase/project",
			imported: modulePath + "/internal/config",
		},
		{
			name:     "usecase to infra",
			source:   modulePath + "/internal/usecase/hook",
			imported: modulePath + "/internal/infra/fs",
		},
		{
			name:     "infra to interface",
			source:   modulePath + "/internal/infra/httpclient",
			imported: modulePath + "/internal/interface/http",
		},
		{
			name:     "CLI to HTTP adapter",
			source:   modulePath + "/internal/interface/cli",
			imported: modulePath + "/internal/interface/http/dto",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateImport(test.source, test.imported); err == nil {
				t.Fatalf("validateImport(%q, %q) returned nil", test.source, test.imported)
			}
		})
	}
}

// baseWriteGuardFile is the one file allowed to call wsstate.SaveBase.
// It is `interface/cli`'s guarded writer; see its own doc comment for
// why the invariant needs an enforcement point rather than nine careful
// callers.
const baseWriteGuardFile = "internal/interface/cli/basewrite.go"

// guardedStateWriters are the wsstate functions that put a docs base on
// disk. Every one of them must be reached through the guard.
var guardedStateWriters = []string{"wsstate.SaveBase("}

// TestBaseWritesGoThroughTheGuard is the meta-test the fourth review
// wave asked for.
//
// The failure class it protects has now been found four times, once per
// wave, and each time in a base write that looked locally correct. Three
// waves fixed three callers; what none of them could fix is that a new
// caller inherits none of the fixes. So "a recorded base may never be
// ahead of the docs the worktree carries" has a single enforcement point
// (interface/cli's writeBase), and this test fails the build the moment
// anything reaches around it.
//
// It reads source text rather than the type graph on purpose: the thing
// being forbidden is a *call to a specific function*, and a package-level
// import rule cannot express it — interface/cli legitimately imports
// wsstate for the config, the note, and LoadBase.
//
// The scope is production code, the same boundary the depguard rule for
// os/exec draws. A test that writes a base file is CONSTRUCTING a
// workspace state to drive something else against; it is not a write
// path that a user's documents depend on, and forcing fixtures through
// the guard would mean fixtures could only build states the guard
// already believes in — which is the opposite of what a regression test
// for this failure class needs.
func TestBaseWritesGoThroughTheGuard(t *testing.T) {
	root := moduleRoot(t)

	for _, file := range goSourceFiles(t, root) {
		relative := filepath.ToSlash(file)
		if relative == baseWriteGuardFile ||
			strings.HasSuffix(relative, "_test.go") ||
			strings.HasPrefix(relative, "internal/infra/wsstate/") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(root, file))
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		for _, writer := range guardedStateWriters {
			if strings.Contains(string(content), writer) {
				t.Errorf("%s calls %s directly.\n"+
					"Every docs-base write must go through the guard in %s, which proves the candidate "+
					"is not ahead of the docs the worktree carries before anything reaches disk. "+
					"Use the statePort's SaveBase/SaveSyncTargetBase instead.",
					file, writer, baseWriteGuardFile)
			}
		}
	}
}

// stateMachinePackages are the packages that decide *what happens next*
// — the case analysis, the sync/publication flows, and the provenance
// rules. sanho-v0.2.md §9 rule 7 requires them to live outside
// `interface/`, and this is that rule implemented.
//
// The reason is the one the guidance-closure suite rests on: a decision
// made inside the CLI can only be tested by driving the CLI, so it
// cannot be table-tested against the state space it actually covers.
// Every time a decision drifted into interface/ it stopped having unit
// tests — and the three reproductions this wave answers were all
// decisions, not renderings.
func TestStateMachinesLiveOutsideInterface(t *testing.T) {
	root := moduleRoot(t)

	// The vocabulary a state machine is written in. Finding one of these
	// declared under interface/ means a decision moved there.
	forbidden := []struct {
		symbol string
		why    string
	}{
		{"func Decide(", "publication case analysis belongs in domain/publish"},
		{"type Case ", "a case enumeration is a domain decision"},
		{"type Status ", "a flow's outcome states belong to the use case that produces them"},
		{"type Resolution ", "the sync-resolution classification belongs to usecase/docsync"},
		{"func ShouldStamp(", "the provenance stamping rule belongs in domain/provenance"},
		{"func SelectBase(", "base selection belongs in domain/provenance"},
	}

	for _, file := range goSourceFiles(t, root) {
		relative := filepath.ToSlash(file)
		if !strings.HasPrefix(relative, "internal/interface/") || strings.HasSuffix(relative, "_test.go") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(root, file))
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		for _, rule := range forbidden {
			if strings.Contains(string(content), rule.symbol) {
				t.Errorf("%s declares %q inside interface/: %s (sanho-v0.2.md §9 rule 7)",
					file, strings.TrimSpace(rule.symbol), rule.why)
			}
		}
	}
}

// goSourceFiles lists every Go file in the module, module-relative and
// slash-separated.
func goSourceFiles(t *testing.T, root string) []string {
	t.Helper()

	var files []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "bin", "data", "tmp":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".go") {
			return nil
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		files = append(files, filepath.ToSlash(relative))
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return files
}

func moduleRoot(t *testing.T) string {
	t.Helper()

	goModPath, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		t.Fatalf("resolve go.mod: %v", err)
	}
	return filepath.Dir(strings.TrimSpace(string(goModPath)))
}

func listInternalPackages(t *testing.T) []listedPackage {
	t.Helper()

	root := moduleRoot(t)

	command := exec.Command("go", "list", "-json", "./internal/...")
	command.Dir = root
	output, err := command.Output()
	if err != nil {
		t.Fatalf("list internal packages: %v", err)
	}

	decoder := json.NewDecoder(strings.NewReader(string(output)))
	var packages []listedPackage
	for decoder.More() {
		var pkg listedPackage
		if err := decoder.Decode(&pkg); err != nil {
			t.Fatalf("decode package graph: %v", err)
		}
		packages = append(packages, pkg)
	}
	return packages
}

func validateImport(source, imported string) error {
	if !strings.HasPrefix(source, modulePath+"/internal/") {
		return nil
	}
	if strings.HasPrefix(imported, modulePath+"/cmd/") {
		return forbiddenImport(source, imported)
	}

	switch {
	case inLayer(source, "config"):
		if isStandardLibrary(imported) {
			return nil
		}
		return forbiddenImport(source, imported)
	case inLayer(source, "domain"):
		if isStandardLibrary(imported) || inLayer(imported, "domain") {
			return nil
		}
		return forbiddenImport(source, imported)
	case inLayer(source, "usecase"):
		if isStandardLibrary(imported) || inLayer(imported, "domain") || inLayer(imported, "usecase") {
			return nil
		}
		return forbiddenImport(source, imported)
	case inLayer(source, "infra"):
		if !strings.HasPrefix(imported, modulePath+"/internal/") ||
			inLayer(imported, "config") ||
			inLayer(imported, "domain") ||
			inLayer(imported, "infra") {
			return nil
		}
		return forbiddenImport(source, imported)
	case strings.HasPrefix(source, modulePath+"/internal/interface/cli"):
		if strings.HasPrefix(imported, modulePath+"/internal/interface/http") {
			return forbiddenImport(source, imported)
		}
	case strings.HasPrefix(source, modulePath+"/internal/interface/http"):
		if strings.HasPrefix(imported, modulePath+"/internal/interface/cli") {
			return forbiddenImport(source, imported)
		}
	}
	return nil
}

func inLayer(importPath, layer string) bool {
	prefix := modulePath + "/internal/" + layer
	return importPath == prefix || strings.HasPrefix(importPath, prefix+"/")
}

func isStandardLibrary(importPath string) bool {
	if strings.HasPrefix(importPath, modulePath+"/") {
		return false
	}
	first, _, _ := strings.Cut(importPath, "/")
	return !strings.Contains(first, ".")
}

func forbiddenImport(source, imported string) error {
	return fmt.Errorf("forbidden architecture dependency: %s imports %s", source, imported)
}
