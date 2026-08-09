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

func newVersionCmd(info BuildInfo) *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print the sanho version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			resolved := withDefaults(info)
			if asJSON {
				return writeCompactJSON(cmd.OutOrStdout(), versionJSON{Name: "sanho", Version: resolved.Version})
			}
			writef(cmd.OutOrStdout(), "sanho %s\n", resolved.Version)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print machine-readable JSON")
	return cmd
}

// withDefaults ensures a build without version information stays explicit.
func withDefaults(info BuildInfo) BuildInfo {
	if info.Version == "" {
		info.Version = "dev"
	}
	return info
}
