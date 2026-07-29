package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"os/exec"
	"sync/atomic"
	"testing"

	"github.com/irootkernel/sanho/internal/domain/client"
)

func TestWorkspaceReportPersistsFailureAndRetryClearsIt(t *testing.T) {
	repo := t.TempDir()
	if out, err := exec.Command("git", "-C", repo, "init", "--initial-branch=main").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}

	var fail atomic.Bool
	fail.Store(true)
	var calls atomic.Int32
	server := newUnixTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPut || r.URL.Path != "/workspaces/workspace-1/docs-hash" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["docs_hash"] != "docs-2" || body["actor_email"] != "actor@example.com" {
			t.Fatalf("body=%v", body)
		}
		if fail.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "unavailable"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
	defer server.Close()

	config := &client.WorkspaceConfig{
		SocketPath:  server.URL,
		WorkspaceID: "workspace-1",
		ActorEmail:  "actor@example.com",
	}
	err := reportWorkspaceDocsHash(context.Background(), repo, config, "docs-2")
	if err == nil {
		t.Fatal("report succeeded while server was failing")
	}
	pending, pendingErr := hasPendingWorkspaceReport(context.Background(), repo)
	if pendingErr != nil || !pending {
		t.Fatalf("pending=%v error=%v", pending, pendingErr)
	}

	fail.Store(false)
	if err := retryPendingWorkspaceReport(context.Background(), repo, config); err != nil {
		t.Fatalf("retry pending report: %v", err)
	}
	pending, pendingErr = hasPendingWorkspaceReport(context.Background(), repo)
	if pendingErr != nil || pending {
		t.Fatalf("pending after retry=%v error=%v", pending, pendingErr)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls=%d want 2", calls.Load())
	}
}
