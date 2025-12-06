package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newStatusCmd creates the status command skeleton.
func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the current docs synchronization status",
		Long: `Display the current workspace docs status including:
- Workspace ID
- Local docs base hash
- Server docs HEAD hash
- Synchronization status (up_to_date, outdated)
- Pending fix status`,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("kkachi status: not implemented yet (Phase 2)")
		},
	}
}
