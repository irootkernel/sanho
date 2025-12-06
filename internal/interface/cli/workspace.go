package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newWorkspaceCmd creates the workspace parent command.
func newWorkspaceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workspace",
		Short: "Manage workspaces on the kkachi-server",
		Long:  `Commands for managing workspaces registered with the kkachi-server.`,
	}

	cmd.AddCommand(newWorkspaceRegisterCmd())
	cmd.AddCommand(newWorkspaceUnregisterCmd())

	return cmd
}

// newWorkspaceRegisterCmd creates the workspace register command skeleton.
func newWorkspaceRegisterCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "register [path]",
		Short: "Register a workspace with the kkachi-server",
		Long: `Register a workspace directory with the kkachi-server.

This is an alternative to 'kkachi init' for registering workspaces
without creating local configuration files.`,
		Args: cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("kkachi workspace register: not implemented yet (Phase 2)")
		},
	}

	// Flags will be added in Phase 2
	// --project
	// --server-url

	return cmd
}

// newWorkspaceUnregisterCmd creates the workspace unregister command skeleton.
func newWorkspaceUnregisterCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unregister",
		Short: "Remove a workspace registration from the kkachi-server",
		Long: `Remove a workspace registration from the kkachi-server.

Note: This does not delete any local files. The local .kkachi.json
and other configuration files will remain.`,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("kkachi workspace unregister: not implemented yet (Phase 2)")
		},
	}

	// Flags will be added in Phase 2
	// --workspace-id

	return cmd
}
