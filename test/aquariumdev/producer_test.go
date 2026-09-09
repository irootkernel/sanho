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
	"testing"

	"github.com/irootkernel/sanho/internal/buildinfo"
)

type artifactManifest struct {
	Schema             string `json:"schema"`
	ProjectID          string `json:"project_id"`
	GitSHA             string `json:"git_sha"`
	DevelopmentVersion string `json:"development_version"`
	ArtifactKind       string `json:"artifact_kind"`
	ArtifactPath       string `json:"artifact_path"`
	SHA256             string `json:"sha256"`
}

type verboseVersion struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	GitSHA  string `json:"git_sha"`
}

func TestProducerContract(t *testing.T) {
	fixture := fixtureRepository(t)

	describeBefore := gitOutput(t, fixture, "status", "--porcelain=v1", "--untracked-files=all")
	description := runMake(t, fixture, "aquarium-dev-describe", "")
	wantDescription := fmt.Sprintf(`{"schema":"aquarium-dev-producer-description/v1","project_id":"sanho","next_version":"%s","artifact_kind":"executable","artifact_path":"bin/sanho"}`, buildinfo.CurrentVersion)
	if description != wantDescription {
		t.Fatalf("description = %q, want %q", description, wantDescription)
	}
	if got := gitOutput(t, fixture, "status", "--porcelain=v1", "--untracked-files=all"); got != describeBefore {
		t.Fatalf("describe changed checkout status from %q to %q", describeBefore, got)
	}

	// An ignored source file in a package would break a normal worktree build.
	// The producer must archive the committed tree, so this file is absent from
	// the build without making the checkout dirty.
	ignored := filepath.Join(fixture, "internal", "buildinfo", "ignored.go")
	appendFile(t, filepath.Join(fixture, ".git", "info", "exclude"), "\ninternal/buildinfo/ignored.go\n")
	writeFile(t, ignored, "package buildinfo\nthis is not valid Go\n")
	if status := gitOutput(t, fixture, "status", "--porcelain=v1", "--untracked-files=all"); status != "" {
		t.Fatalf("ignored source made fixture dirty: %q", status)
	}
	fixtureBeforeBuild := pathSnapshot(t, fixture)
	ignoredBeforeBuild := pathSnapshot(t, ignored)

	output := t.TempDir()
	manifestText := runMake(t, fixture, "aquarium-dev-build", output)
	assertProducerInputsUnchanged(t, fixture, fixtureBeforeBuild, ignored, ignoredBeforeBuild)
	var manifest artifactManifest
	if err := json.Unmarshal([]byte(manifestText), &manifest); err != nil {
		t.Fatalf("decode build manifest %q: %v", manifestText, err)
	}
	if manifest.Schema != "aquarium-dev-artifact-manifest/v1" || manifest.ProjectID != "sanho" || manifest.ArtifactKind != "executable" || manifest.ArtifactPath != "bin/sanho" {
		t.Fatalf("unexpected manifest identity: %+v", manifest)
	}
	gitSHA := gitOutput(t, fixture, "rev-parse", "HEAD")
	if manifest.GitSHA != gitSHA {
		t.Fatalf("manifest git_sha = %q, want %q", manifest.GitSHA, gitSHA)
	}
	if want := buildinfo.CurrentVersion + "-dev." + gitSHA[:12]; manifest.DevelopmentVersion != want {
		t.Fatalf("development_version = %q, want %q", manifest.DevelopmentVersion, want)
	}

	artifact := filepath.Join(output, "bin", "sanho")
	artifactBytes, err := os.ReadFile(artifact)
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	digest := sha256.Sum256(artifactBytes)
	if manifest.SHA256 != "sha256:"+hex.EncodeToString(digest[:]) {
		t.Fatalf("manifest sha256 = %q, want sha256:%s", manifest.SHA256, hex.EncodeToString(digest[:]))
	}
	assertOnlyArtifact(t, output, artifact)

	if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
		var runtimeVersion verboseVersion
		run := exec.Command(artifact, "version", "--verbose", "--json")
		runtimeOutput, err := run.Output()
		if err != nil {
			t.Fatalf("run built artifact: %v", err)
		}
		if err := json.Unmarshal(runtimeOutput, &runtimeVersion); err != nil {
			t.Fatalf("decode runtime version %q: %v", runtimeOutput, err)
		}
		if runtimeVersion.Name != "sanho" || runtimeVersion.Version != manifest.DevelopmentVersion || runtimeVersion.GitSHA != gitSHA {
			t.Fatalf("runtime identity = %+v, want version %q and SHA %q", runtimeVersion, manifest.DevelopmentVersion, gitSHA)
		}
	}

	versionSource := filepath.Join(fixture, "internal", "buildinfo", "version.go")
	versionSourceBefore := mustReadFile(t, versionSource)
	writeFile(t, versionSource, "package buildinfo\n\nconst CurrentVersion = \"invalid\"\n")
	assertTargetFails(t, fixture, "aquarium-dev-describe", "invalid worktree CurrentVersion", t.TempDir(), "internal/buildinfo/version.go has no valid CurrentVersion")
	writeFile(t, versionSource, versionSourceBefore)

	// Git redirect variables belong to the caller's environment. They must not
	// redirect repository discovery, index reads, object writes, or Git traces
	// outside the producer output.
	redirectRoot := t.TempDir()
	redirectBefore := pathSnapshot(t, redirectRoot)
	redirectOutput := t.TempDir()
	redirectManifest := runMakeWithEnv(t, fixture, "aquarium-dev-build", redirectOutput,
		"GIT_DIR="+filepath.Join(redirectRoot, "git-dir"),
		"GIT_WORK_TREE="+filepath.Join(redirectRoot, "work-tree"),
		"GIT_COMMON_DIR="+filepath.Join(redirectRoot, "common-dir"),
		"GIT_INDEX_FILE="+filepath.Join(redirectRoot, "index"),
		"GIT_OBJECT_DIRECTORY="+filepath.Join(redirectRoot, "objects"),
		"GIT_ALTERNATE_OBJECT_DIRECTORIES="+filepath.Join(redirectRoot, "alternates"),
		"GIT_CONFIG_GLOBAL="+filepath.Join(redirectRoot, "global-config"),
		"GIT_CONFIG_SYSTEM="+filepath.Join(redirectRoot, "system-config"),
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=core.hooksPath",
		"GIT_CONFIG_VALUE_0="+filepath.Join(redirectRoot, "hooks"),
		"GIT_TEMPLATE_DIR="+filepath.Join(redirectRoot, "template"),
		"GIT_TRACE="+filepath.Join(redirectRoot, "trace"),
		"GIT_TRACE2_EVENT="+filepath.Join(redirectRoot, "trace2"),
	)
	if !strings.Contains(redirectManifest, `"schema":"aquarium-dev-artifact-manifest/v1"`) {
		t.Fatalf("Git redirect environment changed the build: %q", redirectManifest)
	}
	if got := pathSnapshot(t, redirectRoot); got != redirectBefore {
		t.Fatalf("Git redirect environment wrote outside the producer output: %q", got)
	}
	assertProducerInputsUnchanged(t, fixture, fixtureBeforeBuild, ignored, ignoredBeforeBuild)

	sentinel := filepath.Join(t.TempDir(), "outside-sentinel")
	writeFile(t, sentinel, "preserve\n")
	sentinelBefore := mustReadFile(t, sentinel)
	nonempty := t.TempDir()
	writeFile(t, filepath.Join(nonempty, "existing"), "keep\n")
	assertBuildFails(t, fixture, "non-empty output", nonempty, "AQUARIUM_DEV_OUTPUT must be empty", sentinel, sentinelBefore)

	writeFile(t, filepath.Join(fixture, "untracked.go"), "package main\nfunc untrackedInput() {}\n")
	assertBuildFails(t, fixture, "untracked source", t.TempDir(), "build requires a clean checkout", sentinel, sentinelBefore)
	if err := os.Remove(filepath.Join(fixture, "untracked.go")); err != nil {
		t.Fatalf("remove untracked fixture source: %v", err)
	}
	writeFile(t, filepath.Join(fixture, "README.md"), "dirty\n")
	assertBuildFails(t, fixture, "dirty worktree", t.TempDir(), "build requires a clean checkout", sentinel, sentinelBefore)
	gitRun(t, fixture, "add", "README.md")
	assertBuildFails(t, fixture, "dirty index", t.TempDir(), "build requires a clean checkout", sentinel, sentinelBefore)
	gitRun(t, fixture, "reset", "--quiet", "HEAD", "--", "README.md")
	gitRun(t, fixture, "checkout", "--", "README.md")
	gitRun(t, fixture, "switch", "-c", "feature")
	assertBuildFails(t, fixture, "non-main branch", t.TempDir(), "build requires the local main branch", sentinel, sentinelBefore)
	gitRun(t, fixture, "switch", "--quiet", "main")
	gitRun(t, fixture, "switch", "--quiet", "--detach", "HEAD")
	assertBuildFails(t, fixture, "detached HEAD", t.TempDir(), "build requires local main, not detached HEAD", sentinel, sentinelBefore)

	missing := filepath.Join(t.TempDir(), "missing")
	assertBuildFails(t, fixture, "missing output", missing, "must name an existing empty directory", sentinel, sentinelBefore)
	outputFile := filepath.Join(t.TempDir(), "output-file")
	writeFile(t, outputFile, "file\n")
	assertBuildFails(t, fixture, "file output", outputFile, "must name an existing empty directory", sentinel, sentinelBefore)
	assertBuildFails(t, fixture, "relative output", "relative-output", "must be an absolute path", sentinel, sentinelBefore)
	escapeOutput := t.TempDir()
	escapeTarget := filepath.Join(t.TempDir(), "escape-target")
	if err := os.Symlink(escapeTarget, filepath.Join(escapeOutput, "escape")); err != nil {
		t.Fatalf("create output escape symlink: %v", err)
	}
	assertBuildFails(t, fixture, "output child escape", escapeOutput, "AQUARIUM_DEV_OUTPUT must be empty", sentinel, sentinelBefore)
	symlinkOutputTarget := t.TempDir()
	symlinkOutput := filepath.Join(t.TempDir(), "output")
	if err := os.Symlink(symlinkOutputTarget, symlinkOutput); err != nil {
		t.Fatalf("create output symlink: %v", err)
	}
	assertBuildFails(t, fixture, "output symlink", symlinkOutput, "must name an existing empty directory", sentinel, sentinelBefore)

	// A Git inspection failure must fail closed rather than looking like a
	// clean checkout because status returned no usable output.
	gitRun(t, fixture, "switch", "--quiet", "main")
	indexPath := gitPath(t, fixture, "index")
	indexInfo, err := os.Stat(indexPath)
	if err != nil {
		t.Fatalf("stat Git index: %v", err)
	}
	indexBytes, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read Git index: %v", err)
	}
	writeFile(t, indexPath, "not a Git index\n")
	assertBuildFails(t, fixture, "corrupt Git index", t.TempDir(), "cannot inspect Git checkout status", sentinel, sentinelBefore)
	if err := os.WriteFile(indexPath, indexBytes, indexInfo.Mode().Perm()); err != nil {
		t.Fatalf("restore Git index: %v", err)
	}

	// Local Git attributes are checkout metadata. A producer archive must not
	// let them hide a committed source file.
	appendFile(t, filepath.Join(fixture, ".git", "info", "attributes"), "internal/buildinfo/version.go export-ignore\n")
	localAttributeOutput := t.TempDir()
	fixtureBeforeLocalAttributeBuild := pathSnapshot(t, fixture)
	ignoredBeforeLocalAttributeBuild := pathSnapshot(t, ignored)
	localAttributeManifest := runMake(t, fixture, "aquarium-dev-build", localAttributeOutput)
	assertProducerInputsUnchanged(t, fixture, fixtureBeforeLocalAttributeBuild, ignored, ignoredBeforeLocalAttributeBuild)
	if !strings.Contains(localAttributeManifest, `"schema":"aquarium-dev-artifact-manifest/v1"`) {
		t.Fatalf("local export attributes changed the committed build: %q", localAttributeManifest)
	}
	writeFile(t, versionSource, "package buildinfo\n\nconst CurrentVersion = \"invalid\"\n")
	gitRun(t, fixture, "add", "internal/buildinfo/version.go")
	gitCommit(t, fixture, "invalid version fixture")
	assertBuildFails(t, fixture, "invalid committed CurrentVersion", t.TempDir(), "committed CurrentVersion is invalid", sentinel, sentinelBefore)
	gitRun(t, fixture, "switch", "--quiet", "--detach", "HEAD")

	// A committed syntax error reaches the compiler and remains a build
	// failure; the target must not turn it into a successful manifest.
	gitRun(t, fixture, "switch", "--quiet", "main")
	writeFile(t, versionSource, versionSourceBefore)
	gitRun(t, fixture, "add", "internal/buildinfo/version.go")
	gitCommit(t, fixture, "restore valid version fixture")
	writeFile(t, filepath.Join(fixture, "cmd", "sanho", "main.go"), "package main\nfunc (\n")
	gitRun(t, fixture, "add", "cmd/sanho/main.go")
	gitCommit(t, fixture, "invalid fixture")
	assertBuildFails(t, fixture, "compiler failure", t.TempDir(), "syntax error", sentinel, sentinelBefore)
}

func fixtureRepository(t *testing.T) string {
	t.Helper()
	root := repositoryRoot(t)
	fixture := filepath.Join(t.TempDir(), "repo")
	rootSHA := gitOutput(t, root, "rev-parse", "HEAD")
	gitRun(t, root, "clone", "--quiet", "--template=", "--no-hardlinks", root, fixture)
	gitRun(t, fixture, "switch", "--quiet", "-C", "main", rootSHA)

	diff := gitRawOutput(t, root, "diff", "--binary", "HEAD")
	if diff != "" {
		patch := filepath.Join(t.TempDir(), "working-tree.diff")
		writeFile(t, patch, diff)
		gitRun(t, fixture, "apply", patch)
		gitRun(t, fixture, "add", "-A")
		gitCommit(t, fixture, "test fixture")
	}
	return fixture
}

func runMake(t *testing.T, repository, target, output string) string {
	return runMakeWithEnv(t, repository, target, output)
}

func runMakeWithEnv(t *testing.T, repository, target, output string, extraEnvironment ...string) string {
	t.Helper()
	command := exec.Command("make", "-s", "-C", repository, target)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	environment := []string{
		"GOWORK=" + filepath.Join(repository, "outside.work"),
		"GOFLAGS=-overlay=/outside/overlay.json",
	}
	if output != "" {
		environment = append(environment, "AQUARIUM_DEV_OUTPUT="+output)
	}
	command.Env = environmentWithOverrides(append(environment, extraEnvironment...)...)
	err := command.Run()
	if err != nil {
		t.Fatalf("make %s: %v\n%s", target, err, stderr.String())
	}
	return strings.TrimSuffix(stdout.String(), "\n")
}

func environmentWithOverrides(overrides ...string) []string {
	return environmentWithOverridesFrom(os.Environ(), overrides...)
}

func environmentWithOverridesFrom(base []string, overrides ...string) []string {
	keys := make(map[string]struct{}, len(overrides))
	for _, override := range overrides {
		key, _, _ := strings.Cut(override, "=")
		keys[key] = struct{}{}
	}
	environment := make([]string, 0, len(base)+len(overrides))
	for _, entry := range base {
		key, _, _ := strings.Cut(entry, "=")
		if _, exists := keys[key]; exists {
			continue
		}
		environment = append(environment, entry)
	}
	return append(environment, overrides...)
}

func assertBuildFails(t *testing.T, repository, label, output, diagnostic, sentinel, sentinelBefore string) {
	t.Helper()
	assertTargetFails(t, repository, "aquarium-dev-build", label, output, diagnostic)
	if got := mustReadFile(t, sentinel); got != sentinelBefore {
		t.Fatalf("%s changed external sentinel to %q", label, got)
	}
}

func assertTargetFails(t *testing.T, repository, target, label, output, diagnostic string) {
	t.Helper()
	before := pathSnapshot(t, output)
	command := exec.Command("make", "-s", "-C", repository, target)
	command.Env = environmentWithOverrides("AQUARIUM_DEV_OUTPUT=" + output)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err == nil {
		t.Fatalf("%s unexpectedly passed: %s", label, stdout.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("%s wrote stdout on failure: %q", label, stdout.String())
	}
	if !strings.Contains(stderr.String(), diagnostic) {
		t.Fatalf("%s diagnostic = %q, want substring %q", label, stderr.String(), diagnostic)
	}
	if after := pathSnapshot(t, output); after != before {
		t.Fatalf("%s changed output from %q to %q", label, before, after)
	}
}

func assertOnlyArtifact(t *testing.T, output, artifact string) {
	t.Helper()
	err := filepath.WalkDir(output, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == output || path == filepath.Dir(artifact) || path == artifact {
			return nil
		}
		return fmt.Errorf("unexpected output path %s", path)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func assertProducerInputsUnchanged(t *testing.T, fixture, fixtureBefore, ignored, ignoredBefore string) {
	t.Helper()
	if got := pathSnapshot(t, fixture); got != fixtureBefore {
		t.Fatalf("producer changed source checkout from %q to %q", fixtureBefore, got)
	}
	if got := pathSnapshot(t, ignored); got != ignoredBefore {
		t.Fatalf("producer changed ignored source from %q to %q", ignoredBefore, got)
	}
	artifact := filepath.Join(fixture, "bin", "sanho")
	if _, err := os.Lstat(artifact); err == nil {
		t.Fatalf("producer created an artifact in the source checkout: %s", artifact)
	} else if !os.IsNotExist(err) {
		t.Fatalf("inspect source checkout artifact %s: %v", artifact, err)
	}
}

func gitOutput(t *testing.T, repository string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repository}, arguments...)...)
	command.Env = isolatedGitEnv()
	result, err := command.Output()
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(arguments, " "), err)
	}
	return strings.TrimSpace(string(result))
}

func gitPath(t *testing.T, repository, name string) string {
	t.Helper()
	path := gitOutput(t, repository, "rev-parse", "--git-path", name)
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(repository, path)
}

func gitRawOutput(t *testing.T, repository string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repository}, arguments...)...)
	command.Env = isolatedGitEnv()
	result, err := command.Output()
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(arguments, " "), err)
	}
	return string(result)
}

func gitRun(t *testing.T, repository string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repository}, arguments...)...)
	command.Env = isolatedGitEnv()
	if result, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(arguments, " "), err, result)
	}
}

func gitCommit(t *testing.T, repository, message string) {
	t.Helper()
	gitRun(t, repository,
		"-c", "core.hooksPath=/dev/null",
		"-c", "user.name=aquarium-test",
		"-c", "user.email=aquarium-test@example.invalid",
		"commit", "--quiet", "-m", message,
	)
}

func isolatedGitEnv() []string {
	environment := make([]string, 0, len(os.Environ())+6)
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "GIT_") {
			continue
		}
		environment = append(environment, entry)
	}
	return append(environment,
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_ATTR_NOSYSTEM=1",
		"GIT_TEMPLATE_DIR=",
		"GIT_TERMINAL_PROMPT=0",
	)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func appendFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create parent for %s: %v", path, err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer file.Close()
	if _, err := file.WriteString(content); err != nil {
		t.Fatalf("append %s: %v", path, err)
	}
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}

func pathSnapshot(t *testing.T, path string) string {
	t.Helper()
	if entry, err := os.Lstat(path); err == nil && entry.Mode()&os.ModeSymlink != 0 {
		target, readErr := os.Readlink(path)
		if readErr != nil {
			t.Fatalf("read symlink %s: %v", path, readErr)
		}
		return "->" + target
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return "<missing>"
	}
	var entries []string
	err := filepath.WalkDir(path, func(current string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(path, current)
		if err != nil {
			return err
		}
		if relative == "." {
			entries = append(entries, ".")
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			target, readErr := os.Readlink(current)
			if readErr != nil {
				return readErr
			}
			entries = append(entries, relative+"->"+target)
			return nil
		}
		if entry.IsDir() {
			entries = append(entries, relative+"/")
			return nil
		}
		content, readErr := os.ReadFile(current)
		if readErr != nil {
			return readErr
		}
		entries = append(entries, fmt.Sprintf("%s:%x", relative, content))
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", path, err)
	}
	return strings.Join(entries, "\n")
}
