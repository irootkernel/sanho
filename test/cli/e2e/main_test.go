package e2e

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if strings.TrimSpace(os.Getenv("SANHO_E2E_SOCKET")) != "" {
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
	tempDir, err := os.MkdirTemp("/tmp", "sanho-cli-e2e-")
	if err != nil {
		return nil, fmt.Errorf("create temp directory: %w", err)
	}

	cleanupTempDir := func() {
		_ = os.RemoveAll(tempDir)
	}
	serverBinary := strings.TrimSpace(os.Getenv("SANHO_DAEMON_BINARY"))
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
		"SANHO_HOME="+filepath.Join(tempDir, "home"),
		"SANHO_SOCKET="+filepath.Join(tempDir, "sanhod.sock"),
	)
	cmd.Stdout = &logs
	cmd.Stderr = &logs

	if err := cmd.Start(); err != nil {
		cancel()
		cleanupTempDir()
		return nil, fmt.Errorf("start server: %w", err)
	}

	socketPath := filepath.Join(tempDir, "sanhod.sock")
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer waitCancel()
	for {
		req, _ := http.NewRequestWithContext(waitCtx, http.MethodGet, "http://sanho/healthz", nil)
		resp, healthErr := unixHTTPClient(socketPath, 250*time.Millisecond).Do(req)
		if healthErr == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		select {
		case <-waitCtx.Done():
			cancel()
			_ = cmd.Wait()
			cleanupTempDir()
			return nil, fmt.Errorf("wait for health at %s: %w\nserver logs:\n%s", socketPath, waitCtx.Err(), logs.String())
		case <-time.After(25 * time.Millisecond):
		}
	}
	if err := os.Setenv("SANHO_E2E_SOCKET", socketPath); err != nil {
		cancel()
		_ = cmd.Wait()
		cleanupTempDir()
		return nil, fmt.Errorf("configure server socket: %w", err)
	}

	return func() {
		cancel()
		_ = cmd.Wait()
		_ = os.Unsetenv("SANHO_E2E_SOCKET")
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
