package provenance_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/irootkernel/sanho/internal/domain/provenance"
)

// OID fixtures. Built with strings.Repeat rather than hand-typed hex so
// their lengths are exact by construction (no risk of an off-by-one typo
// silently changing what a "valid" test case actually exercises).
var (
	oidA40 = strings.Repeat("a", 40)
	oidB40 = strings.Repeat("b", 40)
	oidC40 = strings.Repeat("c", 40)

	oidA64 = strings.Repeat("a", 64)
	oidB64 = strings.Repeat("b", 64)

	oidUpper40   = strings.ToUpper(oidA40)
	oidShort39   = strings.Repeat("a", 39)
	oidLong41    = strings.Repeat("a", 41)
	oidMid50     = strings.Repeat("a", 50) // between 40 and 64: neither length is valid
	oid63        = strings.Repeat("a", 63)
	oid65        = strings.Repeat("a", 65)
	oidBadChar40 = strings.Repeat("g", 40) // right length, 'g' is not hex
)

func TestBase_Valid(t *testing.T) {
	tests := []struct {
		name string
		base provenance.Base
		want bool
	}{
		{"valid 40-hex commit, empty tree", provenance.Base{Commit: oidA40}, true},
		{"valid 40-hex commit and tree", provenance.Base{Commit: oidA40, Tree: oidB40}, true},
		{"valid 64-hex commit, empty tree", provenance.Base{Commit: oidA64}, true},
		{"valid 64-hex commit and tree", provenance.Base{Commit: oidA64, Tree: oidB64}, true},
		{"40-hex commit with 64-hex tree: lengths validated independently", provenance.Base{Commit: oidA40, Tree: oidB64}, true},
		{"64-hex commit with 40-hex tree: lengths validated independently", provenance.Base{Commit: oidA64, Tree: oidB40}, true},

		{"zero value: empty commit and empty tree is invalid", provenance.Base{}, false},
		{"empty commit with an otherwise-valid tree is still invalid", provenance.Base{Tree: oidA40}, false},

		{"uppercase commit rejected (lowercase-only pattern)", provenance.Base{Commit: oidUpper40}, false},
		{"uppercase tree rejected", provenance.Base{Commit: oidA40, Tree: oidUpper40}, false},

		{"short commit (39 hex) rejected", provenance.Base{Commit: oidShort39}, false},
		{"long commit (41 hex) rejected", provenance.Base{Commit: oidLong41}, false},
		{"mid-length commit (50 hex, between 40 and 64) rejected", provenance.Base{Commit: oidMid50}, false},
		{"63-hex commit rejected", provenance.Base{Commit: oid63}, false},
		{"65-hex commit rejected", provenance.Base{Commit: oid65}, false},

		{"non-hex character in commit rejected", provenance.Base{Commit: oidBadChar40}, false},
		{"non-hex character in tree rejected even with a valid commit", provenance.Base{Commit: oidA40, Tree: oidBadChar40}, false},
		{"short tree (39 hex) rejected even with a valid commit", provenance.Base{Commit: oidA40, Tree: oidShort39}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.base.Valid(); got != tt.want {
				t.Errorf("Base{Commit: %.8s…, Tree: %.8s…}.Valid() = %v, want %v", tt.base.Commit, tt.base.Tree, got, tt.want)
			}
		})
	}
}

func TestBase_Trailers(t *testing.T) {
	tests := []struct {
		name string
		base provenance.Base
		want []string
	}{
		{
			name: "commit only: tree line omitted",
			base: provenance.Base{Commit: oidA40},
			want: []string{"docs-base: " + oidA40},
		},
		{
			name: "commit and tree: both lines, base first",
			base: provenance.Base{Commit: oidA40, Tree: oidB40},
			want: []string{"docs-base: " + oidA40, "docs-base-tree: " + oidB40},
		},
		{
			name: "zero-value base still renders: Trailers is a pure formatter, not a validator",
			base: provenance.Base{},
			want: []string{"docs-base: "},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.base.Trailers()
			if !slices.Equal(got, tt.want) {
				t.Errorf("Trailers() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestShouldStamp(t *testing.T) {
	tests := []struct {
		name string
		in   provenance.StampInputs
		want bool
	}{
		{
			name: "no history at all: index, head and parent all empty",
			in:   provenance.StampInputs{IndexDocsTree: "", HeadDocsTree: "", HeadParentDocsTree: ""},
			want: false,
		},
		{
			name: "docs tree identical across index, head and parent",
			in:   provenance.StampInputs{IndexDocsTree: oidA40, HeadDocsTree: oidA40, HeadParentDocsTree: oidA40},
			want: false,
		},
		{
			name: "ordinary docs-changing commit: index differs from head",
			in:   provenance.StampInputs{IndexDocsTree: oidB40, HeadDocsTree: oidA40, HeadParentDocsTree: oidA40},
			want: true,
		},
		{
			name: "amend-reword case: index==head, head!=parent",
			in:   provenance.StampInputs{IndexDocsTree: oidB40, HeadDocsTree: oidB40, HeadParentDocsTree: oidA40},
			want: true,
		},
		{
			// Same boolean shape as the amend-reword case above, but a
			// different real-world story: HEAD already changed docs
			// relative to its parent, and the commit being evaluated now
			// makes no further docs edit (index == head). The spec
			// names this the acceptable over-stamp, not a bug.
			name: "benign over-stamp: first non-docs commit after a docs commit",
			in:   provenance.StampInputs{IndexDocsTree: oidC40, HeadDocsTree: oidC40, HeadParentDocsTree: oidB40},
			want: true,
		},
		{
			name: "both clauses true: index differs from head, and head differs from parent",
			in:   provenance.StampInputs{IndexDocsTree: oidC40, HeadDocsTree: oidB40, HeadParentDocsTree: oidA40},
			want: true,
		},
		{
			name: "root commit adding docs: unborn HEAD and absent parent both read as empty tree",
			in:   provenance.StampInputs{IndexDocsTree: oidA40, HeadDocsTree: "", HeadParentDocsTree: ""},
			want: true,
		},
		{
			name: "HEAD is the root commit and already touches docs (no HEAD~, treated as empty)",
			in:   provenance.StampInputs{IndexDocsTree: oidA40, HeadDocsTree: oidA40, HeadParentDocsTree: ""},
			want: true,
		},
		{
			name: "index empty relative to a non-empty head (docs removed from what's being committed)",
			in:   provenance.StampInputs{IndexDocsTree: "", HeadDocsTree: oidA40, HeadParentDocsTree: oidA40},
			want: true,
		},
		{
			name: "message already has docs-base: never stamp again, even though trees differ everywhere",
			in:   provenance.StampInputs{MessageHasBase: true, IndexDocsTree: oidC40, HeadDocsTree: oidB40, HeadParentDocsTree: oidA40},
			want: false,
		},
		{
			name: "message already has docs-base overrides even the fully-empty no-op-trees case",
			in:   provenance.StampInputs{MessageHasBase: true, IndexDocsTree: "", HeadDocsTree: "", HeadParentDocsTree: ""},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := provenance.ShouldStamp(tt.in); got != tt.want {
				t.Errorf("ShouldStamp(%+v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestSelectBase(t *testing.T) {
	tests := []struct {
		name        string
		newestFirst []provenance.CommitTrailers
		wantBase    provenance.Base
		wantOK      bool
	}{
		{
			name: "single commit, valid docs-base, no tree",
			newestFirst: []provenance.CommitTrailers{
				{Commit: "c1", Values: map[string][]string{provenance.TrailerBase: {oidA40}}},
			},
			wantBase: provenance.Base{Commit: oidA40},
			wantOK:   true,
		},
		{
			name: "single commit, valid docs-base and valid docs-base-tree",
			newestFirst: []provenance.CommitTrailers{
				{Commit: "c1", Values: map[string][]string{
					provenance.TrailerBase:     {oidA40},
					provenance.TrailerBaseTree: {oidB40},
				}},
			},
			wantBase: provenance.Base{Commit: oidA40, Tree: oidB40},
			wantOK:   true,
		},
		{
			name: "new key preferred over legacy key present on the same commit",
			newestFirst: []provenance.CommitTrailers{
				{Commit: "c1", Values: map[string][]string{
					provenance.TrailerBase:          {oidA40},
					provenance.LegacyTrailerVersion: {oidB40},
				}},
			},
			wantBase: provenance.Base{Commit: oidA40, Tree: ""},
			wantOK:   true,
		},
		{
			name: "legacy adoption when docs-base is entirely absent",
			newestFirst: []provenance.CommitTrailers{
				{Commit: "c1", Values: map[string][]string{provenance.LegacyTrailerVersion: {oidA40}}},
			},
			wantBase: provenance.Base{Commit: oidA40, Tree: ""},
			wantOK:   true,
		},
		{
			name: "duplicate docs-base disqualifies newest commit; scan falls back to older commit's legacy trailer",
			newestFirst: []provenance.CommitTrailers{
				{Commit: "newest", Values: map[string][]string{provenance.TrailerBase: {oidA40, oidB40}}},
				{Commit: "older", Values: map[string][]string{provenance.LegacyTrailerVersion: {oidC40}}},
			},
			wantBase: provenance.Base{Commit: oidC40},
			wantOK:   true,
		},
		{
			name: "malformed docs-base disqualifies newest commit; scan falls back to older commit's docs-base",
			newestFirst: []provenance.CommitTrailers{
				{Commit: "newest", Values: map[string][]string{provenance.TrailerBase: {oidBadChar40}}},
				{Commit: "older", Values: map[string][]string{provenance.TrailerBase: {oidA40}}},
			},
			wantBase: provenance.Base{Commit: oidA40},
			wantOK:   true,
		},
		{
			// The key nuance: docs-base is PRESENT (though invalid), so
			// the legacy trailer on the very same commit must NOT be
			// consulted as a fallback. The whole commit is disqualified
			// and the scan must move to the next commit in the list.
			name: "invalid docs-base does not fall back to a valid legacy trailer on the same commit",
			newestFirst: []provenance.CommitTrailers{
				{Commit: "newest", Values: map[string][]string{
					provenance.TrailerBase:          {oidBadChar40},
					provenance.LegacyTrailerVersion: {oidA40},
				}},
				{Commit: "older", Values: map[string][]string{provenance.LegacyTrailerVersion: {oidB40}}},
			},
			wantBase: provenance.Base{Commit: oidB40},
			wantOK:   true,
		},
		{
			name: "duplicate legacy values disqualify with nothing older to fall back to",
			newestFirst: []provenance.CommitTrailers{
				{Commit: "c1", Values: map[string][]string{provenance.LegacyTrailerVersion: {oidA40, oidB40}}},
			},
			wantBase: provenance.Base{},
			wantOK:   false,
		},
		{
			name: "malformed legacy value disqualifies with nothing older to fall back to",
			newestFirst: []provenance.CommitTrailers{
				{Commit: "c1", Values: map[string][]string{provenance.LegacyTrailerVersion: {oidBadChar40}}},
			},
			wantBase: provenance.Base{},
			wantOK:   false,
		},
		{
			name: "docs-base-tree with two values leaves Tree empty without disqualifying the commit",
			newestFirst: []provenance.CommitTrailers{
				{Commit: "c1", Values: map[string][]string{
					provenance.TrailerBase:     {oidA40},
					provenance.TrailerBaseTree: {oidB40, oidC40},
				}},
			},
			wantBase: provenance.Base{Commit: oidA40, Tree: ""},
			wantOK:   true,
		},
		{
			name: "malformed docs-base-tree leaves Tree empty without disqualifying the commit",
			newestFirst: []provenance.CommitTrailers{
				{Commit: "c1", Values: map[string][]string{
					provenance.TrailerBase:     {oidA40},
					provenance.TrailerBaseTree: {oidBadChar40},
				}},
			},
			wantBase: provenance.Base{Commit: oidA40, Tree: ""},
			wantOK:   true,
		},
		{
			name: "commit with only unrelated trailers is skipped; scan continues to older commit",
			newestFirst: []provenance.CommitTrailers{
				{Commit: "newest", Values: map[string][]string{"Signed-off-by": {"someone@example.com"}}},
				{Commit: "older", Values: map[string][]string{provenance.TrailerBase: {oidA40}}},
			},
			wantBase: provenance.Base{Commit: oidA40},
			wantOK:   true,
		},
		{
			name: "commit with a nil Values map does not panic and is skipped",
			newestFirst: []provenance.CommitTrailers{
				{Commit: "newest", Values: nil},
				{Commit: "older", Values: map[string][]string{provenance.TrailerBase: {oidA40}}},
			},
			wantBase: provenance.Base{Commit: oidA40},
			wantOK:   true,
		},
		{
			name: "first adoption wins: newest valid docs-base beats an older, different valid docs-base",
			newestFirst: []provenance.CommitTrailers{
				{Commit: "newest", Values: map[string][]string{provenance.TrailerBase: {oidA40}}},
				{Commit: "older", Values: map[string][]string{provenance.TrailerBase: {oidB40}}},
			},
			wantBase: provenance.Base{Commit: oidA40},
			wantOK:   true,
		},
		{
			name:        "empty scan: nil slice",
			newestFirst: nil,
			wantBase:    provenance.Base{},
			wantOK:      false,
		},
		{
			name:        "empty scan: empty non-nil slice",
			newestFirst: []provenance.CommitTrailers{},
			wantBase:    provenance.Base{},
			wantOK:      false,
		},
		{
			name: "every commit disqualified or empty: no base found after exhausting the scan",
			newestFirst: []provenance.CommitTrailers{
				{Commit: "c1", Values: map[string][]string{provenance.TrailerBase: {oidBadChar40}}},
				{Commit: "c2", Values: map[string][]string{}},
				{Commit: "c3", Values: map[string][]string{provenance.LegacyTrailerVersion: {oidShort39}}},
			},
			wantBase: provenance.Base{},
			wantOK:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotBase, gotOK := provenance.SelectBase(tt.newestFirst)
			if gotBase != tt.wantBase || gotOK != tt.wantOK {
				t.Errorf("SelectBase(...) = (%+v, %v), want (%+v, %v)", gotBase, gotOK, tt.wantBase, tt.wantOK)
			}
		})
	}
}
