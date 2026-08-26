package main

import (
	"testing"

	"github.com/irootkernel/sanho/internal/buildinfo"
)

func TestVersionDefaultsToCurrentVersion(t *testing.T) {
	if version != buildinfo.CurrentVersion {
		t.Fatalf("version = %q, want current version %q", version, buildinfo.CurrentVersion)
	}
}
