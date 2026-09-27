package e2e

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSyncAndPullAdmissionDiagnostics(t *testing.T) {
	t.Parallel()
	w := newWorld(t, defaultCanonicalDocs())
	ws := conflictedSync(t, w)
	for _, phase := range []struct{ reason, recovery string }{
		{"active_sync", "sync_inspect_active"},
		{"sync_note_corrupt", "sync_review_corrupt_note"},
	} {
		if phase.reason == "sync_note_corrupt" {
			writeFile(t, ws.path(".git", "sanho", "sync.json"), "{ invalid JSON\n")
		}
		for _, command := range []string{"sync", "pull"} {
			out := ws.run(command, "--json")
			requireExit(t, command+" "+phase.reason, out, 1)
			var envelope struct {
				Error struct {
					Code, Reason string
					Paths        []string
					RecoveryID   string `json:"recovery_id"`
				}
			}
			if err := json.Unmarshal([]byte(out.stdout), &envelope); err != nil {
				t.Fatal(err)
			}
			body := envelope.Error
			if body.Code != "sync_in_progress" || body.Reason != phase.reason || body.RecoveryID != phase.recovery || body.Paths == nil || len(body.Paths) != 0 {
				t.Fatalf("%s diagnosis=%s", command, out.stdout)
			}
			if !strings.Contains(out.stderr, "sanho sync --abort") {
				t.Fatalf("missing stderr guidance: %q", out.stderr)
			}
		}
	}
}

func TestContinueWithoutSyncHasNullRecovery(t *testing.T) {
	t.Parallel()
	w := newWorld(t, defaultCanonicalDocs())
	ws := w.setup("no-active-sync")
	out := ws.run("sync", "--continue", "--json")
	requireExit(t, "continue without a note", out, 1)
	var envelope struct{ Error map[string]json.RawMessage }
	if err := json.Unmarshal([]byte(out.stdout), &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Error) != 5 || string(envelope.Error["reason"]) != `"no_sync"` || string(envelope.Error["paths"]) != "[]" || string(envelope.Error["recovery_id"]) != "null" {
		t.Fatalf("no-sync diagnosis=%s", out.stdout)
	}
	if !strings.Contains(out.stderr, "no sync is in progress") {
		t.Fatalf("stderr=%q", out.stderr)
	}
}

func TestContinueDiagnosticsForDirtyInvalidAndLegacyNotes(t *testing.T) {
	for _, tc := range []struct{ reason, code, recovery string }{
		{"resolution_uncommitted", "docs_dirty", "sync_finish_resolution"},
		{"invalid_sync_target", "sync_in_progress", "sync_review_corrupt_note"},
		{"missing_merge_tree", "sync_in_progress", "sync_review_unverified_resolution"},
	} {
		t.Run(tc.reason, func(t *testing.T) {
			t.Parallel()
			w := newWorld(t, defaultCanonicalDocs())
			ws := conflictedSync(t, w)
			notePath := ws.path(".git", "sanho", "sync.json")
			switch tc.reason {
			case "resolution_uncommitted":
				ws.writeDocs(map[string]string{"api.md": "line one\nRESOLVED\n"})
			case "invalid_sync_target", "missing_merge_tree":
				if tc.reason == "missing_merge_tree" {
					ws.commitDocs("docs: resolve", map[string]string{"api.md": "line one\nRESOLVED\n"})
				}
				var note map[string]json.RawMessage
				if err := json.Unmarshal([]byte(readFile(t, notePath)), &note); err != nil {
					t.Fatal(err)
				}
				if tc.reason == "invalid_sync_target" {
					note["target"] = json.RawMessage(`{}`)
				} else {
					delete(note, "merged_tree")
				}
				data, err := json.Marshal(note)
				if err != nil {
					t.Fatal(err)
				}
				writeFile(t, notePath, string(data))
			}
			noteBefore, baseBefore := readFile(t, notePath), readFile(t, ws.basePath())
			out := ws.run("sync", "--continue", "--json")
			requireExit(t, tc.reason, out, 1)
			var envelope struct{ Error map[string]json.RawMessage }
			if err := json.Unmarshal([]byte(out.stdout), &envelope); err != nil {
				t.Fatal(err)
			}
			body := envelope.Error
			if len(body) != 5 || string(body["code"]) != `"`+tc.code+`"` ||
				string(body["reason"]) != `"`+tc.reason+`"` ||
				string(body["recovery_id"]) != `"`+tc.recovery+`"` || string(body["paths"]) != "[]" {
				t.Fatalf("diagnosis=%s", out.stdout)
			}
			if strings.TrimSpace(out.stderr) == "" {
				t.Fatal("missing human refusal guidance")
			}
			requireEqual(t, "refused note", readFile(t, notePath), noteBefore)
			requireEqual(t, "refused base", readFile(t, ws.basePath()), baseBefore)
		})
	}
}
