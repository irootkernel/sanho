package http

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "sanho-listener-")
	if err != nil {
		t.Fatalf("create short temporary directory: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func TestListenUnixCreatesPrivateSocketAndRemovesItOnClose(t *testing.T) {
	path := filepath.Join(shortTempDir(t), "sanhod.sock")

	listener, err := ListenUnix(path)
	if err != nil {
		t.Fatalf("ListenUnix() error = %v", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("stat socket: %v", err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		t.Fatalf("%s is not a socket", path)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("socket mode = %o, want 600", got)
	}
	if err := listener.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("socket remains after close: %v", err)
	}
}

func TestListenUnixRejectsActiveSocket(t *testing.T) {
	path := filepath.Join(shortTempDir(t), "sanhod.sock")
	active, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen active socket: %v", err)
	}
	defer active.Close()

	if _, err := ListenUnix(path); !errors.Is(err, ErrSocketInUse) {
		t.Fatalf("ListenUnix() error = %v, want ErrSocketInUse", err)
	}
}

func TestListenUnixRecoversStaleSocket(t *testing.T) {
	path := filepath.Join(shortTempDir(t), "sanhod.sock")
	stale, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen stale socket: %v", err)
	}
	if err := stale.Close(); err != nil {
		t.Fatalf("close stale socket: %v", err)
	}

	listener, err := ListenUnix(path)
	if err != nil {
		t.Fatalf("ListenUnix() error = %v", err)
	}
	defer listener.Close()
}

func TestListenUnixPreservesRegularFile(t *testing.T) {
	path := filepath.Join(shortTempDir(t), "sanhod.sock")
	if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
		t.Fatalf("write regular file: %v", err)
	}

	if _, err := ListenUnix(path); !errors.Is(err, ErrSocketPathInvalid) {
		t.Fatalf("ListenUnix() error = %v, want ErrSocketPathInvalid", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read preserved file: %v", err)
	}
	if string(data) != "keep" {
		t.Fatalf("regular file changed: %q", data)
	}
}

func TestUnixListenerDoesNotRemoveReplacementSocket(t *testing.T) {
	path := filepath.Join(shortTempDir(t), "sanhod.sock")
	listener, err := ListenUnix(path)
	if err != nil {
		t.Fatalf("ListenUnix() error = %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove original socket path: %v", err)
	}
	replacement, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen on replacement socket: %v", err)
	}
	defer replacement.Close()

	if err := listener.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("replacement socket was removed: %v", err)
	}
}
