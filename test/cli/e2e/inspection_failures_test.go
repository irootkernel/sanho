package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSyncInspectionWorkspaceRefusals(t *testing.T) {
	for _, state := range []string{"outside", "plain-repo", "subdirectory", "legacy", "invalid-config", "config-io", "worktree-list-failure", "damaged-git-entry", "unlisted-checkout", "linked-no-config", "linked-config-io"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			w := newWorld(t, nil)
			ws := w.newWorkspace("workspace-refusal")
			code := "not_in_workspace"
			switch state {
			case "unlisted-checkout":
				gitDir := ws.path(".git")
				ws.dir = filepath.Join(w.root, "unlisted")
				mkdirAll(t, ws.dir)
				writeFile(t, ws.path(".git"), "gitdir: "+gitDir+"\n")
			case "linked-no-config", "linked-config-io":
				main := ws
				linked := filepath.Join(w.root, "linked")
				main.git("worktree", "add", "-b", "inspection-refusal", linked, "HEAD")
				if state == "linked-config-io" {
					if err := os.Symlink(".sanho.json", main.path(".sanho.json")); err != nil {
						t.Fatal(err)
					}
					code = "internal"
				}
				ws.dir = linked
			case "outside":
				ws.dir = filepath.Join(w.root, "outside")
				mkdirAll(t, ws.dir)
			case "subdirectory":
				writeFile(t, ws.path(".sanho.json"), `{"schema_version":2}`)
				ws.dir = ws.path("subdirectory")
				mkdirAll(t, ws.dir)
			case "legacy":
				writeFile(t, ws.path(".sanho.json"), `{"socket_path":"/unused/sanhod.sock"}`)
				code = "v1_workspace"
			case "invalid-config":
				writeFile(t, ws.path(".sanho.json"), `{}`)
				code = "config_corrupt"
			case "config-io":
				mkdirAll(t, ws.path(".sanho.json"))
				code = "internal"
			case "worktree-list-failure":
				realGit, err := exec.LookPath("git")
				if err != nil {
					t.Fatal(err)
				}
				writeExecutable(t, filepath.Join(w.binDir, "git"), "#!/bin/sh\nfor arg do if [ \"$arg\" = worktree ]; then exit 73; fi; done\nexec "+inspectionShellQuote(realGit)+" \"$@\"\n")
				code = "internal"
			case "damaged-git-entry":
				ws.dir = filepath.Join(w.root, "damaged")
				mkdirAll(t, ws.dir)
				writeFile(t, ws.path(".git"), "gitdir: nonexistent\n")
				code = "internal"
			}
			before := inspectionProtectedState(t, w, "")
			requireInspectionError(t, ws.run("sync", "--inspect", "--json"), code, "")
			if !reflect.DeepEqual(before, inspectionProtectedState(t, w, "")) {
				t.Fatal("workspace refusal changed protected state")
			}
		})
	}
}

func requireInspectionError(t *testing.T, out result, code, reason string) {
	t.Helper()
	requireExit(t, "inspection error", out, 1)
	var document map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out.stdout), &document); err != nil || len(document) != 1 || document["error"] == nil {
		t.Fatalf("partial or missing error envelope: %v %s", err, out.stdout)
	}
	var body struct {
		Code, Message, Reason string
		Paths                 []string
		RecoveryID            *string `json:"recovery_id"`
	}
	if err := json.Unmarshal(document["error"], &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != code || body.Reason != reason || body.Message == "" || strings.TrimSpace(out.stderr) == "" {
		t.Fatalf("error=%s stderr=%s", out.stdout, out.stderr)
	}
	if code == "inspection_unavailable" && (body.Paths == nil || len(body.Paths) != 0 || body.RecoveryID != nil) {
		t.Fatalf("policy refusal invented recovery evidence: %s", out.stdout)
	}
}

func TestSyncInspectionReadFailuresNeverBecomeReady(t *testing.T) {
	for _, fault := range []string{"missing-head", "packed-missing-head", "missing-root-tree", "missing-docs-tree", "missing-blob", "dangling-detached", "invalid-symbolic", "corrupt-index", "note-io", "scan-limit"} {
		t.Run(fault, func(t *testing.T) {
			t.Parallel()
			w := newWorld(t, defaultCanonicalDocs())
			ws := conflictedSync(t, w)
			ws.commitDocs("docs: resolve", map[string]string{"api.md": "resolved\n"})
			removeObject := func(rev string) {
				oid := strings.TrimSpace(ws.git("rev-parse", rev).stdout)
				if err := os.Remove(ws.path(".git", "objects", oid[:2], oid[2:])); err != nil {
					t.Fatal(err)
				}
			}
			code := "internal"
			switch fault {
			case "missing-head", "packed-missing-head":
				if fault == "packed-missing-head" {
					ws.git("pack-refs", "--all")
				}
				removeObject("HEAD")
			case "missing-root-tree":
				removeObject("HEAD^{tree}")
			case "missing-docs-tree":
				removeObject("HEAD:docs")
			case "missing-blob":
				removeObject("HEAD:docs/api.md")
			case "dangling-detached":
				writeFile(t, ws.path(".git", "HEAD"), strings.Repeat("e", 40)+"\n")
			case "invalid-symbolic":
				writeFile(t, ws.path(".git", "HEAD"), "ref: refs/heads/bad..name\n")
			case "corrupt-index":
				writeFile(t, ws.path(".git", "index"), "broken index")
			case "note-io":
				removeFile(t, ws.path(".git", "sanho", "sync.json"))
				mkdirAll(t, ws.path(".git", "sanho", "sync.json"))
			case "scan-limit":
				writeFile(t, ws.docsPath("large.md"), strings.Repeat("a", (10<<20)+1))
				code = "too_large"
			}
			before := inspectionProtectedState(t, w, ws.path(".git", "objects"))
			requireInspectionError(t, ws.run("sync", "--inspect", "--json"), code, "")
			if !reflect.DeepEqual(before, inspectionProtectedState(t, w, ws.path(".git", "objects"))) {
				t.Fatal("failed inspection changed protected state")
			}
		})
	}
}

func TestSyncInspectionInvalidModesRunNoGit(t *testing.T) {
	t.Parallel()
	w := newWorld(t, nil)
	sentinel := filepath.Join(w.root, "git-was-run")
	writeExecutable(t, filepath.Join(w.binDir, "git"), "#!/bin/sh\nprintf invoked > "+inspectionShellQuote(sentinel)+"\nexit 71\n")
	for _, mode := range [][]string{{"--abort"}, {"--continue"}, {"--rebase-onto=HEAD"}, {"--rebase-onto="}} {
		args := append([]string{"sync", "--inspect", "--json"}, mode...)
		requireInspectionError(t, execute(t, w.root, w.env(), cliBinary, args...), "invalid_arguments", "")
	}
	if fileExists(t, sentinel) {
		t.Fatal("invalid mode combination reached Git")
	}
}

func inspectionShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
