// Package main is the entry point for the Sanho CLI.
package main

import (
	"github.com/irootkernel/sanho/internal/buildinfo"
	"github.com/irootkernel/sanho/internal/interface/cli"
)

// version is injected via ldflags for checkout builds. Module builds derive it
// from Go build information instead.
var version = "dev"

func main() {
	cli.Execute(cli.BuildInfo{
		Version: buildinfo.ResolveVersion(version),
	})
}
