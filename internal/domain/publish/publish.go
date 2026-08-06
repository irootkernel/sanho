// Package publish holds the pure decision logic of the v0.2 publication
// algorithm (sanho-v0.2.md §5.3): given locally gathered facts, which of
// the four cases applies, and how the canonical commit is described.
//
// Orchestration (fetching, merging, pushing, retrying) lives in
// usecase/publish and infra/canonical; this package decides, renders,
// and stays trivially testable.
package publish

import "github.com/irootkernel/sanho/internal/domain/provenance"

// Case enumerates §5.3's publication case analysis.
type Case int

const (
	// CaseUpToDate: tip docs tree equals canonical HEAD's tree — nothing
	// to publish.
	CaseUpToDate Case = iota
	// CaseFastForward: base == canonical HEAD and tip differs — publish
	// tip's tree on top of HEAD.
	CaseFastForward
	// CaseAutoMerge: base is a proper ancestor of HEAD — 3-way merge
	// required; publish if clean, reject to sync if conflicted.
	CaseAutoMerge
	// CaseUnknownBase: base commit is not reachable from canonical HEAD
	// (history rewritten) — attempt tree re-anchoring, else reject.
	CaseUnknownBase
)

// Inputs are the facts needed to decide the case.
type Inputs struct {
	Base provenance.Base
	// TipDocsTree is the docs tree OID of the pushed tip (empty-tree OID
	// when the docs dir is absent).
	TipDocsTree string
	// Head / HeadDocsTree describe canonical HEAD after a fresh fetch.
	Head         string
	HeadDocsTree string
	// BaseKnown is true when Base.Commit resolves in canonical history;
	// BaseIsAncestor is true when it is HEAD or an ancestor of HEAD.
	BaseKnown      bool
	BaseIsAncestor bool
}

// Decide implements the §5.3 case analysis. It is total: every input
// combination maps to exactly one case.
func Decide(in Inputs) Case {
	panic("unimplemented (sanho v0.2 P2)")
}

// CommitMeta describes one publication for the canonical commit message
// (§5.3 "canonical commit convention").
type CommitMeta struct {
	// RepoName is the app repository's short name (origin basename or
	// directory name), Branch the pushed branch.
	RepoName string
	Branch   string
	// WorkspaceID and TipOID identify the source for reverse traceability.
	WorkspaceID string
	TipOID      string
	// Subjects are the subjects of app commits since the base that
	// touched docs, oldest first.
	Subjects []string
}

// Subject renders the one-line canonical commit subject.
func (m CommitMeta) Subject() string {
	panic("unimplemented (sanho v0.2 P2)")
}

// Message renders the full canonical commit message (subject + body).
func (m CommitMeta) Message() string {
	panic("unimplemented (sanho v0.2 P2)")
}
