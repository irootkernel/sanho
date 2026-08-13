package cli

// `sanho status` (docs/cli-json.md) and the adapters that feed
// usecase/admin.StatusQuery.

import (
	"context"
	"fmt"
	"io"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/irootkernel/sanho/internal/domain/provenance"
	"github.com/irootkernel/sanho/internal/infra/canonical"
	"github.com/irootkernel/sanho/internal/usecase/admin"

	"github.com/spf13/cobra"
)

// statusJSON is the stable `sanho status --json` schema.
//
//	{
//	  "project":        "<project name>",
//	  "workspace_id":   "<project>:<abs path>",
//	  "base":           {"commit": "<oid>", "tree": "<oid>"} | null,
//	  "canonical": {
//	    "head":          "<oid>",         // "" when empty
//	    "tree":          "<oid>",
//	    "empty":         false,           // nothing published yet
//	    "fetched_ever":  true,
//	    "data_age_seconds": 42
//	  },
//	  "relation": {"known": true, "behind": 2, "ahead": 0},
//	  "publication": {"known": true, "pending": false},
//	  "sync_preview": {"known": true, "clean": false,
//	                   "conflicts": ["docs/api.md"]},
//	  "sync_in_progress": false,
//	  "siblings": [{"workspace_id": "...", "base_commit": "...",
//	                "base_tree": "...", "vs_mine": "behind 1",
//	                "vs_head": "same", "actor_email": "...",
//	                "last_updated_at": "RFC3339"}]
//	}
//
// Errors never appear here: they go to stderr, so stdout is either a
// complete document or nothing (the JSON contract "--json … errors to stderr").
type statusJSON struct {
	Project     string    `json:"project"`
	WorkspaceID string    `json:"workspace_id"`
	Base        *baseJSON `json:"base"`
	Canonical   struct {
		Head            string `json:"head"`
		Tree            string `json:"tree"`
		Empty           bool   `json:"empty"`
		FetchedEver     bool   `json:"fetched_ever"`
		DataAgeSeconds  int64  `json:"data_age_seconds"`
		PublicationURL  string `json:"publication_url"`
		PublicationName string `json:"publication_branch"`
	} `json:"canonical"`
	Relation struct {
		Known  bool `json:"known"`
		Behind int  `json:"behind"`
		Ahead  int  `json:"ahead"`
	} `json:"relation"`
	Publication struct {
		Known   bool `json:"known"`
		Pending bool `json:"pending"`
	} `json:"publication"`
	SyncPreview struct {
		Known     bool     `json:"known"`
		Clean     bool     `json:"clean"`
		Conflicts []string `json:"conflicts"`
	} `json:"sync_preview"`
	WorkingCopy struct {
		Known     bool `json:"known"`
		DocsClean bool `json:"docs_clean"`
	} `json:"working_copy"`
	LocalReadiness struct {
		Sync readinessJSON `json:"sync"`
		Pull readinessJSON `json:"pull"`
	} `json:"local_readiness"`
	SyncInProgress bool          `json:"sync_in_progress"`
	Siblings       []siblingJSON `json:"siblings"`
}

type readinessJSON struct {
	Ready     bool     `json:"ready"`
	BlockedBy []string `json:"blocked_by"`
}

type baseJSON struct {
	Commit string `json:"commit"`
	Tree   string `json:"tree"`
}

type siblingJSON struct {
	WorkspaceID   string    `json:"workspace_id"`
	BaseCommit    string    `json:"base_commit"`
	BaseTree      string    `json:"base_tree"`
	VsMine        string    `json:"vs_mine"`
	VsHead        string    `json:"vs_head"`
	ActorEmail    string    `json:"actor_email"`
	LastUpdatedAt time.Time `json:"last_updated_at"`
}

func newStatusCmd() *cobra.Command {
	var refresh, asJSON bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show this workspace's docs state against canonical",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runStatus(cmd, refresh, asJSON)
		},
	}
	cmd.Flags().BoolVar(&refresh, "refresh", false, "Fetch the canonical repository before reporting")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print machine-readable JSON")
	return cmd
}

func runStatus(cmd *cobra.Command, refresh, asJSON bool) error {
	ctx := cmd.Context()
	ws, err := requireV2Workspace(ctx)
	if err != nil {
		return finishCommand(cmd, nil, asJSON, err)
	}

	store, err := ws.openCanonical()
	if err != nil {
		// The clone is what every canonical fact comes from; a write
		// path recreates it (see cloneMissingMessage).
		return finishCommand(cmd, nil, asJSON, newCloneMissingError(ws.cloneDir()))
	}

	report, err := queryStatus(ctx, ws, store, refresh)
	if err != nil {
		return finishCommand(cmd, nil, asJSON, err)
	}

	if asJSON {
		return writeJSON(cmd.OutOrStdout(), buildStatusJSON(report, store))
	}
	renderStatus(cmd.OutOrStdout(), ws, store, report)
	return nil
}

func queryStatus(ctx context.Context, ws *workspace, store *canonical.Store, refresh bool) (admin.StatusReport, error) {
	query := &admin.StatusQuery{
		Canonical:   store,
		State:       ws.statePort(),
		Registry:    registryPort{ws: ws},
		Preview:     previewPort{ws: ws, store: store},
		Local:       ws.repo,
		Project:     ws.config.Project,
		WorkspaceID: ws.config.WorkspaceID,
		Refresh:     refresh,
	}
	return query.Run(ctx)
}

// --- adapters ---------------------------------------------------------

// registryPort adapts the registry to admin.RegistryPort. It excludes
// this workspace's own row: the report is about the *others*.
type registryPort struct{ ws *workspace }

func (r registryPort) Siblings(ctx context.Context) ([]admin.SiblingEntry, error) {
	file, err := openRegistry()
	if err != nil {
		return nil, err
	}
	state, err := readRegistry(ctx, file)
	if err != nil {
		return nil, err
	}

	mine := r.ws.registryKey()
	keys := make([]string, 0, len(state.Workspaces))
	for key := range state.Workspaces {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	seenPaths := make(map[string]struct{})
	var entries []admin.SiblingEntry
	for _, key := range keys {
		workspace := state.Workspaces[key]
		if key == mine || workspace.Project != r.ws.config.Project ||
			sameFilesystemPath(workspace.LocalPath, r.ws.configRoot) {
			continue
		}
		identity := workspacePathIdentity(workspace.LocalPath)
		if _, duplicate := seenPaths[identity]; duplicate {
			continue
		}
		seenPaths[identity] = struct{}{}
		entries = append(entries, admin.SiblingEntry{
			WorkspaceID:   key,
			Base:          provenance.Base{Commit: workspace.BaseCommit, Tree: workspace.BaseTree},
			ActorEmail:    workspace.ActorEmail,
			LastUpdatedAt: workspace.LastUpdatedAt,
		})
	}
	sortSiblings(entries)
	return entries, nil
}

func sortSiblings(entries []admin.SiblingEntry) {
	for i := 1; i < len(entries); i++ {
		for j := i; j > 0 && entries[j].WorkspaceID < entries[j-1].WorkspaceID; j-- {
			entries[j], entries[j-1] = entries[j-1], entries[j]
		}
	}
}

// previewPort adapts the shared the commit-hook contract prediction to admin.PreviewPort, so
// `sanho status` and the commit warning answer the same question the
// same way.
type previewPort struct {
	ws    *workspace
	store *canonical.Store
}

func (p previewPort) Preview(ctx context.Context, base provenance.Base, head, headTree string) (bool, bool, []string) {
	// `sanho status` reports on the checkout, so its local side is HEAD.
	// (The pre-commit warning asks about the index instead — it reports
	// on the commit being made, M7.)
	oursTree, err := p.ws.repo.HeadDocsTree(ctx)
	if err != nil {
		return false, false, nil
	}
	preview := previewSync(ctx, p.ws, p.store, base, head, headTree, oursTree)
	return preview.Known, preview.Clean, preview.Conflicts
}

// --- rendering --------------------------------------------------------

func renderStatus(out io.Writer, ws *workspace, store *canonical.Store, report admin.StatusReport) {
	writef(out, "workspace : %s\n", ws.root)
	writef(out, "project   : %s\n", report.Project)
	writef(out, "docs repo : %s (branch %s)\n", store.URL(), store.Branch())

	if report.HasBase {
		writef(out, "base      : %s\n", shortOID(report.Base.Commit))
	} else {
		writeln(out, "base      : (none recorded)")
	}

	switch {
	case report.CanonicalEmpty:
		writeln(out, "canonical : (no commits yet — your first push will publish docs)")
	default:
		writef(out, "canonical : %s\n", shortOID(report.Head))
	}
	writef(out, "data      : %s\n", dataAgeLine(report))

	switch {
	case report.CanonicalEmpty:
	case !report.RelationKnown:
		writeln(out, "relation  : unknown (the recorded base is not in the canonical clone)")
	default:
		writef(out, "relation  : behind %d, ahead %d\n", report.Behind, report.Ahead)
	}

	// One sync row, and during a sync it is the sync's own. The behind
	// count is still true in that window — the base deliberately stays at
	// the pre-sync value — but "N behind — 'sanho sync' will merge
	// cleanly" names a command that refuses while a note exists, which is
	// exactly the closure violation D3 forbids. The unfinished sync is
	// the more useful reading of the same state, and it names two
	// commands that do work here.
	if report.SyncInProgress {
		writef(out, "sync      : %s\n", syncNotePendingMessage("IN PROGRESS"))
	} else {
		renderSyncPreview(out, report)
	}
	if report.PublicationKnown && report.PublicationPending {
		writeln(out, "publish   : committed docs changes are pending publication")
	}
	renderWorkingCopy(out, report)
	renderReadiness(out, "sync now  ", report.SyncReadiness)
	renderReadiness(out, "pull now  ", report.PullReadiness)
	renderSiblings(out, report.Siblings)
}

func renderWorkingCopy(out io.Writer, report admin.StatusReport) {
	switch {
	case !report.WorkingCopyKnown:
		writeln(out, "working   : docs state unknown")
	case report.DocsClean:
		writeln(out, "working   : docs clean")
	default:
		writeln(out, "working   : docs have uncommitted changes")
	}
}

func renderReadiness(out io.Writer, label string, readiness admin.Readiness) {
	if readiness.Ready {
		writef(out, "%s: ready (local checks)\n", label)
		return
	}
	writef(out, "%s: blocked (%s)\n", label, readiness.BlockedBy[0])
}

// dataAgeLine always states how old the canonical view is (the guidance contract:
// degraded-mode lines always include data age).
func dataAgeLine(report admin.StatusReport) string {
	if !report.FetchedEver {
		return neverFetchedLine
	}
	if report.DataAge > staleDataThreshold {
		return staleCanonicalLine(report.DataAge)
	}
	return fmt.Sprintf("canonical data is %s old", humanizeAge(report.DataAge))
}

func renderSyncPreview(out io.Writer, report admin.StatusReport) {
	switch {
	case report.CanonicalEmpty, !report.RelationKnown:
		return
	case report.Behind == 0:
		writeln(out, "sync      : up to date")
	default:
		writef(out, "sync      : %s\n", statusBehindLine(
			report.Behind, report.SyncPreviewKnown, report.SyncClean, report.SyncConflicts))
	}
}

func renderSiblings(out io.Writer, siblings []admin.SiblingRow) {
	if len(siblings) == 0 {
		return
	}
	writeln(out, "\nsiblings:")

	table := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	writeln(table, "  WORKSPACE\tBASE\tVS MINE\tVS HEAD\tACTOR\tREPORTED")
	for _, row := range siblings {
		writef(table, "  %s\t%s\t%s\t%s\t%s\t%s\n",
			row.WorkspaceID, shortOID(row.Base.Commit), row.VsMine, row.VsHead,
			row.ActorEmail, formatTimestamp(row.LastUpdatedAt))
	}
	_ = table.Flush()
}

func formatTimestamp(at time.Time) string {
	if at.IsZero() {
		return "never"
	}
	return at.UTC().Format(time.RFC3339)
}

func buildStatusJSON(report admin.StatusReport, store *canonical.Store) statusJSON {
	out := statusJSON{Project: report.Project, WorkspaceID: report.WorkspaceID}
	if report.HasBase {
		out.Base = &baseJSON{Commit: report.Base.Commit, Tree: report.Base.Tree}
	}

	out.Canonical.Head = report.Head
	out.Canonical.Tree = report.HeadTree
	out.Canonical.Empty = report.CanonicalEmpty
	out.Canonical.FetchedEver = report.FetchedEver
	out.Canonical.DataAgeSeconds = int64(report.DataAge / time.Second)
	out.Canonical.PublicationURL = store.URL()
	out.Canonical.PublicationName = store.Branch()

	out.Relation.Known = report.RelationKnown
	out.Relation.Behind = report.Behind
	out.Relation.Ahead = report.Ahead
	out.Publication.Known = report.PublicationKnown
	out.Publication.Pending = report.PublicationPending

	out.SyncPreview.Known = report.SyncPreviewKnown
	out.SyncPreview.Clean = report.SyncClean
	out.SyncPreview.Conflicts = orEmpty(report.SyncConflicts)
	out.WorkingCopy.Known = report.WorkingCopyKnown
	out.WorkingCopy.DocsClean = report.DocsClean
	out.LocalReadiness.Sync = readinessJSON{Ready: report.SyncReadiness.Ready, BlockedBy: orEmpty(report.SyncReadiness.BlockedBy)}
	out.LocalReadiness.Pull = readinessJSON{Ready: report.PullReadiness.Ready, BlockedBy: orEmpty(report.PullReadiness.BlockedBy)}
	out.SyncInProgress = report.SyncInProgress

	out.Siblings = make([]siblingJSON, 0, len(report.Siblings))
	for _, row := range report.Siblings {
		out.Siblings = append(out.Siblings, siblingJSON{
			WorkspaceID:   row.WorkspaceID,
			BaseCommit:    row.Base.Commit,
			BaseTree:      row.Base.Tree,
			VsMine:        row.VsMine,
			VsHead:        row.VsHead,
			ActorEmail:    row.ActorEmail,
			LastUpdatedAt: row.LastUpdatedAt,
		})
	}
	return out
}
