//go:build unix

// lock_invariant_unix_test.go — pins the release-order invariant (sidecar
// unlink happens while the flock is still held) and the lockfileCurrent
// recheck branches. Unix-only: windows never unlinks the sidecar, so the
// invariant and the recheck branches are vacuous there.

package mover

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// TestReleaseUnlinksBeforeFunlock pins the order of the two release steps:
// the sidecar must be unlinked while the flock is still held. Swapping them
// reopens the two-winners race — a waiter granted the lock between funlock
// and unlink passes the lockfileCurrent recheck (the path still names its
// inode) while a third acquirer locks the freshly created sidecar. The
// testHookBetweenUnlinkAndFunlock seam runs between the two steps; from
// there, the sidecar must already be gone and a non-blocking flock on a
// second fd of the same inode must fail. Both assertions invert if the
// order is swapped.
func TestReleaseUnlinksBeforeFunlock(t *testing.T) {
	target := filepath.Join(t.TempDir(), "a.go")
	path := lockPath(target)

	release, err := acquireFileLock(target)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}

	// Second fd on the same inode as the holder's, to probe lock state from
	// inside the hook. Opened before release so the unlink cannot race it.
	probe, err := os.Open(filepath.Clean(path))
	if err != nil {
		t.Fatalf("open probe fd: %v", err)
	}
	defer probe.Close() //nolint:errcheck // read-only probe fd

	hookRan := false
	testHookBetweenUnlinkAndFunlock = func() {
		hookRan = true
		if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf(
				"sidecar still present between unlink and funlock: stat err = %v (unlink must precede funlock)",
				err,
			)
		}
		err := syscall.Flock(int(probe.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			_ = syscall.Flock(int(probe.Fd()), syscall.LOCK_UN)
			t.Error(
				"non-blocking flock succeeded between unlink and funlock: lock was released before the unlink",
			)
		} else if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			t.Errorf("probe flock: got %v, want EWOULDBLOCK", err)
		}
	}
	t.Cleanup(func() { testHookBetweenUnlinkAndFunlock = func() {} })

	if err := release(); err != nil {
		t.Fatalf("release: %v", err)
	}
	if !hookRan {
		t.Fatal("release hook never ran")
	}
}

// TestLockfileCurrent covers the acquire-side recheck branches
// deterministically: same inode (current), path unlinked (stale), and path
// unlinked then re-created (stale — a new acquirer's sidecar, not ours).
func TestLockfileCurrent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".a.go.sflit.lock")
	open := func() *os.File {
		t.Helper()
		//nolint:gosec // sidecar mode test helper
		f, err := os.OpenFile(filepath.Clean(path), os.O_CREATE|os.O_RDONLY, 0o644)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		t.Cleanup(func() { _ = f.Close() })
		return f
	}

	t.Run("same inode is current", func(t *testing.T) {
		f := open()
		current, err := lockfileCurrent(f, path)
		if err != nil {
			t.Fatalf("lockfileCurrent: %v", err)
		}
		if !current {
			t.Fatal("fd and path name the same inode: want current=true")
		}
	})

	t.Run("unlinked path is stale", func(t *testing.T) {
		f := open()
		if err := os.Remove(path); err != nil {
			t.Fatalf("remove: %v", err)
		}
		current, err := lockfileCurrent(f, path)
		if err != nil {
			t.Fatalf("lockfileCurrent: %v", err)
		}
		if current {
			t.Fatal("path unlinked: want current=false")
		}
	})

	t.Run("recreated inode is stale", func(t *testing.T) {
		f := open()
		if err := os.Remove(path); err != nil {
			t.Fatalf("remove: %v", err)
		}
		f2 := open() // re-creates the path with a fresh inode
		_ = f2
		current, err := lockfileCurrent(f, path)
		if err != nil {
			t.Fatalf("lockfileCurrent: %v", err)
		}
		if current {
			t.Fatal("path re-created with a fresh inode: want current=false")
		}
	})
}

// TestSidecarMode pins that a fresh sidecar has group and other read bits set
// (mode & 0o044 != 0, umask-safe). A crash-left sidecar owned by user A must
// remain openable and lockable by user B; 0600 would block the open with EACCES.
func TestSidecarMode(t *testing.T) {
	target := filepath.Join(t.TempDir(), "a.go")
	release, err := acquireFileLock(target)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer func() { _ = release() }()

	info, err := os.Stat(lockPath(target))
	if err != nil {
		t.Fatalf("stat sidecar: %v", err)
	}
	if info.Mode().Perm()&0o044 == 0 {
		t.Errorf(
			"sidecar mode = %o: group and other must have read (at least one of 0044 set) — "+
				"a crash-left sidecar owned by another user must stay openable/lockable",
			info.Mode().Perm(),
		)
	}
}
