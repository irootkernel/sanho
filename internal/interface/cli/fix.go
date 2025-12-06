package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newFixCmd creates the fix command skeleton.
func newFixCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "fix",
		Short: "Complete pending docs merge and push to server",
		Long: `After resolving merge conflicts in the docs directory,
run this command to push the merged documentation to the server.

This command will:
- Verify all conflict markers are resolved
- Push the merged docs to the server
- Clear the pending fix state`,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("kkachi fix: not implemented yet (Phase 5)")
		},
	}
}
