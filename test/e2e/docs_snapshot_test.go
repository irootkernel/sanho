package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestE2E_DocsSnapshot(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	baseURL := requireServer(t, ctx)
	client := &http.Client{Timeout: 10 * time.Second}

	originPath, initialHead := createOriginRepo(t, map[string]string{
		"README.md":      "# Root\n",
		"docs/guide.md":  "Hello, snapshot\n",
		"docs/deep/a.md": "Nested\n",
	})

	projectName := uniqueName("snapshot-project")
	repoID := uniqueName("snapshot-repo")

	addProject(t, client, baseURL, projectName, repoID, originPath)
	t.Cleanup(func() {
		req, _ := http.NewRequest(http.MethodDelete, baseURL+"/projects/"+projectName+"?force=true", nil)
		if resp, err := client.Do(req); err == nil {
			resp.Body.Close()
		}
	})

	head := getHead(t, client, baseURL, projectName)
	if head != initialHead {
		t.Fatalf("head mismatch: got %s want %s", head, initialHead)
	}

	commit, files := getSnapshot(t, client, baseURL, projectName, "")
	if commit != initialHead {
		t.Fatalf("snapshot commit mismatch: got %s want %s", commit, initialHead)
	}
	if files["README.md"] != "# Root\n" {
		t.Fatalf("snapshot README mismatch: %q", files["README.md"])
	}
	if files["docs/guide.md"] != "Hello, snapshot\n" {
		t.Fatalf("snapshot guide mismatch: %q", files["docs/guide.md"])
	}
	if files["docs/deep/a.md"] != "Nested\n" {
		t.Fatalf("snapshot nested mismatch: %q", files["docs/deep/a.md"])
	}

	commit2, files2 := getSnapshot(t, client, baseURL, projectName, initialHead)
	if commit2 != initialHead {
		t.Fatalf("snapshot by commit mismatch: got %s want %s", commit2, initialHead)
	}
	if len(files) != len(files2) {
		t.Fatalf("snapshot file count mismatch: %d vs %d", len(files), len(files2))
	}

	// Unknown commit should return unknown_docs_commit
	unknownCommit := strings.Repeat("a", 40)
	resp, err := client.Get(fmt.Sprintf("%s/docs/snapshot?project=%s&commit=%s", baseURL, projectName, unknownCommit))
	if err != nil {
		t.Fatalf("snapshot unknown commit request failed: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("snapshot unknown commit status = %d", resp.StatusCode)
	}
	var errResp map[string]string
	json.NewDecoder(resp.Body).Decode(&errResp)
	resp.Body.Close()
	if errResp["error"] != "unknown_docs_commit" {
		t.Fatalf("snapshot unknown commit error = %s", errResp["error"])
	}
}
