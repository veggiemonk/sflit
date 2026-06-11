package splitter

import (
	"errors"
	"io/fs"
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
	// Release unlinks the sidecar on unix. On windows it is left behind:
	// deleting an open file needs POSIX delete semantics (best-effort
	// platform, ADR-0001 Amendment 1).
	_, err = os.Stat(lockPath(target))
	if runtime.GOOS == "windows" {
		if err != nil {
			t.Fatalf("lockfile must remain on windows: %v", err)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("lockfile must be removed on release: stat err = %v", err)
	}
}

// TestLockReleaseIdempotent pins the sync.Once guard on release: a second
// call must be a no-op returning nil — in particular it must NOT run
// removeLockFile again, which would unlink the *next* holder's sidecar
// without holding the lock and reopen the two-winners race.
func TestLockReleaseIdempotent(t *testing.T) {
	target := filepath.Join(t.TempDir(), "a.go")
	release1, err := acquireFileLock(target)
	if err != nil {
		t.Fatalf("acquire 1: %v", err)
	}
	if err := release1(); err != nil {
		t.Fatalf("release 1: %v", err)
	}

	// A second holder now owns a fresh sidecar.
	release2, err := acquireFileLock(target)
	if err != nil {
		t.Fatalf("acquire 2: %v", err)
	}
	defer release2() //nolint:errcheck // re-released below

	if err := release1(); err != nil {
		t.Fatalf("double release: got %v, want nil no-op", err)
	}
	if _, err := os.Stat(lockPath(target)); err != nil {
		t.Fatalf("double release unlinked the current holder's sidecar: stat err = %v", err)
	}
	if err := release2(); err != nil {
		t.Fatalf("release 2: %v", err)
	}
}

// TestLockUnlinkNoTwoWinners regresses the two-winners race: release
// unlinks the sidecar, so a waiter granted the lock on the now-dead inode
// must detect it and retry instead of proceeding while a third acquirer
// holds a lock on the freshly created sidecar. Goroutines loop
// acquire/release so unlinks constantly interleave with blocked waiters;
// the atomic occupancy counter detects any overlap (atomics because the
// race detector cannot see the happens-before edge a kernel lock provides).
func TestLockUnlinkNoTwoWinners(t *testing.T) {
	target := filepath.Join(t.TempDir(), "a.go")
	var inCritical atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 50 {
				release, err := acquireFileLock(target)
				if err != nil {
					t.Error(err)
					return
				}
				if n := inCritical.Add(1); n != 1 {
					t.Errorf("%d goroutines in critical section", n)
				}
				runtime.Gosched()
				inCritical.Add(-1)
				if err := release(); err != nil {
					t.Error(err)
				}
			}
		})
	}
	wg.Wait()
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
