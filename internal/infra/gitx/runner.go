// Package gitx is the single git execution port for sanho v0.2
// described by docs/architecture.md's "Git execution policy". Every invocation goes
// through a Runner: argv-only (no shell), non-interactive environment
// (GIT_TERMINAL_PROMPT=0 always; SSH BatchMode on network operations),
// per-command timeouts, and uniform exit classification.
package gitx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
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
	dir         string
	timeout     time.Duration
	network     bool
	extraEnv    []string
	stdoutLimit int64
}

// Option configures a Runner.
type Option func(*Runner)

// WithTimeout overrides DefaultTimeout for this runner.
func WithTimeout(d time.Duration) Option { return func(r *Runner) { r.timeout = d } }

// WithStdoutLimit caps how many bytes of stdout are captured. Output
// past the cap is drained and discarded rather than buffered, so a
// command whose output is unbounded (reading a multi-gigabyte blob to
// sniff its first bytes) cannot exhaust memory. Result.Stdout then holds
// the first limit bytes; the process still runs to completion.
//
// A non-positive limit means "capture everything", which is the default.
func WithStdoutLimit(limit int64) Option { return func(r *Runner) { r.stdoutLimit = limit } }

// WithNetwork marks the runner as performing remote operations: adds
// GIT_SSH_COMMAND with BatchMode=yes and ConnectTimeout, so a missing
// credential fails fast instead of prompting (audit A2.1/L7).
func WithNetwork() Option { return func(r *Runner) { r.network = true } }

// WithEnv appends extra KEY=VALUE pairs (e.g. synthetic author for
// commit-tree). GIT_TERMINAL_PROMPT=0 is always set regardless.
func WithEnv(kv ...string) Option { return func(r *Runner) { r.extraEnv = append(r.extraEnv, kv...) } }

// WithInheritedIndexFile passes this process's own GIT_INDEX_FILE
// through to the child EXPLICITLY, and is a no-op when the variable is
// unset.
//
// It exists so that the one repository-scoped variable sanho genuinely
// depends on is named at a call site instead of arriving by luck.
// Git runs the hooks of a PARTIAL commit (`git commit -- docs`) with
// GIT_INDEX_FILE pointing at a temporary index holding exactly what that
// commit will contain, and the commit path reads it: the provenance contract provenance
// stamp hashes the staged docs tree, and the commit-hook contract marker gate scans the
// staged diff. Both must see the in-flight index rather than
// `$GIT_DIR/index`, which for a pathspec commit describes something
// else.
//
// Attaching it here is what let scrubbedEnvVars grow to cover
// GIT_INDEX_FILE like every other repository-identity variable: the
// inheritance is now a decision made by the workspace that needs it,
// and every OTHER git invocation sanho makes — every command against the
// private canonical clone above all — is free of it.
func WithInheritedIndexFile() Option {
	return func(r *Runner) {
		if value, ok := os.LookupEnv(indexFileEnvVar); ok {
			r.extraEnv = append(r.extraEnv, indexFileEnvVar+"="+value)
		}
	}
}

// indexFileEnvVar is the variable WithInheritedIndexFile forwards and
// scrubbedEnvVars removes from the inherited environment.
const indexFileEnvVar = "GIT_INDEX_FILE"

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
	// StdoutTruncated reports that WithStdoutLimit cut the capture
	// short, so Stdout is a prefix of what git actually wrote.
	StdoutTruncated bool
}

// limitedBuffer is the stdout sink. With limit <= 0 it is an ordinary
// buffer; with a positive limit it keeps the first limit bytes and
// discards the rest, which keeps the child's pipe drained (so it never
// blocks) without growing without bound.
type limitedBuffer struct {
	bytes.Buffer
	limit     int64
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.limit <= 0 {
		return b.Buffer.Write(p)
	}
	room := b.limit - int64(b.Len())
	if room <= 0 {
		b.truncated = true
		return len(p), nil
	}
	if int64(len(p)) <= room {
		return b.Buffer.Write(p)
	}
	if _, err := b.Buffer.Write(p[:room]); err != nil {
		return 0, err
	}
	b.truncated = true
	return len(p), nil
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

// scrubbedEnvVars are the git environment variables that pin a command
// to a specific repository, worktree, or object database. Every Runner
// names its target repository explicitly — its dir, handed to
// exec.Cmd.Dir — so none of these may be inherited from the parent
// process: an inherited value silently redirects a command aimed at
// r.dir onto whatever repository the value actually names, and r.dir
// itself has no say in the matter (confirmed on git 2.50.1: even an
// explicit `-C <dir>` loses to an inherited GIT_DIR).
//
// The concrete danger this closes: git exports an absolute GIT_DIR into
// the environment of every hook it runs inside a LINKED WORKTREE
// (verified on git 2.50.1: a hook in the main worktree exports none of
// these; one in a linked worktree exports
// GIT_DIR=<repo>/.git/worktrees/<name>). Before this scrub, env()
// inherited os.Environ() unfiltered, so a git command sanho issued from
// inside such a hook — even one explicitly rooted via dir at a wholly
// different repository, such as the private canonical clone — silently
// ran against the linked worktree's repository instead. The observed
// consequence: `canonical.reconcileExisting`'s
// `git remote set-url origin <canonical-url>`, meant for the canonical
// clone, ran against the APPLICATION repository instead — permanently
// rewriting the user's own `origin` to the docs clone's URL and
// replacing their `refs/remotes/origin/*` with canonical's — and the
// pre-commit freshness check silently reported distances read from the
// application repository's own history.
//
// GIT_INDEX_FILE is on this list too, and its history is worth stating
// because it was the one exception for a while.
//
// It is exactly as repository-scoped as GIT_DIR, and git exports it into
// the same linked-worktree hook environments. It also has a live,
// load-bearing, same-repository use: `git commit -- docs` — sanho's own
// primary write path — is a PARTIAL commit, so git builds a temporary
// index for it and points the hooks' GIT_INDEX_FILE at that temporary
// index, specifically so they see what the commit will actually contain.
// The the provenance contract provenance stamp and the commit-hook contract staged-marker gate both read it.
// Scrubbing it blindly was tried and measured: every `sanho sync` commit
// silently stopped carrying its `docs-base` trailer, because the
// now-index-file-less write-tree inside the hook read a different index
// or errored, and commit-msg's stamping is fail-open by design (the provenance contract:
// "a commit is worth more than a stamp") — no error, no warning, just a
// commit with no base to re-derive from later.
//
// The resolution is not an exception but an EXPLICIT pass-through: the
// application-repository runner is built with WithInheritedIndexFile,
// which names the value at the one call site that needs it, and every
// other invocation — every command against the private canonical clone
// above all — runs with it scrubbed like everything else. The dependency
// is the same; what changed is that it is now written down and typed
// rather than arriving because nobody removed it.
var scrubbedEnvVars = map[string]bool{
	"GIT_DIR":                          true,
	"GIT_WORK_TREE":                    true,
	indexFileEnvVar:                    true,
	"GIT_COMMON_DIR":                   true,
	"GIT_OBJECT_DIRECTORY":             true,
	"GIT_ALTERNATE_OBJECT_DIRECTORIES": true,
	"GIT_PREFIX":                       true,
	"GIT_CEILING_DIRECTORIES":          true,
	"GIT_QUARANTINE_PATH":              true,
	"GIT_NAMESPACE":                    true,
	"GIT_GRAFT_FILE":                   true,
}

// scrubRepoScopedEnv returns environ with every repository-scoping
// variable named in scrubbedEnvVars removed, preserving the relative
// order of what remains.
func scrubRepoScopedEnv(environ []string) []string {
	scrubbed := make([]string, 0, len(environ))
	for _, kv := range environ {
		key, _, _ := strings.Cut(kv, "=")
		if scrubbedEnvVars[key] {
			continue
		}
		scrubbed = append(scrubbed, kv)
	}
	return scrubbed
}

// env builds the child process environment: the inherited process
// environment with every variable in scrubbedEnvVars stripped, then the
// non-interactive policy variables, then any WithEnv extras, in that
// order. exec.Cmd deduplicates a command's Env in favor
// of the last occurrence of each key, so this ordering lets the policy
// variables win over any conflicting inherited value (e.g. a developer's
// own GIT_SSH_COMMAND) and lets a caller's own WithEnv setting win over
// both — including a WithEnv-supplied GIT_INDEX_FILE overriding an
// inherited one, which is what keeps the scratch-index callers
// (appgit.WorktreeDocsTree and its test-side equivalents) correct —
// regardless of whether a duplicate key happens to appear earlier in
// os.Environ().
func (r *Runner) env() []string {
	env := append(scrubRepoScopedEnv(os.Environ()), "GIT_TERMINAL_PROMPT=0")
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
// stdin, when non-nil, is fed to the child. It carries DATA only — the
// command line is still built entirely from argv (the Git-execution contract L7), so nothing a
// caller streams in can become part of the command. `git cat-file
// --batch` is the reason it exists: one child answering many object
// queries replaces one child per query.
//
// The returned Result reflects whatever stdout/stderr the process wrote
// before it stopped, even when err is non-nil.
func (r *Runner) invoke(ctx context.Context, stdin io.Reader, args ...string) (res Result, err error, exited bool) {
	timeout := r.timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "git", args...)
	cmd.Dir = r.dir
	cmd.Env = r.env()
	if stdin != nil {
		cmd.Stdin = stdin
	}

	var stdout limitedBuffer
	stdout.limit = r.stdoutLimit
	var stderr bytes.Buffer
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

	res = Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes(), ExitCode: -1, StdoutTruncated: stdout.truncated}
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
	return r.RunWithStdin(ctx, nil, args...)
}

// RunWithStdin is Run with data streamed to the child's standard input.
//
// It is the one place a caller may hand git anything other than argv,
// and the boundary is deliberate: stdin carries DATA, never command
// construction. `git cat-file --batch` reads a list of object names from
// it and answers them all from one child process, which is what turns
// the per-file scanners from two spawns per docs file into two spawns
// per scan. Nothing on stdin can add a flag, a pathspec, or a revision;
// the argv-only rule of the Git-execution contract L7 is unchanged.
func (r *Runner) RunWithStdin(ctx context.Context, stdin io.Reader, args ...string) (Result, error) {
	res, err, exited := r.invoke(ctx, stdin, args...)
	if exited {
		return res, &ExitError{Args: args, Result: res}
	}
	return res, err
}

// RunExit executes git and returns the Result for ANY exit code,
// erroring only on spawn failure, timeout, or signal death. Use for
// commands whose non-zero exits carry meaning.
func (r *Runner) RunExit(ctx context.Context, args ...string) (Result, error) {
	res, err, exited := r.invoke(ctx, nil, args...)
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
