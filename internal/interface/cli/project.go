package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newProjectCmd creates the project parent command.
func newProjectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Manage projects on the kkachi-server",
		Long:  `Commands for managing projects registered with the kkachi-server.`,
	}

	cmd.AddCommand(newProjectAddCmd())
	cmd.AddCommand(newProjectDeleteCmd())

	return cmd
}

// newProjectAddCmd creates the project add command skeleton.
func newProjectAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Register a project with the kkachi-server",
		Long: `Register a new project and its associated docs repository with the kkachi-server.

This command requires:
- Project name
- Docs repository URL`,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("kkachi project add: not implemented yet (Phase 2)")
		},
	}

	// Flags will be added in Phase 2
	// --project
	// --docs-repo-url
	// --server-url

	return cmd
}

// newProjectDeleteCmd creates the project delete command skeleton.
func newProjectDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Remove a project from the kkachi-server",
		Long: `Remove a project from the kkachi-server.

Note: This does not delete any local directories or files.
Workspaces associated with this project will no longer function.`,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("kkachi project delete: not implemented yet (Phase 2)")
		},
	}

	// Flags will be added in Phase 2
	// --project
	// --force

	return cmd
}
