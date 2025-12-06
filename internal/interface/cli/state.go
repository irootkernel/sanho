package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newStateCmd creates the state command skeleton.
func newStateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "state",
		Short: "Query server state for registered projects and workspaces",
		Long: `Query the kkachi-server for the current state of docs HEAD
and registered workspaces.

By default, shows only the current project's state.
Use --all to see all projects.`,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("kkachi state: not implemented yet (Phase 5)")
		},
	}

	// Flags will be added in Phase 5
	// --all

	return cmd
}
