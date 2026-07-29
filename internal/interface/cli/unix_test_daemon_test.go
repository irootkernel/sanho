package cli

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

type unixTestDaemon struct {
	SocketPath string
	httpServer *httptest.Server
}

func newUnixTestDaemon(t *testing.T, handler http.Handler) *unixTestDaemon {
	t.Helper()
	tempDir, err := os.MkdirTemp("/tmp", "sanho-cli-")
	if err != nil {
		t.Fatalf("create short temporary directory: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tempDir) })
	socketPath := filepath.Join(tempDir, "sanhod.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen on Unix socket: %v", err)
	}
	httpServer := httptest.NewUnstartedServer(handler)
	httpServer.Listener = listener
	httpServer.Start()
	return &unixTestDaemon{SocketPath: socketPath, httpServer: httpServer}
}

func (s *unixTestDaemon) Close() {
	s.httpServer.Close()
}
