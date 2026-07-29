package e2e_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/irootkernel/sanho/internal/interface/http/dto"
	testutil "github.com/irootkernel/sanho/test/util"
)

const testDaemonBaseURL = "http://sanho"

// configuredSocketPath returns an explicitly configured E2E daemon. With no
// override, tests must launch an isolated daemon instead of reusing a
// developer's daemon and persistent state.
func configuredSocketPath() (string, bool) {
	if path := strings.TrimSpace(os.Getenv("SANHO_E2E_SOCKET")); path != "" {
		return path, true
	}
	return "", false
}

// requireDaemon uses an explicit daemon override when present. Otherwise it
// launches a daemon with a fresh runtime home and Unix socket.
func requireDaemon(t *testing.T, ctx context.Context) (string, *http.Client, string) {
	t.Helper()

	socketPath, configured := configuredSocketPath()
	if configured {
		if !filepath.IsAbs(socketPath) {
			t.Fatalf("SANHO_E2E_SOCKET must be absolute: %q", socketPath)
		}
		if err := pingHealth(socketPath, 2*time.Second); err != nil {
			t.Fatalf("sanhod not reachable at %s: %v", socketPath, err)
		}
		return testDaemonBaseURL, unixHTTPClient(socketPath, 10*time.Second), socketPath
	}

	testDir, err := os.MkdirTemp("/tmp", "sanho-daemon-e2e-")
	if err != nil {
		t.Fatalf("create E2E runtime directory: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(testDir) })
	runtimeHome := filepath.Join(testDir, "home")
	socketPath = filepath.Join(testDir, "sanhod.sock")
	daemonBinary := strings.TrimSpace(os.Getenv("SANHO_DAEMON_BINARY"))
	if daemonBinary == "" {
		daemonBinary = filepath.Join(testDir, "sanhod")
		build := exec.Command("go", "build", "-o", daemonBinary, "./cmd/sanhod")
		build.Dir = repoRoot(t)
		if output, buildErr := build.CombinedOutput(); buildErr != nil {
			t.Fatalf("build sanhod for E2E: %v\noutput:\n%s", buildErr, output)
		}
	}

	daemonCtx, cancel := context.WithCancel(context.Background())
	var logs bytes.Buffer
	cmd := exec.CommandContext(daemonCtx, daemonBinary)
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(),
		"SANHO_HOME="+runtimeHome,
		"SANHO_SOCKET="+socketPath,
	)
	cmd.Stdout = &logs
	cmd.Stderr = &logs

	var stopOnce sync.Once
	stopDaemon := func() {
		stopOnce.Do(func() {
			cancel()
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			_ = cmd.Wait()
		})
	}

	if err := cmd.Start(); err != nil {
		stopDaemon()
		t.Fatalf("failed to start sanhod locally: %v\ndaemon logs:\n%s", err, logs.String())
	}
	t.Cleanup(stopDaemon)

	for {
		if err := pingHealth(socketPath, 100*time.Millisecond); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			stopDaemon()
			t.Fatalf("sanhod not healthy at %s after bootstrap: %v\ndaemon logs:\n%s", socketPath, ctx.Err(), logs.String())
		case <-time.After(25 * time.Millisecond):
		}
	}

	return testDaemonBaseURL, unixHTTPClient(socketPath, 10*time.Second), socketPath
}

// pingHealth performs a single health check with a short timeout.
func pingHealth(socketPath string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, testDaemonBaseURL+"/healthz", nil)
	if err != nil {
		return err
	}

	resp, err := unixHTTPClient(socketPath, timeout).Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check status %d", resp.StatusCode)
	}
	return nil
}

func unixHTTPClient(socketPath string, timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var dialer net.Dialer
				return dialer.DialContext(ctx, "unix", socketPath)
			},
		},
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to determine caller for repo root resolution")
	}
	root := filepath.Join(filepath.Dir(filename), "..", "..")
	abs, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("failed to resolve repo root: %v", err)
	}
	return abs
}

// getCliBinaryE2E returns a sanho binary path for e2e CLI runs.
// It prefers SANHO_CLI_BINARY, then repo-root bin/sanho, otherwise builds a temp binary.
func getCliBinaryE2E(t *testing.T) string {
	t.Helper()

	// Env override
	if bin := strings.TrimSpace(os.Getenv("SANHO_CLI_BINARY")); bin != "" {
		if _, err := os.Stat(bin); err == nil {
			return bin
		}
		t.Logf("SANHO_CLI_BINARY set but not found: %s", bin)
	}

	// Existing bin under repo root
	candidates := []string{
		filepath.Join(repoRoot(t), "bin", "sanho"),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}

	// Build temp binary
	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "sanho")
	cmd := exec.Command("go", "build", "-o", binPath, "./cmd/sanho")
	cmd.Dir = repoRoot(t)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to build sanho binary: %v\noutput:\n%s", err, string(out))
	}
	return binPath
}

// runCmdE2E executes a command and fails the test when it errors.
func runCmdE2E(t *testing.T, dir string, extraEnv map[string]string, name string, args ...string) []byte {
	t.Helper()
	out, err := testutil.RunCmd(dir, extraEnv, name, args...)
	if err != nil {
		t.Fatalf("command %s %v failed: %v\noutput:\n%s", name, args, err, string(out))
	}
	return out
}

// createSnapshotE2E builds a tar.gz snapshot from the provided file map.
func createSnapshotE2E(t *testing.T, files map[string]string) []byte {
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
			t.Fatalf("Failed to write tar header: %v", err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("Failed to write tar content: %v", err)
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("Failed to close tar writer: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("Failed to close gzip writer: %v", err)
	}

	return buf.Bytes()
}

// decodeSnapshotE2E decodes a base64 tar.gz snapshot into a simple file map.
func decodeSnapshotE2E(t *testing.T, encoded string) map[string]string {
	t.Helper()

	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("failed to base64-decode snapshot: %v", err)
	}

	gz, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("failed to create gzip reader: %v", err)
	}
	defer func() {
		_ = gz.Close()
	}()

	tr := tar.NewReader(gz)
	files := make(map[string]string)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("failed to read tar header: %v", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("failed to read tar entry %s: %v", hdr.Name, err)
		}
		files[hdr.Name] = string(data)
	}
	return files
}

// createOriginRepo sets up a bare origin and initial commit with the given files.
// Returns the origin repo path and HEAD hash.
func createOriginRepo(t *testing.T, files map[string]string) (string, string) {
	t.Helper()

	if len(files) == 0 {
		files = map[string]string{
			"docs/index.md": "# Initial\n",
		}
	}

	tmp := sharedRepoTempDir(t)

	originPath := filepath.Join(tmp, "origin")
	if err := os.Mkdir(originPath, 0755); err != nil {
		t.Fatalf("failed to create origin dir: %v", err)
	}
	runCmdE2E(t, "", nil, "git", "init", "--bare", originPath)

	localPath := filepath.Join(tmp, "local")
	runCmdE2E(t, "", nil, "git", "clone", originPath, localPath)
	runCmdE2E(t, localPath, nil, "git", "config", "user.email", "test@example.com")
	runCmdE2E(t, localPath, nil, "git", "config", "user.name", "Test User")

	for path, content := range files {
		full := filepath.Join(localPath, path)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatalf("failed to create dir for %s: %v", full, err)
		}
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write file %s: %v", full, err)
		}
	}

	runCmdE2E(t, localPath, nil, "git", "add", ".")
	runCmdE2E(t, localPath, nil, "git", "commit", "-m", "Initial commit")
	runCmdE2E(t, localPath, nil, "git", "push", "origin", "HEAD")

	head := strings.TrimSpace(string(runCmdE2E(t, localPath, nil, "git", "rev-parse", "HEAD")))
	return originPath, head
}

// sharedRepoTempDir returns a temp directory intended to be visible to sanhod
// even when it's running inside a container. Our docker dev setup mounts /tmp by default.
func sharedRepoTempDir(t *testing.T) string {
	t.Helper()

	base := os.TempDir()
	if runtime.GOOS == "darwin" {
		base = "/tmp"
	}
	dir, err := os.MkdirTemp(base, "sanho-e2e-*")
	if err != nil {
		t.Fatalf("failed to create shared temp dir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(dir)
	})
	return dir
}

// uniqueName returns a reasonably unique string with the given prefix.
func uniqueName(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

func addProject(t *testing.T, client *http.Client, baseURL, projectName, repoID, repoURL string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{
		"project":       projectName,
		"docs_repo_id":  repoID,
		"docs_repo_url": repoURL,
		"actor_email":   "admin@example.com",
	})
	resp, err := client.Post(baseURL+"/projects", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("add project failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		t.Fatalf("add project status = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

func deleteProject(t *testing.T, client *http.Client, baseURL, projectName string, force bool) {
	t.Helper()
	url := fmt.Sprintf("%s/projects/%s?force=%t", baseURL, projectName, force)
	req, _ := http.NewRequest(http.MethodDelete, url, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("delete project failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		t.Fatalf("delete project status = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

func registerWorkspace(t *testing.T, client *http.Client, baseURL string, req dto.RegisterWorkspaceRequest) (string, string) {
	t.Helper()
	body, _ := json.Marshal(req)
	resp, err := client.Post(baseURL+"/workspaces/register", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("register workspace failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		t.Fatalf("register workspace status = %d", resp.StatusCode)
	}
	var wsResp map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&wsResp); err != nil {
		_ = resp.Body.Close()
		t.Fatalf("failed to decode register response: %v", err)
	}
	_ = resp.Body.Close()
	return wsResp["workspace_id"], wsResp["current_docs_head"]
}

func deleteWorkspace(t *testing.T, client *http.Client, baseURL, workspaceID string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/workspaces/%s", baseURL, url.PathEscape(workspaceID)), nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("delete workspace failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		t.Fatalf("delete workspace status = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

func getHead(t *testing.T, client *http.Client, baseURL, project string) string {
	t.Helper()
	resp, err := client.Get(baseURL + "/docs/head?project=" + project)
	if err != nil {
		t.Fatalf("get head failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		t.Fatalf("get head status = %d", resp.StatusCode)
	}
	var headResp map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&headResp); err != nil {
		_ = resp.Body.Close()
		t.Fatalf("failed to decode head response: %v", err)
	}
	_ = resp.Body.Close()
	return headResp["head"]
}

func getSnapshot(t *testing.T, client *http.Client, baseURL, project, commit string) (string, map[string]string) {
	t.Helper()
	url := fmt.Sprintf("%s/docs/snapshot?project=%s", baseURL, project)
	if commit != "" {
		url += "&commit=" + commit
	}
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("get snapshot failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		t.Fatalf("get snapshot status = %d", resp.StatusCode)
	}
	var snapResp dto.GetSnapshotResponse
	if err := json.NewDecoder(resp.Body).Decode(&snapResp); err != nil {
		_ = resp.Body.Close()
		t.Fatalf("failed to decode snapshot response: %v", err)
	}
	_ = resp.Body.Close()
	return snapResp.Commit, decodeSnapshotE2E(t, snapResp.Snapshot)
}
