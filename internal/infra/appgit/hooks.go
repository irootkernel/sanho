package appgit

// The v0.2 hook installer (sanho-v0.2.md §5.10).
//
// Six hooks, one line each, matched by EXACT LINE. That is the whole
// design decision, and it is the fix for audit L3: v0.1 tested
// idempotency with `strings.Contains`, so `sanho hook pre-push` matched
// inside `sanho hook pre-push "$@"` and the two lines coexisted, each
// installing the other's near-duplicate on the next run. Every
// comparison here is between whole trimmed lines, never substrings, in
// both directions — install (is my line already there?) and remove (is
// this line one of mine?).
//
// Two more properties follow from what hook files actually are: they are
// the user's shell scripts, which sanho is a guest in. Foreign lines are
// preserved verbatim on install and on removal; a sanho line is inserted
// *before* a trailing `exit` so it still runs; and a file left holding
// nothing but its shebang after removal is deleted rather than left as a
// no-op stub (audit L5).

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/irootkernel/sanho/internal/infra/fsx"
)

// hookFileMode is the mode a freshly created hook file gets, and
// ownerExecute is the single bit an existing file has added to it.
//
// Git runs a hook as the current user, so owner-execute is the bit that
// decides whether the hook runs at all. Only that one is added: widening
// an existing file to group- or world-executable would be a permission
// change the user did not ask for.
const (
	hookFileMode os.FileMode = 0755
	ownerExecute os.FileMode = 0100
)

// hookShebang heads every hook file sanho creates.
const hookShebang = "#!/bin/sh"

// Hook is one installed hook: the git hook file name and the exact line
// sanho owns inside it.
type Hook struct {
	Name string
	Line string
}

// Hooks is the §5.10 inventory, in table order. The quoted argument
// forms are git's own hook contracts, not decoration: `commit-msg`
// receives the message file as $1, and `pre-push` and `post-rewrite`
// receive their arguments as "$@". `post-commit` is deliberately absent
// — commits do not move the base (§5.7 invariant), so v0.2 removed it.
func Hooks() []Hook {
	return []Hook{
		{Name: "pre-commit", Line: "sanho hook pre-commit"},
		{Name: "commit-msg", Line: `sanho hook commit-msg "$1"`},
		{Name: "pre-push", Line: `sanho hook pre-push "$@"`},
		{Name: "post-checkout", Line: "sanho hook post-checkout"},
		{Name: "post-merge", Line: "sanho hook post-merge"},
		{Name: "post-rewrite", Line: `sanho hook post-rewrite "$@"`},
	}
}

// legacyHooks is the complete v0.1 inventory — seven hooks, including
// `post-commit`, which v0.2 has no replacement for.
//
// Removal consults this table as well as the current one so that
// `sanho migrate` and `sanho clean` leave nothing of the old install
// behind. Note `sanho hook pre-push` without arguments: that is the
// pre-v0.1.7 form whose substring relationship with the quoted form
// caused L3 in the first place, and exact-line matching removes both
// independently.
var legacyHooks = []Hook{
	{Name: "pre-commit", Line: "sanho hook pre-commit"},
	{Name: "commit-msg", Line: `sanho hook commit-msg "$1"`},
	{Name: "pre-push", Line: `sanho hook pre-push "$@"`},
	{Name: "pre-push", Line: "sanho hook pre-push"},
	{Name: "post-checkout", Line: "sanho hook post-checkout"},
	{Name: "post-merge", Line: "sanho hook post-merge"},
	{Name: "post-rewrite", Line: `sanho hook post-rewrite "$@"`},
	{Name: "post-commit", Line: "sanho hook post-commit"},
}

// HookState reports one hook's installation status.
type HookState struct {
	Name string
	Line string
	// Path is the hook file, whether or not it exists.
	Path string
	// Installed is true when the file carries the exact line at least
	// once; Occurrences counts them, so a duplicated line (the L3
	// symptom) is reported rather than rounded down to "installed".
	Installed   bool
	Occurrences int
	// Executable reports whether git would actually run the file.
	Executable bool
	// Legacy lists v0.1 lines still present in this file.
	Legacy []string
}

// InstallHooks writes sanho's line into each of the six hook files,
// creating the files that do not exist and leaving every foreign line
// alone. It is idempotent: a file that already carries the exact line is
// left as it is (except for the executable bit, which is repaired).
func (r *Repo) InstallHooks(ctx context.Context) error {
	dir, err := r.hooksDir(ctx)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("appgit: create hooks directory %s: %w", dir, err)
	}

	for _, hook := range Hooks() {
		if err := installHookLine(filepath.Join(dir, hook.Name), hook.Line); err != nil {
			return err
		}
	}
	return nil
}

// RemoveHooks deletes sanho's lines — the six v0.2 lines and the seven
// v0.1 lines — from every hook file, preserving all foreign content. A
// file left with nothing but its shebang (or nothing at all) is deleted.
func (r *Repo) RemoveHooks(ctx context.Context) error {
	dir, err := r.hooksDir(ctx)
	if err != nil {
		return err
	}

	for name, lines := range removableLines() {
		if err := removeHookLines(filepath.Join(dir, name), lines); err != nil {
			return err
		}
	}
	return nil
}

// HooksStatus reports the six v0.2 hooks for `sanho doctor`, plus any
// hook file that still carries a v0.1-only line (F-L3).
//
// `post-commit` is the case that matters: v0.2 removed it outright, so
// it appears in no v0.2 inventory — and a workspace left holding one
// after a partial migration invoked `sanho hook post-commit` on every
// commit, a subcommand that no longer exists, forever. A check that only
// looked at the six current hooks could not see it.
func (r *Repo) HooksStatus(ctx context.Context) ([]HookState, error) {
	dir, err := r.hooksDir(ctx)
	if err != nil {
		return nil, err
	}

	current := make(map[string]bool, len(Hooks()))
	states := make([]HookState, 0, len(Hooks()))
	for _, hook := range Hooks() {
		current[hook.Name] = true
		state, err := hookStatus(filepath.Join(dir, hook.Name), hook)
		if err != nil {
			return nil, err
		}
		states = append(states, state)
	}

	for _, legacy := range legacyHooks {
		if current[legacy.Name] {
			continue
		}
		state, err := legacyOnlyHookStatus(filepath.Join(dir, legacy.Name), legacy)
		if err != nil {
			return nil, err
		}
		if len(state.Legacy) > 0 {
			states = append(states, state)
		}
	}
	return states, nil
}

// legacyOnlyHookStatus inspects a hook v0.2 has no counterpart for. It
// is reported as Installed so doctor does not also call it "missing" —
// the problem is that it is PRESENT.
func legacyOnlyHookStatus(path string, hook Hook) (HookState, error) {
	state := HookState{Name: hook.Name, Line: hook.Line, Path: path, Installed: true, Executable: true}

	existing, mode, present, err := readHookFile(path)
	if err != nil || !present {
		return state, err
	}
	state.Executable = mode&ownerExecute != 0
	for _, line := range strings.Split(existing, "\n") {
		if sameLine(line, hook.Line) {
			state.Legacy = append(state.Legacy, strings.TrimSpace(line))
		}
	}
	return state, nil
}

// removableLines groups every sanho-owned line by hook file name, so a
// file is rewritten once no matter how many lines it holds.
func removableLines() map[string][]string {
	owned := make(map[string][]string)
	for _, hook := range append(Hooks(), legacyHooks...) {
		if !containsLine(owned[hook.Name], hook.Line) {
			owned[hook.Name] = append(owned[hook.Name], hook.Line)
		}
	}
	return owned
}

// hooksDir asks git where hooks live. `rev-parse --git-path hooks`
// answers correctly for a plain repository, a linked worktree (whose
// hooks resolve through the common directory) and a submodule alike, so
// nothing here has to reason about what `.git` happens to be.
func (r *Repo) hooksDir(ctx context.Context) (string, error) {
	path, err := r.git.Line(ctx, "rev-parse", "--git-path", "hooks")
	if err != nil {
		return "", fmt.Errorf("appgit: resolve hooks directory of %s: %w", r.workDir, err)
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(r.workDir, path)
	}
	return path, nil
}

func installHookLine(path, line string) error {
	existing, mode, present, err := readHookFile(path)
	if err != nil {
		return err
	}

	if !present {
		content := hookShebang + "\n" + line + "\n"
		return writeHookFile(path, content, hookFileMode)
	}

	lines := strings.Split(existing, "\n")
	if containsLine(lines, line) {
		// Already installed. The only thing worth repairing is a hook
		// git would refuse to run.
		if mode&ownerExecute != 0 {
			return nil
		}
		return writeHookFile(path, existing, mode|ownerExecute)
	}
	return writeHookFile(path, insertHookLine(lines, line), mode|ownerExecute)
}

// insertHookLine places line in a foreign hook script. A script whose
// last effective statement is `exit` would otherwise never reach an
// appended line, so the insertion goes above it; everything else is
// appended at the end.
func insertHookLine(lines []string, line string) string {
	if index := lastExitLineIndex(lines); index >= 0 {
		updated := make([]string, 0, len(lines)+1)
		updated = append(updated, lines[:index]...)
		updated = append(updated, line)
		updated = append(updated, lines[index:]...)
		return withTrailingNewline(strings.Join(updated, "\n"))
	}
	return withTrailingNewline(withTrailingNewline(strings.Join(lines, "\n")) + line)
}

func removeHookLines(path string, owned []string) error {
	existing, mode, present, err := readHookFile(path)
	if err != nil || !present {
		return err
	}

	kept := make([]string, 0, len(existing))
	removed := false
	for _, line := range strings.Split(existing, "\n") {
		if containsLine(owned, line) {
			removed = true
			continue
		}
		kept = append(kept, line)
	}
	if !removed {
		return nil
	}

	for len(kept) > 0 && strings.TrimSpace(kept[len(kept)-1]) == "" {
		kept = kept[:len(kept)-1]
	}
	// A file holding nothing but the shebang sanho itself wrote is
	// sanho's leftover, not the user's hook (audit L5).
	if isShebangOnly(kept) {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("appgit: remove empty hook file %s: %w", path, err)
		}
		return nil
	}
	return writeHookFile(path, withTrailingNewline(strings.Join(kept, "\n")), mode)
}

func hookStatus(path string, hook Hook) (HookState, error) {
	state := HookState{Name: hook.Name, Line: hook.Line, Path: path}

	existing, mode, present, err := readHookFile(path)
	if err != nil || !present {
		return state, err
	}
	state.Executable = mode&ownerExecute != 0

	var legacyForHook []string
	for _, legacy := range legacyHooks {
		if legacy.Name == hook.Name && legacy.Line != hook.Line {
			legacyForHook = append(legacyForHook, legacy.Line)
		}
	}

	for _, line := range strings.Split(existing, "\n") {
		switch {
		case sameLine(line, hook.Line):
			state.Occurrences++
		case containsLine(legacyForHook, line):
			state.Legacy = append(state.Legacy, strings.TrimSpace(line))
		}
	}
	state.Installed = state.Occurrences > 0
	return state, nil
}

// ErrHookIsSymlink reports a hook path that is a symbolic link.
//
// sanho rewrites hook files atomically, and an atomic rewrite is a
// rename over the path — which replaces the LINK rather than the file it
// points at. A user who symlinks `.git/hooks/pre-commit` into a shared
// hooks repository means for edits to land there, so silently severing
// the link is a change they did not ask for and would not see. Refuse
// and name the path (F-L1).
var ErrHookIsSymlink = errors.New("the hook path is a symbolic link")

// readHookFile reads a hook script and its permissions. An absent file
// is reported as present=false rather than as an error: "no hook yet" is
// the ordinary state on a fresh repository.
func readHookFile(path string) (content string, mode os.FileMode, present bool, err error) {
	info, err := os.Lstat(path)
	switch {
	case err == nil:
	case os.IsNotExist(err):
		return "", 0, false, nil
	default:
		return "", 0, false, fmt.Errorf("appgit: inspect hook file %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", 0, false, fmt.Errorf("appgit: %s: %w", path, ErrHookIsSymlink)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", 0, false, fmt.Errorf("appgit: read hook file %s: %w", path, err)
	}
	return string(data), info.Mode().Perm(), true, nil
}

// writeHookFile replaces a hook through the shared atomic writer (§5.7,
// audit M5), so an interrupted install never leaves a truncated script
// that git would still try to run.
func writeHookFile(path, content string, mode os.FileMode) error {
	if err := fsx.WriteFileAtomic(path, []byte(content), mode); err != nil {
		return fmt.Errorf("appgit: write hook file %s: %w", path, err)
	}
	return nil
}

// sameLine is the exact-line comparison the whole file rests on:
// surrounding whitespace is ignored, everything else must match
// character for character. It is never a substring test (audit L3).
func sameLine(a, b string) bool { return strings.TrimSpace(a) == strings.TrimSpace(b) }

func containsLine(lines []string, line string) bool {
	for _, candidate := range lines {
		if sameLine(candidate, line) {
			return true
		}
	}
	return false
}

// isShebangOnly reports a residue that is exactly the shebang sanho
// itself writes, and nothing else (F-L2).
//
// The previous rule treated any run of comments as deletable, so a hook
// file whose only remaining content was the user's own documentation —
// a header explaining what the hook is for, a commented-out line they
// meant to restore — was removed along with sanho's line. Comments are
// content. Only the one line sanho created counts as sanho's leftover.
func isShebangOnly(lines []string) bool {
	seenShebang := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "":
			continue
		case trimmed == hookShebang && !seenShebang:
			seenShebang = true
		default:
			return false
		}
	}
	return true
}

// lastExitLineIndex reports the index of a trailing `exit` statement, or
// -1 when the script does not end in one. Trailing blank lines and
// comments are skipped: they do not change what the shell runs.
func lastExitLineIndex(lines []string) int {
	for i := len(lines) - 1; i >= 0; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) > 0 && strings.TrimRight(fields[0], ";") == "exit" {
			return i
		}
		return -1
	}
	return -1
}

func withTrailingNewline(content string) string {
	if content == "" || strings.HasSuffix(content, "\n") {
		return content
	}
	return content + "\n"
}
