// Package provenance defines the v0.2 ancestry-provenance model
// (docs/architecture.md "Provenance"): which canonical state a workspace's docs
// derive from, how that fact is stamped into commit trailers, and how
// it is re-derived from history after HEAD moves.
//
// Everything in this package is pure: no filesystem, no git, no network.
package provenance

import "regexp"

// Trailer keys (docs/architecture.md "Provenance").
const (
	// TrailerBase records the canonical commit the docs derive from.
	TrailerBase = "docs-base"
	// TrailerBaseTree records that commit's docs tree (rewrite anchor).
	TrailerBaseTree = "docs-base-tree"
	// LegacyTrailerVersion is the v0.1 identity-semantics key. Read-only
	// compatibility: a `docs-version: X` commit's docs tree equaled
	// canonical X, so X is also a valid *base* for edits made on top.
	LegacyTrailerVersion = "docs-version"
)

// OIDPattern matches a full lowercase SHA-1 or SHA-256 object ID.
var OIDPattern = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)

// Base is the ancestry pointer: the canonical commit (and its docs tree)
// that the workspace docs derive from. Tree may be empty when unknown
// (e.g. adopted from a legacy docs-version trailer before resolution).
type Base struct {
	Commit string `json:"commit"`
	Tree   string `json:"tree"`
}

// IsZero reports whether no base is recorded at all.
func (b Base) IsZero() bool { return b.Commit == "" && b.Tree == "" }

// Valid reports whether the recorded OIDs are well-formed. An empty
// Tree is permitted (legacy adoption); an empty Commit is not.
func (b Base) Valid() bool {
	if !OIDPattern.MatchString(b.Commit) {
		return false
	}
	return b.Tree == "" || OIDPattern.MatchString(b.Tree)
}

// Trailers renders the trailer lines to append for this base, in order.
// The tree line is omitted when Tree is empty.
func (b Base) Trailers() []string {
	trailers := []string{TrailerBase + ": " + b.Commit}
	if b.Tree != "" {
		trailers = append(trailers, TrailerBaseTree+": "+b.Tree)
	}
	return trailers
}

// StampInputs are the facts the commit-msg hook gathers locally to decide
// whether to stamp (docs/architecture.md "Provenance" stamping rule).
type StampInputs struct {
	// MessageHasBase is true when the commit message already contains a
	// docs-base trailer.
	MessageHasBase bool
	// IndexDocsTree is the docs tree OID of the index being committed.
	IndexDocsTree string
	// HeadDocsTree is the docs tree OID of current HEAD ("" if unborn).
	HeadDocsTree string
	// HeadParentDocsTree is the docs tree OID of HEAD~ ("" if none;
	// treat as the empty tree).
	HeadParentDocsTree string
}

// ShouldStamp implements the provenance contract rule: stamp when the commit changes
// docs relative to HEAD, or when HEAD itself changed docs relative to
// its parent (covers --amend of a docs-touching commit, including
// message-only rewords). Over-stamping is harmless by design.
func ShouldStamp(in StampInputs) bool {
	if in.MessageHasBase {
		return false
	}
	// Plain string equality is the right notion of "same docs tree" for
	// both real OIDs and the "" sentinel: an unborn HEAD ("") and a
	// docs-free HEAD~ ("") are equally "no tree", so "" == "" correctly
	// reads as unchanged, and "" compared against a real OID correctly
	// reads as changed, on either side. No special-casing needed.
	changedInIndex := in.IndexDocsTree != in.HeadDocsTree
	headChangedFromParent := in.HeadDocsTree != in.HeadParentDocsTree
	return changedInIndex || headChangedFromParent
}

// CommitTrailers is one commit's parsed trailer values, used for base
// re-derivation after HEAD moves (the hook contract).
type CommitTrailers struct {
	Commit string
	// Values maps trailer key → values in message order.
	Values map[string][]string
}

// SelectBase picks the base from a newest-first scan of reachable
// stamped commits: the first commit carrying a valid docs-base (with
// optional docs-base-tree), or a valid legacy docs-version adopted as
// {Commit: X, Tree: ""}. Malformed or duplicate-valued trailers on a
// commit disqualify that commit and the scan continues.
func SelectBase(newestFirst []CommitTrailers) (Base, bool) {
	for _, c := range newestFirst {
		if base, ok := adoptFrom(c); ok {
			return base, true
		}
	}
	return Base{}, false
}

// adoptFrom applies the single-commit adoption rule described on
// SelectBase to one commit's trailers: docs-base takes priority whenever
// the key is present at all, valid or not — an invalid (or duplicated)
// docs-base does NOT fall back to a legacy trailer on the *same* commit,
// it disqualifies the commit outright, and the outer scan moves on to
// the next commit. docs-version is consulted only when docs-base is
// entirely absent from this commit.
func adoptFrom(c CommitTrailers) (Base, bool) {
	if values := c.Values[TrailerBase]; len(values) > 0 {
		if len(values) != 1 || !OIDPattern.MatchString(values[0]) {
			return Base{}, false // disqualified: not exactly one valid value
		}
		return Base{Commit: values[0], Tree: singleValidTree(c)}, true
	}

	if values := c.Values[LegacyTrailerVersion]; len(values) > 0 {
		if len(values) != 1 || !OIDPattern.MatchString(values[0]) {
			return Base{}, false // disqualified: not exactly one valid value
		}
		return Base{Commit: values[0]}, true
	}

	return Base{}, false // neither key present on this commit
}

// singleValidTree extracts docs-base-tree when it is present exactly
// once and well-formed. Any other shape (absent, duplicated, malformed)
// yields "" without disqualifying the commit's docs-base adoption — only
// TrailerBase/LegacyTrailerVersion values can disqualify a commit.
func singleValidTree(c CommitTrailers) string {
	values := c.Values[TrailerBaseTree]
	if len(values) == 1 && OIDPattern.MatchString(values[0]) {
		return values[0]
	}
	return ""
}
