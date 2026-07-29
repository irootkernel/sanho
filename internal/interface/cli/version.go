package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
)

type versionJSONOutput struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// newVersionCmd creates the version command.
func newVersionCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print the version of sanho CLI",
		Long:  `Print the version, commit hash, and build date of the sanho CLI.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			version := buildInfo.Version
			if version == "" {
				version = "dev"
			}
			if jsonOutput {
				if err := writeJSON(cmd.OutOrStdout(), versionJSONOutput{
					Name:    "sanho",
					Version: version,
				}); err != nil {
					return withErrorCode("internal_error", errors.Join(ErrInternal, err))
				}
				return nil
			}

			commit := buildInfo.Commit
			if commit == "" {
				commit = "unknown"
			}
			buildDate := buildInfo.BuildDate
			if buildDate == "" {
				buildDate = "unknown"
			}

			cmd.Printf("sanho version %s (commit: %s, built: %s)\n", version, commit, buildDate)
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Print machine-readable JSON")
	return cmd
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
	return fmt.Sprintf("sanho version %s (commit: %s, built: %s)", version, commit, buildDate)
}
