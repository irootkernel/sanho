package appgit_test

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/irootkernel/sanho/internal/infra/appgit"
	"github.com/irootkernel/sanho/internal/infra/gitx"
)

func inspectionRepo(t *testing.T, dir string) *appgit.Repo {
	t.Helper()
	r, err := appgit.NewInspection(context.Background(), dir, "docs")
	if err != nil {
		t.Fatalf("inspection reader: %v", err)
	}
	return r
}

func removeGitObject(t *testing.T, dir, oid string) {
	t.Helper()
	path := gitLine(t, dir, "rev-parse", "--git-path", "objects/"+oid[:2]+"/"+oid[2:])
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
}

func TestHeadReadersDistinguishUnbornAndMissingObjects(t *testing.T) {
	for _, packed := range []bool{false, true} {
		t.Run(fmt.Sprintf("missing-commit-packed-%t", packed), func(t *testing.T) {
			dir, head := newRepoWithDocs(t)
			if packed {
				gitRun(t, dir, "pack-refs", "--all")
			}
			removeGitObject(t, dir, head)
			for _, r := range []*appgit.Repo{newRepoHandle(t, dir), inspectionRepo(t, dir)} {
				if oid, err := r.HeadCommit(context.Background()); err == nil {
					t.Fatalf("missing commit reported as %q", oid)
				}
				if oid, err := r.HeadDocsTree(context.Background()); err == nil {
					t.Fatalf("missing commit docs reported as %q", oid)
				}
				if oid, err := r.WorktreeDocsTree(context.Background()); err == nil {
					t.Fatalf("missing scratch seed reported as %q", oid)
				}
			}
		})
	}
	t.Run("unborn", func(t *testing.T) {
		dir := newRepo(t)
		r := inspectionRepo(t, dir)
		if oid, err := r.HeadCommit(context.Background()); err != nil || oid != "" {
			t.Fatalf("unborn=%q %v", oid, err)
		}
		writeFile(t, dir, "docs/new.md", []byte("new\n"))
		if tree, err := r.WorktreeDocsTree(context.Background()); err != nil || tree == "" {
			t.Fatalf("unborn scratch=%q %v", tree, err)
		}
	})
	t.Run("detached", func(t *testing.T) {
		dir, head := newRepoWithDocs(t)
		gitRun(t, dir, "checkout", "--detach", head)
		if oid, err := inspectionRepo(t, dir).HeadCommit(context.Background()); err != nil || oid != head {
			t.Fatalf("detached=%q %v", oid, err)
		}
	})
	for _, head := range []string{strings.Repeat("f", 40) + "\n", "not a ref\n", "ref: refs/heads/bad..name\n", "ref: refs/tags/absent\n"} {
		t.Run(strings.TrimSpace(head), func(t *testing.T) {
			dir, _ := newRepoWithDocs(t)
			writeFile(t, dir, ".git/HEAD", []byte(head))
			if oid, err := newRepoHandle(t, dir).HeadCommit(context.Background()); err == nil {
				t.Fatalf("invalid HEAD=%q", oid)
			}
		})
	}
}

func TestTreeReadersRejectMissingRequiredObjects(t *testing.T) {
	for _, spec := range []string{"HEAD^{tree}", "HEAD:docs", "HEAD:docs/a.md", "HEAD:docs/nested"} {
		t.Run(spec, func(t *testing.T) {
			dir, _ := newRepoWithDocs(t)
			tree := gitLine(t, dir, "rev-parse", "HEAD:docs")
			removeGitObject(t, dir, gitLine(t, dir, "rev-parse", spec))
			r := inspectionRepo(t, dir)
			if _, err := r.HeadDocsTree(context.Background()); err == nil {
				t.Fatal("missing required object produced docs tree")
			}
			if _, err := r.WorktreeDocsTree(context.Background()); err == nil {
				t.Fatal("missing required object produced scratch tree")
			}
			if spec != "HEAD^{tree}" {
				if _, err := r.DocsTreeChangedPaths(context.Background(), tree, tree); err == nil {
					t.Fatal("equal missing trees accepted")
				}
			}
		})
	}
	t.Run("absent-docs", func(t *testing.T) {
		dir := newRepo(t)
		writeFile(t, dir, "code.txt", []byte("code\n"))
		commitAll(t, dir, "code only")
		r := inspectionRepo(t, dir)
		want, err := r.EmptyTree(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if got, err := r.HeadDocsTree(context.Background()); err != nil || got != want {
			t.Fatalf("absent docs=%q %v", got, err)
		}
	})
}

func TestTreeReadersRejectCorruptBlobContents(t *testing.T) {
	dir, _ := newRepoWithDocs(t)
	blob := gitLine(t, dir, "rev-parse", "HEAD:docs/a.md")
	tree := gitLine(t, dir, "rev-parse", "HEAD:docs")
	var encoded bytes.Buffer
	writer := zlib.NewWriter(&encoded)
	if _, err := writer.Write([]byte("blob 1000\x00short")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	removeGitObject(t, dir, blob)
	writeFile(t, dir, ".git/objects/"+blob[:2]+"/"+blob[2:], encoded.Bytes())
	r := inspectionRepo(t, dir)
	if _, err := r.DocsTreeChangedPaths(context.Background(), tree, tree); err == nil {
		t.Fatal("corrupt blob body became zero drift")
	}
}

func TestAncestryReaderRejectsMissingEntryCommit(t *testing.T) {
	dir, entry := newRepoWithDocs(t)
	writeFile(t, dir, "docs/a.md", []byte("resolved\n"))
	head := commitAll(t, dir, "resolution")
	removeGitObject(t, dir, entry)
	for _, reader := range []*appgit.Repo{newRepoHandle(t, dir), inspectionRepo(t, dir)} {
		ancestor, err := reader.IsAncestor(context.Background(), entry, head)
		var exit *gitx.ExitError
		if ancestor || !errors.As(err, &exit) || exit.Result.ExitCode <= 1 {
			t.Fatalf("missing entry commit became unrelated history: %t %v", ancestor, err)
		}
	}
}

func TestDocsTreeReaderRejectsAFileAtTheDocsPath(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, dir, "docs", []byte("file, not a docs directory\n"))
	head := commitAll(t, dir, "docs file")
	for _, reader := range []*appgit.Repo{newRepoHandle(t, dir), inspectionRepo(t, dir)} {
		if _, err := reader.DocsTreeOf(context.Background(), head); err == nil {
			t.Fatal("docs file became an empty tree")
		}
		if _, err := reader.HeadDocsTree(context.Background()); err == nil {
			t.Fatal("HEAD docs file became an empty tree")
		}
	}
}

func TestInspectionSuppressesInheritedTraceWrites(t *testing.T) {
	dir, _ := newRepoWithDocs(t)
	for _, name := range []string{"GIT_TRACE", "GIT_TRACE2", "GIT_TRACE2_EVENT", "GIT_TRACE2_PERF"} {
		t.Run(name, func(t *testing.T) {
			sentinel := filepath.Join(t.TempDir(), "trace")
			t.Setenv(name, sentinel)
			gitRun(t, dir, "status", "--porcelain")
			if err := os.Remove(sentinel); err != nil {
				t.Fatalf("trace positive control: %v", err)
			}
			if _, err := inspectionRepo(t, dir).WorktreeDocsTree(context.Background()); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
				t.Fatalf("inspection wrote inherited trace: %v", err)
			}
		})
	}
}

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }

func TestInspectionRejectsFiltersBeforeStatusOrScratch(t *testing.T) {
	for _, kind := range []string{"clean", "process"} {
		t.Run(kind, func(t *testing.T) {
			dir, _ := newRepoWithDocs(t)
			sentinel := filepath.Join(t.TempDir(), "executed")
			command := "printf x >> " + shellQuote(sentinel) + "; cat"
			if kind == "process" {
				t.Setenv("SANHO_INSPECTION_FILTER_SENTINEL", sentinel)
				command = shellQuote(os.Args[0]) + " -test.run=^TestInspectionFilterProcessHelper$"
			}
			gitRun(t, dir, "config", "filter.probe."+kind, command)
			gitRun(t, dir, "config", "filter.probe.required", "true")
			writeFile(t, dir, ".gitattributes", []byte("docs/** filter=probe\n"))
			commitAll(t, dir, "configure filter")
			// Positive controls prove both status and scratch reads can execute it.
			for _, read := range []string{"status", "scratch"} {
				if err := os.Remove(sentinel); err != nil && !os.IsNotExist(err) {
					t.Fatal(err)
				}
				future := time.Now().Add(10 * time.Second)
				if err := os.Chtimes(filepath.Join(dir, "docs/a.md"), future, future); err != nil {
					t.Fatal(err)
				}
				ordinary := newRepoHandle(t, dir)
				if read == "status" {
					if _, err := ordinary.DocsClean(context.Background()); err != nil {
						t.Fatal(err)
					}
				} else {
					if _, err := ordinary.WorktreeDocsTree(context.Background()); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := os.Stat(sentinel); err != nil {
					t.Fatalf("%s positive control did not execute filter: %v", read, err)
				}
				if err := os.Remove(sentinel); err != nil {
					t.Fatal(err)
				}
				before, err := os.ReadFile(filepath.Join(dir, ".git/index"))
				if err != nil {
					t.Fatal(err)
				}
				strict := inspectionRepo(t, dir)
				if read == "status" {
					_, err = strict.DocsClean(context.Background())
				} else {
					_, err = strict.WorktreeDocsTree(context.Background())
				}
				if !errors.Is(err, appgit.ErrExternalFilterConfigured) {
					t.Fatalf("%s error=%v", read, err)
				}
				if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
					t.Fatalf("inspection invoked %s filter", kind)
				}
				after, err := os.ReadFile(filepath.Join(dir, ".git/index"))
				if err != nil || !bytes.Equal(before, after) {
					t.Fatal("inspection changed real index")
				}
			}
		})
	}
}

// A real long-running process filter for positive controls. Git drives the
// packet protocol; inspection must never launch this process.
func TestInspectionFilterProcessHelper(t *testing.T) {
	sentinel := os.Getenv("SANHO_INSPECTION_FILTER_SENTINEL")
	if sentinel == "" {
		return
	}
	in := bufio.NewReader(os.Stdin)
	readPacket := func() ([]byte, error) {
		header := make([]byte, 4)
		if _, err := io.ReadFull(in, header); err != nil {
			return nil, err
		}
		n, err := strconv.ParseInt(string(header), 16, 32)
		if err != nil {
			return nil, err
		}
		if n == 0 {
			return nil, nil
		}
		if n < 4 {
			return nil, errors.New("invalid packet")
		}
		data := make([]byte, n-4)
		_, err = io.ReadFull(in, data)
		return data, err
	}
	writePacket := func(data []byte) {
		if _, err := fmt.Fprintf(os.Stdout, "%04x%s", len(data)+4, data); err != nil {
			os.Exit(2)
		}
	}
	flush := func() {
		if _, err := io.WriteString(os.Stdout, "0000"); err != nil {
			os.Exit(2)
		}
	}
	readGroup := func() ([]byte, error) {
		var all []byte
		for {
			data, err := readPacket()
			if err != nil {
				return nil, err
			}
			if data == nil {
				return all, nil
			}
			all = append(all, data...)
		}
	}
	if _, err := readGroup(); err != nil {
		os.Exit(2)
	}
	writePacket([]byte("git-filter-server\n"))
	writePacket([]byte("version=2\n"))
	flush()
	if _, err := readGroup(); err != nil {
		os.Exit(2)
	}
	writePacket([]byte("capability=clean\n"))
	flush()
	for {
		if _, err := readGroup(); err == io.EOF {
			os.Exit(0)
		} else if err != nil {
			os.Exit(2)
		}
		data, err := readGroup()
		if err != nil {
			os.Exit(2)
		}
		f, err := os.OpenFile(sentinel, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			os.Exit(2)
		}
		if _, err = f.WriteString("executed\n"); err != nil {
			os.Exit(2)
		}
		if err = f.Close(); err != nil {
			os.Exit(2)
		}
		writePacket([]byte("status=success\n"))
		flush()
		if len(data) > 0 {
			writePacket(data)
		}
		flush()
		flush()
	}
}

func TestInspectionConfigurationAdmission(t *testing.T) {
	for _, scope := range []string{"local-unused", "global", "includeIf", "worktree", "environment"} {
		t.Run(scope, func(t *testing.T) {
			dir, _ := newRepoWithDocs(t)
			switch scope {
			case "local-unused":
				gitRun(t, dir, "config", "filter.unused.clean", "must-not-run")
			case "global":
				config := filepath.Join(t.TempDir(), "global")
				if err := os.WriteFile(config, []byte("[filter \"unused\"]\nclean = must-not-run\n"), 0600); err != nil {
					t.Fatal(err)
				}
				t.Setenv("GIT_CONFIG_GLOBAL", config)
			case "includeIf":
				config := filepath.Join(t.TempDir(), "included")
				if err := os.WriteFile(config, []byte("[filter \"unused\"]\nprocess = must-not-run\n"), 0600); err != nil {
					t.Fatal(err)
				}
				gitDir, err := filepath.EvalSymlinks(filepath.Join(dir, ".git"))
				if err != nil {
					t.Fatal(err)
				}
				gitRun(t, dir, "config", "includeIf.gitdir:"+gitDir+".path", config)
			case "worktree":
				gitRun(t, dir, "config", "extensions.worktreeConfig", "true")
				gitRun(t, dir, "config", "--worktree", "filter.unused.clean", "must-not-run")
			case "environment":
				t.Setenv("GIT_CONFIG_COUNT", "1")
				t.Setenv("GIT_CONFIG_KEY_0", "filter.unused.process")
				t.Setenv("GIT_CONFIG_VALUE_0", "must-not-run")
			}
			if _, err := inspectionRepo(t, dir).DocsClean(context.Background()); !errors.Is(err, appgit.ErrExternalFilterConfigured) {
				t.Fatalf("%s=%v", scope, err)
			}
		})
	}
	t.Run("last-effective-value", func(t *testing.T) {
		dir, _ := newRepoWithDocs(t)
		gitRun(t, dir, "config", "--add", "filter.unused.clean", "must-not-run")
		gitRun(t, dir, "config", "--add", "filter.unused.clean", "")
		if clean, err := inspectionRepo(t, dir).DocsClean(context.Background()); err != nil || !clean {
			t.Fatalf("empty override=%t %v", clean, err)
		}
	})
	t.Run("changed", func(t *testing.T) {
		dir, _ := newRepoWithDocs(t)
		r := inspectionRepo(t, dir)
		if _, err := r.DocsClean(context.Background()); err != nil {
			t.Fatal(err)
		}
		gitRun(t, dir, "config", "core.autocrlf", "true")
		if _, err := r.WorktreeDocsTree(context.Background()); !errors.Is(err, gitx.ErrInspectionPolicyUnavailable) {
			t.Fatalf("changed config=%v", err)
		}
	})
	t.Run("corrupt-config", func(t *testing.T) {
		dir, _ := newRepoWithDocs(t)
		r := inspectionRepo(t, dir)
		writeFile(t, dir, ".git/config", []byte("[broken\n"))
		if _, err := r.DocsClean(context.Background()); err == nil || errors.Is(err, appgit.ErrExternalFilterConfigured) || errors.Is(err, gitx.ErrInspectionPolicyUnavailable) {
			t.Fatalf("corrupt config=%v", err)
		}
	})
}

func TestInspectionBuiltinConversionsAndNoFSMonitor(t *testing.T) {
	dir, _ := newRepoWithDocs(t)
	writeFile(t, dir, ".gitattributes", []byte("docs/eol.txt text eol=lf\ndocs/ident.txt ident\n"))
	writeFile(t, dir, "docs/eol.txt", []byte("one\ntwo\n"))
	writeFile(t, dir, "docs/ident.txt", []byte("$Id$\n"))
	writeFile(t, dir, "docs/[literal]*, space\n한글.md", []byte("literal\n"))
	commitAll(t, dir, "conversion fixtures")
	writeFile(t, dir, "docs/eol.txt", []byte("one\r\ntwo\r\n"))
	writeFile(t, dir, "docs/ident.txt", []byte("$Id: 0123456789012345678901234567890123456789 $\n"))
	wantClean, err := newRepoHandle(t, dir).DocsClean(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	other := buildDocsTree(t, dir, map[string]blobSpec{"a.md": regular("changed\n")})
	sentinel := filepath.Join(t.TempDir(), "executed")
	program := filepath.Join(t.TempDir(), "fsmonitor")
	if err := os.WriteFile(program, []byte("#!/bin/sh\nprintf x >> "+shellQuote(sentinel)+"\nprintf 'token\\0/\\0'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "config", "core.fsmonitor", program)
	if _, err := newRepoHandle(t, dir).DocsClean(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(sentinel); err != nil {
		t.Fatalf("fsmonitor positive control: %v", err)
	}
	before, err := os.ReadFile(filepath.Join(dir, ".git/index"))
	if err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(10 * time.Second)
	if err := os.Chtimes(filepath.Join(dir, "docs/a.md"), future, future); err != nil {
		t.Fatal(err)
	}
	r := inspectionRepo(t, dir)
	ctx := context.Background()
	if clean, err := r.DocsClean(ctx); err != nil || clean != wantClean {
		t.Fatalf("normalized cleanliness=%t %v, ordinary=%t", clean, err, wantClean)
	}
	head, err := r.HeadDocsTree(ctx)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := r.WorktreeDocsTree(ctx)
	if err != nil || actual != head {
		t.Fatalf("normalized trees %q != %q: %v", actual, head, err)
	}
	// This name-only comparison cannot invoke diff drivers. Its result does
	// not establish a positive trigger for external diff or textconv controls.
	if _, err := r.DocsTreeChangedPaths(ctx, head, other); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatal("inspection executed configured program")
	}
	after, err := os.ReadFile(filepath.Join(dir, ".git/index"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("inspection changed index")
	}
}

func TestInspectionRejectsNewScratchGitlink(t *testing.T) {
	dir, _ := newRepoWithDocs(t)
	nested := filepath.Join(dir, "docs/nested-repo")
	gitRun(t, dir, "init", "-q", nested)
	writeFile(t, nested, "file", []byte("same\n"))
	commitAll(t, nested, "nested")
	before, err := os.ReadFile(filepath.Join(dir, ".git/index"))
	if err != nil {
		t.Fatal(err)
	}
	normalTree, err := newRepoHandle(t, dir).WorktreeDocsTree(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if entries := gitRun(t, dir, "ls-tree", normalTree); !strings.Contains(entries, "160000 commit ") {
		t.Fatalf("ordinary staging did not produce a gitlink: %q", entries)
	}
	if _, err := inspectionRepo(t, dir).WorktreeDocsTree(context.Background()); !errors.Is(err, gitx.ErrInspectionPolicyUnavailable) {
		t.Fatalf("new scratch gitlink admitted: %v", err)
	}
	after, err := os.ReadFile(filepath.Join(dir, ".git/index"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("scratch gitlink staging changed the real index")
	}
}

func TestInspectionCorruptIndexAndCancellationAreErrors(t *testing.T) {
	dir, _ := newRepoWithDocs(t)
	r := inspectionRepo(t, dir)
	writeFile(t, dir, ".git/index", []byte("invalid index"))
	if clean, err := r.DocsClean(context.Background()); err == nil || clean {
		t.Fatalf("corrupt index=%t %v", clean, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.HeadCommit(ctx); err == nil {
		t.Fatal("cancelled HEAD looked unborn")
	}
	if _, err := r.WorktreeDocsTree(ctx); err == nil {
		t.Fatal("cancelled scratch read succeeded")
	}
}

func TestInspectionPreservesLinkedWorktreeAndGitContent(t *testing.T) {
	dir, _ := newRepoWithDocs(t)
	writeFile(t, dir, ".gitattributes", []byte("docs/encoded.txt working-tree-encoding=UTF-16LE\n"))
	writeFile(t, dir, "docs/encoded.txt", []byte{'a', 0, '\n', 0})
	writeFile(t, dir, "docs/ignored.md", []byte("tracked\n"))
	writeFile(t, dir, "docs/executable", []byte("executable\n"))
	if err := os.Chmod(filepath.Join(dir, "docs/executable"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("a.md", filepath.Join(dir, "docs/link")); err != nil {
		t.Fatal(err)
	}
	commitAll(t, dir, "Git content")
	writeFile(t, dir, ".gitignore", []byte("docs/ignored.md\n"))
	head := commitAll(t, dir, "ignore tracked path")
	gitRun(t, dir, "pack-refs", "--all")
	linked := filepath.Join(t.TempDir(), "linked")
	gitRun(t, dir, "worktree", "add", "-q", "-b", "linked", linked)
	r := inspectionRepo(t, linked)
	if got, err := r.HeadCommit(context.Background()); err != nil || got != head {
		t.Fatalf("linked HEAD=%q %v", got, err)
	}
	want, err := r.HeadDocsTree(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got, err := r.WorktreeDocsTree(context.Background()); err != nil || got != want {
		t.Fatalf("Git normalization=%q, want %q: %v", got, want, err)
	}
	writeFile(t, linked, "docs/ignored.md", []byte("changed\n"))
	if got, err := r.WorktreeDocsTree(context.Background()); err != nil || got == want {
		t.Fatalf("tracked ignored change lost: %q %v", got, err)
	}
}

func TestInspectionDisablesIndexHooksAndTraceDestinations(t *testing.T) {
	dir, _ := newRepoWithDocs(t)
	sentinel := filepath.Join(t.TempDir(), "hook")
	hook := filepath.Join(dir, ".git/hooks/post-index-change")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nprintf x >> "+shellQuote(sentinel)+"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := newRepoHandle(t, dir).WorktreeDocsTree(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(sentinel); err != nil {
		t.Fatalf("scratch hook positive control: %v", err)
	}
	traceDir := t.TempDir()
	config := filepath.Join(traceDir, "global")
	if err := os.WriteFile(config, []byte("[trace2]\nnormalTarget = "+filepath.Join(traceDir, "normal")+"\neventTarget = "+filepath.Join(traceDir, "event")+"\nperfTarget = "+filepath.Join(traceDir, "perf")+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", config)
	gitRun(t, dir, "config", "--list")
	for _, name := range []string{"normal", "event", "perf"} {
		if err := os.Remove(filepath.Join(traceDir, name)); err != nil {
			t.Fatalf("trace positive control %s: %v", name, err)
		}
	}
	r := inspectionRepo(t, dir)
	if _, err := r.DocsClean(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := r.WorktreeDocsTree(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{sentinel, filepath.Join(traceDir, "normal"), filepath.Join(traceDir, "event"), filepath.Join(traceDir, "perf")} {
		if _, err := os.Stat(name); !os.IsNotExist(err) {
			t.Fatalf("inspection wrote %s: %v", name, err)
		}
	}
}

func TestInspectionRejectsSubmoduleExecution(t *testing.T) {
	dir, _ := newRepoWithDocs(t)
	nested := filepath.Join(dir, "docs/nested-repo")
	gitRun(t, dir, "init", "-q", nested)
	writeFile(t, nested, "file", []byte("same\n"))
	writeFile(t, nested, ".gitattributes", []byte("file filter=probe\n"))
	commitAll(t, nested, "nested")
	commitAll(t, dir, "gitlink")
	normal := newRepoHandle(t, dir)
	wantTree := gitLine(t, dir, "rev-parse", "HEAD:docs")
	if tree, err := normal.HeadDocsTree(context.Background()); err != nil || tree != wantTree {
		t.Fatalf("ordinary gitlink HEAD tree=%q, want %q: %v", tree, wantTree, err)
	}
	if tree, err := normal.WorktreeDocsTree(context.Background()); err != nil || tree != wantTree {
		t.Fatalf("ordinary gitlink worktree tree=%q, want %q: %v", tree, wantTree, err)
	}
	sentinel := filepath.Join(t.TempDir(), "nested-filter")
	gitRun(t, nested, "config", "filter.probe.clean", "printf x >> "+shellQuote(sentinel)+"; cat")
	future := time.Now().Add(10 * time.Second)
	if err := os.Chtimes(filepath.Join(nested, "file"), future, future); err != nil {
		t.Fatal(err)
	}
	if _, err := newRepoHandle(t, dir).DocsClean(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(sentinel); err != nil {
		t.Fatalf("nested filter positive control: %v", err)
	}
	if _, err := inspectionRepo(t, dir).DocsClean(context.Background()); !errors.Is(err, gitx.ErrInspectionPolicyUnavailable) {
		t.Fatalf("nested status admitted: %v", err)
	}
	// The real index can omit a gitlink that is still present in the scratch seed.
	gitRun(t, dir, "rm", "--cached", "docs/nested-repo")
	if _, err := inspectionRepo(t, dir).WorktreeDocsTree(context.Background()); !errors.Is(err, gitx.ErrInspectionPolicyUnavailable) {
		t.Fatalf("nested scratch admitted: %v", err)
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatalf("nested filter executed: %v", err)
	}
}

func TestObjectReadersNeverLazilyFetchMissingObjects(t *testing.T) {
	dir, _ := newRepoWithDocs(t)
	blob := gitLine(t, dir, "rev-parse", "HEAD:docs/a.md")
	sentinel := filepath.Join(t.TempDir(), "transport")
	helper := filepath.Join(t.TempDir(), "transport")
	if err := os.WriteFile(helper, []byte("#!/bin/sh\nprintf x >> "+shellQuote(sentinel)+"\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "config", "remote.origin.url", "ext::"+helper)
	gitRun(t, dir, "config", "remote.origin.promisor", "true")
	gitRun(t, dir, "config", "protocol.ext.allow", "always")
	removeGitObject(t, dir, blob)
	if _, err := gitx.New(dir).Run(context.Background(), "cat-file", "blob", blob); err == nil {
		t.Fatal("missing blob unexpectedly readable")
	}
	if err := os.Remove(sentinel); err != nil {
		t.Fatalf("lazy fetch positive control: %v", err)
	}
	for _, reader := range []*appgit.Repo{newRepoHandle(t, dir), inspectionRepo(t, dir)} {
		if _, err := reader.HeadDocsTree(context.Background()); err == nil {
			t.Fatal("missing blob became a valid tree")
		}
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatalf("inspection invoked transport: %v", err)
	}
}

func TestScratchReadTreeFailureIsNotRetriedAndCleansUp(t *testing.T) {
	dir, _ := newRepoWithDocs(t)
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin, scratch := t.TempDir(), t.TempDir()
	log := filepath.Join(bin, "read-tree")
	script := "#!/bin/sh\nfor arg do\nif [ \"$arg\" = read-tree ]; then\nprintf '%s\\n' \"$*\" >> " + shellQuote(log) + "\nexit 73\nfi\ndone\nexec " + shellQuote(git) + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMPDIR", scratch)
	if _, err := inspectionRepo(t, dir).WorktreeDocsTree(context.Background()); err == nil {
		t.Fatal("read-tree failure became success")
	}
	data, err := os.ReadFile(log)
	if err != nil || strings.Count(string(data), "\n") != 1 || strings.Contains(string(data), "--empty") {
		t.Fatalf("unexpected seed retry: %q %v", data, err)
	}
	entries, err := os.ReadDir(scratch)
	if err != nil || len(entries) != 0 {
		t.Fatalf("scratch cleanup failed: %v %v", entries, err)
	}
	t.Setenv("PATH", t.TempDir())
	if _, err := newRepoHandle(t, dir).HeadCommit(context.Background()); err == nil {
		t.Fatal("Git launch failure became unborn")
	}
}

func TestReadersRejectUnavailableGitCapabilities(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	for _, control := range []string{"--no-lazy-fetch", "show-ref"} {
		t.Run(control, func(t *testing.T) {
			dir, _ := newRepoWithDocs(t)
			head := gitLine(t, dir, "rev-parse", "HEAD")
			// Keep the commit object, but force HEAD reads through the unborn probe.
			gitRun(t, dir, "update-ref", "-d", "HEAD")
			readers := []*appgit.Repo{newRepoHandle(t, dir), inspectionRepo(t, dir)}
			bin := t.TempDir()
			script := "#!/bin/sh\nfor arg do\nif [ \"$arg\" = " + shellQuote(control) + " ]; then\nexit 129\nfi\ndone\nexec " + shellQuote(git) + " \"$@\"\n"
			if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			for _, reader := range readers {
				reads := []func(context.Context) (string, error){reader.HeadCommit, reader.HeadDocsTree, reader.WorktreeDocsTree}
				if control == "--no-lazy-fetch" {
					reads = append(reads, func(ctx context.Context) (string, error) { return reader.DocsTreeOf(ctx, head) })
					if _, err := reader.IsAncestor(context.Background(), head, head); err == nil {
						t.Fatal("unsupported control became an ancestry answer")
					}
				}
				for _, read := range reads {
					_, err := read(context.Background())
					var exit *gitx.ExitError
					if !errors.As(err, &exit) || exit.Result.ExitCode != 129 {
						t.Fatalf("unsupported %s became a fact: %v", control, err)
					}
				}
			}
		})
	}
}
