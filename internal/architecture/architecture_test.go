package architecture_test

import (
	"encoding/json"
	"fmt"
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

func listInternalPackages(t *testing.T) []listedPackage {
	t.Helper()

	goModPath, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		t.Fatalf("resolve go.mod: %v", err)
	}
	root := filepath.Dir(strings.TrimSpace(string(goModPath)))

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
