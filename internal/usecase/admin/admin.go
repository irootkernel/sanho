// Package admin holds the status computation of sanho v0.2
// (sanho-v0.2.md §5.8): what `sanho status` reports about a workspace,
// its canonical relation, and its siblings.
//
// # Why only status lives here
//
// P3 originally sketched this package as the home of init, clean,
// migrate, doctor and status alike. Only status remained, and the split
// follows the audit's invariant-placement principle: a layer earns its
// place when it holds an *invariant* worth testing independently of how
// it is driven — not merely because a flow is long.
//
// Status is invariant-bearing. "Behind means canonical has commits the
// base lacks", "a sibling relation is unknown unless both bases resolve
// in this clone", "a preview is only trustworthy when the merge actually
// ran" — these are rules with a right and a wrong answer, they are
// shared by the text and --json renderings, and they can be driven
// through states (an empty canonical, an orphaned base, a clone that has
// never fetched) that are awkward to reach through a real repository.
// They belong behind ports.
//
// Init, clean, migrate and doctor are *lifecycle glue*: ordered
// filesystem and git effects — write this file, clone that repository,
// install those hooks, print a summary — whose correctness is the order
// itself and whose only real test is the end-to-end one. Wrapping them
// in ports would produce interfaces with a single implementation and a
// single caller, and would move the ordering (the part that can actually
// be wrong) into the adapter anyway. They live in interface/cli, where
// the effects are, and are covered by the black-box CLI suite. The v0.1
// InitUseCase/CleanUseCase/MigrateUseCase/DoctorUseCase stubs are
// deleted rather than implemented for this reason.
package admin

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/irootkernel/sanho/internal/domain/provenance"
	pubdom "github.com/irootkernel/sanho/internal/domain/publish"
)

// CanonicalPort is the canonical-clone behavior status needs. Every
// method except Fetch is local: `sanho status` answers from the last
// fetch unless --refresh was asked for (§5.2 offline behavior).
type CanonicalPort interface {
	// Fetch updates the clone from origin. Only --refresh calls it.
	Fetch(ctx context.Context) error
	// Head returns the last-fetched canonical head and its docs tree.
	// A canonical repository with no commits reports
	// domain/publish.ErrEmptyBranch.
	Head(ctx context.Context) (commit, tree string, err error)
	// Age reports how old the last successful fetch is; ok is false when
	// the clone has never fetched.
	Age() (age time.Duration, ok bool)
	// ResolveCommit reports whether an OID exists in the clone.
	ResolveCommit(ctx context.Context, oid string) (bool, error)
	// Distance returns how far a is behind and ahead of b.
	Distance(ctx context.Context, a, b string) (behind, ahead int, err error)
}

// StatePort is the workspace-local state status reads.
type StatePort interface {
	LoadBase() (provenance.Base, bool, error)
	SyncInProgress() (bool, error)
}

// RegistryPort supplies the sibling rows.
type RegistryPort interface {
	// Siblings lists the project's other registered workspaces, i.e.
	// every entry except the one keyed by this workspace.
	Siblings(ctx context.Context) ([]SiblingEntry, error)
}

// SiblingEntry is one registry row before its relations are computed.
type SiblingEntry struct {
	WorkspaceID   string
	Base          provenance.Base
	ActorEmail    string
	LastUpdatedAt time.Time
}

// PreviewPort predicts what `sanho sync` would do (§5.6 step 2). It is a
// port of its own because the prediction is optional: when it cannot be
// computed the report degrades to a behind count rather than failing.
type PreviewPort interface {
	// Preview reports whether a sync from base toward head would merge
	// cleanly. known is false when no prediction could be made.
	Preview(ctx context.Context, base provenance.Base, head, headTree string) (known, clean bool, conflicts []string)
}

// LocalPort supplies the committed docs tree at the workspace's HEAD.
// It is deliberately local-only: publication pending is meaningful even
// when canonical cannot be refreshed or resolved.
type LocalPort interface {
	HeadDocsTree(ctx context.Context) (string, error)
}

// DetectPublication compares committed local docs with the last safe
// base only when that comparison has a stable meaning. During a sync the
// base intentionally remains behind, and without a base there is no
// publication checkpoint to compare.
func DetectPublication(ctx context.Context, local LocalPort, base provenance.Base, hasBase, syncInProgress bool) (known, pending bool) {
	if local == nil || !hasBase || syncInProgress {
		return false, false
	}
	localTree, err := local.HeadDocsTree(ctx)
	if err != nil {
		return false, false
	}
	return true, localTree != base.Tree
}

// Relation values for the sibling table.
const (
	RelationSame     = "same"
	RelationDiverged = "diverged"
	RelationUnknown  = "unknown"
)

// StatusReport is the data behind `sanho status` (§5.8): local facts,
// cached canonical facts with age, sync preview, and sibling rows.
type StatusReport struct {
	Project     string
	WorkspaceID string
	Base        provenance.Base
	// HasBase is false for a workspace that has never recorded one —
	// a fresh init against an empty canonical (§5.3 bootstrap).
	HasBase bool
	// Head/HeadTree are from the last fetch; DataAge tells how old.
	Head        string
	HeadTree    string
	DataAge     time.Duration
	FetchedEver bool
	// CanonicalEmpty reports a canonical repository nothing has ever
	// published into. Head is then empty and no relation exists.
	CanonicalEmpty bool
	// Behind/Ahead are commit distances base↔head, meaningful only when
	// RelationKnown. A base the clone cannot resolve (history rewritten,
	// or simply not fetched) yields RelationKnown false rather than a
	// fabricated zero.
	Behind, Ahead int
	RelationKnown bool
	// SyncPreview: would `sanho sync` merge cleanly? Conflict paths when
	// not. SyncPreviewKnown false means no prediction was possible.
	SyncClean        bool
	SyncConflicts    []string
	SyncPreviewKnown bool
	SyncInProgress   bool
	// PublicationKnown is true when a recorded base exists, no sync owns
	// the worktree, and HEAD's docs tree could be read. Pending then means
	// local committed docs differ from the last safely published base.
	PublicationKnown   bool
	PublicationPending bool
	// Siblings are other registered workspaces of the project.
	Siblings []SiblingRow
}

// SiblingRow is one registry entry rendered against this clone.
type SiblingRow struct {
	WorkspaceID   string
	Base          provenance.Base
	VsMine        string // same | ahead N | behind N | diverged | unknown
	VsHead        string
	ActorEmail    string
	LastUpdatedAt time.Time
}

// StatusQuery answers `sanho status` (§5.8).
type StatusQuery struct {
	Canonical CanonicalPort
	State     StatePort
	Registry  RegistryPort
	Preview   PreviewPort
	Local     LocalPort

	Project     string
	WorkspaceID string
	// Refresh fetches before reading, turning the cached answer into a
	// current one.
	Refresh bool
}

// Run assembles the report.
//
// Everything after the base file is best-effort in one specific sense:
// a fact that cannot be established is reported as *unknown* rather than
// guessed or raised as an error. That is the §5.2 asymmetry — read paths
// serve what they have with an explicit staleness signal — and it is
// what makes `sanho status` work offline, on a workspace whose clone was
// never created, and on one whose base was orphaned by a rewrite.
//
// Only two things are hard errors: an unreadable base file (fail closed
// — never guess a base) and a --refresh the user explicitly asked for
// that could not reach origin.
func (q *StatusQuery) Run(ctx context.Context) (StatusReport, error) {
	if q.Refresh {
		if err := q.Canonical.Fetch(ctx); err != nil {
			return StatusReport{}, err
		}
	}

	report := StatusReport{Project: q.Project, WorkspaceID: q.WorkspaceID}
	report.DataAge, report.FetchedEver = q.Canonical.Age()

	base, hasBase, err := q.State.LoadBase()
	if err != nil {
		return StatusReport{}, err
	}
	report.Base, report.HasBase = base, hasBase

	if report.SyncInProgress, err = q.State.SyncInProgress(); err != nil {
		return StatusReport{}, err
	}
	report.PublicationKnown, report.PublicationPending =
		DetectPublication(ctx, q.Local, base, hasBase, report.SyncInProgress)

	head, headTree, err := q.Canonical.Head(ctx)
	switch {
	case err == nil:
		report.Head, report.HeadTree = head, headTree
	case errors.Is(err, pubdom.ErrEmptyBranch):
		report.CanonicalEmpty = true
	default:
		return StatusReport{}, err
	}

	if report.Head != "" && hasBase {
		q.fillRelation(ctx, &report, base)
	}
	if siblings, err := q.Registry.Siblings(ctx); err == nil {
		report.Siblings = q.siblingRows(ctx, siblings, base, hasBase, report.Head)
	}
	return report, nil
}

// fillRelation computes behind/ahead and the sync preview, both of which
// require the base to exist in this clone.
func (q *StatusQuery) fillRelation(ctx context.Context, report *StatusReport, base provenance.Base) {
	known, err := q.Canonical.ResolveCommit(ctx, base.Commit)
	if err != nil || !known {
		return
	}

	behind, ahead, err := q.Canonical.Distance(ctx, base.Commit, report.Head)
	if err != nil {
		return
	}
	report.Behind, report.Ahead, report.RelationKnown = behind, ahead, true

	if q.Preview != nil {
		report.SyncPreviewKnown, report.SyncClean, report.SyncConflicts =
			q.Preview.Preview(ctx, base, report.Head, report.HeadTree)
	}
}

// siblingRows renders each registry entry against this clone.
func (q *StatusQuery) siblingRows(ctx context.Context, entries []SiblingEntry, mine provenance.Base, hasMine bool, head string) []SiblingRow {
	rows := make([]SiblingRow, 0, len(entries))
	for _, entry := range entries {
		row := SiblingRow{
			WorkspaceID:   entry.WorkspaceID,
			Base:          entry.Base,
			ActorEmail:    entry.ActorEmail,
			LastUpdatedAt: entry.LastUpdatedAt,
			VsMine:        RelationUnknown,
			VsHead:        RelationUnknown,
		}
		if hasMine {
			row.VsMine = q.relation(ctx, entry.Base.Commit, mine.Commit)
		}
		row.VsHead = q.relation(ctx, entry.Base.Commit, head)
		rows = append(rows, row)
	}
	return rows
}

// relation describes where a sits relative to b, or "unknown" when this
// clone cannot place them.
//
// Unknown is a real answer, not a fallback. Registry entries are
// *reports* from other checkouts (D4): a sibling may have published from
// a machine whose commits this clone has not fetched, and inventing
// "same" or "behind 0" for a commit it cannot resolve would present a
// guess as an observation.
func (q *StatusQuery) relation(ctx context.Context, a, b string) string {
	if a == "" || b == "" {
		return RelationUnknown
	}
	if a == b {
		return RelationSame
	}
	for _, oid := range []string{a, b} {
		known, err := q.Canonical.ResolveCommit(ctx, oid)
		if err != nil || !known {
			return RelationUnknown
		}
	}

	behind, ahead, err := q.Canonical.Distance(ctx, a, b)
	if err != nil {
		return RelationUnknown
	}
	return describeDistance(behind, ahead)
}

// describeDistance turns a (behind, ahead) pair into the §5.8 vocabulary.
func describeDistance(behind, ahead int) string {
	switch {
	case behind == 0 && ahead == 0:
		return RelationSame
	case behind > 0 && ahead > 0:
		return RelationDiverged
	case ahead > 0:
		return "ahead " + strconv.Itoa(ahead)
	default:
		return "behind " + strconv.Itoa(behind)
	}
}
