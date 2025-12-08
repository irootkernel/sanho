package cli

import (
	"github.com/spf13/cobra"
)

// newHookCmd creates the hook parent command.
func newHookCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hook",
		Short: "Git hook subcommands",
		Long:  `Commands that are invoked by Git hooks for document synchronization.`,
	}

	// Register hook subcommands
	cmd.AddCommand(newPreCommitHookCmd())
	cmd.AddCommand(newPostCheckoutHookCmd())
	cmd.AddCommand(newPostMergeHookCmd())
	cmd.AddCommand(newPostRewriteHookCmd())
	cmd.AddCommand(newPrePushHookCmd())
	cmd.AddCommand(newCommitMsgHookCmd())

	return cmd
}

// newPreCommitHookCmd creates the pre-commit hook command.
func newPreCommitHookCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pre-commit",
		Short: "Pre-commit hook for docs synchronization",
		Long: `Invoked by Git before a commit is created.

This hook will:
- Check for docs changes
- Push docs to server if changed
- Handle outdated state with 3-way merge`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPreCommitHook(cmd)
		},
	}
}

// newPostCheckoutHookCmd creates the post-checkout hook command.
// This is a read-only hook that displays docs status after checkout.
// It always exits with code 0 to not block Git operations.
func newPostCheckoutHookCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "post-checkout [prev-head] [new-head] [branch-flag]",
		Short: "Post-checkout hook for status display",
		Long: `Invoked by Git after a checkout. Displays docs status.

This hook will:
- Check the current docs synchronization status
- Display any pending fix or conflict warnings
- Always exit with code 0 to not block Git operations`,
		Args: cobra.MaximumNArgs(3), // Git passes: prev-HEAD, new-HEAD, branch-flag
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHookStatus(cmd, "post-checkout")
		},
	}
}

// newPostMergeHookCmd creates the post-merge hook command.
// This is a read-only hook that displays docs status after merge.
// It always exits with code 0 to not block Git operations.
func newPostMergeHookCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "post-merge [squash-flag]",
		Short: "Post-merge hook for status display",
		Long: `Invoked by Git after a merge. Displays docs status.

This hook will:
- Check the current docs synchronization status
- Display any pending fix or conflict warnings
- Always exit with code 0 to not block Git operations`,
		Args: cobra.MaximumNArgs(1), // Git passes: squash-flag
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHookStatus(cmd, "post-merge")
		},
	}
}

// newPostRewriteHookCmd creates the post-rewrite hook command.
// This is a read-only hook that displays docs status after rewrite operations.
// It only shows status for rebase operations, not for amend.
// It always exits with code 0 to not block Git operations.
func newPostRewriteHookCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "post-rewrite [rewrite-command]",
		Short: "Post-rewrite hook for status display",
		Long: `Invoked by Git after a rewrite operation (e.g., rebase, amend). Displays docs status.

This hook will:
- Check if the rewrite command is 'rebase'
- If rebase, check the current docs synchronization status
- Display any pending fix or conflict warnings
- Always exit with code 0 to not block Git operations

Note: Status is only shown for rebase operations, not for amend.`,
		Args: cobra.ArbitraryArgs, // Git may pass multiple args (rewrite-command, mapping-file, etc.)
		RunE: func(cmd *cobra.Command, args []string) error {
			// Only run status check for rebase, not for amend or other rewrites
			if len(args) == 0 || args[0] != "rebase" {
				return nil // Silent no-op for non-rebase rewrites
			}
			return runHookStatus(cmd, "post-rewrite")
		},
	}
}

// newPrePushHookCmd creates the pre-push hook command.
func newPrePushHookCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pre-push",
		Short: "Pre-push hook for conflict and pending fix check",
		Long: `Invoked by Git before a push.

This hook will:
- Check for conflict markers in docs
- Check for pending fix state
- Block push if issues are found`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPrePushHook(cmd)
		},
	}
}

// newCommitMsgHookCmd creates the commit-msg hook command.
func newCommitMsgHookCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "commit-msg [message-file]",
		Short: "Commit-msg hook for adding docs-version tag",
		Long: `Invoked by Git when editing a commit message.

This hook will add a docs-version tag to commits that include docs changes.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCommitMsgHook(cmd, args)
		},
	}
}
