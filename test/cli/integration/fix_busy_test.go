package integration

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// TestCLIFixRetriesOnDocsRepoBusy ensures fix surfaces docs_repo_busy after retries.
func TestCLIFixRetriesOnDocsRepoBusy(t *testing.T) {
	cliBinary := getCliBinary(t)

	// Fake daemon that returns docs_repo_busy twice, then success
	var pushCount int32
	daemon := newUnixTestDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/docs/head"):
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(map[string]string{"head": "abcd"}); err != nil {
				t.Errorf("encode response: %v", err)
			}
		case strings.HasPrefix(r.URL.Path, "/docs/push"):
			count := atomic.AddInt32(&pushCount, 1)
			w.Header().Set("Content-Type", "application/json")
			if count <= 2 {
				w.WriteHeader(http.StatusConflict)
				if err := json.NewEncoder(w).Encode(map[string]string{"error": "docs_repo_busy"}); err != nil {
					t.Errorf("encode response: %v", err)
				}
				return
			}
			if err := json.NewEncoder(w).Encode(map[string]any{
				"ok":            true,
				"status":        "updated",
				"new_docs_hash": "efgh",
			}); err != nil {
				t.Errorf("encode response: %v", err)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer daemon.Close()

	// Prepare workspace with pending fix and config
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".sanho.json"), []byte(`{
  "socket_path": "`+daemon.SocketPath+`",
  "project": "p1",
  "workspace_id": "ws1",
  "actor_email": "fix-busy@example.com",
  "docs_dir": "docs"
}`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".sanho_docs_hash"), []byte("abcd"), 0644); err != nil {
		t.Fatalf("write hash: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "docs"), 0755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docs", "index.md"), []byte("# busy\n"), 0644); err != nil {
		t.Fatalf("write docs: %v", err)
	}
	// pending fix required for fix
	if err := os.WriteFile(filepath.Join(dir, ".sanho_pending_fix"), []byte(`{"base_hash":"abcd","remote_hash":"abcd","created_at":"2025-01-01T00:00:00Z"}`), 0644); err != nil {
		t.Fatalf("write pending fix: %v", err)
	}

	cmd := exec.Command(cliBinary, "fix")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("fix should eventually succeed after retries; got error: %v\nOutput:\n%s", err, string(out))
	}

	if pushCount < 3 {
		t.Fatalf("expected retries on docs_repo_busy, pushCount=%d", pushCount)
	}

	hashBytes, err := os.ReadFile(filepath.Join(dir, ".sanho_docs_hash"))
	if err != nil {
		t.Fatalf("read hash: %v", err)
	}
	if strings.TrimSpace(string(hashBytes)) != "efgh" {
		t.Fatalf("docs hash not updated after successful push; got %s", strings.TrimSpace(string(hashBytes)))
	}
}
