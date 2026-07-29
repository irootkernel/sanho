package e2e

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	testutil "github.com/irootkernel/sanho/test/util"
)

func TestMain(m *testing.M) {
	if strings.TrimSpace(os.Getenv("KKACHI_E2E_BASE_URL")) != "" {
		os.Exit(m.Run())
	}

	stopServer, err := startIsolatedServer()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "start isolated sanhod for CLI E2E: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()
	stopServer()
	os.Exit(code)
}

func startIsolatedServer() (func(), error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("reserve port: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		return nil, fmt.Errorf("release reserved port: %w", err)
	}

	tempDir, err := os.MkdirTemp("", "sanho-e2e-server-*")
	if err != nil {
		return nil, fmt.Errorf("create temp directory: %w", err)
	}

	cleanupTempDir := func() {
		_ = os.RemoveAll(tempDir)
	}
	serverBinary := strings.TrimSpace(os.Getenv("KKACHI_SERVER_BINARY"))
	if serverBinary == "" {
		serverBinary = filepath.Join(tempDir, "sanhod")
		build := exec.Command("go", "build", "-o", serverBinary, "./cmd/sanhod")
		build.Dir = cliE2ERepoRoot()
		if output, buildErr := build.CombinedOutput(); buildErr != nil {
			cleanupTempDir()
			return nil, fmt.Errorf("build server: %w\noutput:\n%s", buildErr, output)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	var logs bytes.Buffer
	cmd := exec.CommandContext(ctx, serverBinary)
	cmd.Dir = cliE2ERepoRoot()
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("PORT=%d", port),
		"STATE_FILE_PATH="+filepath.Join(tempDir, "kkachi_state.json"),
	)
	cmd.Stdout = &logs
	cmd.Stderr = &logs

	if err := cmd.Start(); err != nil {
		cancel()
		cleanupTempDir()
		return nil, fmt.Errorf("start server: %w", err)
	}

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer waitCancel()
	if err := testutil.WaitForHealth(waitCtx, baseURL+"/healthz"); err != nil {
		cancel()
		_ = cmd.Wait()
		cleanupTempDir()
		return nil, fmt.Errorf("wait for health at %s: %w\nserver logs:\n%s", baseURL, err, logs.String())
	}
	if err := os.Setenv("KKACHI_E2E_BASE_URL", baseURL); err != nil {
		cancel()
		_ = cmd.Wait()
		cleanupTempDir()
		return nil, fmt.Errorf("configure server URL: %w", err)
	}

	return func() {
		cancel()
		_ = cmd.Wait()
		_ = os.Unsetenv("KKACHI_E2E_BASE_URL")
		cleanupTempDir()
	}, nil
}

func cliE2ERepoRoot() string {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	root, err := filepath.Abs(filepath.Join(filepath.Dir(filename), "..", "..", ".."))
	if err != nil {
		return ""
	}
	return root
}
