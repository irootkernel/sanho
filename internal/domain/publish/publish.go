// Package publish holds the pure decision logic of the v0.2 publication
// algorithm (sanho-v0.2.md §5.3): given locally gathered facts, which of
// the four cases applies, and how the canonical commit is described.
//
// Orchestration (fetching, merging, pushing, retrying) lives in
// usecase/publish and infra/canonical; this package decides, renders,
// and stays trivially testable.
package publish

import (
	"errors"
	"strconv"
	"strings"

	"github.com/irootkernel/sanho/internal/domain/provenance"
)

// Publication transport sentinels.
//
// They live in the domain layer because the two packages that must agree
// on them sit on opposite sides of a layering boundary: infra/canonical
// raises them, usecase/publish reacts to them (CAS retry, fail-closed
// write paths), and the architecture rules forbid a usecase package from
// importing infra. infra/canonical re-exports both under its own names
// (canonical.ErrNonFastForward / canonical.ErrUnreachable) so callers
// that already speak to the clone keep a single vocabulary.
var (
	// ErrNonFastForward is the CAS-loss sentinel: origin rejected the
	// push because its head moved. Callers refetch and retry (§5.3, at
	// most MaxCASRetries attempts).
	ErrNonFastForward = errors.New("canonical push rejected: non-fast-forward")
	// ErrUnreachable wraps failures reaching origin; write paths fail
	// closed on it with the §5.9 canonical-unreachable message.
	ErrUnreachable = errors.New("canonical repository unreachable")
	// ErrEmptyBranch reports that the canonical publication branch
	// carries no commits at all — a brand-new docs repository that
	// nothing has published into yet.
	//
	// It is a distinct sentinel rather than a plain failure because it is
	// not one: it is the ordinary starting state of a fresh project, and
	// each flow has a correct answer for it. Publication bootstraps
	// (root commit, no lease); sync and pull have nothing to consume and
	// report up to date. Both live above infra, hence the sentinel lives
	// here beside its two siblings.
	ErrEmptyBranch = errors.New("canonical publication branch has no commits")
)

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

// String renders the case for diagnostics and JSON output.
func (c Case) String() string {
	switch c {
	case CaseUpToDate:
		return "up_to_date"
	case CaseFastForward:
		return "fast_forward"
	case CaseAutoMerge:
		return "auto_merge"
	case CaseUnknownBase:
		return "unknown_base"
	default:
		return "case(" + strconv.Itoa(int(c)) + ")"
	}
}

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
//
// Precedence, in evaluation order:
//
//  1. TipDocsTree == HeadDocsTree → CaseUpToDate. Checked first and
//     unconditionally: when the tip's docs already match canonical there
//     is nothing to publish, so the state of the base pointer — stale,
//     rewritten, or absent — cannot make the push fail. This is what
//     keeps code-only pushes working in a workspace whose base is
//     orphaned.
//  2. !BaseKnown || !BaseIsAncestor → CaseUnknownBase. Either the base
//     commit does not resolve in canonical at all, or it resolves but
//     sits off the published branch (both are "history was rewritten"
//     from the workspace's point of view; re-anchoring by docs-base-tree
//     is the caller's next move, §5.3 case ④).
//  3. Base.Commit == Head → CaseFastForward (base is known, is an
//     ancestor, and is exactly HEAD: nothing landed upstream since, so
//     the tip's tree may be published directly on top of HEAD).
//  4. otherwise → CaseAutoMerge (base is a *proper* ancestor of HEAD:
//     upstream moved, so the three docs trees must be merged).
func Decide(in Inputs) Case {
	switch {
	case in.TipDocsTree == in.HeadDocsTree:
		return CaseUpToDate
	case !in.BaseKnown || !in.BaseIsAncestor:
		return CaseUnknownBase
	case in.Base.Commit == in.Head:
		return CaseFastForward
	default:
		return CaseAutoMerge
	}
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

// Subject renders the one-line canonical commit subject:
//
//	docs: <repo-name>/<branch> (<N> app commits)
//
// N is the number of Subjects. The wording is fixed by the §5.3
// convention block and is deliberately not pluralized: the format is a
// machine-readable contract that `sanho status` and canonical `git log`
// readers match on, so "(1 app commits)" is correct output.
func (m CommitMeta) Subject() string {
	return "docs: " + m.RepoName + "/" + m.Branch + " (" + strconv.Itoa(len(m.Subjects)) + " app commits)"
}

// Message renders the full canonical commit message: the Subject, a
// blank line, then the §5.3 body —
//
//	source: <workspace-id> @ <app tip OID>
//	commits:
//	  - <subject>
//	  - <subject>
//
// The `commits:` section is omitted entirely when no app commit since
// the base touched docs, so the message never carries a dangling header.
func (m CommitMeta) Message() string {
	var b strings.Builder
	b.WriteString(m.Subject())
	b.WriteString("\n\n")
	b.WriteString("source: ")
	b.WriteString(m.WorkspaceID)
	b.WriteString(" @ ")
	b.WriteString(m.TipOID)
	b.WriteString("\n")
	if len(m.Subjects) > 0 {
		b.WriteString("commits:\n")
		for _, s := range m.Subjects {
			b.WriteString("  - ")
			b.WriteString(s)
			b.WriteString("\n")
		}
	}
	return b.String()
}
