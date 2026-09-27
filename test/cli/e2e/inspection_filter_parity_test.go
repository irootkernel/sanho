package e2e

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSyncInspectionFilterNormalizationPreservesContinue(t *testing.T) {
	for _, driver := range []string{"clean", "process"} {
		t.Run(driver, func(t *testing.T) {
			t.Parallel()
			w := newWorld(t, defaultCanonicalDocs())
			ws := conflictedSync(t, w)
			var note struct {
				Target struct {
					Commit string `json:"commit"`
					Tree   string `json:"tree"`
				} `json:"target"`
			}
			if err := json.Unmarshal([]byte(readFile(t, ws.path(".git", "sanho", "sync.json"))), &note); err != nil {
				t.Fatal(err)
			}
			sentinel := filepath.Join(w.root, "normalizing-filter-invoked")
			command := "printf invoked >> " + inspectionShellQuote(sentinel) + "; sed 's/WORKTREE/CANONICAL/g'"
			if driver == "process" {
				command = "SANHO_INSPECTION_PARITY_FILTER_SENTINEL=" + inspectionShellQuote(sentinel) + " " +
					inspectionShellQuote(os.Args[0]) + " -test.run=^TestSyncInspectionNormalizationProcessHelper$"
			}
			ws.git("config", "filter.normalizing."+driver, command)
			ws.git("config", "filter.normalizing.required", "true")
			writeFile(t, ws.path(".gitattributes"), "docs/api.md filter=normalizing\n")
			ws.writeDocs(map[string]string{"api.md": "line one\nWORKTREE\n"})
			ws.git("add", "--", ".gitattributes", "docs/api.md")
			if !fileExists(t, sentinel) {
				t.Fatal("git add did not execute the configured filter")
			}
			requireEqual(t, "normalized index blob", ws.git("show", ":docs/api.md").stdout, "line one\nCANONICAL\n")
			ws.git("commit", "-m", "docs: resolve through a normalizing filter")
			head := strings.TrimSpace(ws.git("rev-parse", "HEAD").stdout)
			requireEqual(t, "raw resolved worktree", ws.readDocs("api.md"), "line one\nWORKTREE\n")
			requireEqual(t, "normalized committed blob", ws.git("show", "HEAD:docs/api.md").stdout, "line one\nCANONICAL\n")

			forceConversion := func(offset time.Duration) {
				t.Helper()
				stamp := time.Now().Add(offset)
				if err := os.Chtimes(ws.docsPath("api.md"), stamp, stamp); err != nil {
					t.Fatal(err)
				}
			}
			removeFile(t, sentinel)
			forceConversion(time.Hour)
			requireEqual(t, "filter-normalized docs status", ws.git("status", "--porcelain", "--", "docs").stdout, "")
			if !fileExists(t, sentinel) {
				t.Fatal("git status did not execute the configured filter")
			}
			removeFile(t, sentinel)
			forceConversion(2 * time.Hour)
			before := inspectionProtectedState(t, w, ws.path(".git", "objects"))
			requireInspectionError(t, ws.run("sync", "--inspect", "--json"), "inspection_unavailable", "external_filter_configured")
			if fileExists(t, sentinel) || !reflect.DeepEqual(before, inspectionProtectedState(t, w, ws.path(".git", "objects"))) {
				t.Fatal("inspection executed a filter or changed protected state")
			}

			var completed struct {
				Status string `json:"status"`
				Base   struct {
					Commit string `json:"commit"`
					Tree   string `json:"tree"`
				} `json:"base"`
				Commit     string   `json:"commit"`
				Conflicts  []string `json:"conflicts"`
				MergeDrift int      `json:"merge_drift"`
			}
			out := ws.run("sync", "--continue", "--json")
			requireExit(t, "filter-normalized Continue", out, 0)
			if err := json.Unmarshal([]byte(out.stdout), &completed); err != nil {
				t.Fatal(err)
			}
			if completed.Status != "completed" || completed.Base != note.Target || completed.MergeDrift != 1 ||
				completed.Commit != "" || len(completed.Conflicts) != 0 {
				t.Fatalf("wrong normalized completion: %+v", completed)
			}
			if !fileExists(t, sentinel) || syncNoteExists(t, ws) {
				t.Fatal("Continue did not execute the filter and clear the completed note")
			}
			requireEqual(t, "adopted target base", recordedBase(t, ws), note.Target.Commit)
			requireEqual(t, "completion HEAD", strings.TrimSpace(ws.git("rev-parse", "HEAD").stdout), head)
			requireEqual(t, "completion raw worktree", ws.readDocs("api.md"), "line one\nWORKTREE\n")
		})
	}
}

// Git drives this real long-running filter through its packet protocol. Exit
// directly at EOF so the test runner cannot append PASS to the protocol stream.
func TestSyncInspectionNormalizationProcessHelper(t *testing.T) {
	sentinel := os.Getenv("SANHO_INSPECTION_PARITY_FILTER_SENTINEL")
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
			writePacket(bytes.ReplaceAll(data, []byte("WORKTREE"), []byte("CANONICAL")))
		}
		flush()
		flush()
	}
}
