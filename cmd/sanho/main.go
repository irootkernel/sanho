// Package main is the entry point for the Sanho CLI.
package main

import (
	"github.com/irootkernel/sanho/internal/buildinfo"
	"github.com/irootkernel/sanho/internal/interface/cli"
)

// version defaults to the planned stable version and remains overridable via
// ldflags for explicit release verification builds.
var version = buildinfo.CurrentVersion

// gitSHA is injected by exact-commit development builds. Ordinary source and
// release builds leave it empty, which the verbose version diagnostic reports
// as unknown.
var gitSHA string

func main() {
	cli.Execute(cli.BuildInfo{
		Version: buildinfo.ResolveVersion(version),
		GitSHA:  gitSHA,
	})
}
