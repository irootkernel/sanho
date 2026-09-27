package e2e

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

type inspectionReport struct {
	State string  `json:"state"`
	Head  *string `json:"head"`
	Note  *struct {
		PreviousBase *struct {
			Commit *string `json:"commit"`
			Tree   *string `json:"tree"`
		} `json:"previous_base"`
		Target struct {
			Commit *string `json:"commit"`
			Tree   *string `json:"tree"`
		} `json:"target"`
		EntryHead  *string  `json:"entry_head"`
		EntryTree  *string  `json:"entry_docs_tree"`
		MergedTree *string  `json:"merged_tree"`
		Conflicts  []string `json:"conflicts"`
	} `json:"note"`
	Checks []struct {
		Name, State string
		Reason      *string
		Paths       []string
	} `json:"checks"`
	Comparison *struct {
		ExpectedTree      string   `json:"expected_tree"`
		ActualTree        string   `json:"actual_tree"`
		AllowedChanges    []string `json:"allowed_changes"`
		UnexpectedChanges []string `json:"unexpected_changes"`
	} `json:"comparison"`
	Continuation struct {
		Ready      bool
		Reason     *string
		Paths      []string
		RecoveryID *string `json:"recovery_id"`
	} `json:"continuation"`
}

// Unlike snapshotTree, inspection protects the real index, refs, logs and all
// operation metadata. Only unreferenced application objects may be added.
func inspectionProtectedState(t *testing.T, w *world, objectDir string) map[string]treeEntry {
	t.Helper()
	entries := map[string]treeEntry{}
	err := filepath.WalkDir(w.root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path == objectDir {
				return filepath.SkipDir
			}
			return nil
		}
		value, err := describePath(path, entry)
		if err == nil {
			entries[path] = value
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func inspectUnchanged(t *testing.T, ws *workspace) inspectionReport {
	t.Helper()
	return inspectUnchangedWithObjects(t, ws, ws.path(".git", "objects"))
}

func inspectUnchangedWithObjects(t *testing.T, ws *workspace, objectDir string) inspectionReport {
	t.Helper()
	before := inspectionProtectedState(t, ws.w, objectDir)
	out := ws.run("sync", "--inspect", "--json")
	requireExit(t, "inspection", out, 0)
	if out.stderr != "" {
		t.Fatalf("diagnosis wrote stderr: %q", out.stderr)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out.stdout), &fields); err != nil || len(fields) != 6 {
		t.Fatalf("inspection envelope: %v %s", err, out.stdout)
	}
	var report inspectionReport
	if err := json.Unmarshal([]byte(out.stdout), &report); err != nil {
		t.Fatal(err)
	}
	names := []string{"sync_note", "markers", "docs_clean", "entry_history", "merge_tree", "non_conflict_preservation"}
	if len(report.Checks) != len(names) || report.Continuation.Paths == nil {
		t.Fatalf("missing checks/paths: %s", out.stdout)
	}
	for i, c := range report.Checks {
		if c.Name != names[i] || c.Paths == nil {
			t.Fatalf("check order/paths: %s", out.stdout)
		}
	}
	human := ws.run("sync", "--inspect")
	requireExit(t, "human inspection", human, 0)
	requireContains(t, "human state", human.stdout, "Sync state: "+report.State)
	if report.Continuation.Reason != nil {
		requireContains(t, "human reason", human.stdout, *report.Continuation.Reason)
	}
	if report.Continuation.RecoveryID != nil {
		requireContains(t, "human recovery", human.stdout, "Recovery ID: "+strconv.Quote(*report.Continuation.RecoveryID))
	}
	if !reflect.DeepEqual(before, inspectionProtectedState(t, ws.w, objectDir)) {
		t.Fatal("inspection changed protected files, Git state, clone, or registry")
	}
	return report
}

func TestSyncInspectionLinkedWorktreePrivateNote(t *testing.T) {
	t.Parallel()
	w := newWorld(t, defaultCanonicalDocs())
	main := conflictedSync(t, w)
	linked := &workspace{w: w, name: "linked", dir: filepath.Join(w.root, "linked \n")}
	main.git("worktree", "add", "-b", "inspection-linked", linked.dir, "HEAD")
	if inspectUnchangedWithObjects(t, linked, main.path(".git", "objects")).State != "none" {
		t.Fatal("linked inspection borrowed the main worktree note")
	}
	gitDir := strings.TrimSuffix(linked.git("rev-parse", "--absolute-git-dir").stdout, "\n")
	writeFile(t, filepath.Join(gitDir, "sanho", "sync.json"), "{")
	if inspectUnchangedWithObjects(t, linked, main.path(".git", "objects")).State != "corrupt" {
		t.Fatal("linked private corrupt note was not diagnosed")
	}
	if inspectUnchanged(t, main).State != "active" {
		t.Fatal("linked inspection changed the main sync")
	}
}

func TestSyncInspectionUnbornAndDetachedHead(t *testing.T) {
	t.Parallel()
	w := newWorld(t, defaultCanonicalDocs())
	ws := w.setup("detached")
	ws.git("checkout", "--detach", "HEAD")
	if inspectUnchanged(t, ws).Head == nil {
		t.Fatal("valid detached HEAD lost its identity")
	}
	unborn := &workspace{w: w, name: "unborn", dir: filepath.Join(w.root, "unborn")}
	mkdirAll(t, unborn.dir)
	w.git(unborn.dir, "init", "-b", "main")
	writeFile(t, unborn.path(".sanho.json"), readFile(t, ws.path(".sanho.json")))
	report := inspectUnchanged(t, unborn)
	if report.Head != nil || report.State != "none" {
		t.Fatalf("positive unborn diagnosis=%+v", report)
	}
}

func editInspectionNote(t *testing.T, ws *workspace, edit func(map[string]json.RawMessage)) {
	t.Helper()
	path := ws.path(".git", "sanho", "sync.json")
	var note map[string]json.RawMessage
	if err := json.Unmarshal([]byte(readFile(t, path)), &note); err != nil {
		t.Fatal(err)
	}
	edit(note)
	data, err := json.Marshal(note)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, string(data))
}

func TestSyncInspectionDiagnosesLocalCompletion(t *testing.T) {
	for _, reason := range []string{"no_sync", "markers_remaining", "resolution_uncommitted", "sync_note_corrupt", "invalid_sync_target", "missing_merge_tree", "foreign_entry_history", "non_conflict_paths_changed", "ready"} {
		t.Run(reason, func(t *testing.T) {
			t.Parallel()
			w := newWorld(t, defaultCanonicalDocs())
			var ws *workspace
			switch reason {
			case "no_sync":
				ws = w.setup("no-note")
			case "foreign_entry_history":
				ws = reachSyncContinueForeignHistory(t, w).ws
			case "non_conflict_paths_changed":
				ws = reachSyncContinueUnverified(t, w).ws
			default:
				ws = conflictedSync(t, w)
			}
			switch reason {
			case "resolution_uncommitted":
				ws.writeDocs(map[string]string{"api.md": "resolved\n"})
			case "sync_note_corrupt":
				writeFile(t, ws.path(".git", "sanho", "sync.json"), "{")
			case "invalid_sync_target":
				editInspectionNote(t, ws, func(n map[string]json.RawMessage) { n["target"] = json.RawMessage(`{}`) })
			case "missing_merge_tree", "ready":
				ws.commitDocs("docs: resolve", map[string]string{"api.md": "resolved\n"})
				if reason == "missing_merge_tree" {
					editInspectionNote(t, ws, func(n map[string]json.RawMessage) {
						delete(n, "merged_tree")
						delete(n, "entry_head")
						delete(n, "entry_docs_tree")
					})
				}
			}
			report := inspectUnchanged(t, ws)
			if report.Head == nil || *report.Head != strings.TrimSpace(ws.git("rev-parse", "HEAD").stdout) {
				t.Fatal("inspection lost full HEAD identity")
			}
			if reason == "ready" {
				var recorded map[string]any
				if err := json.Unmarshal([]byte(readFile(t, ws.path(".git", "sanho", "sync.json"))), &recorded); err != nil {
					t.Fatal(err)
				}
				noteJSON, err := json.Marshal(report.Note)
				if err != nil {
					t.Fatal(err)
				}
				var reported map[string]any
				if err := json.Unmarshal(noteJSON, &reported); err != nil {
					t.Fatal(err)
				}
				for key, diskKey := range map[string]string{"previous_base": "prev_base", "target": "target", "entry_head": "entry_head", "entry_docs_tree": "entry_docs_tree", "merged_tree": "merged_tree", "conflicts": "conflicts"} {
					if reported[key] == nil || !reflect.DeepEqual(reported[key], recorded[diskKey]) {
						t.Fatalf("note field %s=%v, recorded=%v", key, reported[key], recorded[diskKey])
					}
				}
				if !report.Continuation.Ready || report.Continuation.Reason != nil || report.Continuation.RecoveryID != nil ||
					report.Comparison == nil || !reflect.DeepEqual(report.Comparison.AllowedChanges, []string{"docs/api.md"}) ||
					report.Comparison.UnexpectedChanges == nil || len(report.Comparison.UnexpectedChanges) != 0 {
					t.Fatalf("ready report=%+v", report)
				}
				for _, c := range report.Checks {
					if c.State != "passed" || c.Reason != nil {
						t.Fatalf("ready check=%+v", c)
					}
				}
				return
			}
			if report.Continuation.Ready || report.Continuation.Reason == nil || *report.Continuation.Reason != reason {
				t.Fatalf("wrong first blocker: %+v", report)
			}
			if reason != "no_sync" {
				wantRecovery := map[string]string{
					"markers_remaining": "sync_finish_resolution", "resolution_uncommitted": "sync_finish_resolution",
					"sync_note_corrupt": "sync_review_corrupt_note", "invalid_sync_target": "sync_review_corrupt_note",
					"missing_merge_tree": "sync_review_unverified_resolution", "non_conflict_paths_changed": "sync_review_unverified_resolution",
					"foreign_entry_history": "sync_return_to_entry_history",
				}[reason]
				if report.Continuation.RecoveryID == nil || *report.Continuation.RecoveryID != wantRecovery {
					t.Fatalf("recovery=%v, want %q", report.Continuation.RecoveryID, wantRecovery)
				}
			}
			state := "active"
			switch reason {
			case "no_sync":
				state = "none"
				if report.Continuation.RecoveryID != nil {
					t.Fatal("absent sync gained a recovery")
				}
			case "sync_note_corrupt", "invalid_sync_target":
				state = "corrupt"
			}
			if report.State != state || (state != "active" && report.Note != nil) {
				t.Fatalf("note state=%+v", report)
			}
			blocked := false
			for _, c := range report.Checks {
				if blocked && c.State != "not_evaluated" {
					t.Fatalf("check after blocker was evaluated: %+v", c)
				}
				if c.State == "blocked" {
					blocked = true
					if c.Reason == nil || *c.Reason != reason || !reflect.DeepEqual(c.Paths, report.Continuation.Paths) {
						t.Fatal("check and continuation evidence disagree")
					}
				}
			}
			if !blocked {
				t.Fatal("missing blocking check")
			}
			if reason == "missing_merge_tree" && (report.Checks[3].State != "not_applicable" || report.Checks[3].Reason == nil ||
				*report.Checks[3].Reason != "entry_history_unrecorded" || report.Note.MergedTree != nil || report.Note.EntryHead != nil) {
				t.Fatal("legacy absence was treated as proof or corruption")
			}
			if reason == "markers_remaining" && !reflect.DeepEqual(report.Continuation.Paths, []string{"docs/api.md"}) {
				t.Fatal("marker path lost")
			}
			if reason == "non_conflict_paths_changed" && (report.Comparison == nil ||
				!reflect.DeepEqual(report.Comparison.UnexpectedChanges, []string{"docs/guide.md"}) ||
				!reflect.DeepEqual(report.Continuation.Paths, []string{"docs/guide.md"})) {
				t.Fatal("unexpected path partition lost")
			}
			if reason != "non_conflict_paths_changed" && report.Comparison != nil {
				t.Fatal("comparison was invented before its check")
			}
		})
	}
}

func TestSyncInspectionWithoutCloneOrReachableRemote(t *testing.T) {
	t.Parallel()
	w := newWorld(t, defaultCanonicalDocs())
	ws := conflictedSync(t, w)
	ws.commitDocs("docs: resolve", map[string]string{"api.md": "resolved\n"})
	if err := os.RemoveAll(ws.cloneDir()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(w.origin, w.origin+".offline"); err != nil {
		t.Fatal(err)
	}
	if !inspectUnchanged(t, ws).Continuation.Ready {
		t.Fatal("local inspection depended on the unavailable canonical")
	}
	if fileExists(t, ws.cloneDir()) {
		t.Fatal("inspection repaired the clone")
	}
}

func TestSyncInspectionWholeDocsPathsAndFilterPrecedence(t *testing.T) {
	t.Parallel()
	w := newWorld(t, defaultCanonicalDocs())
	ws := conflictedSync(t, w)
	ws.writeDocs(map[string]string{"api.md": "resolved\n", "a, \"한글\"\n.md": "<<<<<<< ours\na\n=======\nb\n>>>>>>> theirs\n"})
	ws.git("config", "filter.unused.clean", "exit 79")
	report := inspectUnchanged(t, ws)
	want := []string{"docs/a, \"한글\"\n.md"}
	if !reflect.DeepEqual(report.Continuation.Paths, want) || *report.Continuation.Reason != "markers_remaining" {
		t.Fatalf("whole-worktree marker evidence=%+v", report)
	}
	human := ws.sanho("sync", "--inspect").stdout
	if strings.Contains(human, want[0]) || !strings.Contains(human, `\n.md"`) {
		t.Fatalf("human path was not quoted: %q", human)
	}
}
