package docsync_test

import (
	"context"
	"testing"

	"github.com/irootkernel/sanho/internal/usecase/docsync"
)

// A canonical repository nothing has ever published into is the ordinary
// starting state of a new project (sanho-v0.2.md §5.3 bootstrap), not a
// failure. Consuming and reconciling have the same correct answer there:
// there is nothing upstream, so do nothing and say so. The first `git
// push` creates canonical's root commit; only then is there anything to
// sync with.

func TestSyncAgainstEmptyCanonicalIsANoOp(t *testing.T) {
	f := newEmptyCanonicalFlow(t, map[string]string{"a.md": "alpha\n"})

	docsBefore := f.docsSnapshot(t)
	statusBefore := f.status(t)
	headBefore := f.head(t)

	result := f.sync(t, docsync.Options{})

	if result.Status != docsync.StatusUpToDate {
		t.Fatalf("Status = %v, want StatusUpToDate", result.Status)
	}
	if result.CommitOID != "" {
		t.Fatalf("CommitOID = %q, want no commit", result.CommitOID)
	}
	// There is no canonical commit to name, so the result names none.
	if !result.NewBase.IsZero() {
		t.Fatalf("NewBase = %+v, want zero", result.NewBase)
	}

	requireDocs(t, f, docsBefore)
	if status := f.status(t); status != statusBefore {
		t.Fatalf("status = %q, want %q", status, statusBefore)
	}
	if head := f.head(t); head != headBefore {
		t.Fatal("sync against an empty canonical committed")
	}
	if f.hasBase(t) {
		t.Fatal("sync against an empty canonical recorded a base")
	}
	if _, ok := f.note(t); ok {
		t.Fatal("sync against an empty canonical wrote a sync note")
	}
}

func TestPullAgainstEmptyCanonicalIsANoOp(t *testing.T) {
	f := newEmptyCanonicalFlow(t, map[string]string{"a.md": "alpha\n"})

	docsBefore := f.docsSnapshot(t)
	headBefore := f.head(t)

	result := f.pull(t, false)

	if result.Status != docsync.StatusUpToDate {
		t.Fatalf("Status = %v, want StatusUpToDate", result.Status)
	}
	requireDocs(t, f, docsBefore)
	if head := f.head(t); head != headBefore {
		t.Fatal("pull against an empty canonical committed")
	}
	if f.hasBase(t) {
		t.Fatal("pull against an empty canonical recorded a base")
	}
}

// A --rebase-onto target is a different matter: naming a commit in a
// repository that has none is a mistake, so it is reported rather than
// silently treated as "nothing to do".
func TestSyncRebaseOntoAgainstEmptyCanonicalIsRefused(t *testing.T) {
	f := newEmptyCanonicalFlow(t, map[string]string{"a.md": "alpha\n"})

	_, err := f.use.Run(context.Background(), docsync.Options{
		RebaseOnto: "0123456789abcdef0123456789abcdef01234567",
	})
	if err == nil {
		t.Fatal("sync --rebase-onto against an empty canonical succeeded, want a refusal")
	}
	if f.hasBase(t) {
		t.Fatal("a refused sync recorded a base")
	}
}
