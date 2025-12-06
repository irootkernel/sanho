package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newVersionCmd creates the version command.
func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version of kkachi CLI",
		Long:  `Print the version, commit hash, and build date of the kkachi CLI.`,
		Run: func(cmd *cobra.Command, args []string) {
			version := buildInfo.Version
			if version == "" {
				version = "dev"
			}
			commit := buildInfo.Commit
			if commit == "" {
				commit = "unknown"
			}
			buildDate := buildInfo.BuildDate
			if buildDate == "" {
				buildDate = "unknown"
			}

			cmd.Printf("kkachi version %s (commit: %s, built: %s)\n", version, commit, buildDate)
		},
	}
}

// FormatVersion returns a formatted version string for testing.
func FormatVersion(version, commit, buildDate string) string {
	if version == "" {
		version = "dev"
	}
	if commit == "" {
		commit = "unknown"
	}
	if buildDate == "" {
		buildDate = "unknown"
	}
	return fmt.Sprintf("kkachi version %s (commit: %s, built: %s)", version, commit, buildDate)
}
