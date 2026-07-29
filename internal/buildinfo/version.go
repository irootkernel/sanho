package buildinfo

import (
	"runtime/debug"
	"strings"
)

const DevelopmentVersion = "dev"

// ResolveVersion prefers an injected release version and otherwise uses the
// module version recorded by go install.
func ResolveVersion(injected string) string {
	moduleVersion := ""
	if info, ok := debug.ReadBuildInfo(); ok {
		moduleVersion = info.Main.Version
	}
	return resolveVersion(injected, moduleVersion)
}

func resolveVersion(injected, moduleVersion string) string {
	if version := strings.TrimSpace(injected); version != "" && version != DevelopmentVersion {
		return version
	}
	if version := strings.TrimSpace(moduleVersion); version != "" && version != "(devel)" {
		return version
	}
	return DevelopmentVersion
}
