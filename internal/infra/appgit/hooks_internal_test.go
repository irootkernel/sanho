package appgit

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGeneratedHookLinesPreserveForeignExitSemantics(t *testing.T) {
	tests := []struct {
		name       string
		foreign    string
		hook       Hook
		binary     string
		preserve   bool
		wantStatus int
	}{
		{name: "fallthrough failure", foreign: "false\n", hook: Hook{Name: "pre-commit", Line: "sanho hook pre-commit"}, binary: "/bin/true", preserve: true, wantStatus: 1},
		{name: "successful foreign hook", foreign: "true\n", hook: Hook{Name: "pre-commit", Line: "sanho hook pre-commit"}, binary: "/bin/true", preserve: true, wantStatus: 0},
		{name: "missing commit binary fails open", hook: Hook{Name: "pre-commit", Line: "sanho hook pre-commit"}, binary: "/definitely/missing/sanho", wantStatus: 0},
		{name: "missing publish binary fails closed", hook: Hook{Name: "pre-push", Line: `sanho hook pre-push "$@"`}, binary: "/definitely/missing/sanho", wantStatus: 127},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "hook")
			content := "#!/bin/sh\n" + tt.foreign + currentHookLine(tt.hook, shellQuote(tt.binary), false, tt.preserve) + "\n"
			if err := os.WriteFile(path, []byte(content), 0755); err != nil {
				t.Fatalf("write hook: %v", err)
			}
			err := exec.Command(path).Run()
			got := 0
			if err != nil {
				var exitErr *exec.ExitError
				if !errors.As(err, &exitErr) {
					t.Fatalf("run hook: %v", err)
				}
				got = exitErr.ExitCode()
			}
			if got != tt.wantStatus {
				t.Fatalf("exit status = %d, want %d\n%s", got, tt.wantStatus, content)
			}
		})
	}
}

func TestInsertHookLinePreservesDynamicExitAndConstantExit(t *testing.T) {
	tests := []struct {
		name       string
		foreign    string
		wantStatus int
	}{
		{name: "dynamic exit", foreign: "#!/bin/sh\nfalse\nexit $?\n", wantStatus: 1},
		{name: "bare exit", foreign: "#!/bin/sh\nfalse\nexit\n", wantStatus: 1},
		{name: "constant exit", foreign: "#!/bin/sh\nfalse\nexit 0\n", wantStatus: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines := strings.Split(tt.foreign, "\n")
			line := currentHookLine(Hook{Name: "pre-commit", Line: "sanho hook pre-commit"}, shellQuote("/bin/true"), false, needsStatusPreservation(lines))
			content := insertHookLine(lines, line)
			path := filepath.Join(t.TempDir(), "hook")
			if err := os.WriteFile(path, []byte(content), 0755); err != nil {
				t.Fatalf("write hook: %v", err)
			}
			err := exec.Command(path).Run()
			got := 0
			if err != nil {
				var exitErr *exec.ExitError
				if !errors.As(err, &exitErr) {
					t.Fatalf("run hook: %v", err)
				}
				got = exitErr.ExitCode()
			}
			if got != tt.wantStatus {
				t.Fatalf("exit status = %d, want %d\n%s", got, tt.wantStatus, content)
			}
		})
	}
}

func TestShellQuoteRoundTripsAnApostrophe(t *testing.T) {
	value := "/tmp/Sanho's bin/sanho"
	command := "value=" + shellQuote(value) + "; [ \"$value\" = " + shellQuote(value) + " ]"
	if err := exec.Command("/bin/sh", "-c", command).Run(); err != nil {
		t.Fatalf("quoted value did not round-trip: %v", err)
	}
	if !validHistoricalBinaryToken(shellQuote(value)) {
		t.Fatalf("recognizer rejected %q", shellQuote(value))
	}
}
