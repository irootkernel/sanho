package appgit_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/irootkernel/sanho/internal/infra/appgit"
)

// The §5.10 installer, against real git hook directories. The property
// under test throughout is exact-line matching (audit L3) and the
// preservation of foreign hook content.

func hooksDir(t *testing.T, dir string) string {
	t.Helper()
	return filepath.Join(dir, ".git", "hooks")
}

func readHook(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(hooksDir(t, dir), name))
	if err != nil {
		t.Fatalf("read hook %s: %v", name, err)
	}
	return string(data)
}

func hookExists(t *testing.T, dir, name string) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join(hooksDir(t, dir), name))
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	t.Fatalf("stat hook %s: %v", name, err)
	return false
}

func writeHook(t *testing.T, dir, name, content string, mode os.FileMode) {
	t.Helper()
	path := filepath.Join(hooksDir(t, dir), name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("create hooks dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatalf("write hook %s: %v", name, err)
	}
}

func countLine(content, line string) int {
	count := 0
	for _, candidate := range strings.Split(content, "\n") {
		if strings.TrimSpace(candidate) == strings.TrimSpace(line) {
			count++
		}
	}
	return count
}

func TestInstallHooksCreatesTheSixHooks(t *testing.T) {
	dir, _ := newRepoWithDocs(t)
	repo := newRepoHandle(t, dir)

	if err := repo.InstallHooks(context.Background()); err != nil {
		t.Fatalf("InstallHooks() error = %v", err)
	}

	want := map[string]string{
		"pre-commit":    "sanho hook pre-commit",
		"commit-msg":    `sanho hook commit-msg "$1"`,
		"pre-push":      `sanho hook pre-push "$@"`,
		"post-checkout": `sanho hook post-checkout "$@"`,
		"post-merge":    "sanho hook post-merge",
		"post-rewrite":  `sanho hook post-rewrite "$@"`,
	}
	var installed []string
	for name, line := range want {
		content := readHook(t, dir, name)
		if !strings.HasPrefix(content, "#!/bin/sh\n") {
			t.Errorf("hook %s does not start with a shebang:\n%s", name, content)
		}
		if countLine(content, line) != 1 {
			t.Errorf("hook %s = %q, want exactly one %q", name, content, line)
		}
		info, err := os.Stat(filepath.Join(hooksDir(t, dir), name))
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
		if info.Mode().Perm()&0100 == 0 {
			t.Errorf("hook %s mode = %o, want it executable", name, info.Mode().Perm())
		}
		installed = append(installed, name)
	}
	sort.Strings(installed)
	if len(installed) != 6 {
		t.Fatalf("installed %v, want six hooks", installed)
	}

	// post-commit is gone in v0.2 (§5.10) and must not be created.
	if hookExists(t, dir, "post-commit") {
		t.Error("InstallHooks() created a post-commit hook, want none")
	}
}

func TestInstallHooksIsIdempotent(t *testing.T) {
	dir, _ := newRepoWithDocs(t)
	repo := newRepoHandle(t, dir)
	ctx := context.Background()

	if err := repo.InstallHooks(ctx); err != nil {
		t.Fatalf("first InstallHooks() error = %v", err)
	}
	first := readHook(t, dir, "pre-push")
	if err := repo.InstallHooks(ctx); err != nil {
		t.Fatalf("second InstallHooks() error = %v", err)
	}

	if second := readHook(t, dir, "pre-push"); second != first {
		t.Fatalf("second install changed the hook:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
	if got := countLine(first, `sanho hook pre-push "$@"`); got != 1 {
		t.Fatalf("pre-push carries %d sanho lines, want 1", got)
	}
}

// The L3 regression: the unquoted v0.1 line is a *substring* of the v0.2
// line. Exact-line matching must treat them as different lines — the
// v0.2 line is added, and the presence of the old one does not make the
// installer think its work is done.
func TestInstallHooksTreatsTheLegacyPrePushLineAsADistinctLine(t *testing.T) {
	dir, _ := newRepoWithDocs(t)
	repo := newRepoHandle(t, dir)

	writeHook(t, dir, "pre-push", "#!/bin/sh\nsanho hook pre-push\n", 0755)
	if err := repo.InstallHooks(context.Background()); err != nil {
		t.Fatalf("InstallHooks() error = %v", err)
	}

	content := readHook(t, dir, "pre-push")
	if got := countLine(content, `sanho hook pre-push "$@"`); got != 1 {
		t.Fatalf("content = %q, want exactly one v0.2 pre-push line", content)
	}
	if got := countLine(content, "sanho hook pre-push"); got != 1 {
		t.Fatalf("content = %q, want the legacy line preserved until removal", content)
	}
}

func TestInstallHooksPreservesForeignContent(t *testing.T) {
	dir, _ := newRepoWithDocs(t)
	repo := newRepoHandle(t, dir)

	writeHook(t, dir, "pre-commit", "#!/bin/sh\nmake lint\nnpm run check\n", 0755)
	if err := repo.InstallHooks(context.Background()); err != nil {
		t.Fatalf("InstallHooks() error = %v", err)
	}

	content := readHook(t, dir, "pre-commit")
	for _, line := range []string{"make lint", "npm run check", "sanho hook pre-commit"} {
		if countLine(content, line) != 1 {
			t.Errorf("content = %q, want it to carry %q once", content, line)
		}
	}
}

func TestInstallHooksInsertsBeforeATrailingExit(t *testing.T) {
	dir, _ := newRepoWithDocs(t)
	repo := newRepoHandle(t, dir)

	writeHook(t, dir, "post-merge", "#!/bin/sh\nmake generate\nexit 0\n", 0755)
	if err := repo.InstallHooks(context.Background()); err != nil {
		t.Fatalf("InstallHooks() error = %v", err)
	}

	// An appended line after `exit 0` would never run.
	lines := strings.Split(strings.TrimRight(readHook(t, dir, "post-merge"), "\n"), "\n")
	want := []string{"#!/bin/sh", "make generate", "sanho hook post-merge", "exit 0"}
	if strings.Join(lines, "|") != strings.Join(want, "|") {
		t.Fatalf("hook lines = %v, want %v", lines, want)
	}
}

func TestInstallHooksRepairsANonExecutableHook(t *testing.T) {
	dir, _ := newRepoWithDocs(t)
	repo := newRepoHandle(t, dir)

	writeHook(t, dir, "pre-commit", "#!/bin/sh\nsanho hook pre-commit\n", 0644)
	if err := repo.InstallHooks(context.Background()); err != nil {
		t.Fatalf("InstallHooks() error = %v", err)
	}

	info, err := os.Stat(filepath.Join(hooksDir(t, dir), "pre-commit"))
	if err != nil {
		t.Fatalf("stat pre-commit: %v", err)
	}
	if info.Mode().Perm()&0100 == 0 {
		t.Fatalf("mode = %o, want the executable bit repaired", info.Mode().Perm())
	}
}

func TestRemoveHooksDeletesShebangOnlyFiles(t *testing.T) {
	dir, _ := newRepoWithDocs(t)
	repo := newRepoHandle(t, dir)
	ctx := context.Background()

	if err := repo.InstallHooks(ctx); err != nil {
		t.Fatalf("InstallHooks() error = %v", err)
	}
	if err := repo.RemoveHooks(ctx); err != nil {
		t.Fatalf("RemoveHooks() error = %v", err)
	}

	// Every file sanho created held only a shebang and its own line, so
	// nothing of the user's remains to keep (audit L5).
	for _, hook := range appgit.Hooks() {
		if hookExists(t, dir, hook.Name) {
			t.Errorf("hook %s survived removal, want the stub deleted", hook.Name)
		}
	}
}

func TestRemoveHooksPreservesForeignContentAndTheExecutableBit(t *testing.T) {
	dir, _ := newRepoWithDocs(t)
	repo := newRepoHandle(t, dir)
	ctx := context.Background()

	writeHook(t, dir, "pre-commit", "#!/bin/sh\nmake lint\n", 0700)
	if err := repo.InstallHooks(ctx); err != nil {
		t.Fatalf("InstallHooks() error = %v", err)
	}
	if err := repo.RemoveHooks(ctx); err != nil {
		t.Fatalf("RemoveHooks() error = %v", err)
	}

	content := readHook(t, dir, "pre-commit")
	if countLine(content, "make lint") != 1 {
		t.Fatalf("content = %q, want the user's line preserved", content)
	}
	if strings.Contains(content, "sanho hook") {
		t.Fatalf("content = %q, want no sanho line", content)
	}
	info, err := os.Stat(filepath.Join(hooksDir(t, dir), "pre-commit"))
	if err != nil {
		t.Fatalf("stat pre-commit: %v", err)
	}
	if info.Mode().Perm() != 0700 {
		t.Fatalf("mode = %o, want the file's own 0700 preserved", info.Mode().Perm())
	}
}

// migrate and clean must leave nothing of a v0.1 install behind,
// including the post-commit hook v0.2 has no replacement for and the
// unquoted pre-push line.
func TestRemoveHooksRemovesTheLegacyV01Lines(t *testing.T) {
	dir, _ := newRepoWithDocs(t)
	repo := newRepoHandle(t, dir)

	legacy := map[string]string{
		"pre-commit":    "sanho hook pre-commit",
		"commit-msg":    `sanho hook commit-msg "$1"`,
		"pre-push":      "sanho hook pre-push",
		"post-checkout": "sanho hook post-checkout",
		"post-merge":    "sanho hook post-merge",
		"post-rewrite":  `sanho hook post-rewrite "$@"`,
		"post-commit":   "sanho hook post-commit",
	}
	for name, line := range legacy {
		writeHook(t, dir, name, "#!/bin/sh\n"+line+"\n", 0755)
	}

	if err := repo.RemoveHooks(context.Background()); err != nil {
		t.Fatalf("RemoveHooks() error = %v", err)
	}
	for name := range legacy {
		if hookExists(t, dir, name) {
			t.Errorf("legacy hook %s survived removal:\n%s", name, readHook(t, dir, name))
		}
	}
}

func TestHooksStatusReportsInstalledMissingAndDuplicated(t *testing.T) {
	dir, _ := newRepoWithDocs(t)
	repo := newRepoHandle(t, dir)
	ctx := context.Background()

	if err := repo.InstallHooks(ctx); err != nil {
		t.Fatalf("InstallHooks() error = %v", err)
	}
	// Hand-duplicate one line and drop another file entirely.
	writeHook(t, dir, "pre-commit", "#!/bin/sh\nsanho hook pre-commit\nsanho hook pre-commit\n", 0755)
	if err := os.Remove(filepath.Join(hooksDir(t, dir), "post-merge")); err != nil {
		t.Fatalf("remove post-merge: %v", err)
	}
	writeHook(t, dir, "pre-push", "#!/bin/sh\nsanho hook pre-push \"$@\"\nsanho hook pre-push\n", 0755)

	states, err := repo.HooksStatus(ctx)
	if err != nil {
		t.Fatalf("HooksStatus() error = %v", err)
	}
	byName := map[string]appgit.HookState{}
	for _, state := range states {
		byName[state.Name] = state
	}

	if got := byName["pre-commit"].Occurrences; got != 2 {
		t.Errorf("pre-commit occurrences = %d, want 2", got)
	}
	if byName["post-merge"].Installed {
		t.Error("post-merge reported installed, want missing")
	}
	if !byName["pre-push"].Installed || len(byName["pre-push"].Legacy) != 1 {
		t.Errorf("pre-push = %+v, want installed with one legacy line", byName["pre-push"])
	}
	if !byName["commit-msg"].Executable {
		t.Error("commit-msg reported non-executable, want executable")
	}
}

// --- F-L1 / F-L2: a hook file is the user's, and sanho is a guest ------

// TestInstallHooksRefusesASymlinkedHook.
//
// sanho rewrites hook files atomically, and an atomic rewrite is a
// rename over the path — which replaces the LINK, not the file it points
// at. A user who symlinks .git/hooks/pre-commit into a shared hooks
// repository means their edits to land there; severing that silently is
// a change they did not ask for and would not see.
func TestInstallHooksRefusesASymlinkedHook(t *testing.T) {
	dir, _ := newRepoWithDocs(t)
	repo := newRepoHandle(t, dir)

	shared := filepath.Join(dir, "shared-pre-commit")
	if err := os.WriteFile(shared, []byte("#!/bin/sh\necho shared\n"), 0755); err != nil {
		t.Fatalf("write the shared hook: %v", err)
	}
	link := filepath.Join(dir, ".git", "hooks", "pre-commit")
	if err := os.MkdirAll(filepath.Dir(link), 0755); err != nil {
		t.Fatalf("create the hooks directory: %v", err)
	}
	if err := os.Symlink(shared, link); err != nil {
		t.Skipf("symlinks are not available here: %v", err)
	}

	err := repo.InstallHooks(context.Background())
	if !errors.Is(err, appgit.ErrHookIsSymlink) {
		t.Fatalf("InstallHooks = %v, want ErrHookIsSymlink", err)
	}
	if !strings.Contains(err.Error(), link) {
		t.Errorf("error = %v, want it to name the hook path", err)
	}

	// The link and its target are exactly as they were.
	info, lstatErr := os.Lstat(link)
	if lstatErr != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the symlink was replaced (lstat err %v)", lstatErr)
	}
	data, readErr := os.ReadFile(shared)
	if readErr != nil || !strings.Contains(string(data), "echo shared") {
		t.Fatalf("the shared hook was rewritten: %q (%v)", data, readErr)
	}
}

// TestRemoveHooksKeepsAUsersCommentOnlyHook.
//
// The deletion rule is "the file holds nothing but the shebang sanho
// itself wrote". Treating any run of comments as deletable took the
// user's own documentation with it — a header explaining what the hook
// is for, a commented-out line they meant to restore. Comments are
// content.
func TestRemoveHooksKeepsAUsersCommentOnlyHook(t *testing.T) {
	dir, _ := newRepoWithDocs(t)
	repo := newRepoHandle(t, dir)
	ctx := context.Background()

	path := filepath.Join(dir, ".git", "hooks", "pre-commit")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("create the hooks directory: %v", err)
	}
	original := "#!/bin/sh\n# our team's pre-commit notes live here\n# TODO: re-enable the linter\n"
	if err := os.WriteFile(path, []byte(original), 0755); err != nil {
		t.Fatalf("write the hook: %v", err)
	}

	if err := repo.InstallHooks(ctx); err != nil {
		t.Fatalf("InstallHooks: %v", err)
	}
	if err := repo.RemoveHooks(ctx); err != nil {
		t.Fatalf("RemoveHooks: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the user's comment-only hook was deleted: %v", err)
	}
	for _, want := range []string{"our team's pre-commit notes", "re-enable the linter"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("hook file lost %q:\n%s", want, data)
		}
	}
	if strings.Contains(string(data), "sanho hook") {
		t.Errorf("sanho's line survived removal:\n%s", data)
	}
}
