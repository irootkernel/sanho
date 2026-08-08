package cli

import (
	"github.com/spf13/cobra"
)

// versionJSON is the stable `sanho version --json` schema, carried over
// from v0.1 unchanged so existing scripts keep working:
//
//	{"name": "sanho", "version": "<version>"}
//
// Commit and build date stay out of it deliberately: they are
// diagnostics for a human reading the text form, not identifiers a
// machine should branch on.
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
			writef(cmd.OutOrStdout(), "sanho version %s (commit: %s, built: %s)\n",
				resolved.Version, resolved.Commit, resolved.BuildDate)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print machine-readable JSON")
	return cmd
}

// withDefaults fills the placeholders a build without ldflags leaves
// empty, so the output never has a blank field.
func withDefaults(info BuildInfo) BuildInfo {
	if info.Version == "" {
		info.Version = "dev"
	}
	if info.Commit == "" {
		info.Commit = "unknown"
	}
	if info.BuildDate == "" {
		info.BuildDate = "unknown"
	}
	return info
}
