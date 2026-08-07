package cli

// `sanho state` (sanho-v0.2.md §5.8): the registry dump. It reads
// ~/.sanho/state.json under the shared lock — no daemon, no socket.

import (
	"context"
	"errors"
	"io"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/irootkernel/sanho/internal/infra/registry"

	"github.com/spf13/cobra"
)

// stateJSON is the stable `sanho state --json` schema:
//
//	{
//	  "home":     "<sanho home directory>",
//	  "scope":    "<project name>" | "all",
//	  "projects": [{"name": "...", "docs_repo_url": "...",
//	                "head": "<oid>"}],
//	  "workspaces": [{"workspace_id": "...", "project": "...",
//	                  "local_path": "...", "base_commit": "...",
//	                  "base_tree": "...", "actor_email": "...",
//	                  "last_updated_at": "RFC3339"}]
//	}
//
// `head` is present only when the command runs inside a workspace of
// that project — canonical heads live in per-workspace clones (§5.2), so
// there is nowhere else to read one from. It is omitted rather than
// guessed.
type stateJSON struct {
	Home       string          `json:"home"`
	Scope      string          `json:"scope"`
	Projects   []projectJSON   `json:"projects"`
	Workspaces []workspaceJSON `json:"workspaces"`
}

type projectJSON struct {
	Name        string `json:"name"`
	DocsRepoURL string `json:"docs_repo_url"`
	Head        string `json:"head,omitempty"`
}

type workspaceJSON struct {
	WorkspaceID   string    `json:"workspace_id"`
	Project       string    `json:"project"`
	LocalPath     string    `json:"local_path"`
	BaseCommit    string    `json:"base_commit"`
	BaseTree      string    `json:"base_tree"`
	ActorEmail    string    `json:"actor_email"`
	LastUpdatedAt time.Time `json:"last_updated_at"`
}

// scopeAll is the Scope value for a `--all` dump.
const scopeAll = "all"

func newStateCmd() *cobra.Command {
	var all, asJSON bool

	cmd := &cobra.Command{
		Use:   "state",
		Short: "Show the registered projects and workspaces",
		Long: `Print the cross-workspace registry (~/.sanho/state.json).

Inside a managed workspace the output is scoped to that workspace's project;
--all prints every registration.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return runState(cmd, all, asJSON) },
	}
	cmd.Flags().BoolVar(&all, "all", false, "Print every project, not just this workspace's")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print machine-readable JSON")
	return cmd
}

func runState(cmd *cobra.Command, all, asJSON bool) error {
	ctx := cmd.Context()

	file, err := openRegistry()
	if err != nil {
		return finishCommand(cmd, nil, asJSON, err)
	}
	state, err := readRegistry(ctx, file)
	if err != nil {
		return finishCommand(cmd, nil, asJSON, err)
	}

	// `sanho state` reads the registry, which exists outside any
	// workspace, so running it elsewhere is legitimate: it simply has no
	// project to scope to and no clone to read heads from.
	ws, wsErr := openWorkspace(ctx)
	if wsErr != nil && !errors.Is(wsErr, errNotWorkspace) && !errors.Is(wsErr, errV1Workspace) {
		return finishCommand(cmd, nil, asJSON, wsErr)
	}
	inWorkspace := wsErr == nil

	scope := scopeAll
	if !all && inWorkspace {
		scope = ws.config.Project
	}

	document := stateJSON{Home: file.HomeDir(), Scope: scope}
	document.Projects = collectProjects(ctx, state, scope, ws, inWorkspace)
	document.Workspaces = collectWorkspaces(state, scope)

	if asJSON {
		return writeJSON(cmd.OutOrStdout(), document)
	}
	renderState(cmd.OutOrStdout(), document)
	return nil
}

// collectProjects lists the registered projects in scope, attaching the
// canonical head only for the project of the workspace we are standing
// in.
func collectProjects(ctx context.Context, state registry.State, scope string, ws *workspace, inWorkspace bool) []projectJSON {
	projects := make([]projectJSON, 0, len(state.Projects))
	for name, project := range state.Projects {
		if scope != scopeAll && name != scope {
			continue
		}
		row := projectJSON{Name: name, DocsRepoURL: project.DocsRepoURL}
		if inWorkspace && name == ws.config.Project {
			row.Head = cachedCanonicalHead(ctx, ws)
		}
		projects = append(projects, row)
	}
	sort.Slice(projects, func(i, j int) bool { return projects[i].Name < projects[j].Name })
	return projects
}

// cachedCanonicalHead reads the last-fetched head, best effort. No
// clone, no fetch, no head — all three are ordinary states here, and
// none is worth failing a registry dump over.
func cachedCanonicalHead(ctx context.Context, ws *workspace) string {
	store := canonicalOrNil(ws)
	if store == nil {
		return ""
	}
	head, _, err := store.Head(ctx)
	if err != nil {
		return ""
	}
	return head
}

func collectWorkspaces(state registry.State, scope string) []workspaceJSON {
	project := ""
	if scope != scopeAll {
		project = scope
	}

	rows := projectWorkspaces(state, project)
	out := make([]workspaceJSON, 0, len(rows))
	for key, entry := range state.Workspaces {
		if project != "" && entry.Project != project {
			continue
		}
		out = append(out, workspaceJSON{
			WorkspaceID:   key,
			Project:       entry.Project,
			LocalPath:     entry.LocalPath,
			BaseCommit:    entry.BaseCommit,
			BaseTree:      entry.BaseTree,
			ActorEmail:    entry.ActorEmail,
			LastUpdatedAt: entry.LastUpdatedAt,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].WorkspaceID < out[j].WorkspaceID })
	return out
}

func renderState(out io.Writer, document stateJSON) {
	writef(out, "home  : %s\n", document.Home)
	writef(out, "scope : %s\n", document.Scope)

	if len(document.Projects) == 0 {
		writeln(out, "\nno projects are registered")
		return
	}

	writeln(out, "\nprojects:")
	table := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	writeln(table, "  PROJECT\tDOCS REPO\tHEAD")
	for _, project := range document.Projects {
		head := project.Head
		if head == "" {
			head = "-"
		} else {
			head = shortOID(head)
		}
		writef(table, "  %s\t%s\t%s\n", project.Name, project.DocsRepoURL, head)
	}
	_ = table.Flush()

	if len(document.Workspaces) == 0 {
		writeln(out, "\nno workspaces are registered")
		return
	}

	writeln(out, "\nworkspaces:")
	table = tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	writeln(table, "  WORKSPACE\tPROJECT\tBASE\tACTOR\tREPORTED")
	for _, entry := range document.Workspaces {
		writef(table, "  %s\t%s\t%s\t%s\t%s\n",
			entry.LocalPath, entry.Project, shortOID(entry.BaseCommit),
			entry.ActorEmail, formatTimestamp(entry.LastUpdatedAt))
	}
	_ = table.Flush()
}
