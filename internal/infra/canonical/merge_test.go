package canonical

import (
	"context"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// TestMergeTreeMatrix is the C2 regression ground: the merge matrix
// required by the AGENTS.md testing rules — 0/1/2/3-hunk conflicts, symlinks, mode
// changes, binaries, delete-vs-edit, DELETES ON BOTH SIDES, and an empty
// tree on one side — run against real git, with the exit-code and
// conflict-path parsing asserted for each.
func TestMergeTreeMatrix(t *testing.T) {
	factory := newTreeFactory(t)
	ctx := context.Background()

	tests := []struct {
		name          string
		base          map[string]entry
		ours          map[string]entry
		theirs        map[string]entry
		wantClean     bool
		wantConflicts []string
		// wantHunks asserts how many conflict regions the merged blob
		// carries, for the content-conflict cases.
		wantHunks int
		hunkPath  string
		// wantPaths, when set, asserts the exact contents of the result
		// tree — which is the only way a DELETION can be checked, since
		// a resurrected path shows up nowhere in a clean/conflict verdict.
		wantPaths []string
	}{
		{
			name:      "zero conflicts: disjoint files",
			base:      map[string]entry{"a.md": text("a\n"), "b.md": text("b\n")},
			ours:      map[string]entry{"a.md": text("a ours\n"), "b.md": text("b\n")},
			theirs:    map[string]entry{"a.md": text("a\n"), "b.md": text("b theirs\n")},
			wantClean: true,
		},
		{
			name:      "zero conflicts: same edit on both sides",
			base:      map[string]entry{"a.md": text("a\n")},
			ours:      map[string]entry{"a.md": text("a changed\n")},
			theirs:    map[string]entry{"a.md": text("a changed\n")},
			wantClean: true,
		},
		{
			name:          "one conflicting hunk",
			base:          map[string]entry{"a.md": hunkFile("A", "B", "C")},
			ours:          map[string]entry{"a.md": hunkFile("A-ours", "B", "C")},
			theirs:        map[string]entry{"a.md": hunkFile("A-theirs", "B", "C")},
			wantClean:     false,
			wantConflicts: []string{"a.md"},
			wantHunks:     1,
			hunkPath:      "a.md",
		},
		{
			name:          "two conflicting hunks",
			base:          map[string]entry{"a.md": hunkFile("A", "B", "C")},
			ours:          map[string]entry{"a.md": hunkFile("A-ours", "B-ours", "C")},
			theirs:        map[string]entry{"a.md": hunkFile("A-theirs", "B-theirs", "C")},
			wantClean:     false,
			wantConflicts: []string{"a.md"},
			wantHunks:     2,
			hunkPath:      "a.md",
		},
		{
			name:          "three conflicting hunks",
			base:          map[string]entry{"a.md": hunkFile("A", "B", "C")},
			ours:          map[string]entry{"a.md": hunkFile("A-ours", "B-ours", "C-ours")},
			theirs:        map[string]entry{"a.md": hunkFile("A-theirs", "B-theirs", "C-theirs")},
			wantClean:     false,
			wantConflicts: []string{"a.md"},
			wantHunks:     3,
			hunkPath:      "a.md",
		},
		{
			name:          "conflicts in two separate files",
			base:          map[string]entry{"a.md": hunkFile("A", "B", "C"), "b.md": hunkFile("A", "B", "C")},
			ours:          map[string]entry{"a.md": hunkFile("A-ours", "B", "C"), "b.md": hunkFile("A-ours", "B", "C")},
			theirs:        map[string]entry{"a.md": hunkFile("A-theirs", "B", "C"), "b.md": hunkFile("A-theirs", "B", "C")},
			wantClean:     false,
			wantConflicts: []string{"a.md", "b.md"},
		},
		{
			name:      "symlink added on one side survives the merge",
			base:      map[string]entry{"a.md": text("a\n")},
			ours:      map[string]entry{"a.md": text("a\n"), "target.md": text("t\n"), "link.md": link("target.md")},
			theirs:    map[string]entry{"a.md": text("a theirs\n")},
			wantClean: true,
		},
		{
			name:      "mode change on one side, content edit on the other",
			base:      map[string]entry{"run.sh": text("echo\n")},
			ours:      map[string]entry{"run.sh": exe("echo\n")},
			theirs:    map[string]entry{"run.sh": text("echo more\n")},
			wantClean: true,
		},
		{
			name:          "binary changed on both sides",
			base:          map[string]entry{"img.png": binary("base")},
			ours:          map[string]entry{"img.png": binary("ours")},
			theirs:        map[string]entry{"img.png": binary("theirs")},
			wantClean:     false,
			wantConflicts: []string{"img.png"},
		},
		{
			name:          "delete on one side, edit on the other",
			base:          map[string]entry{"a.md": text("a\n"), "keep.md": text("k\n")},
			ours:          map[string]entry{"keep.md": text("k\n")},
			theirs:        map[string]entry{"a.md": text("a theirs\n"), "keep.md": text("k\n")},
			wantClean:     false,
			wantConflicts: []string{"a.md"},
		},
		{
			// Deleting on both sides is agreement: the same removal from both
			// sides is agreement, not a conflict, and the path must be
			// gone from the result rather than resurrected from the base.
			// A merge that resurrects it republishes a document the team
			// deleted, which reads as an upstream regression nobody made.
			name:      "delete on both sides",
			base:      map[string]entry{"a.md": text("a\n"), "keep.md": text("keep\n")},
			ours:      map[string]entry{"keep.md": text("keep\n")},
			theirs:    map[string]entry{"keep.md": text("keep\n")},
			wantClean: true,
			wantPaths: []string{"keep.md"},
		},
		{
			// The same agreement while each side ALSO edits a different
			// file, so the deletion is decided beside real changes rather
			// than in isolation.
			name:      "delete on both sides while each side edits elsewhere",
			base:      map[string]entry{"a.md": text("a\n"), "ours.md": text("o\n"), "theirs.md": text("t\n")},
			ours:      map[string]entry{"ours.md": text("o changed\n"), "theirs.md": text("t\n")},
			theirs:    map[string]entry{"ours.md": text("o\n"), "theirs.md": text("t changed\n")},
			wantClean: true,
			wantPaths: []string{"ours.md", "theirs.md"},
		},
		{
			// Both sides delete the last document. The docs directory
			// becomes empty, which is a legitimate merge result — the
			// refusal to PUBLISH an empty docs tree is a separate gate
			// (the publication contract, F-H2) and must not be pre-empted by the merge
			// reporting a conflict here.
			name:      "delete on both sides emptying the docs tree",
			base:      map[string]entry{"a.md": text("a\n")},
			ours:      map[string]entry{},
			theirs:    map[string]entry{},
			wantClean: true,
			wantPaths: []string{},
		},
		{
			name:          "empty tree on our side against an upstream edit",
			base:          map[string]entry{"a.md": text("a\n")},
			ours:          map[string]entry{},
			theirs:        map[string]entry{"a.md": text("a theirs\n")},
			wantClean:     false,
			wantConflicts: []string{"a.md"},
		},
		{
			name:      "empty tree on our side with no upstream change",
			base:      map[string]entry{"a.md": text("a\n")},
			ours:      map[string]entry{},
			theirs:    map[string]entry{"a.md": text("a\n")},
			wantClean: true,
		},
		{
			name:      "empty base: only one side adds files",
			base:      map[string]entry{},
			ours:      map[string]entry{"a.md": text("a\n")},
			theirs:    map[string]entry{},
			wantClean: true,
		},
		{
			name:          "empty base: both sides add the same path differently",
			base:          map[string]entry{},
			ours:          map[string]entry{"a.md": text("ours\n")},
			theirs:        map[string]entry{"a.md": text("theirs\n")},
			wantClean:     false,
			wantConflicts: []string{"a.md"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			base := factory.tree(t, test.base)
			ours := factory.tree(t, test.ours)
			theirs := factory.tree(t, test.theirs)

			got, err := MergeTree(ctx, factory.dir, base, ours, theirs)
			if err != nil {
				t.Fatalf("MergeTree: %v", err)
			}
			if got.Clean != test.wantClean {
				t.Fatalf("Clean = %v, want %v (conflicts %v)", got.Clean, test.wantClean, got.Conflicts)
			}
			if got.Tree == "" {
				t.Fatal("MergeTree returned an empty result tree")
			}
			if test.wantClean {
				if len(got.Conflicts) != 0 {
					t.Fatalf("clean merge reported conflicts %v", got.Conflicts)
				}
			} else if !reflect.DeepEqual(got.Conflicts, test.wantConflicts) {
				t.Fatalf("Conflicts = %v, want %v", got.Conflicts, test.wantConflicts)
			}
			if test.wantPaths != nil {
				requireTreePaths(t, factory, got.Tree, test.wantPaths)
			}
			if test.wantHunks > 0 {
				merged := factory.blobAt(t, got.Tree, test.hunkPath)
				if n := countMarkerStarts(merged); n != test.wantHunks {
					t.Fatalf("merged %s has %d conflict regions, want %d:\n%s",
						test.hunkPath, n, test.wantHunks, merged)
				}
			}
		})
	}
}

// TestMergeTreeSymlinkRoundTrip pins H1's regression: content moves as
// git objects, so a symlink survives a merge as a symlink.
func TestMergeTreeSymlinkRoundTrip(t *testing.T) {
	factory := newTreeFactory(t)

	base := factory.tree(t, map[string]entry{"a.md": text("a\n")})
	ours := factory.tree(t, map[string]entry{
		"a.md":      text("a\n"),
		"target.md": text("target body\n"),
		"link.md":   link("target.md"),
	})
	theirs := factory.tree(t, map[string]entry{"a.md": text("a upstream\n")})

	got, err := MergeTree(context.Background(), factory.dir, base, ours, theirs)
	if err != nil {
		t.Fatalf("MergeTree: %v", err)
	}
	if !got.Clean {
		t.Fatalf("merge was not clean: %v", got.Conflicts)
	}

	var symlinkLine string
	for _, line := range factory.lsTree(t, got.Tree) {
		if strings.HasSuffix(line, "\tlink.md") {
			symlinkLine = line
		}
	}
	if symlinkLine == "" {
		t.Fatalf("merged tree lost link.md:\n%s", strings.Join(factory.lsTree(t, got.Tree), "\n"))
	}
	if !strings.HasPrefix(symlinkLine, "120000 blob ") {
		t.Fatalf("link.md is not a symlink in the merged tree: %q", symlinkLine)
	}
	if target := factory.blobAt(t, got.Tree, "link.md"); target != "target.md" {
		t.Fatalf("symlink target = %q, want %q", target, "target.md")
	}
}

// TestMergeTreeFileModeChangePreserved asserts the executable bit set on
// one side survives a content edit on the other.
func TestMergeTreeFileModeChangePreserved(t *testing.T) {
	factory := newTreeFactory(t)

	base := factory.tree(t, map[string]entry{"run.sh": text("echo one\n")})
	ours := factory.tree(t, map[string]entry{"run.sh": exe("echo one\n")})
	theirs := factory.tree(t, map[string]entry{"run.sh": text("echo two\n")})

	got, err := MergeTree(context.Background(), factory.dir, base, ours, theirs)
	if err != nil {
		t.Fatalf("MergeTree: %v", err)
	}
	if !got.Clean {
		t.Fatalf("merge was not clean: %v", got.Conflicts)
	}
	lines := factory.lsTree(t, got.Tree)
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "100755 blob ") {
		t.Fatalf("executable bit not preserved: %v", lines)
	}
	if body := factory.blobAt(t, got.Tree, "run.sh"); body != "echo two\n" {
		t.Fatalf("merged content = %q, want the upstream edit", body)
	}
}

// TestMergeTreeConflictMarkerLabels pins audit L1: markers name the
// sanho refs, never OIDs or temp paths.
func TestMergeTreeConflictMarkerLabels(t *testing.T) {
	factory := newTreeFactory(t)

	base := factory.tree(t, map[string]entry{"a.md": hunkFile("A", "B", "C")})
	ours := factory.tree(t, map[string]entry{"a.md": hunkFile("A-ours", "B", "C")})
	theirs := factory.tree(t, map[string]entry{"a.md": hunkFile("A-theirs", "B", "C")})

	got, err := MergeTree(context.Background(), factory.dir, base, ours, theirs)
	if err != nil {
		t.Fatalf("MergeTree: %v", err)
	}
	if got.Clean {
		t.Fatal("expected a conflicted merge")
	}

	merged := factory.blobAt(t, got.Tree, "a.md")
	if !strings.Contains(merged, "<<<<<<< "+labelOurs) {
		t.Errorf("merged content lacks the %q label:\n%s", labelOurs, merged)
	}
	if !strings.Contains(merged, ">>>>>>> "+labelUpstream) {
		t.Errorf("merged content lacks the %q label:\n%s", labelUpstream, merged)
	}
}

// TestMergeTreeCleansUpTempRefs asserts the fixed-name refs do not
// outlive the merge; a leftover would silently seed the next one.
func TestMergeTreeCleansUpTempRefs(t *testing.T) {
	factory := newTreeFactory(t)
	ctx := context.Background()

	base := factory.tree(t, map[string]entry{"a.md": hunkFile("A", "B", "C")})
	ours := factory.tree(t, map[string]entry{"a.md": hunkFile("A-ours", "B", "C")})
	theirs := factory.tree(t, map[string]entry{"a.md": hunkFile("A-theirs", "B", "C")})

	if _, err := MergeTree(ctx, factory.dir, base, ours, theirs); err != nil {
		t.Fatalf("MergeTree: %v", err)
	}

	refs := gitRun(t, factory.dir, "for-each-ref", "--format=%(refname)")
	for _, ref := range []string{refOurs, refUpstream} {
		if strings.Contains(refs, ref) {
			t.Errorf("%s survived the merge; refs are:\n%s", ref, refs)
		}
	}
}

// TestMergeTreeIsDeterministic asserts the synthetic commit identity is
// pinned: the same three trees must produce the same result tree OID on
// every run, so a re-merge after a lost CAS race is comparable.
func TestMergeTreeIsDeterministic(t *testing.T) {
	factory := newTreeFactory(t)
	ctx := context.Background()

	base := factory.tree(t, map[string]entry{"a.md": text("a\n"), "b.md": text("b\n")})
	ours := factory.tree(t, map[string]entry{"a.md": text("a ours\n"), "b.md": text("b\n")})
	theirs := factory.tree(t, map[string]entry{"a.md": text("a\n"), "b.md": text("b theirs\n")})

	first, err := MergeTree(ctx, factory.dir, base, ours, theirs)
	if err != nil {
		t.Fatalf("first MergeTree: %v", err)
	}
	second, err := MergeTree(ctx, factory.dir, base, ours, theirs)
	if err != nil {
		t.Fatalf("second MergeTree: %v", err)
	}
	if first.Tree != second.Tree {
		t.Fatalf("merge is not deterministic: %s then %s", first.Tree, second.Tree)
	}

	// The same must hold for a conflicted merge, whose result tree
	// embeds generated marker text.
	conflictBase := factory.tree(t, map[string]entry{"a.md": hunkFile("A", "B", "C")})
	conflictOurs := factory.tree(t, map[string]entry{"a.md": hunkFile("A-ours", "B", "C")})
	conflictTheirs := factory.tree(t, map[string]entry{"a.md": hunkFile("A-theirs", "B", "C")})

	firstConflict, err := MergeTree(ctx, factory.dir, conflictBase, conflictOurs, conflictTheirs)
	if err != nil {
		t.Fatalf("first conflicted MergeTree: %v", err)
	}
	secondConflict, err := MergeTree(ctx, factory.dir, conflictBase, conflictOurs, conflictTheirs)
	if err != nil {
		t.Fatalf("second conflicted MergeTree: %v", err)
	}
	if firstConflict.Tree != secondConflict.Tree {
		t.Fatalf("conflicted merge is not deterministic: %s then %s", firstConflict.Tree, secondConflict.Tree)
	}
}

// TestMergeTreeAcceptsTheUnwrittenEmptyTree closes the one input whose
// existence is not guaranteed by the object database: appgit derives the
// empty-tree OID with `hash-object` and no -w (it must not write into
// the app repository), and FetchFromApp cannot import an object that was
// never written. Git resolves the empty tree natively in any repository,
// and this pins that both commit-tree and merge-tree agree.
func TestMergeTreeAcceptsTheUnwrittenEmptyTree(t *testing.T) {
	factory := newTreeFactory(t)
	ctx := context.Background()

	// Derived, never written: the same way appgit.EmptyTree obtains it.
	empty := gitLine(t, factory.dir, "hash-object", "-t", "tree", os.DevNull)

	base := factory.tree(t, map[string]entry{"a.md": text("alpha\n")})
	added := factory.tree(t, map[string]entry{"a.md": text("alpha\n"), "b.md": text("beta\n")})
	edited := factory.tree(t, map[string]entry{"a.md": text("alpha upstream\n")})

	got, err := MergeTree(ctx, factory.dir, empty, empty, added)
	if err != nil {
		t.Fatalf("MergeTree with an empty base and empty ours: %v", err)
	}
	if !got.Clean {
		t.Fatalf("merge was not clean: %v", got.Conflicts)
	}

	// Deleting everything locally while upstream edits is a conflict,
	// and must be reported as one rather than failing on the input.
	deleted, err := MergeTree(ctx, factory.dir, base, empty, edited)
	if err != nil {
		t.Fatalf("MergeTree with an empty ours tree: %v", err)
	}
	if deleted.Clean {
		t.Fatal("deleting a file upstream still edits merged cleanly")
	}
	if len(deleted.Conflicts) != 1 || deleted.Conflicts[0] != "a.md" {
		t.Fatalf("conflicts = %v, want [a.md]", deleted.Conflicts)
	}
}

// TestMergeTreeRejectsMissingTree asserts a bad input surfaces as an
// error, not as a silently "clean" merge.
func TestMergeTreeRejectsMissingTree(t *testing.T) {
	factory := newTreeFactory(t)
	base := factory.tree(t, map[string]entry{"a.md": text("a\n")})

	const absent = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	if _, err := MergeTree(context.Background(), factory.dir, base, absent, base); err == nil {
		t.Fatal("MergeTree accepted a tree that does not exist")
	}
}

// TestParseMergeTree covers the -z wire format directly, including the
// informational-message fallback that no ordinary conflict class
// exercises.
func TestParseMergeTree(t *testing.T) {
	const tree = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	nul := func(fields ...string) []byte {
		return []byte(strings.Join(fields, "\x00"))
	}

	tests := []struct {
		name          string
		stdout        []byte
		wantTree      string
		wantConflicts []string
	}{
		{
			name:     "clean merge is a bare tree with a trailing newline",
			stdout:   []byte(tree + "\x00\n"),
			wantTree: tree,
		},
		{
			name: "stage entries name the conflicted paths once each",
			stdout: nul(
				tree,
				"100644 1111111111111111111111111111111111111111 1\tdocs/a.md",
				"100644 2222222222222222222222222222222222222222 2\tdocs/a.md",
				"100644 3333333333333333333333333333333333333333 3\tdocs/a.md",
				"",
				"1", "docs/a.md", "CONFLICT (contents)", "CONFLICT (content): Merge conflict in docs/a.md\n",
				"",
			),
			wantTree:      tree,
			wantConflicts: []string{"docs/a.md"},
		},
		{
			name: "paths with tabs and spaces survive the NUL framing",
			stdout: nul(
				tree,
				"100644 1111111111111111111111111111111111111111 1\tdocs/a b\tc.md",
				"",
			),
			wantTree:      tree,
			wantConflicts: []string{"docs/a b\tc.md"},
		},
		{
			name: "informational messages are the fallback when no stage entries exist",
			stdout: nul(
				tree,
				"",
				"1", "docs/a.md", "Auto-merging", "Auto-merging docs/a.md\n",
				"2", "docs/a.md", "docs/b.md", "CONFLICT (rename/rename)", "CONFLICT: renamed\n",
				"",
			),
			wantTree:      tree,
			wantConflicts: []string{"docs/a.md", "docs/b.md"},
		},
		{
			name: "non-CONFLICT messages contribute no paths",
			stdout: nul(
				tree,
				"",
				"1", "docs/a.md", "Auto-merging", "Auto-merging docs/a.md\n",
				"",
			),
			wantTree: tree,
		},
		{
			name: "a truncated message section stops the walk instead of guessing",
			stdout: nul(
				tree,
				"",
				"3", "docs/a.md",
			),
			wantTree: tree,
		},
		{
			name:     "empty output yields nothing",
			stdout:   nil,
			wantTree: "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotTree, gotConflicts := parseMergeTree(test.stdout)
			if gotTree != test.wantTree {
				t.Errorf("tree = %q, want %q", gotTree, test.wantTree)
			}
			if !reflect.DeepEqual(gotConflicts, test.wantConflicts) {
				t.Errorf("conflicts = %v, want %v", gotConflicts, test.wantConflicts)
			}
		})
	}
}

// requireTreePaths asserts the exact set of paths a result tree holds.
//
// It is what makes a DELETION checkable. Clean/conflict says nothing
// about a path that came back: a merge that resurrected a document both
// sides removed reports a clean merge and republishes content the team
// deleted, which reads downstream as an upstream regression nobody made.
func requireTreePaths(t *testing.T, factory *treeFactory, tree string, want []string) {
	t.Helper()

	var got []string
	for _, line := range factory.lsTree(t, tree) {
		_, path, _ := strings.Cut(line, "\t")
		got = append(got, path)
	}
	sort.Strings(got)
	sorted := append([]string{}, want...)
	sort.Strings(sorted)

	if len(got) == 0 && len(sorted) == 0 {
		return
	}
	if !reflect.DeepEqual(got, sorted) {
		t.Fatalf("merged tree holds %v, want %v", got, sorted)
	}
}
