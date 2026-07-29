package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunVersion(t *testing.T) {
	previousVersion := version
	version = "v1.2.3"
	t.Cleanup(func() { version = previousVersion })

	var output bytes.Buffer
	if err := run([]string{"--version"}, &output); err != nil {
		t.Fatalf("run(--version) error = %v", err)
	}
	if got := strings.TrimSpace(output.String()); got != "sanhod version v1.2.3" {
		t.Fatalf("version output = %q", got)
	}
}
