package e2e

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestSyncInspectionCancellationPreservesProtectedState(t *testing.T) {
	t.Parallel()
	w := newWorld(t, defaultCanonicalDocs())
	ws := conflictedSync(t, w)
	ws.commitDocs("docs: resolve", map[string]string{"api.md": "resolved\n"})
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	control := t.TempDir()
	ready, gate := filepath.Join(control, "ready"), filepath.Join(control, "gate")
	if err := syscall.Mkfifo(gate, 0600); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(w.root, "cancelled-program-invoked")
	program := filepath.Join(w.binDir, "fsmonitor-probe")
	writeExecutable(t, program, "#!/bin/sh\nprintf invoked > "+inspectionShellQuote(sentinel)+"\n")
	ws.git("config", "core.fsmonitor", program)
	// Pause at the first config admission read, after real discovery and note reads.
	writeExecutable(t, filepath.Join(w.binDir, "git"), "#!/bin/sh\nfor arg do\n"+
		"if [ \"$arg\" = config ]; then : > "+inspectionShellQuote(ready)+
		"; read value < "+inspectionShellQuote(gate)+"; fi\ndone\nexec "+inspectionShellQuote(realGit)+" \"$@\"\n")
	before := inspectionProtectedState(t, w, ws.path(".git", "objects"))
	cmd := exec.Command(cliBinary, "sync", "--inspect", "--json")
	cmd.Dir, cmd.Env = ws.dir, ws.env()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	reaped := false
	t.Cleanup(func() {
		if !reaped {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			<-done
		}
	})
	deadline := time.After(10 * time.Second)
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for !fileExists(t, ready) {
		select {
		case err := <-done:
			reaped = true
			t.Fatalf("inspection exited before cancellation: %v %s %s", err, &stdout, &stderr)
		case <-deadline:
			t.Fatal("inspection did not reach cancellation gate")
		case <-tick.C:
		}
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		reaped = true
		if err == nil || stdout.Len() != 0 {
			t.Fatalf("cancelled inspection returned success evidence: %v %s", err, &stdout)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("cancelled inspection did not exit")
	}
	if fileExists(t, sentinel) || !reflect.DeepEqual(before, inspectionProtectedState(t, w, ws.path(".git", "objects"))) {
		t.Fatal("cancelled inspection changed protected state or executed a configured program")
	}
}

func TestSyncInspectionNeverStartsConfiguredFilters(t *testing.T) {
	for _, driver := range []string{"clean", "process"} {
		for _, state := range []string{"ready", "none", "corrupt", "markers"} {
			t.Run(driver+"/"+state, func(t *testing.T) {
				t.Parallel()
				w := newWorld(t, defaultCanonicalDocs())
				ws := conflictedSync(t, w)
				ws.commitDocs("docs: resolve", map[string]string{"api.md": "resolved\n"})
				sentinel := filepath.Join(w.root, "external-filter-invoked")
				program := filepath.Join(w.binDir, "filter-probe")
				body := "#!/bin/sh\nprintf invoked > " + inspectionShellQuote(sentinel) + "\n"
				if driver == "clean" {
					body += "cat\n"
				} else {
					body += "exit 79\n"
				}
				writeExecutable(t, program, body)
				writeFile(t, ws.path(".gitattributes"), "docs/*.md filter=inspection-probe\n")
				ws.git("config", "filter.inspection-probe."+driver, inspectionShellQuote(program))
				ws.git("config", "filter.inspection-probe.required", "true")
				// Force a conversion instead of letting the cached index hide it.
				stamp := time.Now().Add(2 * time.Hour)
				if err := os.Chtimes(ws.docsPath("api.md"), stamp, stamp); err != nil {
					t.Fatal(err)
				}
				control := ws.gitExit("add", "--", "docs/api.md")
				if !fileExists(t, sentinel) || (driver == "clean" && control.exitCode != 0) || (driver == "process" && control.exitCode == 0) {
					t.Fatalf("filter positive control did not execute as expected: %+v", control)
				}
				removeFile(t, sentinel)
				if err := os.Chtimes(ws.docsPath("api.md"), stamp.Add(time.Hour), stamp.Add(time.Hour)); err != nil {
					t.Fatal(err)
				}
				switch state {
				case "none":
					removeFile(t, ws.path(".git", "sanho", "sync.json"))
				case "corrupt":
					writeFile(t, ws.path(".git", "sanho", "sync.json"), "{")
				case "markers":
					writeFile(t, ws.docsPath("outside.md"), "<<<<<<< a\na\n=======\nb\n>>>>>>> b\n")
				}
				before := inspectionProtectedState(t, w, ws.path(".git", "objects"))
				if state == "ready" {
					out := ws.run("sync", "--inspect", "--json")
					requireInspectionError(t, out, "inspection_unavailable", "external_filter_configured")
					if strings.Contains(out.combined(), w.root) || strings.Contains(out.combined(), "filter-probe") {
						t.Fatal("availability output leaked configured program or path")
					}
				} else {
					report := inspectUnchanged(t, ws)
					want := map[string]string{"none": "no_sync", "corrupt": "sync_note_corrupt", "markers": "markers_remaining"}[state]
					if report.Continuation.Reason == nil || *report.Continuation.Reason != want {
						t.Fatalf("filter displaced an earlier diagnosis: %+v", report)
					}
				}
				if !reflect.DeepEqual(before, inspectionProtectedState(t, w, ws.path(".git", "objects"))) || fileExists(t, sentinel) {
					t.Fatal("inspection executed a filter or changed protected state")
				}
			})
		}
	}
}

func TestSyncInspectionControlsApplyBeforeDiscoveryAndNestedReads(t *testing.T) {
	t.Parallel()
	w := newWorld(t, defaultCanonicalDocs())
	ws := conflictedSync(t, w)
	ws.commitDocs("docs: resolve", map[string]string{"api.md": "resolved\n"})
	sentinel := filepath.Join(w.root, "configured-program-invoked")
	program := filepath.Join(w.binDir, "configured-probe")
	writeExecutable(t, program, "#!/bin/sh\nprintf invoked > "+inspectionShellQuote(sentinel)+"\nexit 0\n")
	ws.git("config", "core.fsmonitor", program)
	ws.git("config", "diff.external", program)
	ws.git("config", "diff.inspection.textconv", program)
	writeFile(t, ws.path(".gitattributes"), "docs/*.md diff=inspection\n")
	for _, hook := range []string{"post-index-change", "post-checkout", "reference-transaction"} {
		writeExecutable(t, ws.hookPath(hook), "#!/bin/sh\nexec "+inspectionShellQuote(program)+"\n")
	}
	// A real external diff confirms the sentinel can observe configured execution.
	ws.git("diff", "--ext-diff", "HEAD~1", "HEAD", "--", "docs")
	if !fileExists(t, sentinel) {
		t.Fatal("external-program positive control did not execute")
	}
	removeFile(t, sentinel)
	trace := filepath.Join(w.root, "trace-output")
	env := append(ws.env(), "GIT_TRACE="+trace, "GIT_TRACE2_EVENT="+trace, "GIT_TRACE2_PERF="+trace)
	before := inspectionProtectedState(t, w, ws.path(".git", "objects"))
	out := execute(t, ws.dir, env, cliBinary, "sync", "--inspect", "--json")
	requireExit(t, "controlled inspection", out, 0)
	if !strings.Contains(out.stdout, `"ready": true`) || fileExists(t, sentinel) || fileExists(t, trace) ||
		!reflect.DeepEqual(before, inspectionProtectedState(t, w, ws.path(".git", "objects"))) {
		t.Fatalf("configured effects or wrong verdict: %+v", out)
	}
}

func TestSyncInspectionCapabilityLaunchAndConfigFailures(t *testing.T) {
	for _, fault := range []string{"unsupported-policy", "launch", "config-read", "read-tree"} {
		t.Run(fault, func(t *testing.T) {
			t.Parallel()
			w := newWorld(t, defaultCanonicalDocs())
			ws := conflictedSync(t, w)
			ws.commitDocs("docs: resolve", map[string]string{"api.md": "resolved\n"})
			realGit, err := exec.LookPath("git")
			if err != nil {
				t.Fatal(err)
			}
			code, reason := "internal", ""
			script := "#!/bin/sh\n"
			switch fault {
			case "unsupported-policy":
				script += "exit 129\n"
				code, reason = "inspection_unavailable", "execution_policy_unavailable"
			case "launch":
				script = "#!/nonexistent/inspection-interpreter\n"
			case "config-read":
				script += "for arg do if [ \"$arg\" = config ]; then exit 73; fi; done\n"
			case "read-tree":
				script += "for arg do if [ \"$arg\" = read-tree ]; then exit 74; fi; done\n"
			}
			script += "exec " + inspectionShellQuote(realGit) + " \"$@\"\n"
			writeExecutable(t, filepath.Join(w.binDir, "git"), script)
			before := inspectionProtectedState(t, w, ws.path(".git", "objects"))
			requireInspectionError(t, ws.run("sync", "--inspect", "--json"), code, reason)
			if !reflect.DeepEqual(before, inspectionProtectedState(t, w, ws.path(".git", "objects"))) {
				t.Fatal("unavailable or failed read changed protected state")
			}
		})
	}
}
