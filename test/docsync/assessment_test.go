package docsync_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/irootkernel/sanho/internal/infra/appgit"
	"github.com/irootkernel/sanho/internal/usecase/docsync"
)

func TestAssessmentWholeDocsMarkersPrecedeFilterAdmission(t *testing.T) {
	f := newFlow(t, map[string]string{"a.md": "local\n"}, map[string]string{"a.md": "local\n"})
	base := f.adoptCanonicalHeadAsBase(t)
	tree, err := f.repo.HeadDocsTree(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	note := docsync.SyncNote{Target: base, PrevBase: base, EntryHead: f.head(t), EntryDocsTree: tree, MergedTree: tree, Conflicts: []string{"docs/a.md"}}
	if err := f.use.State.SaveSyncNote(note); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.appDir, ".gitignore"), []byte("docs/ignored.md\n"), 0644); err != nil {
		t.Fatal(err)
	}
	f.writeDocs(t, map[string]string{
		"untracked, [x]*.md": "<<<<<<< local\nleft\n=======\nright\n>>>>>>> upstream\n",
		"ignored.md":         "<<<<<<< local\nleft\n=======\nright\n>>>>>>> upstream\n",
	})
	sentinel := filepath.Join(t.TempDir(), "filter")
	gitRun(t, f.appDir, "config", "filter.probe.clean", "touch "+sentinel)
	reader, err := appgit.NewInspection(context.Background(), f.appDir, docsDir)
	if err != nil {
		t.Fatal(err)
	}
	assessment, err := docsync.AssessCompletion(context.Background(), reader, f.use.State)
	if err != nil || assessment.Blocker == nil || !errors.Is(assessment.Blocker, docsync.ErrMarkersRemain) {
		t.Fatalf("assessment=%+v error=%v", assessment, err)
	}
	if want := []string{"docs/ignored.md", "docs/untracked, [x]*.md"}; !reflect.DeepEqual(assessment.Blocker.Paths, want) {
		t.Fatalf("marker paths=%q, want %q", assessment.Blocker.Paths, want)
	}
	if assessment.Checks[2].State != docsync.CheckNotEvaluated {
		t.Fatal("assessment reached status after marker blocker")
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatalf("inspection executed a filter: %v", err)
	}
	loaded, exists, err := f.use.State.LoadSyncNote()
	if err != nil || !exists || !reflect.DeepEqual(loaded, note) || f.base(t) != base {
		t.Fatalf("assessment changed persisted state: %+v %v %v", loaded, exists, err)
	}
}
