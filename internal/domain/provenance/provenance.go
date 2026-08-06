// Package provenance defines the v0.2 ancestry-provenance model
// (sanho-v0.2.md D2, §5.1): which canonical state a workspace's docs
// derive from, how that fact is stamped into commit trailers, and how
// it is re-derived from history after HEAD moves.
//
// Everything in this package is pure: no filesystem, no git, no network.
package provenance

import "regexp"

// Trailer keys (sanho-v0.2.md §5.1).
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
	panic("unimplemented (sanho v0.2 P1)")
}

// Trailers renders the trailer lines to append for this base, in order.
// The tree line is omitted when Tree is empty.
func (b Base) Trailers() []string {
	panic("unimplemented (sanho v0.2 P1)")
}

// StampInputs are the facts the commit-msg hook gathers locally to decide
// whether to stamp (sanho-v0.2.md §5.1 stamping rule).
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

// ShouldStamp implements the §5.1 rule: stamp when the commit changes
// docs relative to HEAD, or when HEAD itself changed docs relative to
// its parent (covers --amend of a docs-touching commit, including
// message-only rewords). Over-stamping is harmless by design.
func ShouldStamp(in StampInputs) bool {
	panic("unimplemented (sanho v0.2 P1)")
}

// CommitTrailers is one commit's parsed trailer values, used for base
// re-derivation after HEAD moves (§5.10).
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
	panic("unimplemented (sanho v0.2 P1)")
}
