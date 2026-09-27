package e2e

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

// The caller has proved inspection preserved this fixture before completion.
func requireInspectionContinueParity(t *testing.T, ws *workspace, report inspectionReport) {
	t.Helper()
	notePath := ws.path(".git", "sanho", "sync.json")
	noteBefore, err := os.ReadFile(notePath)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	baseBefore := readFile(t, ws.basePath())
	head := strings.TrimSpace(ws.git("rev-parse", "HEAD").stdout)
	out := ws.run("sync", "--continue", "--json")
	if report.Continuation.Ready {
		requireExit(t, "ready Continue", out, 0)
		var completed struct {
			Status, Commit string
			Base           struct{ Commit, Tree string }
			Conflicts      []string
			MergeDrift     int `json:"merge_drift"`
		}
		if err := json.Unmarshal([]byte(out.stdout), &completed); err != nil {
			t.Fatal(err)
		}
		if completed.Status != "completed" || completed.Commit != "" || completed.Conflicts == nil || len(completed.Conflicts) != 0 ||
			completed.MergeDrift != len(report.Comparison.AllowedChanges) || completed.Base.Commit != *report.Note.Target.Commit ||
			completed.Base.Tree != *report.Note.Target.Tree || recordedBase(t, ws) != completed.Base.Commit || syncNoteExists(t, ws) {
			t.Fatalf("inspection and completion disagree: report=%+v completed=%s", report, out.stdout)
		}
	} else {
		requireExit(t, "blocked Continue", out, 1)
		var envelope struct {
			Error struct {
				Code       string
				Reason     *string
				Paths      []string
				RecoveryID *string `json:"recovery_id"`
			}
		}
		if err := json.Unmarshal([]byte(out.stdout), &envelope); err != nil {
			t.Fatal(err)
		}
		code := "sync_in_progress"
		switch *report.Continuation.Reason {
		case "markers_remaining":
			code = "markers_present"
		case "resolution_uncommitted":
			code = "docs_dirty"
		}
		body := envelope.Error
		if body.Code != code || !reflect.DeepEqual(body.Reason, report.Continuation.Reason) ||
			!reflect.DeepEqual(body.Paths, report.Continuation.Paths) || !reflect.DeepEqual(body.RecoveryID, report.Continuation.RecoveryID) || out.stderr == "" {
			t.Fatalf("inspection and refusal disagree: report=%+v error=%s stderr=%s", report, out.stdout, out.stderr)
		}
		noteAfter, err := os.ReadFile(notePath)
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(noteBefore, noteAfter) || baseBefore != readFile(t, ws.basePath()) {
			t.Fatal("refused completion changed its note or base")
		}
	}
	requireEqual(t, "Continue HEAD", strings.TrimSpace(ws.git("rev-parse", "HEAD").stdout), head)
}

func TestSyncInspectionLocalAndLegacyParity(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "take-ours", true: "legacy-entry"}[legacy], func(t *testing.T) {
			t.Parallel()
			w := newWorld(t, defaultCanonicalDocs())
			ws := conflictedSync(t, w)
			head := ws.git("rev-parse", "HEAD").stdout
			ws.git("restore", "--source=HEAD", "--staged", "--worktree", "--", "docs")
			if legacy {
				editInspectionNote(t, ws, func(n map[string]json.RawMessage) {
					delete(n, "entry_head")
					delete(n, "entry_docs_tree")
				})
			}
			writeFile(t, ws.path("code.txt"), "staged code\n")
			ws.git("add", "--", "code.txt")
			writeFile(t, ws.path("README.md"), "unstaged code\n")
			staged := ws.git("diff", "--cached", "--binary").stdout
			report := inspectUnchanged(t, ws)
			if !report.Continuation.Ready || (legacy && (report.Checks[3].State != "not_applicable" || *report.Checks[3].Reason != "entry_history_unrecorded")) {
				t.Fatalf("local/legacy resolution not accepted: %+v", report)
			}
			requireInspectionContinueParity(t, ws, report)
			requireEqual(t, "no new resolution commit", ws.git("rev-parse", "HEAD").stdout, head)
			requireEqual(t, "staged code preserved", ws.git("diff", "--cached", "--binary").stdout, staged)
			requireEqual(t, "unstaged code preserved", readFile(t, ws.path("README.md")), "unstaged code\n")
		})
	}
}

func TestSyncInspectionDirtyDocsParity(t *testing.T) {
	for _, state := range []string{"staged", "unstaged", "untracked"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			w := newWorld(t, defaultCanonicalDocs())
			ws := conflictedSync(t, w)
			ws.commitDocs("docs: resolve", map[string]string{"api.md": "resolved\n"})
			path := "api.md"
			if state == "untracked" {
				path = "new.md"
			}
			writeFile(t, ws.docsPath(path), "later edit\n")
			if state == "staged" {
				ws.git("add", "--", "docs/"+path)
			}
			report := inspectUnchanged(t, ws)
			if report.Continuation.Reason == nil || *report.Continuation.Reason != "resolution_uncommitted" {
				t.Fatalf("dirty report=%+v", report)
			}
			requireInspectionContinueParity(t, ws, report)
		})
	}
}

func TestSyncInspectionReadyDoesNotAuthorizeLaterState(t *testing.T) {
	for _, changed := range []string{"docs", "head", "note"} {
		t.Run(changed, func(t *testing.T) {
			t.Parallel()
			w := newWorld(t, defaultCanonicalDocs())
			ws := conflictedSync(t, w)
			ws.commitDocs("docs: resolve", map[string]string{"api.md": "resolved\n"})
			if !inspectUnchanged(t, ws).Continuation.Ready {
				t.Fatal("fixture was not ready")
			}
			want := "markers_remaining"
			switch changed {
			case "docs":
				writeFile(t, ws.docsPath("api.md"), "<<<<<<< ours\na\n=======\nb\n>>>>>>> theirs\n")
			case "head":
				foreign := strings.TrimSpace(ws.git("commit-tree", "HEAD^{tree}", "-m", "unrelated history").stdout)
				ws.git("update-ref", "refs/heads/main", foreign)
				want = "foreign_entry_history"
			case "note":
				editInspectionNote(t, ws, func(n map[string]json.RawMessage) { n["target"] = json.RawMessage(`{}`) })
				want = "invalid_sync_target"
			}
			report := inspectUnchanged(t, ws)
			if report.Continuation.Reason == nil || *report.Continuation.Reason != want {
				t.Fatalf("changed state=%+v", report)
			}
			requireInspectionContinueParity(t, ws, report)
		})
	}
}

func TestSyncInspectionGitContentParity(t *testing.T) {
	t.Parallel()
	w := newWorld(t, defaultCanonicalDocs())
	ws := w.setup("git-content")
	ws.commitDocs("docs: seed removable paths", map[string]string{"old.md": "rename me\n", "deleted.md": "delete me\n"})
	writeFile(t, ws.path(".gitattributes"), "docs/eol.txt text eol=crlf\ndocs/ident.txt ident\ndocs/encoded.txt working-tree-encoding=UTF-16LE\n")
	ws.writeDocs(map[string]string{
		"api.md": "line one\nMINE\n", "binary.dat": "a\x00b\n", "executable": "executable\n",
		"ignored.md": "tracked before ignored\n", "eol.txt": "one\r\ntwo\r\n", "ident.txt": "$Id$\n", "encoded.txt": "a\x00\n\x00",
	})
	if err := os.Chmod(ws.docsPath("executable"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("api.md", ws.docsPath("link")); err != nil {
		t.Fatal(err)
	}
	ws.git("mv", "--", "docs/old.md", "docs/renamed.md")
	ws.git("rm", "--", "docs/deleted.md")
	ws.git("add", "-A")
	ws.git("commit", "-m", "docs: local Git content")
	writeFile(t, ws.path(".gitignore"), readFile(t, ws.path(".gitignore"))+"\ndocs/ignored.md\n")
	ws.git("add", ".gitignore")
	ws.git("commit", "-m", "chore: ignore tracked document")
	w.advanceCanonical(map[string]string{"api.md": "line one\nTHEIRS\n"}, "canonical: conflict")
	requireContains(t, "content sync", ws.sanho("sync").combined(), "have conflicts")
	ws.commitDocs("docs: resolve", map[string]string{"api.md": "resolved\n"})
	// These bytes differ from the blobs but represent the same Git content.
	writeFile(t, ws.docsPath("eol.txt"), "one\r\ntwo\r\n")
	writeFile(t, ws.docsPath("ident.txt"), "$Id: 0123456789012345678901234567890123456789 $\n")
	writeFile(t, ws.docsPath("encoded.txt"), "a\x00\n\x00")
	requireEqual(t, "normalized EOL blob", ws.git("show", "HEAD:docs/eol.txt").stdout, "one\ntwo\n")
	requireEqual(t, "normalized encoding blob", ws.git("show", "HEAD:docs/encoded.txt").stdout, "a\n")
	report := inspectUnchanged(t, ws)
	if !report.Continuation.Ready || !reflect.DeepEqual(report.Comparison.AllowedChanges, []string{"docs/api.md"}) {
		t.Fatalf("normalized content not accepted: %+v\nstatus=%s\ndiff=%s", report, ws.git("status", "--porcelain", "--", "docs").stdout, ws.git("diff", "--", "docs").stdout)
	}
	requireEqual(t, "normalized worktree tree", report.Comparison.ActualTree, strings.TrimSpace(ws.git("rev-parse", "HEAD:docs").stdout))
	requireContains(t, "executable tree mode", ws.git("ls-tree", "HEAD", "--", "docs/executable").stdout, "100755")
	requireContains(t, "symlink tree mode", ws.git("ls-tree", "HEAD", "--", "docs/link").stdout, "120000")
	if fileExists(t, ws.docsPath("old.md")) || fileExists(t, ws.docsPath("deleted.md")) {
		t.Fatal("rename/deletion fixture lost")
	}
	before := ws.git("diff", "--binary", "HEAD", "--", "docs").stdout
	requireInspectionContinueParity(t, ws, report)
	requireEqual(t, "completion content preservation", ws.git("diff", "--binary", "HEAD", "--", "docs").stdout, before)
}

func TestSyncInspectionCustomLiteralPathsParity(t *testing.T) {
	t.Parallel()
	name := "[literal]*, 한글\n.md"
	docsDir := "manuals, [draft]\n한글"
	w := newWorld(t, map[string]string{name: "base\n"})
	ws := w.newWorkspace("custom-literal").initAndAdopt("--docs-dir", docsDir)
	path := docsDir + "/" + name
	writeFile(t, ws.path(docsDir, name), "ours\n")
	writeFile(t, ws.path("manuals, d\n한글", "literal, 한글\n.md"), "outside docs\n")
	ws.git("add", "-A")
	ws.git("commit", "-m", "docs: custom path edit")
	w.advanceCanonical(map[string]string{name: "theirs\n"}, "canonical: custom path conflict")
	requireContains(t, "custom sync", ws.sanho("sync").combined(), "have conflicts")
	report := inspectUnchanged(t, ws)
	if !reflect.DeepEqual(report.Continuation.Paths, []string{path}) {
		t.Fatalf("literal marker paths=%+v", report)
	}
	requireInspectionContinueParity(t, ws, report)
	writeFile(t, ws.path(docsDir, name), "resolved\n")
	ws.git("--literal-pathspecs", "add", "--", path)
	ws.git("commit", "-m", "docs: custom path resolution")
	report = inspectUnchanged(t, ws)
	if !report.Continuation.Ready || !reflect.DeepEqual(report.Comparison.AllowedChanges, []string{path}) {
		t.Fatalf("literal drift=%+v", report)
	}
	requireInspectionContinueParity(t, ws, report)
	requireEqual(t, "outside docs preserved", readFile(t, ws.path("manuals, d\n한글", "literal, 한글\n.md")), "outside docs\n")
}
