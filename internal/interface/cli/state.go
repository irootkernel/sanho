package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/SeventeenthEarth/kkachi/internal/domain/docs"
	"github.com/SeventeenthEarth/kkachi/internal/infra/fs"
	"github.com/SeventeenthEarth/kkachi/internal/infra/httpclient"
)

// stateTimeout is the timeout for state operations.
const stateTimeout = 30 * time.Second

// newStateCmd creates the state command.
func newStateCmd() *cobra.Command {
	var showAll bool
	var serverURLFlag string

	cmd := &cobra.Command{
		Use:   "state",
		Short: "Query server state for registered projects and workspaces",
		Long: `Query the kkachi-server for the current state of docs HEAD
and registered workspaces.

By default, shows only the current project's state.
Use --all to see all projects.

When using --all outside a kkachi workspace, provide --server-url.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStateCommand(cmd, showAll, serverURLFlag)
		},
	}

	cmd.Flags().BoolVar(&showAll, "all", false, "Show all projects and workspaces")
	cmd.Flags().StringVar(&serverURLFlag, "server-url", "", "Server URL (required with --all when outside a workspace)")

	return cmd
}

// runStateCommand executes the kkachi state logic.
func runStateCommand(cmd *cobra.Command, showAll bool, serverURLFlag string) error {
	ctx, cancel := context.WithTimeout(context.Background(), stateTimeout)
	defer cancel()

	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		cmd.PrintErrf("kkachi state: failed to get current directory: %v\n", err)
		return err
	}

	// Load config to get server URL and project
	configLoader := fs.NewFileConfigLoader()
	config, err := configLoader.Load(cwd)

	var serverURL string
	var currentProject docs.ProjectName

	if err != nil {
		// Config not found
		if showAll && serverURLFlag != "" {
			// --all with explicit --server-url: allow running without workspace
			serverURL = serverURLFlag
		} else if showAll {
			// --all without --server-url: suggest using --server-url
			cmd.PrintErrf("kkachi state: no .kkachi.json found.\n")
			cmd.PrintErrf("When using --all outside a workspace, provide --server-url flag.\n")
			cmd.PrintErrf("Example: kkachi state --all --server-url http://localhost:5789\n")
			return err
		} else if errors.Is(err, fs.ErrConfigNotFound) {
			cmd.PrintErrf("kkachi state: this directory is not a kkachi workspace.\n")
			cmd.PrintErrf("Please run 'kkachi init' first or use --all with --server-url.\n")
			return err
		} else {
			cmd.PrintErrf("kkachi state: failed to load config: %v\n", err)
			return err
		}
	} else {
		// Config found
		serverURL = config.ServerURL
		currentProject = config.Project
		// If --server-url provided, it overrides config
		if serverURLFlag != "" {
			serverURL = serverURLFlag
		}
	}

	// Create HTTP client
	httpClient := httpclient.NewHTTPClient(serverURL)

	// Get state from server
	var resp httpclient.StateResponse
	if showAll {
		resp, err = httpClient.GetState(ctx, nil)
	} else {
		resp, err = httpClient.GetState(ctx, &currentProject)
	}

	if err != nil {
		if errors.Is(err, httpclient.ErrUnknownProject) {
			cmd.PrintErrf("kkachi state: project '%s' is not registered on server.\n", currentProject)
			cmd.PrintErrf("Please run 'kkachi project add' to register the project.\n")
			return err
		}
		cmd.PrintErrf("kkachi state: failed to get state from server: %v\n", err)
		return err
	}

	// Output the state
	if showAll {
		printAllState(cmd, resp)
	} else {
		printProjectState(cmd, currentProject, resp)
	}

	return nil
}

// printProjectState prints state for the current project only.
func printProjectState(cmd *cobra.Command, project docs.ProjectName, resp httpclient.StateResponse) {
	fmt.Fprintf(cmd.OutOrStdout(), "kkachi state:\n")
	fmt.Fprintf(cmd.OutOrStdout(), "  project: %s\n", project)

	// Get docs head for this project
	if head, ok := resp.DocsHeads[string(project)]; ok {
		fmt.Fprintf(cmd.OutOrStdout(), "  docs_head: %s\n", head)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "  docs_head: (not found)\n")
	}

	// Filter workspaces for this project
	fmt.Fprintf(cmd.OutOrStdout(), "  workspaces:\n")
	count := 0
	for _, ws := range resp.Workspaces {
		if ws.Project == string(project) {
			count++
			printWorkspace(cmd, ws, "    ")
		}
	}
	if count == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "    (none)\n")
	}
}

// printAllState prints state for all projects.
func printAllState(cmd *cobra.Command, resp httpclient.StateResponse) {
	fmt.Fprintf(cmd.OutOrStdout(), "kkachi state --all:\n")

	// Print docs heads
	fmt.Fprintf(cmd.OutOrStdout(), "  docs_heads:\n")
	if len(resp.DocsHeads) == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "    (none)\n")
	} else {
		for project, head := range resp.DocsHeads {
			fmt.Fprintf(cmd.OutOrStdout(), "    %s: %s\n", project, head)
		}
	}

	// Print all workspaces
	fmt.Fprintf(cmd.OutOrStdout(), "  workspaces:\n")
	if len(resp.Workspaces) == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "    (none)\n")
	} else {
		for _, ws := range resp.Workspaces {
			printWorkspace(cmd, ws, "    ")
		}
	}
}

// printWorkspace prints a single workspace summary.
func printWorkspace(cmd *cobra.Command, ws httpclient.WorkspaceSummary, indent string) {
	fmt.Fprintf(cmd.OutOrStdout(), "%s- workspace_id: %s\n", indent, ws.WorkspaceID)
	fmt.Fprintf(cmd.OutOrStdout(), "%s  project: %s\n", indent, ws.Project)
	fmt.Fprintf(cmd.OutOrStdout(), "%s  docs_hash: %s\n", indent, ws.DocsHash)
	if ws.LastReportedAt != nil {
		fmt.Fprintf(cmd.OutOrStdout(), "%s  last_reported_at: %s\n", indent, *ws.LastReportedAt)
	}
	if ws.LastActorEmail != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "%s  last_actor: %s\n", indent, ws.LastActorEmail)
	}
}
