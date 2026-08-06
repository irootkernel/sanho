// Package admin groups the workspace lifecycle and observation flows of
// sanho v0.2: init, clean, migrate, doctor, and the status/state
// queries (sanho-v0.2.md §5.8, §8). Like every v0.2 usecase it works
// against ports; no daemon, no network on read paths beyond the
// canonical clone's cached data.
package admin

import (
	"context"
	"time"

	"github.com/irootkernel/sanho/internal/domain/provenance"
)

// StatusReport is the data behind `sanho status` (§5.8): local facts,
// cached canonical facts with age, sync preview, and sibling rows.
type StatusReport struct {
	Project     string
	WorkspaceID string
	Base        provenance.Base
	// Head/HeadTree are from the last fetch; DataAge tells how old.
	Head        string
	HeadTree    string
	DataAge     time.Duration
	FetchedEver bool
	// Behind/Ahead are commit distances base↔head.
	Behind, Ahead int
	// SyncPreview: would `sanho sync` merge cleanly? Conflict paths when
	// not.
	SyncClean      bool
	SyncConflicts  []string
	SyncInProgress bool
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

// InitUseCase creates or adopts a workspace (§5.8 init; reuse mode as
// in v0.1). MigrateUseCase performs the §8 v0.1→v0.2 procedure.
// CleanUseCase removes a workspace (strictly read-only under DryRun —
// audit M4 regression requirement). DoctorUseCase runs the §5.8 checks.
//
// Concrete port sets are defined in P3 alongside the implementations;
// the exported names below fix the seams for command wiring.
type InitUseCase struct{}
type CleanUseCase struct{ DryRun bool }
type MigrateUseCase struct{}
type DoctorUseCase struct{ Fix bool }
type StatusQuery struct{ Refresh bool }

func (u *InitUseCase) Run(ctx context.Context) error {
	panic("unimplemented (sanho v0.2 P3)")
}

func (u *CleanUseCase) Run(ctx context.Context) error {
	panic("unimplemented (sanho v0.2 P3)")
}

func (u *MigrateUseCase) Run(ctx context.Context) error {
	panic("unimplemented (sanho v0.2 P3)")
}

func (u *DoctorUseCase) Run(ctx context.Context) error {
	panic("unimplemented (sanho v0.2 P3)")
}

func (q *StatusQuery) Run(ctx context.Context) (StatusReport, error) {
	panic("unimplemented (sanho v0.2 P3)")
}
