// Package fsx provides the durable-write and file-locking primitives
// shared by every sanho state store (docs/architecture.md "State and persistence"; audit M5/M3).
//
// WriteFileAtomic is the v0.1 daemon writeAtomic promoted to a shared
// utility: unique temp name in the target directory → chmod → write →
// fsync(file) → rename → fsync(directory). WithFlock serializes
// cross-process access to the registry.
package fsx

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// DefaultLockTimeout bounds how long WithFlock waits for the lock.
const DefaultLockTimeout = 5 * time.Second

// lockPollInterval is how often WithFlock retries a non-blocking flock
// attempt while waiting for the holder to release it. Polling (rather
// than a blocking syscall.Flock call in a background goroutine) keeps
// acquisition interruptible by ctx without risking a goroutine that
// stays blocked in the kernel past the caller's deadline and later
// acquires a lock nobody will ever release.
const lockPollInterval = 20 * time.Millisecond

// ErrLockTimeout is returned when the lock is not acquired in time.
// Callers surface it with the lock path and a next step (the guidance contract).
var ErrLockTimeout = errors.New("timed out waiting for file lock")

// WriteFileAtomic durably replaces path with data: unique temp file in
// the same directory, permissions perm, fsync on the file, atomic
// rename, fsync on the directory. Never leaves a partial target.
//
// The target directory must already exist; WriteFileAtomic does not
// create it (callers that need a fresh directory, e.g. wsstate's sync
// note, create it themselves before calling in).
func WriteFileAtomic(path string, data []byte, perm os.FileMode) (returnErr error) {
	dir := filepath.Dir(path)

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("fsx: create temp file for %s: %w", path, err)
	}
	tmpPath := tmp.Name()
	defer func() {
		if returnErr != nil {
			_ = os.Remove(tmpPath)
		}
	}()

	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("fsx: chmod temp file for %s: %w", path, err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("fsx: write temp file for %s: %w", path, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("fsx: fsync temp file for %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("fsx: close temp file for %s: %w", path, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("fsx: rename into place %s: %w", path, err)
	}

	dirFile, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("fsx: open directory %s for fsync: %w", dir, err)
	}
	if err := errors.Join(dirFile.Sync(), dirFile.Close()); err != nil {
		return fmt.Errorf("fsx: fsync directory %s: %w", dir, err)
	}
	return nil
}

// WithFlock runs fn while holding an exclusive flock on lockPath
// (created 0600 if absent). Acquisition respects ctx and
// DefaultLockTimeout, returning ErrLockTimeout on expiry. The lock is
// always released; fn's error is returned verbatim.
func WithFlock(ctx context.Context, lockPath string, fn func() error) error {
	if ctx == nil {
		ctx = context.Background()
	}

	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf("fsx: open lock file %s: %w", lockPath, err)
	}
	defer func() { _ = f.Close() }()

	if err := acquireFlock(ctx, f, lockPath); err != nil {
		return err
	}
	defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }()

	return fn()
}

// acquireFlock polls for an exclusive, non-blocking flock on f until it
// succeeds, ctx is done, or DefaultLockTimeout (bounded further by any
// earlier ctx deadline) elapses.
func acquireFlock(ctx context.Context, f *os.File, lockPath string) error {
	deadline := time.Now().Add(DefaultLockTimeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}

	fd := int(f.Fd())
	for {
		flockErr := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
		if flockErr == nil {
			return nil
		}
		if !errors.Is(flockErr, syscall.EWOULDBLOCK) && !errors.Is(flockErr, syscall.EAGAIN) {
			return fmt.Errorf("fsx: lock %s: %w", lockPath, flockErr)
		}

		remaining := time.Until(deadline)
		if remaining <= 0 {
			return fmt.Errorf("%w: %s", ErrLockTimeout, lockPath)
		}
		wait := lockPollInterval
		if remaining < wait {
			wait = remaining
		}

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("%w: %s", ErrLockTimeout, lockPath)
		case <-timer.C:
		}
	}
}
