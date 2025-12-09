package e2e

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// createOriginRepo initializes a bare repo and seeds an initial commit in a cloned working tree.
func createOriginRepo(t *testing.T, files map[string]string) (originPath string, head string) {
	t.Helper()
	tmp := t.TempDir()
	originPath = filepath.Join(tmp, fmt.Sprintf("origin-%d", time.Now().UnixNano()))
	mustMkDir(t, originPath)
	runCmd(t, "", "git", "init", "--bare", originPath)

	localPath := filepath.Join(tmp, "local")
	runCmd(t, "", "git", "clone", originPath, localPath)
	runCmd(t, localPath, "git", "config", "user.email", "e2e@example.com")
	runCmd(t, localPath, "git", "config", "user.name", "E2E User")

	for path, content := range files {
		full := filepath.Join(localPath, path)
		mustMkDirAll(t, filepath.Dir(full))
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			t.Fatalf("write file %s: %v", full, err)
		}
	}
	runCmd(t, localPath, "git", "add", ".")
	runCmd(t, localPath, "git", "commit", "-m", "initial")
	runCmd(t, localPath, "git", "push", "origin", "HEAD")
	head = strings.TrimSpace(string(runCmd(t, localPath, "git", "rev-parse", "HEAD")))
	return originPath, head
}

func mustMkDir(t *testing.T, path string) {
	t.Helper()
	if err := os.Mkdir(path, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func mustMkDirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatalf("mkdir -p %s: %v", path, err)
	}
}

func runCmd(t *testing.T, dir string, name string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command %s %v failed: %v\noutput:\n%s", name, args, err, string(out))
	}
	return out
}

// registerProjectViaCLI registers a project using the CLI and returns repoID.
func registerProjectViaCLI(t *testing.T, cli, serverURL, project, repoURL, cwd string) string {
	t.Helper()
	cmd := exec.Command(cli, "project", "add",
		"--server-url", serverURL,
		"--project", project,
		"--docs-repo-url", repoURL,
	)
	cmd.Dir = cwd
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("project add failed: %v\nOutput:\n%s", err, string(out))
	}
	// repo id is the basename of repoURL
	return filepath.Base(strings.TrimSuffix(repoURL, ".git"))
}

// registerWorkspaceViaCLI registers a workspace using the CLI and returns workspaceID & current head.
func registerWorkspaceViaCLI(t *testing.T, cli, serverURL, project, cwd string) (workspaceID string, currentHead string) {
	t.Helper()
	cmd := exec.Command(cli, "workspace", "register",
		"--server-url", serverURL,
		"--project", project,
		"--yes",
		cwd,
	)
	cmd.Dir = cwd
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("workspace register failed: %v\nOutput:\n%s", err, string(out))
	}
	lines := strings.Split(string(out), "\n")
	for _, ln := range lines {
		if strings.Contains(ln, "workspace_id") {
			parts := strings.Fields(ln)
			if len(parts) > 0 {
				workspaceID = parts[len(parts)-1]
			}
		}
		if strings.Contains(ln, "current_docs_head") {
			parts := strings.Fields(ln)
			if len(parts) > 0 {
				currentHead = parts[len(parts)-1]
			}
		}
	}
	if workspaceID == "" {
		t.Fatalf("workspace_id not found in output:\n%s", string(out))
	}
	return workspaceID, currentHead
}

func deleteProjectViaCLI(t *testing.T, cli, serverURL, project string, force bool) {
	t.Helper()
	args := []string{"project", "delete", "--server-url", serverURL, "--project", project, "--yes"}
	if force {
		args = append(args, "--force")
	}
	cmd := exec.Command(cli, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("project delete failed: %v\nOutput:\n%s", err, string(out))
	}
}

func deleteWorkspaceViaCLI(t *testing.T, cli, serverURL, workspaceID string) {
	t.Helper()
	cmd := exec.Command(cli, "workspace", "unregister",
		"--server-url", serverURL,
		"--workspace-id", workspaceID,
		"--yes",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("workspace unregister failed: %v\nOutput:\n%s", err, string(out))
	}
}

// fetchHeadViaHTTP gets /docs/head for validation.
func fetchHeadViaHTTP(t *testing.T, serverURL, project string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	reqURL := fmt.Sprintf("%s/docs/head?project=%s", strings.TrimRight(serverURL, "/"), url.QueryEscape(project))
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get head failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get head status = %d", resp.StatusCode)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode head resp: %v", err)
	}
	return body["head"]
}

// fetchStateViaHTTP retrieves /state for validation.
func fetchStateViaHTTP(t *testing.T, serverURL string) map[string]interface{} {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(serverURL, "/")+"/state", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get state failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("state status = %d", resp.StatusCode)
	}
	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode state resp: %v", err)
	}
	return body
}

// writeConfig writes .kkachi.json with provided values.
func writeConfig(t *testing.T, dir, serverURL, project, workspaceID, actorEmail string) {
	t.Helper()
	cfg := map[string]any{
		"server_url":   serverURL,
		"project":      project,
		"workspace_id": workspaceID,
		"actor_email":  actorEmail,
		"docs_dir":     "docs",
	}
	data, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, ".kkachi.json"), data, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

// writeDocsHash writes .kkachi_docs_hash.
func writeDocsHash(t *testing.T, dir, hash string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, ".kkachi_docs_hash"), []byte(hash), 0644); err != nil {
		t.Fatalf("write docs hash: %v", err)
	}
}

// writePendingFix writes pending fix file.
func writePendingFix(t *testing.T, dir, baseHash, remoteHash string) {
	t.Helper()
	state := map[string]string{
		"base_hash":   baseHash,
		"remote_hash": remoteHash,
		"created_at":  time.Now().UTC().Format(time.RFC3339),
	}
	data, _ := json.MarshalIndent(state, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, ".kkachi_pending_fix"), data, 0644); err != nil {
		t.Fatalf("write pending fix: %v", err)
	}
}

func setGitUser(t *testing.T, dir, email string) {
	t.Helper()
	runCmd(t, dir, "git", "config", "user.email", email)
	runCmd(t, dir, "git", "config", "user.name", "CLI E2E User")
}

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	runCmd(t, dir, "git", "init")
}

// createSnapshotTar builds a tar.gz from file map.
func createSnapshotTar(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	for name, content := range files {
		hdr := &tar.Header{
			Name: name,
			Mode: 0644,
			Size: int64(len(content)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("write header %s: %v", name, err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("write content %s: %v", name, err)
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return buf.Bytes()
}

// pushDocsViaHTTP pushes a snapshot to the server and returns new docs hash.
func pushDocsViaHTTP(t *testing.T, serverURL, workspaceID, baseHash string, files map[string]string, actor string) string {
	t.Helper()
	snapshot := createSnapshotTar(t, files)
	reqBody, _ := json.Marshal(map[string]string{
		"workspace_id":   workspaceID,
		"base_docs_hash": baseHash,
		"docs_snapshot":  base64.StdEncoding.EncodeToString(snapshot),
		"actor_email":    actor,
	})
	resp, err := http.Post(strings.TrimRight(serverURL, "/")+"/docs/push", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("push docs failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("push docs status=%d body=%s", resp.StatusCode, string(body))
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode push resp: %v", err)
	}
	if nh, ok := body["new_docs_hash"].(string); ok && nh != "" {
		return nh
	}
	if ch, ok := body["current_docs_hash"].(string); ok && ch != "" {
		return ch
	}
	t.Fatalf("push response missing docs hash: %+v", body)
	return ""
}
