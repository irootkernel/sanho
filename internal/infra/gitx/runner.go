// Package gitx is the single git execution port for sanho v0.2
// (sanho-v0.2.md §7 L7). Every git invocation in the codebase goes
// through a Runner: argv-only (no shell), non-interactive environment
// (GIT_TERMINAL_PROMPT=0 always; SSH BatchMode on network operations),
// per-command timeouts, and uniform exit classification.
package gitx

import (
	"context"
	"fmt"
	"time"
)

// DefaultTimeout bounds a single git invocation unless overridden.
const DefaultTimeout = 60 * time.Second

// NetworkConnectTimeout is the SSH connect timeout applied by
// WithNetwork.
const NetworkConnectTimeout = 10 * time.Second

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

// Run executes git with the runner's policy. A non-zero exit returns
// (*ExitError); spawn failures, timeouts, and signal deaths return
// other errors. Timeout kills the process group.
func (r *Runner) Run(ctx context.Context, args ...string) (Result, error) {
	panic("unimplemented (sanho v0.2 P1)")
}

// RunExit executes git and returns the Result for ANY exit code,
// erroring only on spawn failure, timeout, or signal death. Use for
// commands whose non-zero exits carry meaning.
func (r *Runner) RunExit(ctx context.Context, args ...string) (Result, error) {
	panic("unimplemented (sanho v0.2 P1)")
}

// Line runs git and returns the first stdout line, trimmed. Errors as
// Run does; empty output yields "".
func (r *Runner) Line(ctx context.Context, args ...string) (string, error) {
	panic("unimplemented (sanho v0.2 P1)")
}
