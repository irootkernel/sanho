package httpclient

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

type unixTestServer struct {
	URL    string
	server *httptest.Server
}

func newUnixTestServer(t *testing.T, handler http.Handler) *unixTestServer {
	t.Helper()
	tempDir, err := os.MkdirTemp("/tmp", "sanho-httpclient-")
	if err != nil {
		t.Fatalf("create short temporary directory: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tempDir) })
	socketPath := filepath.Join(tempDir, "sanhod.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen on Unix socket: %v", err)
	}
	server := httptest.NewUnstartedServer(handler)
	server.Listener = listener
	server.Start()
	return &unixTestServer{URL: socketPath, server: server}
}

func (s *unixTestServer) Close() {
	s.server.Close()
}
