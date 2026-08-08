package docsync_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/irootkernel/sanho/internal/domain/provenance"
	"github.com/irootkernel/sanho/internal/usecase/docsync"
)

// TestPullFastForwards is the consume-only fast path: the worktree docs
// are canonical content unchanged, so they are simply replaced and the
// base moves. Nothing is committed unless asked.
func TestPullFastForwards(t *testing.T) {
	f := newFlow(t,
		map[string]string{"a.md": "alpha\n"},
		map[string]string{"a.md": "alpha\n"},
	)
	f.adoptCanonicalHeadAsBase(t)
	f.upstream(t, map[string]string{"a.md": "alpha upstream\n", "b.md": "beta\n"})
	target, targetTree := f.canonicalHead(t)
	before := f.head(t)

	result := f.pull(t, false)

	if result.Status != docsync.StatusSynced {
		t.Fatalf("status = %v, want synced", result.Status)
	}
	requireDocs(t, f, map[string]string{"a.md": "alpha upstream\n", "b.md": "beta\n"})
	if want := (provenance.Base{Commit: target, Tree: targetTree}); f.base(t) != want {
		t.Fatalf("base = %+v, want %+v", f.base(t), want)
	}
	if head := f.head(t); head != before {
		t.Fatalf("a plain pull committed (%s -> %s)", before, head)
	}
	if result.CommitOID != "" {
		t.Fatalf("CommitOID = %s, want none", result.CommitOID)
	}

	// The update is staged, so the user can commit it whenever they like.
	status := f.status(t)
	if !strings.Contains(status, "M  "+docsDir+"/a.md") || !strings.Contains(status, "A  "+docsDir+"/b.md") {
		t.Fatalf("status = %q, want the pulled docs staged", status)
	}
}

// TestPullWithCommit records the same update as a sync-style commit.
func TestPullWithCommit(t *testing.T) {
	f := newFlow(t,
		map[string]string{"a.md": "alpha\n"},
		map[string]string{"a.md": "alpha\n"},
	)
	f.adoptCanonicalHeadAsBase(t)
	f.upstream(t, map[string]string{"a.md": "alpha upstream\n"})
	target, _ := f.canonicalHead(t)

	writeFile(t, f.appDir, "src/app.go", "package main // work in progress\n")
	gitRun(t, f.appDir, "add", "--", "src/app.go")
	before := f.head(t)

	result := f.pull(t, true)

	if n := f.commitsSince(t, before); n != 1 {
		t.Fatalf("pull --commit created %d commits, want exactly 1", n)
	}
	head := f.head(t)
	if result.CommitOID != head {
		t.Fatalf("CommitOID = %s, want HEAD %s", result.CommitOID, head)
	}
	if subject := gitLine(t, f.appDir, "log", "-1", "--format=%s", head); subject != "[SANHO] Sync docs to "+target[:12] {
		t.Fatalf("subject = %q, want the sync convention", subject)
	}
	for _, path := range f.changedPaths(t, head) {
		if !strings.HasPrefix(path, docsDir+"/") {
			t.Fatalf("the pull commit touched %s", path)
		}
	}
	if status := f.status(t); status != "M  src/app.go" {
		t.Fatalf("status = %q, want the user's staged work alone", status)
	}
}

// TestPullUpToDate: base is canonical head and the docs match it, so
// pull reports and does nothing.
func TestPullUpToDate(t *testing.T) {
	files := map[string]string{"a.md": "alpha\n"}
	f := newFlow(t, files, files)
	base := f.adoptCanonicalHeadAsBase(t)
	before := f.head(t)

	result := f.pull(t, true)

	if result.Status != docsync.StatusUpToDate {
		t.Fatalf("status = %v, want up to date", result.Status)
	}
	if f.base(t) != base {
		t.Fatalf("base = %+v, want it left at %+v", f.base(t), base)
	}
	if f.head(t) != before {
		t.Fatal("an up-to-date pull committed")
	}
	if status := f.status(t); status != "" {
		t.Fatalf("an up-to-date pull dirtied the workspace:\n%s", status)
	}
}

// TestPullRefuses covers the states where pull is the wrong verb. Each
// message has to send the user to `sanho sync`, which succeeds in every
// one of them (guidance closure, D3).
func TestPullRefuses(t *testing.T) {
	tests := []struct {
		name    string
		arrange func(t *testing.T, f *flow)
		want    error
	}{
		{
			name: "uncommitted local docs edits",
			arrange: func(t *testing.T, f *flow) {
				f.adoptCanonicalHeadAsBase(t)
				f.upstream(t, map[string]string{"a.md": "alpha upstream\n"})
				writeFile(t, f.appDir, docsDir+"/a.md", "alpha local\n")
			},
			want: docsync.ErrPullNeedsSync,
		},
		{
			name: "committed local docs edits",
			arrange: func(t *testing.T, f *flow) {
				f.adoptCanonicalHeadAsBase(t)
				f.upstream(t, map[string]string{"a.md": "alpha upstream\n"})
				f.writeDocs(t, map[string]string{"a.md": "alpha local\n"})
				f.commitAll(t, "docs: local edit")
			},
			want: docsync.ErrPullNeedsSync,
		},
		{
			name: "no recorded base",
			arrange: func(t *testing.T, f *flow) {
				f.upstream(t, map[string]string{"a.md": "alpha upstream\n"})
			},
			want: docsync.ErrPullNeedsSync,
		},
		{
			name: "a conflicted sync is in progress",
			arrange: func(t *testing.T, f *flow) {
				f.adoptCanonicalHeadAsBase(t)
				f.upstream(t, map[string]string{"a.md": hunkFile("A-upstream", "B", "C")})
				f.writeDocs(t, map[string]string{"a.md": hunkFile("A-local", "B", "C")})
				f.commitAll(t, "docs: local edit")
				if result := f.sync(t, docsync.Options{}); result.Status != docsync.StatusConflicts {
					t.Fatalf("fixture is wrong: sync status = %v", result.Status)
				}
			},
			want: docsync.ErrSyncInProgress,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newFlow(t,
				map[string]string{"a.md": hunkFile("A", "B", "C")},
				map[string]string{"a.md": hunkFile("A", "B", "C")},
			)
			test.arrange(t, f)

			docsBefore := f.docsSnapshot(t)
			statusBefore := f.status(t)
			headBefore := f.head(t)

			_, err := f.use.Pull(context.Background(), false)
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
			// The sentinel states the fact and nothing more: the advice
			// that names 'sanho sync' is the CLI catalog's, where the
			// closure suite can prove it runnable (F-H6).
			if strings.Contains(err.Error(), "sanho ") {
				t.Errorf("message = %q, want a command-free sentinel", err)
			}

			requireDocs(t, f, docsBefore)
			if status := f.status(t); status != statusBefore {
				t.Fatalf("status = %q, want %q", status, statusBefore)
			}
			if head := f.head(t); head != headBefore {
				t.Fatal("a refused pull committed")
			}
		})
	}
}
