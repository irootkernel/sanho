package fsx_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/irootkernel/sanho/internal/infra/fsx"
)

func TestWriteFileAtomic_ContentAndPermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "example.json")
	data := []byte(`{"hello":"world"}`)

	if err := fsx.WriteFileAtomic(path, data, 0640); err != nil {
		t.Fatalf("WriteFileAtomic() error = %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("content = %q, want %q", got, data)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0640 {
		t.Fatalf("perm = %o, want %o", perm, os.FileMode(0640))
	}

	assertNoLeftoverTemp(t, dir)
}

func TestWriteFileAtomic_OverwriteReplacesContentAndPermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "example.json")

	if err := fsx.WriteFileAtomic(path, []byte("first"), 0644); err != nil {
		t.Fatalf("first WriteFileAtomic() error = %v", err)
	}
	if err := fsx.WriteFileAtomic(path, []byte("second"), 0600); err != nil {
		t.Fatalf("second WriteFileAtomic() error = %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(got) != "second" {
		t.Fatalf("content = %q, want %q", got, "second")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Fatalf("perm = %o, want 0600 (perm must come from the call, not linger from the old file)", perm)
	}

	assertNoLeftoverTemp(t, dir)
}

// TestWriteFileAtomic_NoPartialTargetOnRenameFailure forces the final
// rename to fail (a regular file cannot be renamed onto an existing
// directory on darwin or linux) and asserts the target is left exactly
// as it was — no partial/corrupt write — and the temp file is cleaned
// up. Using a directory as the obstruction (rather than a permission
// trick) keeps the test deterministic even when run as root.
func TestWriteFileAtomic_NoPartialTargetOnRenameFailure(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "blocked")
	if err := os.Mkdir(target, 0755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}

	err := fsx.WriteFileAtomic(target, []byte("payload"), 0644)
	if err == nil {
		t.Fatal("WriteFileAtomic() error = nil, want failure renaming onto a directory")
	}

	info, statErr := os.Stat(target)
	if statErr != nil {
		t.Fatalf("Stat() error = %v", statErr)
	}
	if !info.IsDir() {
		t.Fatal("target was replaced despite the failed rename — partial write leaked")
	}

	assertNoLeftoverTemp(t, dir)
}

// TestWriteFileAtomic_ConcurrentWritersNeverCorrupt fires 10 goroutines
// at the same target path simultaneously. Because every writer's temp
// file has a unique name and the swap into place is a single rename(2),
// the final content must always equal exactly one writer's full
// payload — never a truncated or interleaved mix.
func TestWriteFileAtomic_ConcurrentWritersNeverCorrupt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shared.json")

	const writers = 10
	payloads := make([][]byte, writers)
	for i := range payloads {
		payloads[i] = []byte(fmt.Sprintf(`{"writer":%d,"padding":%q}`, i, strings.Repeat("x", i+1)))
	}

	var wg sync.WaitGroup
	errCh := make(chan error, writers)
	for i := range writers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := fsx.WriteFileAtomic(path, payloads[i], 0644); err != nil {
				errCh <- err
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("WriteFileAtomic() error = %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	matched := false
	for _, p := range payloads {
		if bytes.Equal(got, p) {
			matched = true
			break
		}
	}
	if !matched {
		t.Fatalf("final content %q does not exactly match any single writer's payload — corruption or interleaving", got)
	}

	assertNoLeftoverTemp(t, dir)
}

// TestWithFlock_MutualExclusion runs two independent WithFlock calls
// (each opens its own file descriptor on lockPath, as real callers
// would) concurrently and records wall-clock intervals for their
// critical sections. Since flock is scoped per open-file-description,
// same-process contention between two separately opened fds is a real
// test of the exclusion, not a no-op.
func TestWithFlock_MutualExclusion(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "state.lock")

	type interval struct{ start, end time.Time }
	var mu sync.Mutex
	var intervals []interval

	var wg sync.WaitGroup
	errCh := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := fsx.WithFlock(context.Background(), lockPath, func() error {
				start := time.Now()
				time.Sleep(75 * time.Millisecond)
				end := time.Now()
				mu.Lock()
				intervals = append(intervals, interval{start, end})
				mu.Unlock()
				return nil
			})
			if err != nil {
				errCh <- err
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("WithFlock() error = %v", err)
	}

	if len(intervals) != 2 {
		t.Fatalf("got %d critical-section runs, want 2", len(intervals))
	}
	a, b := intervals[0], intervals[1]
	if a.start.Before(b.end) && b.start.Before(a.end) {
		t.Fatalf("critical sections overlapped (%v..%v and %v..%v) — flock did not serialize", a.start, a.end, b.start, b.end)
	}
}

// TestWithFlock_ErrLockTimeoutOnHeldLock holds the lock from outside
// WithFlock (a separate fd, as another process would) and checks that a
// contending WithFlock call gives up with ErrLockTimeout, naming the
// lock path, well before the 5s DefaultLockTimeout — bounded here by a
// short ctx deadline.
func TestWithFlock_ErrLockTimeoutOnHeldLock(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "state.lock")
	holdExternalLock(t, lockPath)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := fsx.WithFlock(ctx, lockPath, func() error {
		t.Fatal("fn must not run while the lock is held elsewhere")
		return nil
	})
	elapsed := time.Since(start)

	if !errors.Is(err, fsx.ErrLockTimeout) {
		t.Fatalf("WithFlock() error = %v, want ErrLockTimeout", err)
	}
	if !strings.Contains(err.Error(), lockPath) {
		t.Fatalf("error %q does not name the lock path %q", err.Error(), lockPath)
	}
	if elapsed >= fsx.DefaultLockTimeout {
		t.Fatalf("WithFlock() took %v, want well under DefaultLockTimeout (%v)", elapsed, fsx.DefaultLockTimeout)
	}
}

// TestWithFlock_CtxCancel holds the lock externally (so acquisition
// must wait) and cancels the caller's context shortly after starting;
// WithFlock must return promptly rather than waiting out
// DefaultLockTimeout.
func TestWithFlock_CtxCancel(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "state.lock")
	holdExternalLock(t, lockPath)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err := fsx.WithFlock(ctx, lockPath, func() error {
		t.Fatal("fn must not run: lock is held and ctx was cancelled")
		return nil
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("WithFlock() error = nil, want an error after ctx cancellation")
	}
	if !errors.Is(err, fsx.ErrLockTimeout) {
		t.Fatalf("WithFlock() error = %v, want ErrLockTimeout", err)
	}
	if elapsed >= fsx.DefaultLockTimeout {
		t.Fatalf("WithFlock() took %v, want to return promptly on ctx cancellation (well under DefaultLockTimeout %v)", elapsed, fsx.DefaultLockTimeout)
	}
}

// TestWithFlock_ReturnsFnErrorVerbatimAndAlwaysReleases checks both
// halves of the contract in one flow: a failing fn's error comes back
// unwrapped, and the lock is still released afterward so a later,
// independent caller can acquire it.
func TestWithFlock_ReturnsFnErrorVerbatimAndAlwaysReleases(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "state.lock")

	sentinel := errors.New("boom")
	err := fsx.WithFlock(context.Background(), lockPath, func() error {
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("WithFlock() error = %v, want sentinel %v verbatim", err, sentinel)
	}

	ran := false
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := fsx.WithFlock(ctx, lockPath, func() error {
		ran = true
		return nil
	}); err != nil {
		t.Fatalf("second WithFlock() error = %v", err)
	}
	if !ran {
		t.Fatal("second WithFlock() did not run fn — lock from the failed call was not released")
	}
}

func TestWithFlock_CreatesLockFileWithPrivatePermissions(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "state.lock")

	if err := fsx.WithFlock(context.Background(), lockPath, func() error { return nil }); err != nil {
		t.Fatalf("WithFlock() error = %v", err)
	}

	info, err := os.Stat(lockPath)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Fatalf("lock file perm = %o, want 0600", perm)
	}
}

// holdExternalLock opens and flocks lockPath outside of fsx.WithFlock,
// simulating another process holding it. The lock and fd are released
// via t.Cleanup.
func holdExternalLock(t *testing.T, lockPath string) {
	t.Helper()
	holder, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatalf("open lock file %s: %v", lockPath, err)
	}
	if err := syscall.Flock(int(holder.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatalf("Flock() error = %v", err)
	}
	t.Cleanup(func() {
		_ = syscall.Flock(int(holder.Fd()), syscall.LOCK_UN)
		_ = holder.Close()
	})
}

func assertNoLeftoverTemp(t *testing.T, dir string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, ".*.tmp-*"))
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("leftover temp files: %v", matches)
	}
}
