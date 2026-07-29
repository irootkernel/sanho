// Package main is the entry point for the Sanho CLI.
package main

import (
	"github.com/irootkernel/sanho/internal/interface/cli"
)

// Build-time variables injected via ldflags
var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

func main() {
	cli.Execute(cli.BuildInfo{
		Version:   version,
		Commit:    commit,
		BuildDate: buildDate,
	})
}
