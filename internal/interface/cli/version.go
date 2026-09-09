package cli

import (
	"github.com/spf13/cobra"
)

// versionJSON is the stable `sanho version --json` schema, carried over
// from v0.1 unchanged so existing scripts keep working:
//
//	{"name": "sanho", "version": "<version>"}
type versionJSON struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type verboseVersionJSON struct {
	Name    string  `json:"name"`
	Version string  `json:"version"`
	GitSHA  *string `json:"git_sha"`
}

func newVersionCmd(info BuildInfo) *cobra.Command {
	var asJSON, versionVerbose bool

	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print the sanho version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			resolved := withDefaults(info)
			if asJSON {
				if verbose || versionVerbose {
					return writeCompactJSON(cmd.OutOrStdout(), verboseVersionJSON{
						Name:    "sanho",
						Version: resolved.Version,
						GitSHA:  optionalGitSHA(resolved.GitSHA),
					})
				}
				return writeCompactJSON(cmd.OutOrStdout(), versionJSON{Name: "sanho", Version: resolved.Version})
			}
			if verbose || versionVerbose {
				sha := resolved.GitSHA
				if sha == "" {
					sha = "unknown"
				}
				writef(cmd.OutOrStdout(), "sanho %s (git_sha %s)\n", resolved.Version, sha)
				return nil
			}
			writef(cmd.OutOrStdout(), "sanho %s\n", resolved.Version)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print machine-readable JSON")
	cmd.Flags().BoolVarP(&versionVerbose, "verbose", "v", false, "Print the exact build commit")
	return cmd
}

// withDefaults ensures a build without version information stays explicit.
func withDefaults(info BuildInfo) BuildInfo {
	if info.Version == "" {
		info.Version = "dev"
	}
	return info
}

func optionalGitSHA(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
