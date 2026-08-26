package buildinfo

import (
	"runtime/debug"
	"strings"
)

const (
	// CurrentVersion is the planned stable version reported by source builds.
	CurrentVersion     = "v0.2.8"
	DevelopmentVersion = "dev"
)

// ResolveVersion prefers the configured source or build version and otherwise
// uses the module version recorded by go install.
func ResolveVersion(configured string) string {
	moduleVersion := ""
	if info, ok := debug.ReadBuildInfo(); ok {
		moduleVersion = info.Main.Version
	}
	return resolveVersion(configured, moduleVersion)
}

func resolveVersion(configured, moduleVersion string) string {
	if version := strings.TrimSpace(configured); version != "" && version != DevelopmentVersion {
		return version
	}
	if version := strings.TrimSpace(moduleVersion); version != "" && version != "(devel)" {
		return version
	}
	return DevelopmentVersion
}
