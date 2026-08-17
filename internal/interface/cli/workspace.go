package cli

import (
	"fmt"
	"os"

	"github.com/irootkernel/sanho/internal/infra/registry"

	"github.com/spf13/cobra"
)

func newWorkspaceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workspace",
		Short: "Manage workspace registrations",
		RunE:  runGroup,
	}
	cmd.AddCommand(newWorkspaceForgetCmd())
	return cmd
}

func newWorkspaceForgetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "forget <workspace-id>",
		Short: "Remove a stale workspace registration",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkspaceForget(cmd, args[0])
		},
	}
}

func runWorkspaceForget(cmd *cobra.Command, workspaceID string) error {
	file, err := openRegistry()
	if err != nil {
		return err
	}
	var localPath string
	if err := updateRegistry(cmd.Context(), file, func(state *registry.State) error {
		workspace, ok := state.Workspaces[workspaceID]
		if !ok {
			return fmt.Errorf("workspace %q is not registered", workspaceID)
		}
		localPath = workspace.LocalPath
		if _, statErr := os.Stat(localPath); statErr == nil {
			return fmt.Errorf("workspace %q still exists at %s; live workspace registrations must be removed through workspace cleanup", workspaceID, localPath)
		} else if !os.IsNotExist(statErr) {
			return fmt.Errorf("inspect workspace %q at %s: %w", workspaceID, localPath, statErr)
		}
		delete(state.Workspaces, workspaceID)
		return nil
	}); err != nil {
		return err
	}
	writef(cmd.OutOrStdout(), "sanho: forgot stale workspace %s (%s)\n", workspaceID, localPath)
	return nil
}
