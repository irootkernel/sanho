package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/irootkernel/sanho/internal/domain/provenance"
	"github.com/irootkernel/sanho/internal/infra/appgit"
	"github.com/irootkernel/sanho/internal/infra/gitx"
	"github.com/irootkernel/sanho/internal/infra/wsstate"
	"github.com/irootkernel/sanho/internal/usecase/docsync"
)

type changingInspectionReader struct {
	docsync.CompletionReader
	beforeHead func(int)
	calls      int
}

func (r *changingInspectionReader) HeadCommit(ctx context.Context) (string, error) {
	r.calls++
	r.beforeHead(r.calls)
	return r.CompletionReader.HeadCommit(ctx)
}

func TestInspectionInvalidatesChangedObservation(t *testing.T) {
	for _, change := range []string{"head", "same-note-replacement", "note-content", "note-removal", "note-creation", "corrupt-replacement", "intermediate-head"} {
		t.Run(change, func(t *testing.T) {
			t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
			t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
			dir, ctx := t.TempDir(), context.Background()
			run := gitx.New(dir)
			git := func(args ...string) string {
				t.Helper()
				r, err := run.Run(ctx, args...)
				if err != nil {
					t.Fatal(err)
				}
				return strings.TrimSpace(string(r.Stdout))
			}
			git("init", "-b", "main")
			git("config", "user.name", "Inspection Test")
			git("config", "user.email", "inspection@example.test")
			git("-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "initial")
			head := git("rev-parse", "HEAD")
			emptyTree := git("rev-parse", "HEAD^{tree}")
			gitDir := filepath.Join(dir, ".git")
			note := wsstate.SyncNote{Target: provenance.Base{Commit: head, Tree: emptyTree}, MergedTree: emptyTree}
			save := func() {
				t.Helper()
				if err := wsstate.SaveSyncNote(gitDir, note); err != nil {
					t.Fatal(err)
				}
			}
			if change != "note-creation" {
				save()
			}
			if change == "corrupt-replacement" {
				if err := os.WriteFile(filepath.Join(gitDir, wsstate.SyncNoteRelPath), []byte("{"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			repo, err := appgit.NewInspection(ctx, dir, "docs")
			if err != nil {
				t.Fatal(err)
			}
			reader := &changingInspectionReader{CompletionReader: repo, beforeHead: func(call int) {
				if call == 2 {
					switch change {
					case "head", "intermediate-head":
						git("-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "concurrent")
					case "same-note-replacement", "note-creation", "corrupt-replacement":
						save()
					case "note-content":
						if err := os.WriteFile(filepath.Join(gitDir, wsstate.SyncNoteRelPath), []byte("{}"), 0600); err != nil {
							t.Fatal(err)
						}
					case "note-removal":
						if err := wsstate.ClearSyncNote(gitDir); err != nil {
							t.Fatal(err)
						}
					}
				}
				if change == "intermediate-head" && call == 3 {
					git("update-ref", "refs/heads/main", head)
				}
			}}
			got, err := inspectSync(ctx, reader, gitDir)
			if err != nil {
				t.Fatal(err)
			}
			if got.State != "changed" || got.Head != nil || got.Note != nil || got.Comparison != nil || got.Continuation.Ready ||
				got.Continuation.Reason == nil || *got.Continuation.Reason != "observation_changed" || got.Continuation.RecoveryID != nil ||
				got.Continuation.Paths == nil || len(got.Continuation.Paths) != 0 || len(got.Checks) != 6 {
				t.Fatalf("changed report=%+v", got)
			}
			for _, c := range got.Checks {
				if c.State != docsync.CheckNotEvaluated || c.Reason != nil || c.Paths == nil || len(c.Paths) != 0 {
					t.Fatalf("stale check=%+v", c)
				}
			}
			var human bytes.Buffer
			renderSyncInspection(&human, got)
			for _, evidence := range []string{"Sync state: changed", "HEAD: null", "Sync note: null", "Comparison: null", "Local completion checks ready: false", `Reason: "observation_changed"`, "Recovery ID: null"} {
				if !strings.Contains(human.String(), evidence) {
					t.Fatalf("changed report lacks %q: %s", evidence, &human)
				}
			}
		})
	}
}

func TestInspectionLegacyNullsAndQuotedEvidence(t *testing.T) {
	head := strings.Repeat("a", 40)
	path := "docs/a, \"한글\"\n\t.md"
	a := docsync.CompletionAssessment{
		Note:   &docsync.SyncNote{Target: provenance.Base{Commit: head}, PrevBase: provenance.Base{Commit: head}, Conflicts: []string{path}},
		Checks: []docsync.CompletionCheck{{Name: "sync_note", State: docsync.CheckPassed}, {Name: "entry_history", State: docsync.CheckNotApplicable}},
	}
	got := buildInspectionJSON(head, a)
	data, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if got.Note.Target.Tree != nil || got.Note.PreviousBase.Tree != nil || got.Note.EntryHead != nil || got.Note.EntryDocsTree != nil || got.Note.MergedTree != nil ||
		got.Checks[1].Reason == nil || *got.Checks[1].Reason != "entry_history_unrecorded" {
		t.Fatalf("legacy fields=%s", data)
	}
	var roundTrip inspectionJSON
	if err := json.Unmarshal(data, &roundTrip); err != nil || roundTrip.Note.Conflicts[0] != path {
		t.Fatalf("path round trip: %v %s", err, data)
	}
	var text bytes.Buffer
	renderSyncInspection(&text, got)
	if strings.Contains(text.String(), path) || !strings.Contains(text.String(), `\n\t.md"`) || !strings.Contains(text.String(), "entry_history: not_applicable") {
		t.Fatalf("unquoted or missing human evidence: %s", text.String())
	}
}
