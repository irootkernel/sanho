package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newInitCmd creates the init command skeleton.
func newInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a workspace for kkachi",
		Long: `Initialize the current directory as a kkachi workspace.

This command will:
- Register the workspace with the kkachi-server
- Create .kkachi.json configuration file
- Install Git hooks for document synchronization`,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("kkachi init: not implemented yet (Phase 2)")
		},
	}

	// Flags will be added in Phase 2
	// --server-url
	// --project
	// --docs-dir
	// --docs-repo-url

	return cmd
}
