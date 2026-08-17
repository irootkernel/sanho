// Package publish holds the pure decision logic of the v0.2 publication
// algorithm (docs/architecture.md "Publication"): given locally gathered facts, which of
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
	// push because its head moved. Callers refetch and retry (the publication contract, at
	// most MaxCASRetries attempts).
	ErrNonFastForward = errors.New("canonical push rejected: non-fast-forward")
	// ErrUnreachable wraps failures reaching origin; write paths fail
	// closed on it with the guidance contract canonical-unreachable message.
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

// Case enumerates the publication contract's publication case analysis.
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

// Decide implements the publication contract case analysis. It is total: every input
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
//     is the caller's next move, the publication contract).
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
// (the publication contract "canonical commit convention").
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
//	[SANHO] Publish docs from <repo-name>/<branch> (<N> app commits)
//
// N is the number of Subjects. The wording is fixed by the publication contract
// convention block and is deliberately not pluralized: the format is a
// machine-readable contract that `sanho status` and canonical `git log`
// readers match on, so "(1 app commits)" is correct output.
func (m CommitMeta) Subject() string {
	return "[SANHO] Publish docs from " + m.RepoName + "/" + m.Branch + " (" + strconv.Itoa(len(m.Subjects)) + " app commits)"
}

// Message renders the full canonical commit message: the Subject, a
// blank line, then the publication contract body —
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

// The tokens ParseCommitMeta reads. They are deliberately spelled here
// rather than shared with Subject/Message: those two render the most
// safety-critical string the product writes, and rewriting them to
// consume constants would be an unrequested edit to working code. What
// binds the two halves instead is the round-trip property in
// publish_test.go — ParseCommitMeta(m.Message()) must return m — which
// fails the build the moment either side drifts.
const (
	subjectPrefix   = "[SANHO] Publish docs from "
	subjectCountSep = " ("
	subjectSuffix   = " app commits)"
	sourcePrefix    = "source: "
	sourceSeparator = " @ "
	commitsHeader   = "commits:"
	commitsItem     = "  - "
)

// ParseCommitMeta decodes a canonical commit message back into the
// CommitMeta that produced it, reporting false for any message Message
// did not write.
//
// It is the reader half of a contract the product has so far only
// written: Subject's doc comment already calls the format "a
// machine-readable contract that ... readers match on", and CommitMeta
// records WorkspaceID and TipOID expressly "for reverse traceability".
// `sanho log` is the reader that makes those claims true.
//
// Parsing is strict, and the false return is a real answer rather than a
// failure. Canonical history is not all Sanho commits — docs writers
// commit into the canonical repository directly — so a message that does
// not match exactly is an ordinary foreign commit, and reporting a
// half-decoded `source:` for it would assert provenance nothing proved.
// This is the same discipline provenance.adoptFrom applies to a
// malformed trailer: disqualify the record, do not salvage it.
func ParseCommitMeta(message string) (CommitMeta, bool) {
	lines := trimTrailingBlanks(strings.Split(strings.ReplaceAll(message, "\r\n", "\n"), "\n"))
	if len(lines) < 3 {
		return CommitMeta{}, false
	}

	meta, subjectCount, ok := parseSubjectLine(lines[0])
	if !ok {
		return CommitMeta{}, false
	}
	// Message writes exactly one blank line between subject and body.
	if lines[1] != "" {
		return CommitMeta{}, false
	}
	if !parseSourceLine(lines[2], &meta) {
		return CommitMeta{}, false
	}

	body := lines[3:]
	if subjectCount == 0 {
		// Message omits the header entirely rather than leaving a
		// dangling one, so anything here means a different format.
		return meta, len(body) == 0
	}
	if len(body) == 0 || body[0] != commitsHeader {
		return CommitMeta{}, false
	}
	for _, line := range body[1:] {
		item, found := strings.CutPrefix(line, commitsItem)
		if !found {
			return CommitMeta{}, false
		}
		meta.Subjects = append(meta.Subjects, item)
	}
	// Subject derives its count from len(Subjects), so a message whose
	// two halves disagree was not written by Message.
	if len(meta.Subjects) != subjectCount {
		return CommitMeta{}, false
	}
	return meta, true
}

// parseSubjectLine splits "[SANHO] Publish docs from <repo>/<branch> (<N> app commits)".
//
// The count group is located from the right and the repository/branch
// boundary from the left, because only that pairing survives both real
// inputs: a branch may contain slashes (`feature/x`), while a repository
// name taken from a directory may contain spaces and parentheses.
func parseSubjectLine(line string) (CommitMeta, int, bool) {
	rest, ok := strings.CutPrefix(line, subjectPrefix)
	if !ok {
		return CommitMeta{}, 0, false
	}
	open := strings.LastIndex(rest, subjectCountSep)
	if open < 0 {
		return CommitMeta{}, 0, false
	}
	digits, ok := strings.CutSuffix(rest[open+len(subjectCountSep):], subjectSuffix)
	if !ok {
		return CommitMeta{}, 0, false
	}
	count, err := strconv.Atoi(digits)
	// Atoi also accepts a sign and leading zeros, and Subject renders
	// neither: it writes strconv.Itoa(len(Subjects)). Requiring the
	// canonical form back keeps "-0", "+1" and "01" out — each of which
	// would otherwise be reported as a publication and given a source no
	// Sanho publication ever wrote. count < 0 is still checked because
	// "-1" does survive the round trip.
	if err != nil || count < 0 || strconv.Itoa(count) != digits {
		return CommitMeta{}, 0, false
	}
	source := rest[:open]
	slash := strings.Index(source, "/")
	if slash < 0 {
		return CommitMeta{}, 0, false
	}
	return CommitMeta{RepoName: source[:slash], Branch: source[slash+1:]}, count, true
}

// parseSourceLine splits "source: <workspace-id> @ <app tip OID>".
//
// The separator is found from the right: a workspace ID is
// "<project>:<absolute path>", and a path is free to contain " @ ".
func parseSourceLine(line string, meta *CommitMeta) bool {
	source, ok := strings.CutPrefix(line, sourcePrefix)
	if !ok {
		return false
	}
	sep := strings.LastIndex(source, sourceSeparator)
	if sep < 0 {
		return false
	}
	meta.WorkspaceID = source[:sep]
	meta.TipOID = source[sep+len(sourceSeparator):]
	return meta.WorkspaceID != "" && provenance.OIDPattern.MatchString(meta.TipOID)
}

// trimTrailingBlanks drops the empty strings Split leaves behind for the
// message's final newline, and any git added normalizing it.
func trimTrailingBlanks(lines []string) []string {
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
