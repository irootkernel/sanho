package http

import (
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"time"
)

var (
	ErrSocketInUse       = errors.New("sanho socket is already in use")
	ErrSocketPathInvalid = errors.New("sanho socket path is not a socket")
)

type unixListener struct {
	net.Listener
	path      string
	ownedInfo os.FileInfo
	once      sync.Once
}

// ListenUnix creates a private Unix socket, recovering only a stale socket file.
func ListenUnix(path string) (net.Listener, error) {
	info, err := os.Lstat(path)
	switch {
	case err == nil:
		if info.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("%w: %s", ErrSocketPathInvalid, path)
		}
		conn, dialErr := net.DialTimeout("unix", path, 250*time.Millisecond)
		if dialErr == nil {
			_ = conn.Close()
			return nil, fmt.Errorf("%w: %s", ErrSocketInUse, path)
		}
		if err := os.Remove(path); err != nil {
			return nil, fmt.Errorf("remove stale sanho socket: %w", err)
		}
	case os.IsNotExist(err):
	case err != nil:
		return nil, fmt.Errorf("inspect sanho socket: %w", err)
	}

	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("listen on sanho socket: %w", err)
	}
	if unix, ok := listener.(*net.UnixListener); ok {
		unix.SetUnlinkOnClose(false)
	}
	if err := os.Chmod(path, 0600); err != nil {
		_ = listener.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("secure sanho socket: %w", err)
	}
	ownedInfo, err := os.Lstat(path)
	if err != nil {
		_ = listener.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("inspect created sanho socket: %w", err)
	}
	return &unixListener{Listener: listener, path: path, ownedInfo: ownedInfo}, nil
}

func (l *unixListener) Close() error {
	err := l.Listener.Close()
	l.once.Do(func() {
		info, statErr := os.Lstat(l.path)
		if statErr == nil && os.SameFile(info, l.ownedInfo) {
			_ = os.Remove(l.path)
		}
	})
	return err
}
