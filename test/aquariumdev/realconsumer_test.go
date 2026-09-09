//go:build darwin && arm64

package aquariumdev

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/irootkernel/sanho/internal/buildinfo"
)

const producerProbeLimit = 30 * time.Second

type managerResult struct {
	Schema    string         `json:"schema"`
	Operation string         `json:"operation"`
	Status    string         `json:"status"`
	ProjectID string         `json:"project_id"`
	Message   string         `json:"message"`
	Details   map[string]any `json:"details"`
}

type managerError struct {
	Schema string `json:"schema"`
	Error  struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type nativeInvocation struct {
	Arguments []string
	ExitCode  int
	Stdout    string
	Stderr    string
	Duration  time.Duration
}

// TestRealConsumer is opt-in because the portable repository checks cannot
// assume that another checkout is available. The dedicated Make target
// supplies both environment variables and refuses to run without them.
func TestRealConsumer(t *testing.T) {
	if os.Getenv("SANHO_REALCONSUMER") != "1" {
		t.Skip("real Aquarium consumer check is opt-in; run make aquarium-dev-realconsumer")
	}
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("the Aquarium development consumer supports Darwin arm64 only")
	}

	consumerRoot := requiredAbsolutePath(t, "SANHO_AQUARIUM_ROOT")
	consumerRevision := os.Getenv("SANHO_AQUARIUM_REVISION")
	if len(consumerRevision) != 40 || strings.Trim(consumerRevision, "0123456789abcdef") != "" {
		t.Fatalf("SANHO_AQUARIUM_REVISION must be a full lowercase commit SHA")
	}
	if got := gitOutput(t, consumerRoot, "rev-parse", "HEAD"); got != consumerRevision {
		t.Fatalf("Aquarium checkout revision = %q, want %q", got, consumerRevision)
	}
	if status := gitOutput(t, consumerRoot, "status", "--porcelain=v1", "--untracked-files=all"); status != "" {
		t.Fatalf("Aquarium checkout is dirty:\n%s", status)
	}
	consumerCLI := filepath.Join(consumerRoot, "plugins", "aquarium", "tools", "aquarium-dev", "aquarium_dev.py")
	if info, err := os.Stat(consumerCLI); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("Aquarium CLI is unavailable at %s: %v", consumerCLI, err)
	}

	fixture := fixtureRepository(t)
	// This ignored, invalid Go file exists only in the original checkout. The
	// manager's exact clone must build the admitted tree without seeing it.
	extraSource := filepath.Join(fixture, "internal", "buildinfo", "consumer-only.go")
	appendFile(t, filepath.Join(fixture, ".git", "info", "exclude"), "\ninternal/buildinfo/consumer-only.go\n")
	writeFile(t, extraSource, "package buildinfo\nthis is not valid Go\n")
	if status := gitOutput(t, fixture, "status", "--porcelain=v1", "--untracked-files=all"); status != "" {
		t.Fatalf("ignored consumer-only input made fixture dirty: %q", status)
	}

	workspace := t.TempDir()
	hostRoot := filepath.Join(workspace, "aquarium-dev")
	if err := os.Mkdir(hostRoot, 0o700); err != nil {
		t.Fatalf("create manager root: %v", err)
	}
	t.Cleanup(func() { clearImmutableTree(t, hostRoot) })

	realMake, err := exec.LookPath("make")
	if err != nil {
		t.Fatalf("locate make: %v", err)
	}
	wrapperDir := filepath.Join(workspace, "bin")
	if err := os.Mkdir(wrapperDir, 0o700); err != nil {
		t.Fatalf("create wrapper directory: %v", err)
	}
	writeMakeWrapper(t, wrapperDir, realMake)
	environment := consumerEnvironment(wrapperDir, realMake, "")

	describe, describeDuration := timedMake(t, fixture, "aquarium-dev-describe", "", environment)
	if describe != fmt.Sprintf(`{"schema":"aquarium-dev-producer-description/v1","project_id":"sanho","next_version":"%s","artifact_kind":"executable","artifact_path":"bin/sanho"}`, buildinfo.CurrentVersion) {
		t.Fatalf("realconsumer describe = %q", describe)
	}
	coldOutput := filepath.Join(workspace, "cold-build")
	if err := os.Mkdir(coldOutput, 0o700); err != nil {
		t.Fatalf("create cold build output: %v", err)
	}
	coldManifestText, coldBuildDuration := timedMake(t, fixture, "aquarium-dev-build", coldOutput, environment)
	var coldManifest artifactManifest
	if err := json.Unmarshal([]byte(coldManifestText), &coldManifest); err != nil {
		t.Fatalf("decode cold build manifest %q: %v", coldManifestText, err)
	}
	fixtureSHA := gitOutput(t, fixture, "rev-parse", "HEAD")
	if coldManifest.GitSHA != fixtureSHA || coldManifest.DevelopmentVersion != buildinfo.CurrentVersion+"-dev."+fixtureSHA[:12] {
		t.Fatalf("cold build identity = %+v, want SHA %q", coldManifest, fixtureSHA)
	}
	if runtimeOutput := runBinary(t, filepath.Join(coldOutput, coldManifest.ArtifactPath), "version", "--verbose", "--json"); runtimeOutput.GitSHA != fixtureSHA || runtimeOutput.Version != coldManifest.DevelopmentVersion {
		t.Fatalf("cold build runtime identity = %+v, want version %q and SHA %q", runtimeOutput, coldManifest.DevelopmentVersion, fixtureSHA)
	}
	t.Logf("producer timing: go=%s git=%s platform=%s/%s dependency-cache=cold-per-output describe=%s build=%s", commandVersion(t, "go", "version"), commandVersion(t, "git", "--version"), runtime.GOOS, runtime.GOARCH, describeDuration, coldBuildDuration)

	enroll := runNative(t, consumerRoot, consumerCLI, hostRoot, environment, "enroll", "--repository", fixture, "--approve-enrollment", "--approve-hook")
	requireNativeSuccess(t, enroll)

	buildStart := time.Now()
	firstBuild := runNative(t, consumerRoot, consumerCLI, hostRoot, environment, "rebuild", "--repository", fixture, "--approve-build")
	firstBuild.Duration = time.Since(buildStart)
	firstDetails := requireNativeSuccess(t, firstBuild)
	firstSHA := requiredDetail(t, firstDetails, "git_sha")
	if firstSHA != fixtureSHA {
		t.Fatalf("native build git_sha = %q, want %q", firstSHA, fixtureSHA)
	}
	firstArtifact := requiredDetail(t, firstDetails, "artifact")
	firstDigest := requiredDetail(t, firstDetails, "sha256")
	assertPublishedGeneration(t, hostRoot, firstSHA, firstArtifact, firstDigest)
	t.Logf("native consumer timing: rebuild=%s manager-probe=30s full-build=600s", firstBuild.Duration)

	currentTarget := readCurrentTarget(t, hostRoot)
	for _, testCase := range []struct {
		name string
		mode string
		code string
	}{
		{name: "malformed manifest", mode: "malformed", code: "producer_manifest_invalid"},
		{name: "wrong SHA", mode: "sha", code: "producer_manifest_invalid"},
		{name: "checksum mismatch", mode: "checksum", code: "checksum_mismatch"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			result := runNative(t, consumerRoot, consumerCLI, hostRoot, consumerEnvironment(wrapperDir, realMake, testCase.mode), "rebuild", "--repository", fixture, "--approve-build")
			if result.ExitCode == 0 {
				t.Fatalf("invalid producer output unexpectedly passed: %s", result.Stdout)
			}
			if got := nativeErrorCode(t, result); got != testCase.code {
				t.Fatalf("consumer error code = %q, want %q", got, testCase.code)
			}
			if got := readCurrentTarget(t, hostRoot); got != currentTarget {
				t.Fatalf("failed publication changed current selector from %q to %q", currentTarget, got)
			}
			assertPublishedGeneration(t, hostRoot, firstSHA, firstArtifact, firstDigest)
			if matches, err := filepath.Glob(filepath.Join(hostRoot, "artifacts", "sanho", ".staging-*")); err != nil {
				t.Fatalf("inspect staging: %v", err)
			} else if len(matches) != 0 {
				t.Fatalf("failed publication left staging directories: %v", matches)
			}
		})
	}

	marker := filepath.Join(fixture, "consumer-second-commit.txt")
	writeFile(t, marker, "second committed generation\n")
	gitRun(t, fixture, "add", filepath.Base(marker))
	gitCommit(t, fixture, "second consumer generation")
	secondSHA := gitOutput(t, fixture, "rev-parse", "HEAD")
	secondBuild := runNative(t, consumerRoot, consumerCLI, hostRoot, environment, "rebuild", "--repository", fixture, "--approve-build")
	requireNativeSuccess(t, secondBuild)
	if got := readCurrentTarget(t, hostRoot); !strings.Contains(got, secondSHA) {
		t.Fatalf("second build current selector = %q, want SHA %q", got, secondSHA)
	}
	cleanupOld := runNative(t, consumerRoot, consumerCLI, hostRoot, environment, "cleanup", "--project-id", "sanho", "--git-sha", firstSHA)
	requireNativeSuccess(t, cleanupOld)
	retiredGeneration := filepath.Join(hostRoot, "artifacts", "sanho", firstSHA)
	if _, err := os.Lstat(retiredGeneration); err == nil {
		t.Fatalf("retired generation still exists after cleanup: %s", retiredGeneration)
	} else if !os.IsNotExist(err) {
		t.Fatalf("inspect retired generation %s: %v", retiredGeneration, err)
	}
	currentGeneration := filepath.Join(hostRoot, "artifacts", "sanho", secondSHA)
	if entry, err := os.Lstat(currentGeneration); err != nil {
		t.Fatalf("inspect current generation %s: %v", currentGeneration, err)
	} else if entry.Mode()&os.ModeSymlink != 0 || !entry.IsDir() {
		t.Fatalf("current generation is not a directory: %s", currentGeneration)
	}
	cleanupCurrent := runNative(t, consumerRoot, consumerCLI, hostRoot, environment, "cleanup", "--project-id", "sanho", "--git-sha", secondSHA)
	currentDetails := requireNativeSuccess(t, cleanupCurrent)
	cleanupStatus, ok := currentDetails["current"].(bool)
	if !ok || !cleanupStatus {
		t.Fatalf("cleanup of current generation did not report current=true: %+v", currentDetails)
	}
	if got := readCurrentTarget(t, hostRoot); !strings.Contains(got, secondSHA) {
		t.Fatalf("cleanup changed selected generation: %q", got)
	}

	if status := gitOutput(t, consumerRoot, "status", "--porcelain=v1", "--untracked-files=all"); status != "" {
		t.Fatalf("real consumer check changed external Aquarium checkout:\n%s", status)
	}
}

func TestConsumerEnvironmentStripsInheritedGit(t *testing.T) {
	expectedRoot := filepath.Join(t.TempDir(), "expected")
	hostileRoot := filepath.Join(t.TempDir(), "hostile")
	for _, repository := range []string{expectedRoot, hostileRoot} {
		if err := os.Mkdir(repository, 0o700); err != nil {
			t.Fatalf("create disposable repository: %v", err)
		}
		gitRun(t, repository, "init", "--quiet", "-b", "main")
		writeFile(t, filepath.Join(repository, "README.md"), "disposable\n")
		gitRun(t, repository, "add", "README.md")
		gitCommit(t, repository, "disposable repository")
	}

	hostileConfig := filepath.Join(t.TempDir(), "gitconfig")
	hostileHooks := filepath.Join(t.TempDir(), "hostile-hooks")
	writeFile(t, hostileConfig, "[core]\n\thooksPath = "+hostileHooks+"\n")
	t.Setenv("GIT_DIR", filepath.Join(hostileRoot, ".git"))
	t.Setenv("GIT_WORK_TREE", hostileRoot)
	t.Setenv("GIT_CONFIG_GLOBAL", hostileConfig)
	t.Setenv("GIT_CONFIG_SYSTEM", hostileConfig)
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "core.hooksPath")
	t.Setenv("GIT_CONFIG_VALUE_0", hostileHooks)

	realMake, err := exec.LookPath("make")
	if err != nil {
		t.Fatalf("locate make: %v", err)
	}
	environment := consumerEnvironment(t.TempDir(), realMake, "")
	rootCommand := exec.Command("git", "-C", expectedRoot, "rev-parse", "--show-toplevel")
	rootCommand.Env = environment
	rootOutput, err := rootCommand.Output()
	if err != nil {
		t.Fatalf("resolve expected repository with consumer environment: %v", err)
	}
	gotRoot, err := filepath.EvalSymlinks(strings.TrimSpace(string(rootOutput)))
	if err != nil {
		t.Fatalf("resolve reported repository root: %v", err)
	}
	wantRoot, err := filepath.EvalSymlinks(expectedRoot)
	if err != nil {
		t.Fatalf("resolve expected repository root: %v", err)
	}
	if gotRoot != wantRoot {
		t.Fatalf("consumer Git root = %q, want %q", gotRoot, wantRoot)
	}

	configCommand := exec.Command("git", "-C", expectedRoot, "config", "--get", "core.hooksPath")
	configCommand.Env = environment
	configOutput, err := configCommand.CombinedOutput()
	if err == nil || strings.TrimSpace(string(configOutput)) != "" {
		t.Fatalf("consumer Git config inherited hostile hooksPath: exit=%v output=%q", err, configOutput)
	}
}

func TestTimedMakePreservesGitIsolation(t *testing.T) {
	repository := filepath.Join(t.TempDir(), "producer")
	if err := os.Mkdir(repository, 0o700); err != nil {
		t.Fatalf("create producer repository: %v", err)
	}
	gitRun(t, repository, "init", "--quiet", "-b", "main")
	writeFile(t, filepath.Join(repository, "Makefile"), `SHELL := /bin/bash

.PHONY: check-git-isolation

check-git-isolation:
	@set -euo pipefail; \
		repository="$$(git -C . rev-parse --show-toplevel)"; \
		if [[ "$$repository" != "$$(pwd -P)" ]]; then \
			printf '%s\n' 'hostile Git repository environment leaked' >&2; \
			exit 1; \
		fi; \
		if git -C . config --get core.hooksPath >/dev/null 2>&1; then \
			printf '%s\n' 'hostile Git config leaked' >&2; \
			exit 1; \
		fi
`)

	hostileRoot := filepath.Join(t.TempDir(), "hostile")
	if err := os.Mkdir(hostileRoot, 0o700); err != nil {
		t.Fatalf("create hostile repository: %v", err)
	}
	gitRun(t, hostileRoot, "init", "--quiet", "-b", "main")
	hostileConfig := filepath.Join(t.TempDir(), "gitconfig")
	hostileHooks := filepath.Join(t.TempDir(), "hostile-hooks")
	writeFile(t, hostileConfig, "[core]\n\thooksPath = "+hostileHooks+"\n")
	tracePath := filepath.Join(t.TempDir(), "git-trace")
	trace2Path := filepath.Join(t.TempDir(), "git-trace2")
	t.Setenv("GIT_DIR", filepath.Join(hostileRoot, ".git"))
	t.Setenv("GIT_WORK_TREE", hostileRoot)
	t.Setenv("GIT_CONFIG_GLOBAL", hostileConfig)
	t.Setenv("GIT_CONFIG_SYSTEM", hostileConfig)
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "core.hooksPath")
	t.Setenv("GIT_CONFIG_VALUE_0", hostileHooks)
	t.Setenv("GIT_TRACE", tracePath)
	t.Setenv("GIT_TRACE2_EVENT", trace2Path)

	realMake, err := exec.LookPath("make")
	if err != nil {
		t.Fatalf("locate make: %v", err)
	}
	environment := consumerEnvironment(t.TempDir(), realMake, "")
	if _, duration := timedMake(t, repository, "check-git-isolation", t.TempDir(), environment); duration <= 0 {
		t.Fatal("timed Make returned a non-positive duration")
	}
	for _, path := range []string{tracePath, trace2Path} {
		if _, err := os.Stat(path); err == nil {
			t.Fatalf("hostile Git trace was written to %s", path)
		} else if !os.IsNotExist(err) {
			t.Fatalf("inspect hostile Git trace %s: %v", path, err)
		}
	}
}

func requiredAbsolutePath(t *testing.T, name string) string {
	t.Helper()
	value := os.Getenv(name)
	if value == "" || !filepath.IsAbs(value) {
		t.Fatalf("%s must be an absolute path", name)
	}
	info, err := os.Stat(value)
	if err != nil || !info.IsDir() {
		t.Fatalf("%s must name an existing directory: %v", name, err)
	}
	return filepath.Clean(value)
}

func consumerEnvironment(wrapperDir, realMake, mode string) []string {
	path := wrapperDir + string(os.PathListSeparator) + os.Getenv("PATH")
	return environmentWithOverridesFrom(isolatedGitEnv(),
		"HOME="+filepath.Join(wrapperDir, "home"),
		"PATH="+path,
		"PYTHONDONTWRITEBYTECODE=1",
		"SANHO_TEST_MAKE_MODE="+mode,
		"SANHO_REAL_MAKE="+realMake,
	)
}

func writeMakeWrapper(t *testing.T, directory, realMake string) {
	t.Helper()
	wrapper := filepath.Join(directory, "make")
	content := `#!/bin/sh
set -eu
is_build=0
is_dry=0
for argument in "$@"; do
    [ "$argument" = "aquarium-dev-build" ] && is_build=1
    [ "$argument" = "-n" ] && is_dry=1
done
output=$("$SANHO_REAL_MAKE" "$@") || {
    status=$?
    printf '%s\n' "$output"
    exit "$status"
}
if [ "$is_build" -eq 1 ] && [ "$is_dry" -eq 0 ] && [ -n "${SANHO_TEST_MAKE_MODE:-}" ]; then
    case "$SANHO_TEST_MAKE_MODE" in
        malformed)
            printf '%s\n' '{"malformed":true}'
            ;;
        sha)
            printf '%s\n' "$output" | sed -E 's/"git_sha":"[0-9a-f]{40}"/"git_sha":"0000000000000000000000000000000000000000"/'
            ;;
        checksum)
            printf '%s\n' "$output" | sed -E 's/sha256:[0-9a-f]{64}/sha256:0000000000000000000000000000000000000000000000000000000000000000/'
            ;;
        *)
            printf '%s\n' "$output"
            ;;
    esac
else
    printf '%s\n' "$output"
fi
`
	writeFile(t, wrapper, content)
	if err := os.Chmod(wrapper, 0o700); err != nil {
		t.Fatalf("make wrapper permissions: %v", err)
	}
}

func runNative(t *testing.T, consumerRoot, consumerCLI, hostRoot string, environment []string, arguments ...string) nativeInvocation {
	t.Helper()
	commandArguments := append([]string{"-B", consumerCLI, "--host-root", hostRoot}, arguments...)
	command := exec.Command("python3", commandArguments...)
	command.Dir = consumerRoot
	command.Env = environment
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	started := time.Now()
	err := command.Run()
	exitCode := 0
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			exitCode = exitError.ExitCode()
		} else {
			exitCode = -1
		}
	}
	return nativeInvocation{
		Arguments: append([]string{"python3"}, commandArguments...),
		ExitCode:  exitCode,
		Stdout:    stdout.String(),
		Stderr:    stderr.String(),
		Duration:  time.Since(started),
	}
}

func requireNativeSuccess(t *testing.T, invocation nativeInvocation) map[string]any {
	t.Helper()
	if invocation.ExitCode != 0 {
		t.Fatalf("native command %s exited %d: %s", strings.Join(invocation.Arguments, " "), invocation.ExitCode, invocation.Stderr)
	}
	var result managerResult
	if err := json.Unmarshal([]byte(invocation.Stdout), &result); err != nil {
		t.Fatalf("decode native success for %s: %v (%q)", strings.Join(invocation.Arguments, " "), err, invocation.Stdout)
	}
	if result.Schema != "aquarium-dev-manager-result/v1" {
		t.Fatalf("native result schema = %q", result.Schema)
	}
	return result.Details
}

func nativeErrorCode(t *testing.T, invocation nativeInvocation) string {
	t.Helper()
	var result managerError
	if err := json.Unmarshal([]byte(invocation.Stderr), &result); err != nil {
		t.Fatalf("decode native error for %s: %v (%q)", strings.Join(invocation.Arguments, " "), err, invocation.Stderr)
	}
	if result.Schema != "aquarium-dev-error/v1" {
		t.Fatalf("native error schema = %q", result.Schema)
	}
	return result.Error.Code
}

func requiredDetail(t *testing.T, details map[string]any, name string) string {
	t.Helper()
	value, ok := details[name].(string)
	if !ok || value == "" {
		t.Fatalf("native result detail %q is absent: %+v", name, details)
	}
	return value
}

func assertPublishedGeneration(t *testing.T, hostRoot, gitSHA, artifact, digest string) {
	t.Helper()
	artifactsRoot := filepath.Join(hostRoot, "artifacts", "sanho")
	entries, err := os.ReadDir(artifactsRoot)
	if err != nil {
		t.Fatalf("enumerate published generations: %v", err)
	}
	if len(entries) != 1 || !entries[0].IsDir() || entries[0].Name() != gitSHA {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("published generations = %v, want exactly [%s]", names, gitSHA)
	}
	current := filepath.Join(hostRoot, "current", "sanho")
	resolved, err := filepath.EvalSymlinks(current)
	if err != nil {
		t.Fatalf("resolve current selector: %v", err)
	}
	wantGeneration, err := filepath.EvalSymlinks(filepath.Join(hostRoot, "artifacts", "sanho", gitSHA))
	if err != nil {
		t.Fatalf("resolve published generation: %v", err)
	}
	if resolved != wantGeneration {
		t.Fatalf("current = %s, want %s", resolved, wantGeneration)
	}
	artifactResolved, err := filepath.EvalSymlinks(artifact)
	if err != nil {
		t.Fatalf("resolve artifact detail: %v", err)
	}
	if artifactResolved != filepath.Join(wantGeneration, "bin", "sanho") {
		t.Fatalf("artifact detail = %q", artifact)
	}
	manifestBytes, err := os.ReadFile(filepath.Join(wantGeneration, ".aquarium-manifest.json"))
	if err != nil {
		t.Fatalf("read published manifest: %v", err)
	}
	var manifest artifactManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatalf("decode published manifest: %v", err)
	}
	artifactBytes, err := os.ReadFile(artifact)
	if err != nil {
		t.Fatalf("read published artifact: %v", err)
	}
	hash := sha256.Sum256(artifactBytes)
	wantDigest := "sha256:" + hex.EncodeToString(hash[:])
	if manifest.GitSHA != gitSHA || manifest.SHA256 != wantDigest || digest != wantDigest {
		t.Fatalf("published identity = %+v, details digest = %q, want SHA %q and digest %q", manifest, digest, gitSHA, wantDigest)
	}
	command, err := filepath.EvalSymlinks(filepath.Join(hostRoot, "bin", "sanho"))
	if err != nil || command != filepath.Join(wantGeneration, "bin", "sanho") {
		t.Fatalf("command selector = %s, want %s (err=%v)", command, filepath.Join(wantGeneration, "bin", "sanho"), err)
	}
	runtimeVersion := runBinary(t, artifact, "version", "--verbose", "--json")
	if runtimeVersion.Name != "sanho" || runtimeVersion.Version != manifest.DevelopmentVersion || runtimeVersion.GitSHA != gitSHA {
		t.Fatalf("published runtime identity = %+v, manifest = %+v", runtimeVersion, manifest)
	}
}

func readCurrentTarget(t *testing.T, hostRoot string) string {
	t.Helper()
	target, err := os.Readlink(filepath.Join(hostRoot, "current", "sanho"))
	if err != nil {
		t.Fatalf("read current selector: %v", err)
	}
	return target
}

func timedMake(t *testing.T, repository, target, output string, environment []string) (string, time.Duration) {
	t.Helper()
	arguments := []string{"-s", "-C", repository, target}
	if output != "" {
		environment = environmentWithOverridesFrom(environment, "AQUARIUM_DEV_OUTPUT="+output)
	}
	command := exec.Command("make", arguments...)
	command.Env = environment
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	started := time.Now()
	if err := command.Start(); err != nil {
		t.Fatalf("start timed %s: %v", target, err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	timer := time.NewTimer(producerProbeLimit)
	defer timer.Stop()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("make %s failed after %s: %v\n%s", target, time.Since(started), err, stderr.String())
		}
	case <-timer.C:
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGTERM)
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		<-done
		t.Fatalf("make %s exceeded the %s Sanho compatibility limit", target, producerProbeLimit)
	}
	return strings.TrimSuffix(stdout.String(), "\n"), time.Since(started)
}

func runBinary(t *testing.T, path string, arguments ...string) verboseVersion {
	t.Helper()
	result := exec.Command(path, arguments...)
	output, err := result.Output()
	if err != nil {
		t.Fatalf("run %s: %v", path, err)
	}
	var identity verboseVersion
	if err := json.Unmarshal(output, &identity); err != nil {
		t.Fatalf("decode %s identity %q: %v", path, output, err)
	}
	return identity
}

func commandVersion(t *testing.T, command string, argument string) string {
	t.Helper()
	output, err := exec.Command(command, argument).Output()
	if err != nil {
		t.Fatalf("run %s %s: %v", command, argument, err)
	}
	return strings.TrimSpace(string(output))
}

func clearImmutableTree(t *testing.T, root string) {
	t.Helper()
	var paths []string
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		paths = append(paths, path)
		return nil
	}); err != nil {
		t.Logf("walk disposable manager root for cleanup: %v", err)
		return
	}
	for index := len(paths) - 1; index >= 0; index-- {
		path := paths[index]
		entry, err := os.Lstat(path)
		if err != nil || entry.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if err := syscall.Chflags(path, 0); err != nil {
			t.Logf("clear immutable flag on %s: %v", path, err)
		}
		if entry.IsDir() {
			_ = os.Chmod(path, 0o700)
		} else {
			_ = os.Chmod(path, 0o600)
		}
	}
}
