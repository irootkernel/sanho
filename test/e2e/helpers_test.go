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

// configuredBaseURL returns an explicitly configured E2E server. With no
// override, tests must launch an isolated server instead of reusing a
// developer's daemon and persistent state.
func configuredBaseURL() (string, bool) {
	if base := strings.TrimSpace(os.Getenv("SANHO_E2E_BASE_URL")); base != "" {
		return strings.TrimRight(base, "/"), true
	}
	return "", false
}

// requireServer uses an explicit server override when present. Otherwise it
// launches a server with a fresh state file on an ephemeral loopback port.
func requireServer(t *testing.T, ctx context.Context) string {
	t.Helper()

	base, configured := configuredBaseURL()
	if configured {
		healthErr := pingHealth(base, 2*time.Second)
		if healthErr == nil {
			return base
		}

		baseURL, err := url.Parse(base)
		if err != nil {
			t.Fatalf("invalid SANHO_E2E_BASE_URL %q: %v", base, err)
		}
		if !isLocalHost(baseURL.Hostname()) {
			t.Skipf("Skipping E2E: sanhod not reachable at %s (%v)", base, healthErr)
		}
	} else {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("reserve E2E server port: %v", err)
		}
		port := listener.Addr().(*net.TCPAddr).Port
		if err := listener.Close(); err != nil {
			t.Fatalf("release E2E server port: %v", err)
		}
		base = fmt.Sprintf("http://127.0.0.1:%d", port)
	}

	baseURL, err := url.Parse(base)
	if err != nil {
		t.Fatalf("invalid SANHO_E2E_BASE_URL %q: %v", base, err)
	}
	port := portOrDefault(baseURL)
	testDir := t.TempDir()
	statePath := filepath.Join(testDir, "sanho_state.json")
	serverBinary := strings.TrimSpace(os.Getenv("SANHO_DAEMON_BINARY"))
	if serverBinary == "" {
		serverBinary = filepath.Join(testDir, "sanhod")
		build := exec.Command("go", "build", "-o", serverBinary, "./cmd/sanhod")
		build.Dir = repoRoot(t)
		if output, buildErr := build.CombinedOutput(); buildErr != nil {
			t.Fatalf("build sanhod for E2E: %v\noutput:\n%s", buildErr, output)
		}
	}

	serverCtx, cancel := context.WithCancel(context.Background())
	var logs bytes.Buffer
	cmd := exec.CommandContext(serverCtx, serverBinary)
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(),
		"PORT="+port,
		"STATE_FILE_PATH="+statePath,
	)
	cmd.Stdout = &logs
	cmd.Stderr = &logs

	var stopOnce sync.Once
	stopServer := func() {
		stopOnce.Do(func() {
			cancel()
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			_ = cmd.Wait()
		})
	}

	if err := cmd.Start(); err != nil {
		stopServer()
		t.Fatalf("failed to start sanhod locally: %v\nserver logs:\n%s", err, logs.String())
	}
	t.Cleanup(stopServer)

	if err := testutil.WaitForHealth(ctx, base+"/healthz"); err != nil {
		stopServer()
		t.Fatalf("sanhod not healthy at %s after bootstrap: %v\nserver logs:\n%s", base, err, logs.String())
	}

	return base
}

// pingHealth performs a single health check with a short timeout.
func pingHealth(base string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/healthz", nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check status %d", resp.StatusCode)
	}
	return nil
}

// isLocalHost returns true for loopback or unspecified hosts.
func isLocalHost(host string) bool {
	if host == "" || host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsUnspecified())
}

func portOrDefault(u *url.URL) string {
	if port := u.Port(); port != "" {
		return port
	}
	return "5789"
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
	defer gz.Close()

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
		resp.Body.Close()
		t.Fatalf("add project status = %d", resp.StatusCode)
	}
	resp.Body.Close()
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
		resp.Body.Close()
		t.Fatalf("delete project status = %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func registerWorkspace(t *testing.T, client *http.Client, baseURL string, req dto.RegisterWorkspaceRequest) (string, string) {
	t.Helper()
	body, _ := json.Marshal(req)
	resp, err := client.Post(baseURL+"/workspaces/register", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("register workspace failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("register workspace status = %d", resp.StatusCode)
	}
	var wsResp map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&wsResp); err != nil {
		resp.Body.Close()
		t.Fatalf("failed to decode register response: %v", err)
	}
	resp.Body.Close()
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
		resp.Body.Close()
		t.Fatalf("delete workspace status = %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func getHead(t *testing.T, client *http.Client, baseURL, project string) string {
	t.Helper()
	resp, err := client.Get(baseURL + "/docs/head?project=" + project)
	if err != nil {
		t.Fatalf("get head failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("get head status = %d", resp.StatusCode)
	}
	var headResp map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&headResp); err != nil {
		resp.Body.Close()
		t.Fatalf("failed to decode head response: %v", err)
	}
	resp.Body.Close()
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
		resp.Body.Close()
		t.Fatalf("get snapshot status = %d", resp.StatusCode)
	}
	var snapResp dto.GetSnapshotResponse
	if err := json.NewDecoder(resp.Body).Decode(&snapResp); err != nil {
		resp.Body.Close()
		t.Fatalf("failed to decode snapshot response: %v", err)
	}
	resp.Body.Close()
	return snapResp.Commit, decodeSnapshotE2E(t, snapResp.Snapshot)
}
