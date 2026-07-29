package cli

import (
	"context"
	"errors"
	"os"
	"sort"
	"time"

	"github.com/spf13/cobra"

	"github.com/irootkernel/sanho/internal/domain/docs"
	"github.com/irootkernel/sanho/internal/infra/fs"
	"github.com/irootkernel/sanho/internal/infra/httpclient"
)

// stateTimeout is the timeout for state operations.
const stateTimeout = 30 * time.Second

type stateJSONOutput struct {
	Scope      string               `json:"scope"`
	Project    *string              `json:"project"`
	DocsHeads  map[string]string    `json:"docs_heads"`
	Workspaces []stateJSONWorkspace `json:"workspaces"`
}

type stateJSONWorkspace struct {
	WorkspaceID    string  `json:"workspace_id"`
	Project        string  `json:"project"`
	DocsHash       string  `json:"docs_hash"`
	LastReportedAt *string `json:"last_reported_at"`
	LastActor      *string `json:"last_actor"`
}

// newStateCmd creates the state command.
func newStateCmd() *cobra.Command {
	var showAll bool
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "state",
		Short: "Query daemon state for registered projects and workspaces",
		Long: `Query the sanhod for the current state of docs HEAD
and registered workspaces.

By default, shows only the current project's state.
Use --all to see all projects.

Outside a sanho workspace, --all uses the configured or default daemon socket.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStateCommand(cmd, showAll, jsonOutput)
		},
	}

	cmd.Flags().BoolVar(&showAll, "all", false, "Show all projects and workspaces")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Print machine-readable JSON")

	return cmd
}

// runStateCommand executes the sanho state logic.
func runStateCommand(cmd *cobra.Command, showAll bool, jsonOutput bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), stateTimeout)
	defer cancel()

	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		if !jsonOutput {
			cmd.PrintErrf("sanho state: failed to get current directory: %v\n", err)
		}
		return withErrorCode("internal_error", err)
	}

	// Load config to get daemon socket path and project
	configLoader := fs.NewFileConfigLoader()
	config, err := configLoader.Load(cwd)

	var socketPath string
	var currentProject docs.ProjectName

	if err != nil {
		// Config not found
		if showAll && errors.Is(err, fs.ErrConfigNotFound) {
			socketPath = ""
		} else if errors.Is(err, fs.ErrConfigNotFound) {
			if !jsonOutput {
				cmd.PrintErrf("sanho state: this directory is not a sanho workspace.\n")
				cmd.PrintErrf("Please run 'sanho init' first or use --all with --socket.\n")
			}
			return withErrorCode("not_in_workspace", err)
		} else {
			if !jsonOutput {
				cmd.PrintErrf("sanho state: failed to load config: %v\n", err)
			}
			return withErrorCode("invalid_workspace_config", err)
		}
	} else {
		// Config found
		socketPath = config.SocketPath
		currentProject = config.Project
	}

	// Create HTTP client
	httpClient, err := newDaemonClient(socketPath)
	if err != nil {
		return withErrorCode("invalid_socket_path", err)
	}

	// Get state from daemon
	var resp httpclient.StateResponse
	if showAll {
		resp, err = httpClient.GetState(ctx, nil)
	} else {
		resp, err = httpClient.GetState(ctx, &currentProject)
	}

	if err != nil {
		if errors.Is(err, httpclient.ErrUnknownProject) {
			if !jsonOutput {
				cmd.PrintErrf("sanho state: project '%s' is not registered on daemon.\n", currentProject)
				cmd.PrintErrf("Please run 'sanho project add' to register the project.\n")
			}
			return withErrorCode("unknown_project", err)
		}
		if !jsonOutput {
			cmd.PrintErrf("sanho state: failed to get state from daemon: %v\n", err)
		}
		return withErrorCode("daemon_request_failed", err)
	}

	// Output the state
	if jsonOutput {
		output := buildStateJSONOutput(showAll, currentProject, resp)
		if err := writeJSON(cmd.OutOrStdout(), output); err != nil {
			return withErrorCode("internal_error", errors.Join(ErrInternal, err))
		}
		return nil
	}
	if showAll {
		printAllState(cmd, resp)
	} else {
		printProjectState(cmd, currentProject, resp)
	}

	return nil
}

func buildStateJSONOutput(showAll bool, project docs.ProjectName, resp httpclient.StateResponse) stateJSONOutput {
	output := stateJSONOutput{
		Scope:      "all",
		DocsHeads:  make(map[string]string),
		Workspaces: make([]stateJSONWorkspace, 0, len(resp.Workspaces)),
	}
	if showAll {
		for name, head := range resp.DocsHeads {
			output.DocsHeads[name] = head
		}
	} else {
		output.Scope = "project"
		projectName := string(project)
		output.Project = &projectName
		if head, ok := resp.DocsHeads[projectName]; ok {
			output.DocsHeads[projectName] = head
		}
	}

	for _, ws := range resp.Workspaces {
		if !showAll && ws.Project != string(project) {
			continue
		}
		var lastActor *string
		if ws.LastActorEmail != "" {
			actor := ws.LastActorEmail
			lastActor = &actor
		}
		output.Workspaces = append(output.Workspaces, stateJSONWorkspace{
			WorkspaceID:    ws.WorkspaceID,
			Project:        ws.Project,
			DocsHash:       ws.DocsHash,
			LastReportedAt: ws.LastReportedAt,
			LastActor:      lastActor,
		})
	}
	sort.Slice(output.Workspaces, func(i, j int) bool {
		if output.Workspaces[i].Project != output.Workspaces[j].Project {
			return output.Workspaces[i].Project < output.Workspaces[j].Project
		}
		return output.Workspaces[i].WorkspaceID < output.Workspaces[j].WorkspaceID
	})
	return output
}

// printProjectState prints state for the current project only.
func printProjectState(cmd *cobra.Command, project docs.ProjectName, resp httpclient.StateResponse) {
	cmd.Print("sanho state:\n")
	cmd.Printf("  project: %s\n", project)

	// Get docs head for this project
	if head, ok := resp.DocsHeads[string(project)]; ok {
		cmd.Printf("  docs_head: %s\n", head)
	} else {
		cmd.Print("  docs_head: (not found)\n")
	}

	// Filter workspaces for this project
	cmd.Print("  workspaces:\n")
	count := 0
	for _, ws := range resp.Workspaces {
		if ws.Project == string(project) {
			count++
			printWorkspace(cmd, ws, "    ")
		}
	}
	if count == 0 {
		cmd.Print("    (none)\n")
	}
}

// printAllState prints state for all projects.
func printAllState(cmd *cobra.Command, resp httpclient.StateResponse) {
	cmd.Print("sanho state --all:\n")

	// Print docs heads
	cmd.Print("  docs_heads:\n")
	if len(resp.DocsHeads) == 0 {
		cmd.Print("    (none)\n")
	} else {
		for project, head := range resp.DocsHeads {
			cmd.Printf("    %s: %s\n", project, head)
		}
	}

	// Print all workspaces
	cmd.Print("  workspaces:\n")
	if len(resp.Workspaces) == 0 {
		cmd.Print("    (none)\n")
	} else {
		for _, ws := range resp.Workspaces {
			printWorkspace(cmd, ws, "    ")
		}
	}
}

// printWorkspace prints a single workspace summary.
func printWorkspace(cmd *cobra.Command, ws httpclient.WorkspaceSummary, indent string) {
	cmd.Printf("%s- workspace_id: %s\n", indent, ws.WorkspaceID)
	cmd.Printf("%s  project: %s\n", indent, ws.Project)
	cmd.Printf("%s  docs_hash: %s\n", indent, ws.DocsHash)
	if ws.LastReportedAt != nil {
		cmd.Printf("%s  last_reported_at: %s\n", indent, *ws.LastReportedAt)
	}
	if ws.LastActorEmail != "" {
		cmd.Printf("%s  last_actor: %s\n", indent, ws.LastActorEmail)
	}
}
