package splitter

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
)

func TestLockPathSidecar(t *testing.T) {
	got := lockPath(filepath.Join("dir", "a.go"))
	want := filepath.Join("dir", ".a.go.sflit.lock")
	if got != want {
		t.Fatalf("lockPath: got %q want %q", got, want)
	}
}

func TestLockAcquireReleaseReacquire(t *testing.T) {
	target := filepath.Join(t.TempDir(), "a.go")
	release, err := acquireFileLock(target)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if err := release(); err != nil {
		t.Fatalf("release: %v", err)
	}
	release, err = acquireFileLock(target)
	if err != nil {
		t.Fatalf("re-acquire: %v", err)
	}
	if err := release(); err != nil {
		t.Fatalf("release 2: %v", err)
	}
	// The sidecar lockfile is deliberately left behind: unlinking a locked
	// file lets a third process lock a fresh inode while a waiter holds the
	// dead one — two winners.
	if _, err := os.Stat(lockPath(target)); err != nil {
		t.Fatalf("lockfile must remain: %v", err)
	}
}

// TestLockMutualExclusion drives the lock from concurrent goroutines, each
// with its own file descriptor (flock/LockFileEx exclude between fds, not
// just between processes). An atomic occupancy counter detects any overlap
// inside the critical section; atomics are used because the race detector
// cannot see the happens-before edge a kernel lock provides.
func TestLockMutualExclusion(t *testing.T) {
	target := filepath.Join(t.TempDir(), "a.go")
	var inCritical, total atomic.Int32
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			release, err := acquireFileLock(target)
			if err != nil {
				t.Error(err)
				return
			}
			if n := inCritical.Add(1); n != 1 {
				t.Errorf("%d goroutines in critical section", n)
			}
			runtime.Gosched()
			total.Add(1)
			inCritical.Add(-1)
			if err := release(); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if total.Load() != 16 {
		t.Fatalf("total: got %d want 16", total.Load())
	}
}
