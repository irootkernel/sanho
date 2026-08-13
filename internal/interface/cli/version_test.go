package cli

import (
	"bytes"
	"testing"
)

func TestVersionTextIsCompact(t *testing.T) {
	cmd := newVersionCmd(BuildInfo{Version: "v0.2.6"})
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute version: %v", err)
	}

	const want = "sanho v0.2.6\n"
	if got := stdout.String(); got != want {
		t.Fatalf("version output = %q, want %q", got, want)
	}
}

func TestVersionJSONIsCompact(t *testing.T) {
	cmd := newVersionCmd(BuildInfo{Version: "v0.2.6"})
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute version --json: %v", err)
	}

	const want = "{\"name\":\"sanho\",\"version\":\"v0.2.6\"}\n"
	if got := stdout.String(); got != want {
		t.Fatalf("version --json output = %q, want %q", got, want)
	}
}
