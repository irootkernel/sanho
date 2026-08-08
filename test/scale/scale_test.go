package scale

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	scaleFileCount   = 1000
	scaleCommitCount = 500
)

func TestLargeRepositoryRoundTrip(t *testing.T) {
	if os.Getenv("SANHO_SCALE") != "1" {
		t.Skip("set SANHO_SCALE=1 and run make test-scale")
	}
	binary := os.Getenv("SANHO_CLI_BINARY")
	if binary == "" || !filepath.IsAbs(binary) {
		t.Fatal("SANHO_CLI_BINARY must name the absolute checkout-built binary")
	}

	root := t.TempDir()
	env := testEnv()
	canonical := filepath.Join(root, "canonical.git")
	seed := filepath.Join(root, "canonical-seed")
	mustMkdir(t, seed)
	run(t, root, env, "git", "init", "--quiet", "-b", "main", "--bare", canonical)
	run(t, seed, env, "git", "init", "--quiet", "-b", "main")
	run(t, seed, env, "git", "config", "gc.auto", "0")

	payload := strings.Repeat("0123456789abcdef", 3280)
	for i := 0; i < scaleFileCount-1; i++ {
		writeFile(t, filepath.Join(seed, fmt.Sprintf("file-%04d.txt", i)),
			fmt.Sprintf("file %04d\n", i)+payload)
	}
	writeFile(t, filepath.Join(seed, "history.md"), "revision 000\n")
	run(t, seed, env, "git", "add", "-A")
	run(t, seed, env, "git", "commit", "--quiet", "-m", "canonical: scale seed")
	for i := 1; i < scaleCommitCount; i++ {
		writeFile(t, filepath.Join(seed, "history.md"), fmt.Sprintf("revision %03d\n", i))
		run(t, seed, env, "git", "add", "history.md")
		run(t, seed, env, "git", "commit", "--quiet", "-m", fmt.Sprintf("canonical: revision %03d", i))
	}
	seedStart := time.Now()
	run(t, seed, env, "git", "push", "--quiet", canonical, "main")
	t.Logf("seed push: %s", time.Since(seedStart))

	alpha := newApp(t, root, "alpha", canonical, binary)
	bravo := newApp(t, root, "bravo", canonical, binary)
	t.Logf("sanho init alpha: %s", alpha.initDuration)
	t.Logf("sanho init bravo: %s", bravo.initDuration)

	statusStart := time.Now()
	run(t, alpha.dir, alpha.env, binary, "status")
	t.Logf("cached sanho status: %s", time.Since(statusStart))

	updated := "scale update\n" + payload
	writeFile(t, filepath.Join(alpha.dir, "docs", "file-0000.txt"), updated)
	run(t, alpha.dir, alpha.env, "git", "add", "docs/file-0000.txt")
	run(t, alpha.dir, alpha.env, "git", "commit", "--quiet", "-m", "docs: scale publication")
	pushStart := time.Now()
	run(t, alpha.dir, alpha.env, "git", "push", "--quiet", "origin", "main")
	t.Logf("publication push: %s", time.Since(pushStart))

	syncStart := time.Now()
	run(t, bravo.dir, bravo.env, binary, "sync")
	t.Logf("sanho sync: %s", time.Since(syncStart))
	if got := readFile(t, filepath.Join(bravo.dir, "docs", "file-0000.txt")); got != updated {
		t.Fatalf("synced file length = %d, want %d", len(got), len(updated))
	}

	head := strings.TrimSpace(run(t, canonical, env, "git", "rev-parse", "refs/heads/main"))
	commits := strings.TrimSpace(run(t, canonical, env, "git", "rev-list", "--count", head))
	if commits != "501" {
		t.Fatalf("canonical commit count = %s, want 501", commits)
	}
	paths := strings.Fields(run(t, canonical, env, "git", "ls-tree", "-r", "--name-only", head))
	if len(paths) != scaleFileCount {
		t.Fatalf("canonical file count = %d, want %d", len(paths), scaleFileCount)
	}
	for _, app := range []*scaleApp{alpha, bravo} {
		t.Logf("%s private clone bytes: %d", app.name, treeBytes(t, filepath.Join(app.dir, ".git", "sanho", "canonical")))
	}
}

type scaleApp struct {
	name         string
	dir          string
	env          []string
	initDuration time.Duration
}

func newApp(t *testing.T, root, name, canonical, binary string) *scaleApp {
	t.Helper()
	dir := filepath.Join(root, name)
	codeOrigin := filepath.Join(root, name+"-code.git")
	home := filepath.Join(root, name+"-sanho-home")
	mustMkdir(t, dir)
	mustMkdir(t, home)
	env := append(testEnv(), "SANHO_HOME="+home)
	run(t, root, env, "git", "init", "--quiet", "-b", "main", "--bare", codeOrigin)
	run(t, dir, env, "git", "init", "--quiet", "-b", "main")
	run(t, dir, env, "git", "remote", "add", "origin", codeOrigin)
	writeFile(t, filepath.Join(dir, "README.md"), name+"\n")
	run(t, dir, env, "git", "add", "README.md")
	run(t, dir, env, "git", "commit", "--quiet", "-m", "chore: seed application")

	started := time.Now()
	run(t, dir, env, binary, "init",
		"--project", "scale",
		"--docs-repo-url", canonical,
		"--actor-email", "scale@example.test")
	duration := time.Since(started)
	run(t, dir, env, "git", "add", ".gitignore")
	run(t, dir, env, "git", "commit", "--quiet", "-m", "docs: adopt scale canonical")
	return &scaleApp{name: name, dir: dir, env: env, initDuration: duration}
}

func testEnv() []string {
	return append(os.Environ(),
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_SYSTEM="+os.DevNull,
		"GIT_AUTHOR_NAME=Scale Author",
		"GIT_AUTHOR_EMAIL=scale@example.test",
		"GIT_COMMITTER_NAME=Scale Committer",
		"GIT_COMMITTER_EMAIL=scale@example.test",
		"GIT_TERMINAL_PROMPT=0",
	)
}

func run(t *testing.T, dir string, env []string, program string, args ...string) string {
	t.Helper()
	cmd := exec.Command(program, args...)
	cmd.Dir = dir
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s %s failed: %v\nstdout:\n%s\nstderr:\n%s",
			program, strings.Join(args, " "), err, stdout.String(), stderr.String())
	}
	return stdout.String()
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	mustMkdir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func treeBytes(t *testing.T, root string) int64 {
	t.Helper()
	var total int64
	err := filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return total
}
