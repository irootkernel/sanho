// Package fsx provides the durable-write and file-locking primitives
// shared by every sanho state store (sanho-v0.2.md §5.7; audit M5/M3).
//
// WriteFileAtomic is the v0.1 daemon writeAtomic promoted to a shared
// utility: unique temp name in the target directory → chmod → write →
// fsync(file) → rename → fsync(directory). WithFlock serializes
// cross-process access to the registry.
package fsx

import (
	"context"
	"errors"
	"os"
	"time"
)

// DefaultLockTimeout bounds how long WithFlock waits for the lock.
const DefaultLockTimeout = 5 * time.Second

// ErrLockTimeout is returned when the lock is not acquired in time.
// Callers surface it with the lock path and a next step (§5.9).
var ErrLockTimeout = errors.New("timed out waiting for file lock")

// WriteFileAtomic durably replaces path with data: unique temp file in
// the same directory, permissions perm, fsync on the file, atomic
// rename, fsync on the directory. Never leaves a partial target.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	panic("unimplemented (sanho v0.2 P1)")
}

// WithFlock runs fn while holding an exclusive flock on lockPath
// (created 0600 if absent). Acquisition respects ctx and
// DefaultLockTimeout, returning ErrLockTimeout on expiry. The lock is
// always released; fn's error is returned verbatim.
func WithFlock(ctx context.Context, lockPath string, fn func() error) error {
	panic("unimplemented (sanho v0.2 P1)")
}
