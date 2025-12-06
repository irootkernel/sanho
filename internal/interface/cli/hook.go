package cli

import (
	"fmt"

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

// newPreCommitHookCmd creates the pre-commit hook command skeleton.
func newPreCommitHookCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pre-commit",
		Short: "Pre-commit hook for docs synchronization",
		Long: `Invoked by Git before a commit is created.

This hook will:
- Check for docs changes
- Push docs to server if changed
- Handle outdated state with 3-way merge`,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("kkachi hook pre-commit: not implemented yet (Phase 4)")
		},
	}
}

// newPostCheckoutHookCmd creates the post-checkout hook command skeleton.
func newPostCheckoutHookCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "post-checkout",
		Short: "Post-checkout hook for status display",
		Long:  `Invoked by Git after a checkout. Displays docs status.`,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("kkachi hook post-checkout: not implemented yet (Phase 3)")
		},
	}
}

// newPostMergeHookCmd creates the post-merge hook command skeleton.
func newPostMergeHookCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "post-merge",
		Short: "Post-merge hook for status display",
		Long:  `Invoked by Git after a merge. Displays docs status.`,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("kkachi hook post-merge: not implemented yet (Phase 3)")
		},
	}
}

// newPostRewriteHookCmd creates the post-rewrite hook command skeleton.
func newPostRewriteHookCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "post-rewrite",
		Short: "Post-rewrite hook for status display",
		Long:  `Invoked by Git after a rewrite (e.g., rebase). Displays docs status.`,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("kkachi hook post-rewrite: not implemented yet (Phase 3)")
		},
	}
}

// newPrePushHookCmd creates the pre-push hook command skeleton.
func newPrePushHookCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pre-push",
		Short: "Pre-push hook for conflict and pending fix check",
		Long: `Invoked by Git before a push.

This hook will:
- Check for conflict markers in docs
- Check for pending fix state
- Block push if issues are found`,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("kkachi hook pre-push: not implemented yet (Phase 5)")
		},
	}
}

// newCommitMsgHookCmd creates the commit-msg hook command skeleton.
func newCommitMsgHookCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "commit-msg [message-file]",
		Short: "Commit-msg hook for adding docs-version tag",
		Long: `Invoked by Git when editing a commit message.

This hook will add a docs-version tag to commits that include docs changes.`,
		Args: cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("kkachi hook commit-msg: not implemented yet (Phase 4)")
		},
	}
}
