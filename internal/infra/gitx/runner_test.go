package gitx

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// No mocks below the git boundary: every test here drives the real git
// binary against real temp-directory fixtures (project rule; see
// sanho-v0.2.md §9.1).

// newFixtureRepo creates a fresh git repository under t.TempDir() using
// a raw git invocation (not the Runner under test, to keep fixture
// bootstrap independent of the code being verified).
func newFixtureRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cmd := exec.Command("git", "init", "-q", dir)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init fixture: %v\n%s", err, out)
	}
	return dir
}

// parseEnvDump parses "KEY=VALUE" lines (as produced by the `env`
// command) into a map.
func parseEnvDump(output []byte) map[string]string {
	env := make(map[string]string)
	for _, line := range strings.Split(string(output), "\n") {
		if line == "" {
			continue
		}
		if i := strings.IndexByte(line, '='); i >= 0 {
			env[line[:i]] = line[i+1:]
		}
	}
	return env
}

// unsetEnvForTest unsets key for the duration of the test, restoring
// any prior value on cleanup. Unlike t.Setenv it can produce a truly
// absent variable rather than one set to "".
func unsetEnvForTest(t *testing.T, key string) {
	t.Helper()
	old, had := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unsetenv %s: %v", key, err)
	}
	t.Cleanup(func() {
		if had {
			if err := os.Setenv(key, old); err != nil {
				t.Errorf("restore env %s: %v", key, err)
			}
		}
	})
}

// requireProcessGone polls until the process recorded in pidFile (as a
// bare PID written by the fixture alias) is no longer alive, or fails
// the test after a bounded wait. It exists to prove a timed-out or
// canceled invocation actually killed its child instead of merely
// abandoning it.
func requireProcessGone(t *testing.T, pidFile string) {
	t.Helper()
	pidBytes, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("read pid file %s: %v", pidFile, err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
	if err != nil {
		t.Fatalf("parse pid %q: %v", pidBytes, err)
	}

	const budget = 2 * time.Second
	deadline := time.Now().Add(budget)
	for {
		killErr := syscall.Kill(pid, 0)
		if errors.Is(killErr, syscall.ESRCH) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("pid %d still alive %s after the runner reported the command done", pid, budget)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// sleeperAlias returns a `-c` config argument defining a test-only git
// alias that records its own PID to pidFile and then sleeps far longer
// than any timeout used in these tests. exec replaces the shell with
// sleep itself, so the recorded PID is the one that actually blocks.
func sleeperAlias(pidFile string) string {
	return fmt.Sprintf("alias.sleepy=!echo $$ > %s; exec sleep 5", pidFile)
}

// writeLines writes lines newline-joined (with a trailing newline) to path.
func writeLines(t *testing.T, path string, lines []string) {
	t.Helper()
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// mergeFileFixture writes base/current/other files under dir with n
// well-separated single-line conflicts: at n positions, both current
// and other change the same line differently from base. The positions
// are spaced far enough apart (verified empirically) that git's merge
// treats them as n independent conflict hunks instead of coalescing
// them into one, making this the exit-code sentinel for the C2 audit
// finding (sanho-v0.2.md §7 C2: "merge-file exit-code misread").
func mergeFileFixture(t *testing.T, dir string, n int) (current, base, other string) {
	t.Helper()
	const total = 40
	const gap = 15
	const offset = 2
	baseLines := make([]string, total)
	curLines := make([]string, total)
	otherLines := make([]string, total)
	for i := range total {
		baseLines[i] = fmt.Sprintf("context-%02d", i)
		curLines[i] = baseLines[i]
		otherLines[i] = baseLines[i]
	}
	for h := range n {
		pos := offset + h*gap
		if pos >= total {
			t.Fatalf("mergeFileFixture: n=%d needs more than %d context lines", n, total)
		}
		baseLines[pos] = fmt.Sprintf("conflict-%d-base", h)
		curLines[pos] = fmt.Sprintf("conflict-%d-ours", h)
		otherLines[pos] = fmt.Sprintf("conflict-%d-theirs", h)
	}
	base = filepath.Join(dir, "base.txt")
	current = filepath.Join(dir, "current.txt")
	other = filepath.Join(dir, "other.txt")
	writeLines(t, base, baseLines)
	writeLines(t, current, curLines)
	writeLines(t, other, otherLines)
	return current, base, other
}

// sameDir reports whether path resolves to the same directory as want,
// comparing by device+inode (via os.SameFile) rather than by string so
// that platform path/symlink normalization (e.g. macOS's /tmp ->
// /private/tmp) cannot produce a false mismatch.
func sameDir(t *testing.T, path, want string) bool {
	t.Helper()
	gotInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %q: %v", path, err)
	}
	wantInfo, err := os.Stat(want)
	if err != nil {
		t.Fatalf("stat %q: %v", want, err)
	}
	return os.SameFile(gotInfo, wantInfo)
}

func TestRun_Success(t *testing.T) {
	dir := newFixtureRepo(t)
	r := New(dir)

	res, err := r.Run(t.Context(), "rev-parse", "--show-toplevel")
	if err != nil {
		t.Fatalf("Run: unexpected error: %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", res.ExitCode)
	}
	if len(res.Stderr) != 0 {
		t.Errorf("Stderr = %q, want empty", res.Stderr)
	}
	if len(res.Stdout) == 0 {
		t.Fatal("Stdout is empty, want the resolved toplevel path")
	}

	// This is the discriminating half of the check: the fixture repo's
	// tempdir may not string-equal what git prints (macOS resolves
	// /tmp's symlink), and a Runner that silently ignored r.dir would
	// still print *some* toplevel path (e.g. this module's own repo)
	// without necessarily failing, so a bare "err == nil" assertion
	// would not catch that regression.
	got := strings.TrimRight(string(res.Stdout), "\n")
	if !sameDir(t, got, dir) {
		t.Errorf("rev-parse --show-toplevel = %q, want the fixture dir %q (working dir not honored)", got, dir)
	}
}

func TestRun_CapturesStreamsSeparately(t *testing.T) {
	dir := newFixtureRepo(t)
	r := New(dir)

	// Test-only alias writing distinct, recognizable content to each
	// stream in a single invocation, to prove stdout/stderr are
	// captured independently rather than merged.
	res, err := r.Run(t.Context(), "-c", "alias.dualstream=!echo out-line; echo err-line 1>&2", "dualstream")
	if err != nil {
		t.Fatalf("Run: unexpected error: %v", err)
	}
	if got := strings.TrimSpace(string(res.Stdout)); got != "out-line" {
		t.Errorf("Stdout = %q, want %q", got, "out-line")
	}
	if got := strings.TrimSpace(string(res.Stderr)); got != "err-line" {
		t.Errorf("Stderr = %q, want %q", got, "err-line")
	}
}

func TestLine(t *testing.T) {
	dir := newFixtureRepo(t)
	r := New(dir)
	ctx := t.Context()

	t.Run("trims a single line", func(t *testing.T) {
		got, err := r.Line(ctx, "rev-parse", "--show-toplevel")
		if err != nil {
			t.Fatalf("Line: unexpected error: %v", err)
		}
		if !sameDir(t, got, dir) {
			t.Errorf("Line = %q, want the fixture dir %q", got, dir)
		}
	})

	t.Run("returns only the first line", func(t *testing.T) {
		got, err := r.Line(ctx, "-c", "alias.multi=!printf 'first\\nsecond\\n'", "multi")
		if err != nil {
			t.Fatalf("Line: unexpected error: %v", err)
		}
		if got != "first" {
			t.Errorf("Line = %q, want %q", got, "first")
		}
	})

	t.Run("trims trailing whitespace", func(t *testing.T) {
		got, err := r.Line(ctx, "-c", "alias.padded=!printf 'value  \\n'", "padded")
		if err != nil {
			t.Fatalf("Line: unexpected error: %v", err)
		}
		if got != "value" {
			t.Errorf("Line = %q, want %q", got, "value")
		}
	})

	t.Run("empty output yields empty string", func(t *testing.T) {
		got, err := r.Line(ctx, "-c", "alias.empty=!true", "empty")
		if err != nil {
			t.Fatalf("Line: unexpected error: %v", err)
		}
		if got != "" {
			t.Errorf("Line = %q, want %q", got, "")
		}
	})
}

func TestNonZeroExit(t *testing.T) {
	dir := newFixtureRepo(t)
	r := New(dir)
	ctx := t.Context()

	t.Run("Run returns *ExitError", func(t *testing.T) {
		_, err := r.Run(ctx, "rev-parse", "--verify", "does-not-exist")
		if err == nil {
			t.Fatal("Run: expected an error for an unresolvable revision, got nil")
		}
		var exitErr *ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("Run error = %v (%T), want *ExitError", err, err)
		}
		if exitErr.Result.ExitCode != 128 {
			t.Errorf("ExitCode = %d, want 128", exitErr.Result.ExitCode)
		}
		if !strings.Contains(string(exitErr.Result.Stderr), "fatal") {
			t.Errorf("Stderr = %q, want it to contain %q", exitErr.Result.Stderr, "fatal")
		}
		if len(exitErr.Result.Stdout) != 0 {
			t.Errorf("Stdout = %q, want empty", exitErr.Result.Stdout)
		}
	})

	t.Run("RunExit returns the same code with nil error", func(t *testing.T) {
		res, err := r.RunExit(ctx, "rev-parse", "--verify", "does-not-exist")
		if err != nil {
			t.Fatalf("RunExit: unexpected error: %v", err)
		}
		if res.ExitCode != 128 {
			t.Errorf("ExitCode = %d, want 128", res.ExitCode)
		}
		if !strings.Contains(string(res.Stderr), "fatal") {
			t.Errorf("Stderr = %q, want it to contain %q", res.Stderr, "fatal")
		}
	})
}

// TestRunExit_PreservesMergeFileExitCodes is the C2-contract sentinel at
// runner level (sanho-v0.2.md §7 C2): a v0.1 bug misread merge-file's
// exit code, wedging on a 2-hunk conflict. RunExit must report the exact
// exit code across the meaningful range (0 clean, 1 hunk, 2 hunks).
func TestRunExit_PreservesMergeFileExitCodes(t *testing.T) {
	r := New(t.TempDir())
	ctx := t.Context()

	for _, tc := range []struct {
		name     string
		hunks    int
		wantExit int
	}{
		{"clean merge", 0, 0},
		{"one conflicting hunk", 1, 1},
		{"two conflicting hunks", 2, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			current, base, other := mergeFileFixture(t, t.TempDir(), tc.hunks)
			res, err := r.RunExit(ctx, "merge-file", "-p", current, base, other)
			if err != nil {
				t.Fatalf("RunExit: unexpected error: %v", err)
			}
			if res.ExitCode != tc.wantExit {
				t.Errorf("ExitCode = %d, want %d\nstdout:\n%s\nstderr:\n%s", res.ExitCode, tc.wantExit, res.Stdout, res.Stderr)
			}
		})
	}
}

func TestEnvironmentPolicy(t *testing.T) {
	dir := newFixtureRepo(t)
	ctx := t.Context()
	const dumpAlias = "alias.dumpenv=!env"

	t.Run("GIT_TERMINAL_PROMPT is always 0, overriding any inherited value", func(t *testing.T) {
		t.Setenv("GIT_TERMINAL_PROMPT", "1")
		r := New(dir)
		res, err := r.Run(ctx, "-c", dumpAlias, "dumpenv")
		if err != nil {
			t.Fatalf("Run: unexpected error: %v", err)
		}
		env := parseEnvDump(res.Stdout)
		if got := env["GIT_TERMINAL_PROMPT"]; got != "0" {
			t.Errorf("GIT_TERMINAL_PROMPT = %q, want %q", got, "0")
		}
	})

	t.Run("GIT_SSH_COMMAND is absent without WithNetwork", func(t *testing.T) {
		unsetEnvForTest(t, "GIT_SSH_COMMAND")
		r := New(dir)
		res, err := r.Run(ctx, "-c", dumpAlias, "dumpenv")
		if err != nil {
			t.Fatalf("Run: unexpected error: %v", err)
		}
		env := parseEnvDump(res.Stdout)
		if got, present := env["GIT_SSH_COMMAND"]; present {
			t.Errorf("GIT_SSH_COMMAND unexpectedly present: %q", got)
		}
	})

	t.Run("WithNetwork sets GIT_SSH_COMMAND from NetworkConnectTimeout and overrides any inherited value", func(t *testing.T) {
		t.Setenv("GIT_SSH_COMMAND", "should-be-overridden")
		r := New(dir, WithNetwork())
		res, err := r.Run(ctx, "-c", dumpAlias, "dumpenv")
		if err != nil {
			t.Fatalf("Run: unexpected error: %v", err)
		}
		env := parseEnvDump(res.Stdout)
		want := fmt.Sprintf("ssh -o BatchMode=yes -o ConnectTimeout=%d", int(NetworkConnectTimeout/time.Second))
		if got := env["GIT_SSH_COMMAND"]; got != want {
			t.Errorf("GIT_SSH_COMMAND = %q, want %q", got, want)
		}
	})

	t.Run("WithEnv extras reach the child", func(t *testing.T) {
		r := New(dir, WithEnv("GITX_TEST_EXTRA=present"))
		res, err := r.Run(ctx, "-c", dumpAlias, "dumpenv")
		if err != nil {
			t.Fatalf("Run: unexpected error: %v", err)
		}
		env := parseEnvDump(res.Stdout)
		if got := env["GITX_TEST_EXTRA"]; got != "present" {
			t.Errorf("GITX_TEST_EXTRA = %q, want %q", got, "present")
		}
	})
}

func TestRun_Timeout(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "sleeper.pid")
	r := New(dir, WithTimeout(100*time.Millisecond))

	start := time.Now()
	_, err := r.Run(t.Context(), "-c", sleeperAlias(pidFile), "sleepy")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Run: expected an error for a command that outlives the timeout, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Run error = %v, want errors.Is(err, context.DeadlineExceeded)", err)
	}
	var exitErr *ExitError
	if errors.As(err, &exitErr) {
		t.Errorf("Run error is *ExitError (%v); a timeout must not be classified as a normal exit", exitErr)
	}
	if elapsed >= 5*time.Second {
		t.Errorf("Run blocked for %s, want well under 5s (the underlying command sleeps 5s)", elapsed)
	}

	requireProcessGone(t, pidFile)
}

func TestRun_ContextCancellation(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "sleeper.pid")
	// A generous runner timeout so only the explicit cancel below fires.
	r := New(dir, WithTimeout(10*time.Second))

	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err := r.Run(ctx, "-c", sleeperAlias(pidFile), "sleepy")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Run: expected an error for a canceled context, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Run error = %v, want errors.Is(err, context.Canceled)", err)
	}
	var exitErr *ExitError
	if errors.As(err, &exitErr) {
		t.Errorf("Run error is *ExitError (%v); cancellation must not be classified as a normal exit", exitErr)
	}
	if elapsed >= 5*time.Second {
		t.Errorf("Run blocked for %s, want well under 5s (the underlying command sleeps 5s)", elapsed)
	}

	requireProcessGone(t, pidFile)
}

// TestRunExit_TimeoutStillErrors guards RunExit's "any exit code is
// success-shaped" contract from accidentally swallowing a timeout: a
// killed process's synthetic exit status must not be reported the same
// way as a real one.
func TestRunExit_TimeoutStillErrors(t *testing.T) {
	dir := t.TempDir()
	r := New(dir, WithTimeout(100*time.Millisecond))

	_, err := r.RunExit(t.Context(), "-c", "alias.sleepy=!sleep 5", "sleepy")
	if err == nil {
		t.Fatal("RunExit: expected an error on timeout, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("RunExit error = %v, want errors.Is(err, context.DeadlineExceeded)", err)
	}
}
