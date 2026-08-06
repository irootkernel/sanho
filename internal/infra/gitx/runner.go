// Package gitx is the single git execution port for sanho v0.2
// (sanho-v0.2.md §7 L7). Every git invocation in the codebase goes
// through a Runner: argv-only (no shell), non-interactive environment
// (GIT_TERMINAL_PROMPT=0 always; SSH BatchMode on network operations),
// per-command timeouts, and uniform exit classification.
package gitx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// DefaultTimeout bounds a single git invocation unless overridden.
const DefaultTimeout = 60 * time.Second

// NetworkConnectTimeout is the SSH connect timeout applied by
// WithNetwork.
const NetworkConnectTimeout = 10 * time.Second

// killWaitDelay bounds how long an invocation waits, after its context
// is done, for the killed process group to finish exiting and release
// its stdout/stderr pipes. SIGKILL cannot be blocked or ignored, so this
// only guards against pathological cleanup delays; it is not the
// expected path.
const killWaitDelay = 3 * time.Second

// Runner executes git with a fixed working directory and policy.
type Runner struct {
	dir      string
	timeout  time.Duration
	network  bool
	extraEnv []string
}

// Option configures a Runner.
type Option func(*Runner)

// WithTimeout overrides DefaultTimeout for this runner.
func WithTimeout(d time.Duration) Option { return func(r *Runner) { r.timeout = d } }

// WithNetwork marks the runner as performing remote operations: adds
// GIT_SSH_COMMAND with BatchMode=yes and ConnectTimeout, so a missing
// credential fails fast instead of prompting (audit A2.1/L7).
func WithNetwork() Option { return func(r *Runner) { r.network = true } }

// WithEnv appends extra KEY=VALUE pairs (e.g. synthetic author for
// commit-tree). GIT_TERMINAL_PROMPT=0 is always set regardless.
func WithEnv(kv ...string) Option { return func(r *Runner) { r.extraEnv = append(r.extraEnv, kv...) } }

// New returns a Runner rooted at dir ("" = process cwd).
func New(dir string, opts ...Option) *Runner {
	r := &Runner{dir: dir, timeout: DefaultTimeout}
	for _, o := range opts {
		o(r)
	}
	return r
}

// Result is the outcome of one git invocation.
type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// ExitError is returned by Run when git exits non-zero. Callers that
// expect specific non-zero codes (e.g. rev-list --count semantics,
// diff --quiet) use RunExit instead.
type ExitError struct {
	Args   []string
	Result Result
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("git %v: exit %d: %s", e.Args, e.Result.ExitCode, e.Result.Stderr)
}

// env builds the child process environment: the inherited process
// environment, then the non-interactive policy variables, then any
// WithEnv extras, in that order. exec.Cmd deduplicates a command's Env
// in favor of the last occurrence of each key, so this ordering lets
// the policy variables win over any conflicting inherited value (e.g. a
// developer's own GIT_SSH_COMMAND), regardless of whether a duplicate
// key happens to appear earlier in os.Environ().
func (r *Runner) env() []string {
	env := append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if r.network {
		connectSecs := int(NetworkConnectTimeout / time.Second)
		env = append(env, fmt.Sprintf("GIT_SSH_COMMAND=ssh -o BatchMode=yes -o ConnectTimeout=%d", connectSecs))
	}
	return append(env, r.extraEnv...)
}

// invoke runs git with the runner's policy and classifies the outcome.
// exited reports whether err (if non-nil) represents a genuine non-zero
// exit status, as opposed to a spawn failure, timeout, cancellation, or
// external signal death; Run and RunExit use it to decide how to wrap
// err.
//
// The returned Result reflects whatever stdout/stderr the process wrote
// before it stopped, even when err is non-nil.
func (r *Runner) invoke(ctx context.Context, args ...string) (res Result, err error, exited bool) {
	timeout := r.timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "git", args...)
	cmd.Dir = r.dir
	cmd.Env = r.env()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Run git in its own process group so a timeout or cancellation can
	// kill the whole subtree (ssh, credential helpers, shell-alias
	// children) instead of just the git process itself.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		if killErr := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); killErr != nil {
			if errors.Is(killErr, syscall.ESRCH) {
				return os.ErrProcessDone
			}
			return killErr
		}
		return nil
	}
	cmd.WaitDelay = killWaitDelay

	runErr := cmd.Run()

	res = Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes(), ExitCode: -1}
	if cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
	}

	if runErr == nil {
		return res, nil, false
	}

	// A context deadline or explicit cancellation takes priority over
	// how the killed process happened to exit: cmd.Wait's own error
	// does not wrap ctx.Err() once Cancel has already produced a
	// (necessarily unsuccessful) signal death, so report the reason
	// callers can actually act on instead of an opaque "signal: killed".
	if ctxErr := runCtx.Err(); ctxErr != nil {
		return res, fmt.Errorf("gitx: git %s: %w", strings.Join(args, " "), ctxErr), false
	}

	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		if res.ExitCode >= 0 {
			// Genuine exit status; Run/RunExit decide how to surface it.
			return res, runErr, true
		}
		// Terminated by a signal that was not our own timeout/cancel
		// above (e.g. something external killed the git process).
		return res, fmt.Errorf("gitx: git %s: %w", strings.Join(args, " "), runErr), false
	}

	// Spawn failure: git not found, fork/exec error, etc.
	return res, fmt.Errorf("gitx: git %s: %w", strings.Join(args, " "), runErr), false
}

// Run executes git with the runner's policy. A non-zero exit returns
// (*ExitError); spawn failures, timeouts, and signal deaths return
// other errors. Timeout kills the process group.
func (r *Runner) Run(ctx context.Context, args ...string) (Result, error) {
	res, err, exited := r.invoke(ctx, args...)
	if exited {
		return res, &ExitError{Args: args, Result: res}
	}
	return res, err
}

// RunExit executes git and returns the Result for ANY exit code,
// erroring only on spawn failure, timeout, or signal death. Use for
// commands whose non-zero exits carry meaning.
func (r *Runner) RunExit(ctx context.Context, args ...string) (Result, error) {
	res, err, exited := r.invoke(ctx, args...)
	if exited {
		return res, nil
	}
	return res, err
}

// Line runs git and returns the first stdout line, trimmed. Errors as
// Run does; empty output yields "".
func (r *Runner) Line(ctx context.Context, args ...string) (string, error) {
	res, err := r.Run(ctx, args...)
	if err != nil {
		return "", err
	}
	line := res.Stdout
	if i := bytes.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	return strings.TrimRight(string(line), " \t\r\n"), nil
}
