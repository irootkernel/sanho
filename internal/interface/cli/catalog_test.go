package cli

// The build-failing half of the guidance contract (sanho-v0.2.md §5.9,
// §9 rule 2).
//
// D3 is normative: every advised command must succeed in the state where
// it is advised. The e2e closure suite (test/cli/e2e) proves the
// *succeed* half against the real binary. This file proves the
// *enumerable* half — that no guidance can be added to messages.go
// without also being added to the catalog, and therefore to the e2e
// suite's fixture table.
//
// It works by reading messages.go as source rather than by inspecting
// values: a message is only guidance if a human reads it, and the only
// place its text exists before rendering is the literal in the file.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// messagesFile is the file the scan reads. The test runs with the
// package directory as its working directory.
const messagesFile = "messages.go"

// advisedSanhoCommand and advisedGitCommand recognize a next command
// named inside a message literal.
//
// Both require the verb, not just the program name: every message begins
// with the `sanho: ` prefix, and "sanho: docs are up to date" advises
// nothing. Requiring `sanho ` + a real subcommand is what separates the
// prefix from the guidance.
var (
	advisedSanhoCommand = regexp.MustCompile(`\bsanho (?:init|status|state|sync|pull|clean|doctor|project|hook|migrate|version)\b`)
	advisedGitCommand   = regexp.MustCompile(`\bgit (?:add|commit|push|pull|fetch|checkout|log|-C)\b`)
)

// catalogInfrastructure are the declarations that make up the catalog
// itself. Their literals quote commands because they *describe* them, so
// scanning them would ask the catalog to contain an entry for itself.
var catalogInfrastructure = map[string]bool{
	"Catalog":                true,
	"CatalogEntry":           true,
	"ClosureScenarios":       true,
	"sampleBaseOID":          true,
	"sampleHeadOID":          true,
	"samplePlaceholderClone": true,
}

// TestEveryAdvisingMessageIsInTheCatalog is the gate §9 rule 2 asks for:
// adding a message that names a next command, without a catalog entry,
// fails the build.
func TestEveryAdvisingMessageIsInTheCatalog(t *testing.T) {
	cataloged := map[string]bool{}
	for _, entry := range Catalog {
		cataloged[entry.Source] = true
	}

	declared, advising := scanMessagesFile(t)

	for _, source := range advising {
		if !cataloged[source] {
			t.Errorf("%s names a next command but has no Catalog entry; "+
				"add one (with a closure scenario) or the guidance cannot be proven closed",
				source)
		}
	}

	// And the other direction: a catalog entry must describe something
	// that still exists, so renaming a renderer cannot leave the catalog
	// quietly pointing at nothing.
	for _, entry := range Catalog {
		if !declared[entry.Source] {
			t.Errorf("catalog entry %q names source %q, which %s does not declare",
				entry.ID, entry.Source, messagesFile)
		}
	}
}

// TestCatalogEntriesAreMarkedWithAClosureScenario is the §9 rule 2
// marking requirement: an entry whose sample quotes a command must name
// the scenario that proves it.
func TestCatalogEntriesAreMarkedWithAClosureScenario(t *testing.T) {
	for _, entry := range Catalog {
		names := advisedSanhoCommand.MatchString(entry.Sample) ||
			advisedGitCommand.MatchString(entry.Sample)

		switch {
		case names && entry.Scenario == "":
			t.Errorf("catalog entry %q quotes a command but names no closure scenario", entry.ID)
		case entry.Scenario == "":
			t.Errorf("catalog entry %q names no closure scenario", entry.ID)
		}
		if len(entry.NextCommands) == 0 {
			t.Errorf("catalog entry %q advises no command; it does not belong in the catalog", entry.ID)
		}
	}
}

// TestCatalogEntriesAreConsistent pins the invariants the e2e harness
// relies on: unique identities, and a Match that really is a substring
// of the rendering it claims to describe.
func TestCatalogEntriesAreConsistent(t *testing.T) {
	ids := map[string]bool{}
	scenarios := map[string]bool{}

	for _, entry := range Catalog {
		if entry.ID == "" {
			t.Error("a catalog entry has no ID")
			continue
		}
		if ids[entry.ID] {
			t.Errorf("catalog ID %q is used twice", entry.ID)
		}
		ids[entry.ID] = true

		if scenarios[entry.Scenario] {
			t.Errorf("closure scenario %q is claimed by two entries; scenarios are one-to-one with entries",
				entry.Scenario)
		}
		scenarios[entry.Scenario] = true

		if entry.Match == "" {
			t.Errorf("catalog entry %q has no Match substring", entry.ID)
			continue
		}
		if !strings.Contains(entry.Sample, entry.Match) {
			t.Errorf("catalog entry %q: Match %q is not in its own rendering:\n%s",
				entry.ID, entry.Match, entry.Sample)
		}
	}
}

// TestClosureScenariosIsTheCatalogManifest pins the manifest the e2e
// suite compares its fixture table against.
func TestClosureScenariosIsTheCatalogManifest(t *testing.T) {
	manifest := ClosureScenarios()
	if len(manifest) != len(Catalog) {
		t.Fatalf("ClosureScenarios() has %d entries, want %d (one per catalog entry)",
			len(manifest), len(Catalog))
	}
	if !sort.StringsAreSorted(manifest) {
		t.Errorf("ClosureScenarios() = %v, want it sorted", manifest)
	}

	want := make([]string, 0, len(Catalog))
	for _, entry := range Catalog {
		want = append(want, entry.Scenario)
	}
	sort.Strings(want)
	for i := range want {
		if manifest[i] != want[i] {
			t.Fatalf("ClosureScenarios() = %v, want %v", manifest, want)
		}
	}
}

// scanMessagesFile parses messages.go and reports every top-level
// declaration name, plus the subset whose string literals name a next
// command.
func scanMessagesFile(t *testing.T) (declared map[string]bool, advising []string) {
	t.Helper()

	fileSet := token.NewFileSet()
	parsed, err := parser.ParseFile(fileSet, messagesFile, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", messagesFile, err)
	}

	declared = map[string]bool{}
	for _, decl := range parsed.Decls {
		for name, node := range declarationBodies(decl) {
			if catalogInfrastructure[name] {
				continue
			}
			declared[name] = true
			if namesACommand(t, node) {
				advising = append(advising, name)
			}
		}
	}
	sort.Strings(advising)
	return declared, advising
}

// declarationBodies maps each top-level name a declaration introduces to
// the node whose literals belong to it.
func declarationBodies(decl ast.Decl) map[string]ast.Node {
	bodies := map[string]ast.Node{}

	switch typed := decl.(type) {
	case *ast.FuncDecl:
		if typed.Body != nil {
			bodies[typed.Name.Name] = typed.Body
		}
	case *ast.GenDecl:
		for _, spec := range typed.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range value.Names {
				// A const block with one value per name maps name to
				// value; anything else (a shared initializer) is
				// attributed to every name it produces, which is the
				// conservative reading.
				if i < len(value.Values) {
					bodies[name.Name] = value.Values[i]
					continue
				}
				if len(value.Values) == 1 {
					bodies[name.Name] = value.Values[0]
				}
			}
		}
	}
	return bodies
}

// namesACommand reports whether any string literal under node quotes a
// sanho or git command.
func namesACommand(t *testing.T, node ast.Node) bool {
	t.Helper()

	found := false
	ast.Inspect(node, func(n ast.Node) bool {
		literal, ok := n.(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return true
		}
		text, err := strconv.Unquote(literal.Value)
		if err != nil {
			t.Fatalf("unquote literal %s: %v", literal.Value, err)
		}
		if advisedSanhoCommand.MatchString(text) || advisedGitCommand.MatchString(text) {
			found = true
			return false
		}
		return true
	})
	return found
}
