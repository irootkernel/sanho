package canonical

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/irootkernel/sanho/internal/infra/gitx"
)

// Every test in this package runs against a real git binary; there are
// no fakes below the git boundary (AGENTS.md Git-boundary testing rule).

// TestMain isolates the suite from the developer's git configuration.
// Publication depends on default git behavior (merge drivers, autocrlf,
// hooks, signing), so a stray ~/.gitconfig must not be able to change
// what these tests observe.
func TestMain(m *testing.M) {
	for key, value := range map[string]string{
		"GIT_CONFIG_GLOBAL":   os.DevNull,
		"GIT_CONFIG_SYSTEM":   os.DevNull,
		"GIT_AUTHOR_NAME":     "Test Author",
		"GIT_AUTHOR_EMAIL":    "author@example.test",
		"GIT_COMMITTER_NAME":  "Test Committer",
		"GIT_COMMITTER_EMAIL": "committer@example.test",
	} {
		if err := os.Setenv(key, value); err != nil {
			fmt.Fprintf(os.Stderr, "set %s: %v\n", key, err)
			os.Exit(1)
		}
	}
	os.Exit(m.Run())
}

// entry is one file in a composed tree.
type entry struct {
	content []byte
	// mode is the filesystem mode; 0755 becomes git mode 100755.
	mode os.FileMode
	// symlink, when set, makes the entry a symlink to that target
	// instead of a regular file.
	symlink string
}

func text(content string) entry { return entry{content: []byte(content)} }

func exe(content string) entry { return entry{content: []byte(content), mode: 0755} }

func link(target string) entry { return entry{symlink: target} }

func binary(payload string) entry {
	return entry{content: append([]byte{0x89, 'P', 'N', 'G', 0x00, 0x1a}, payload...)}
}

// gitRun runs git in dir and fails the test on error.
func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	res, err := gitx.New(dir).Run(context.Background(), args...)
	if err != nil {
		t.Fatalf("git %s in %s: %v", strings.Join(args, " "), dir, err)
	}
	return string(res.Stdout)
}

// gitLine runs git in dir and returns the first output line.
func gitLine(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := gitx.New(dir).Line(context.Background(), args...)
	if err != nil {
		t.Fatalf("git %s in %s: %v", strings.Join(args, " "), dir, err)
	}
	return out
}

// materialize writes files into dir, replacing whatever docs content was
// there before so composed trees never inherit leftovers.
func materialize(t *testing.T, dir string, files map[string]entry) {
	t.Helper()

	names, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	for _, name := range names {
		if name.Name() == ".git" {
			continue
		}
		if err := os.RemoveAll(filepath.Join(dir, name.Name())); err != nil {
			t.Fatalf("clear %s: %v", filepath.Join(dir, name.Name()), err)
		}
	}

	for path, file := range files {
		full := filepath.Join(dir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatalf("create directory for %s: %v", full, err)
		}
		if file.symlink != "" {
			if err := os.Symlink(file.symlink, full); err != nil {
				t.Fatalf("symlink %s: %v", full, err)
			}
			continue
		}
		mode := file.mode
		if mode == 0 {
			mode = 0644
		}
		if err := os.WriteFile(full, file.content, mode); err != nil {
			t.Fatalf("write %s: %v", full, err)
		}
	}
}

// newWorkRepo creates an initialized non-bare repository on branch main.
func newWorkRepo(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("create %s: %v", dir, err)
	}
	gitRun(t, dir, "init", "--quiet", "-b", "main")
	return dir
}

// treeFactory composes trees in a scratch repository so that several
// trees live in one object database — the precondition MergeTree states
// for its three inputs.
type treeFactory struct {
	dir string
}

func newTreeFactory(t *testing.T) *treeFactory {
	t.Helper()
	return &treeFactory{dir: newWorkRepo(t, "trees")}
}

// tree writes files into the scratch worktree and returns the resulting
// tree OID, without creating a commit.
func (f *treeFactory) tree(t *testing.T, files map[string]entry) string {
	t.Helper()
	materialize(t, f.dir, files)
	gitRun(t, f.dir, "add", "-A", "--", ".")
	return gitLine(t, f.dir, "write-tree")
}

// blobAt reads a path out of a tree.
func (f *treeFactory) blobAt(t *testing.T, tree, path string) string {
	t.Helper()
	return gitRun(t, f.dir, "cat-file", "blob", tree+":"+path)
}

// lsTree lists a tree recursively as "<mode> <type> <oid>\t<path>" lines.
func (f *treeFactory) lsTree(t *testing.T, tree string) []string {
	t.Helper()
	var out []string
	for _, line := range strings.Split(gitRun(t, f.dir, "ls-tree", "-r", tree), "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}

// hunkFile builds a file with three widely separated regions. The gap is
// 20 lines because git's merge machinery coalesces conflict regions that
// sit closer than about 13 lines of common context — the P1 empirical
// finding that makes multi-hunk conflicts reproducible (audit C2's blind
// spot).
func hunkFile(first, second, third string) entry {
	gap := make([]string, 20)
	for i := range gap {
		gap[i] = fmt.Sprintf("context line %02d", i)
	}
	spacer := strings.Join(gap, "\n")
	return text(first + "\n" + spacer + "\n" + second + "\n" + spacer + "\n" + third + "\n")
}

// countMarkerStarts counts conflict regions in merged content.
func countMarkerStarts(content string) int {
	count := 0
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "<<<<<<< ") {
			count++
		}
	}
	return count
}
