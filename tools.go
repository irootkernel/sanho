//go:build tools

package tools

// This file tracks tool dependencies (e.g., dev-time binaries) in go.mod/go.sum.
// See https://go.dev/wiki/Modules#how-can-i-track-tool-dependencies-for-a-module
import (
	_ "github.com/air-verse/air"
)
