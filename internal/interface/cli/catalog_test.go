package cli

// The build-failing half of the guidance contract documented in
// architecture's "User guidance and exit codes" section.
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
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// messagesFile is the file the catalog itself lives in. The test runs
// with the package directory as its working directory.
const messagesFile = "messages.go"

// sentinelPackages are the two use-case packages whose error sentinels
// reach a user through the CLI (F-H6a).
//
// They are scanned for the same reason the CLI files are: a sentinel
// that spelled `run 'sanho sync'` was guidance, printed verbatim by
// renderError, that no closure fixture could see — which is exactly how
// `sanho pull`'s advice came to name a command that fails where it is
// printed. Guidance belongs in the catalog; sentinels state facts.
var sentinelPackages = []string{
	"../../usecase/docsync",
	"../../usecase/publish",
}

// helpTextFields are the cobra fields that carry documentation rather
// than guidance. A `Long:` description explaining that `sanho sync
// --abort` exists is a manual page, not a message printed in a state
// that must make it runnable, so the scan does not descend into them.
var helpTextFields = map[string]bool{
	"Use": true, "Short": true, "Long": true, "Example": true,
}

// selfIdentifyingOutput are the two renderers whose literals name a
// command because the output IS that command's own — `sanho doctor:
// <path>` heads the doctor report, `sanho version <v>` is the version
// line. Neither advises anything; both would otherwise trip the scan
// for quoting their own name.
//
// The list is short and must stay short: anything added here is
// guidance the closure suite cannot see, so a third entry is a signal
// that the wording belongs in messages.go instead.
var selfIdentifyingOutput = map[string]bool{
	"renderDoctor":  true,
	"newVersionCmd": true,
}

// advisedSanhoCommand and advisedGitCommand recognize a next command
// named inside a message literal.
//
// Both require the verb, not just the program name: every message begins
// with the `sanho: ` prefix, and "sanho: docs are up to date" advises
// nothing. Requiring `sanho ` + a real subcommand is what separates the
// prefix from the guidance.
//
// `workspace` is the one subcommand deliberately absent, because it is
// the only name that is also an ordinary noun in this domain: "a sanho
// workspace must be initialized at the repository root" states a fact
// and advises nothing, and listing the word here would turn every such
// sentence into guidance the catalog has to carry.
var (
	advisedSanhoCommand = regexp.MustCompile(`\bsanho (?:init|status|diff|log|show|check|state|sync|pull|clean|doctor|project|hook|migrate|version)\b`)
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

// TestEveryAdvisingMessageIsInTheCatalog is the gate the testing contract asks for:
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

	// The other half of F-H6a: guidance must not exist anywhere else.
	// A command named in doctor.go, in a sentinel, or in any renderer
	// that is not in messages.go is guidance the closure suite cannot
	// enumerate, and therefore guidance nothing proves runnable.
	for _, file := range guidanceScanFiles(t) {
		for _, name := range advisingDeclarations(t, file) {
			t.Errorf("%s: %s names a next command outside %s; move the wording into a "+
				"messages.go renderer with a Catalog entry and a closure fixture",
				file, name, messagesFile)
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

// TestCatalogEntriesAreMarkedWithAClosureScenario is the testing contract
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
		requireCoherentPrerequisites(t, entry)
	}
}

// requireCoherentPrerequisites keeps the multi-step half of the contract
// enforceable: a sequence the e2e suite runs must be a sequence of
// commands this entry actually advises, or the closure proof is about
// something the user was never told.
func requireCoherentPrerequisites(t *testing.T, entry CatalogEntry) {
	t.Helper()

	advised := map[string]bool{}
	for _, command := range entry.NextCommands {
		advised[command] = true
	}
	for command, steps := range entry.Prerequisites {
		if !advised[command] {
			t.Errorf("catalog entry %q: Prerequisites name %q, which is not one of its NextCommands",
				entry.ID, command)
		}
		if len(steps) == 0 {
			t.Errorf("catalog entry %q: %q has an empty prerequisite list; omit the key instead",
				entry.ID, command)
		}
		for _, step := range steps {
			if step == command {
				t.Errorf("catalog entry %q: %q is listed as its own prerequisite", entry.ID, command)
			}
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
		// Cobra help text documents commands; it is not printed in a
		// state that has to make them runnable.
		if pair, ok := n.(*ast.KeyValueExpr); ok {
			if key, isIdent := pair.Key.(*ast.Ident); isIdent && helpTextFields[key.Name] {
				return false
			}
		}
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

// guidanceScanFiles lists every non-test Go file the widened gate reads:
// this package, plus the two use-case packages whose sentinels reach a
// user (F-H6a). messages.go is excluded — it is scanned separately, by
// name, and is the one place guidance is allowed to live.
func guidanceScanFiles(t *testing.T) []string {
	t.Helper()

	var files []string
	for _, dir := range append([]string{"."}, sentinelPackages...) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.HasSuffix(name, ".go") ||
				strings.HasSuffix(name, "_test.go") || name == messagesFile {
				continue
			}
			files = append(files, filepath.Join(dir, name))
		}
	}
	sort.Strings(files)
	return files
}

// advisingDeclarations parses one file and reports the declarations
// whose string literals name a next command.
func advisingDeclarations(t *testing.T, path string) []string {
	t.Helper()

	fileSet := token.NewFileSet()
	parsed, err := parser.ParseFile(fileSet, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	var advising []string
	for _, decl := range parsed.Decls {
		for name, node := range declarationBodies(decl) {
			if selfIdentifyingOutput[name] {
				continue
			}
			if namesACommand(t, node) {
				advising = append(advising, name)
			}
		}
	}
	sort.Strings(advising)
	return advising
}

// --- English-only hygiene (the guidance contract, audit L4; F-L9) ------------------------

// allowedNonASCII are the two characters the guidance contract's normative templates use
// verbatim: the em dash that separates a state from its advice, and the
// right arrow that renders a base→head transition.
var allowedNonASCII = map[rune]bool{'—': true, '→': true}

// TestUserFacingStringsAreEnglishASCII replaces the hand-maintained
// allow-list the previous version carried (F-L9).
//
// A list of "files we remembered to check" is a list that goes stale the
// first time somebody adds a file. This walks the AST of every non-test
// declaration in the package instead, so a message in a file nobody
// thought about is covered the moment it is written.
func TestUserFacingStringsAreEnglishASCII(t *testing.T) {
	for _, path := range append([]string{messagesFile}, guidanceScanFiles(t)...) {
		if !strings.HasPrefix(path, ".") && !strings.HasSuffix(filepath.Dir(path), "cli") {
			// Only this package's own output is in scope; the use-case
			// packages are scanned for guidance, not for typography.
			continue
		}
		fileSet := token.NewFileSet()
		parsed, err := parser.ParseFile(fileSet, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		ast.Inspect(parsed, func(n ast.Node) bool {
			literal, ok := n.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			text, unquoteErr := strconv.Unquote(literal.Value)
			if unquoteErr != nil {
				return true
			}
			for _, r := range text {
				if r < 128 || allowedNonASCII[r] {
					continue
				}
				t.Errorf("%s:%d: string literal contains %q; user-facing text is English ASCII "+
					"apart from the em dash and right arrow of the guidance contract templates",
					path, fileSet.Position(literal.Pos()).Line, r)
				return false
			}
			return true
		})
	}
}
