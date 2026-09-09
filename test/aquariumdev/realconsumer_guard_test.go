package aquariumdev

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type realConsumerGuardCase struct {
	name       string
	unameS     string
	unameM     string
	goos       string
	goarch     string
	root       string
	revision   string
	diagnostic string
	dispatch   bool
}

// TestRealConsumerPrerequisites exercises the Make boundary rather than the
// Darwin-only Go test. The command stubs are process-local, so these cases do
// not mutate the caller's Go environment and prove rejected inputs never
// dispatch `go test`.
func TestRealConsumerPrerequisites(t *testing.T) {
	repository := repositoryRoot(t)
	validRoot := t.TempDir()
	validRevision := strings.Repeat("a", 40)
	cases := []realConsumerGuardCase{
		{
			name:       "unsupported host",
			unameS:     "Linux",
			unameM:     "x86_64",
			goos:       "darwin",
			goarch:     "arm64",
			root:       validRoot,
			revision:   validRevision,
			diagnostic: "requires Darwin arm64",
		},
		{
			name:       "Go target mismatch",
			unameS:     "Darwin",
			unameM:     "arm64",
			goos:       "darwin",
			goarch:     "amd64",
			root:       validRoot,
			revision:   validRevision,
			diagnostic: "requires a Darwin arm64 Go target",
		},
		{
			name:       "Go OS mismatch",
			unameS:     "Darwin",
			unameM:     "arm64",
			goos:       "linux",
			goarch:     "arm64",
			root:       validRoot,
			revision:   validRevision,
			diagnostic: "requires a Darwin arm64 Go target",
		},
		{
			name:       "absent root",
			unameS:     "Darwin",
			unameM:     "arm64",
			goos:       "darwin",
			goarch:     "arm64",
			revision:   validRevision,
			diagnostic: "SANHO_AQUARIUM_ROOT must name the absolute Aquarium checkout",
		},
		{
			name:       "relative root",
			unameS:     "Darwin",
			unameM:     "arm64",
			goos:       "darwin",
			goarch:     "arm64",
			root:       "relative/aquarium",
			revision:   validRevision,
			diagnostic: "SANHO_AQUARIUM_ROOT must name the absolute Aquarium checkout",
		},
		{
			name:       "absent revision",
			unameS:     "Darwin",
			unameM:     "arm64",
			goos:       "darwin",
			goarch:     "arm64",
			root:       validRoot,
			diagnostic: "SANHO_AQUARIUM_REVISION must be a full lowercase commit SHA",
		},
		{
			name:       "malformed revision",
			unameS:     "Darwin",
			unameM:     "arm64",
			goos:       "darwin",
			goarch:     "arm64",
			root:       validRoot,
			revision:   "not-a-sha",
			diagnostic: "SANHO_AQUARIUM_REVISION must be a full lowercase commit SHA",
		},
		{
			name:       "uppercase revision",
			unameS:     "Darwin",
			unameM:     "arm64",
			goos:       "darwin",
			goarch:     "arm64",
			root:       validRoot,
			revision:   strings.Repeat("A", 40),
			diagnostic: "SANHO_AQUARIUM_REVISION must be a full lowercase commit SHA",
		},
		{
			name:       "short revision",
			unameS:     "Darwin",
			unameM:     "arm64",
			goos:       "darwin",
			goarch:     "arm64",
			root:       validRoot,
			revision:   strings.Repeat("a", 39),
			diagnostic: "SANHO_AQUARIUM_REVISION must be a full lowercase commit SHA",
		},
		{
			name:       "long revision",
			unameS:     "Darwin",
			unameM:     "arm64",
			goos:       "darwin",
			goarch:     "arm64",
			root:       validRoot,
			revision:   strings.Repeat("a", 41),
			diagnostic: "SANHO_AQUARIUM_REVISION must be a full lowercase commit SHA",
		},
		{
			name:     "valid prerequisites dispatch consumer test",
			unameS:   "Darwin",
			unameM:   "arm64",
			goos:     "darwin",
			goarch:   "arm64",
			root:     validRoot,
			revision: validRevision,
			dispatch: true,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := runRealConsumerGuard(t, repository, testCase)
			if testCase.dispatch {
				if result.exitCode != 0 {
					t.Fatalf("valid guard exit code = %d, want 0\nstderr: %s", result.exitCode, result.stderr)
				}
				if !result.goTestDispatched {
					t.Fatal("valid prerequisites did not dispatch go test")
				}
				if !strings.Contains(result.goTestMarker, "SANHO_REALCONSUMER=1\n") {
					t.Fatalf("go test marker did not record the opt-in environment: %q", result.goTestMarker)
				}
				wantArguments := "ARG=test\nARG=./test/aquariumdev\nARG=-run\nARG=^(TestRealConsumer|TestConsumerEnvironmentStripsInheritedGit|TestTimedMakePreservesGitIsolation)$\nARG=-count=1\nARG=-v\nARG=-timeout\nARG=70m\n"
				if !strings.Contains(result.goTestMarker, wantArguments) {
					t.Fatalf("go test marker arguments = %q, want %q", result.goTestMarker, wantArguments)
				}
				return
			}
			if result.exitCode != 2 {
				t.Fatalf("guard exit code = %d, want 2\nstderr: %s", result.exitCode, result.stderr)
			}
			if result.stdout != "" {
				t.Fatalf("guard wrote stdout on rejection: %q", result.stdout)
			}
			if !strings.Contains(result.stderr, testCase.diagnostic) {
				t.Fatalf("guard diagnostic = %q, want substring %q", result.stderr, testCase.diagnostic)
			}
			if result.goTestDispatched {
				t.Fatal("rejected prerequisite dispatched go test")
			}
		})
	}
}

type realConsumerGuardResult struct {
	exitCode         int
	stdout           string
	stderr           string
	goTestDispatched bool
	goTestMarker     string
}

func runRealConsumerGuard(t *testing.T, repository string, testCase realConsumerGuardCase) realConsumerGuardResult {
	t.Helper()
	binDir := t.TempDir()
	marker := filepath.Join(binDir, "go-test-dispatched")
	writeExecutable(t, filepath.Join(binDir, "uname"), `#!/bin/sh
set -eu
case "${1:-}" in
    -s) printf '%s\n' "${SANHO_GUARD_UNAME_S}" ;;
    -m) printf '%s\n' "${SANHO_GUARD_UNAME_M}" ;;
    *) exit 97 ;;
esac
`)
	writeExecutable(t, filepath.Join(binDir, "go"), `#!/bin/sh
set -eu
case "${1:-}" in
    env)
        case "${2:-}" in
            GOOS) printf '%s\n' "${SANHO_GUARD_GOOS}" ;;
            GOARCH) printf '%s\n' "${SANHO_GUARD_GOARCH}" ;;
            *) exit 98 ;;
        esac
        ;;
    test)
        {
            printf 'SANHO_REALCONSUMER=%s\n' "${SANHO_REALCONSUMER:-}"
            for argument in "$@"; do
                printf 'ARG=%s\n' "$argument"
            done
        } > "${SANHO_GUARD_TEST_MARKER}"
        if [ "${SANHO_GUARD_GO_TEST_MODE:-reject}" = pass ]; then
            exit 0
        fi
        exit 99
        ;;
    *) exit 100 ;;
esac
`)
	realMake, err := exec.LookPath("make")
	if err != nil {
		t.Fatalf("locate make: %v", err)
	}
	path := binDir + string(os.PathListSeparator) + os.Getenv("PATH")
	command := exec.Command(realMake, "-s", "-C", repository, "aquarium-dev-realconsumer")
	command.Env = environmentWithOverrides(
		"PATH="+path,
		"SANHO_GUARD_TEST_MARKER="+marker,
		"SANHO_GUARD_UNAME_S="+testCase.unameS,
		"SANHO_GUARD_UNAME_M="+testCase.unameM,
		"SANHO_GUARD_GOOS="+testCase.goos,
		"SANHO_GUARD_GOARCH="+testCase.goarch,
		"SANHO_GUARD_GO_TEST_MODE="+map[bool]string{true: "pass", false: "reject"}[testCase.dispatch],
		"SANHO_AQUARIUM_ROOT="+testCase.root,
		"SANHO_AQUARIUM_REVISION="+testCase.revision,
	)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	exitCode := 0
	if err := command.Run(); err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			exitCode = exitError.ExitCode()
		} else {
			exitCode = -1
		}
	}
	markerContent, markerErr := os.ReadFile(marker)
	if markerErr != nil && !os.IsNotExist(markerErr) {
		t.Fatalf("read go test marker: %v", markerErr)
	}
	return realConsumerGuardResult{
		exitCode:         exitCode,
		stdout:           stdout.String(),
		stderr:           stderr.String(),
		goTestDispatched: markerErr == nil,
		goTestMarker:     string(markerContent),
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatalf("write executable %s: %v", path, err)
	}
}
