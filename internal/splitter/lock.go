// lock.go — per-file advisory lock around the commit window (ADR-0001).
//
// The lock is taken on a sidecar file, not the target itself: temp+rename
// swaps the target's inode, so a waiter flocking the target would hold a
// lock on a dead inode. On release the sidecar is unlinked while the lock
// is still held (unix). Unlinking creates its own hazard — a waiter can be
// granted the lock on the unlinked inode while a third process locks a
// freshly created sidecar, two winners — so acquirers re-check after
// locking that the fd and the path still name the same file (os.SameFile)
// and retry on mismatch. Only the lock holder unlinks, so a recheck that
// passes under the lock proves the live inode is held. On windows the
// sidecar is never unlinked (deleting an open file needs POSIX delete
// semantics; best-effort platform, ADR-0001 Amendment 1), the recheck
// passes trivially, and lock files remain — inert and safe to gitignore.
// The kernel releases the lock on process death, so crashes leave no stale
// locks.
//
// Platform lock calls live in lock_unix.go / lock_windows.go, adapted from
// github.com/gofrs/flock (BSD-3-Clause, Copyright 2015 Tim Heckman,
// 2018-2024 The Gofrs) rather than imported.

package splitter

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

// testHookBetweenUnlinkAndFunlock runs in release between the sidecar
// unlink and the flock release. Tests use it to pin that order: the unlink
// must happen while the lock is still held, or the two-winners race the
// acquire-side recheck closes is reopened. No-op outside tests.
var testHookBetweenUnlinkAndFunlock = func() {}

// lockPath returns the sidecar lockfile path for target:
// dir/name.go -> dir/.name.go.sflit.lock.
func lockPath(target string) string {
	dir, base := filepath.Split(target)
	return filepath.Join(dir, "."+base+".sflit.lock")
}

// acquireFileLock takes an exclusive advisory lock on target's sidecar
// lockfile, blocking until it is available, and returns the func that
// releases it. Locks exclude between file descriptors, not just between
// processes.
func acquireFileLock(target string) (release func() error, err error) {
	path := filepath.Clean(lockPath(target))
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_RDONLY, 0o600)
		if err != nil {
			return nil, err
		}
		if err := flockExclusive(f); err != nil {
			_ = f.Close()
			return nil, err
		}
		current, err := lockfileCurrent(f, path)
		if err != nil {
			_ = funlock(f)
			_ = f.Close()
			return nil, err
		}
		if !current {
			// A releasing holder unlinked this inode between open and lock;
			// the lock held is on a dead file. Drop it and lock the fresh one.
			_ = funlock(f)
			_ = f.Close()
			continue
		}
		// Idempotent: a second call must not run removeLockFile without
		// holding the lock — that would unlink the current holder's sidecar
		// and reopen the two-winners race the recheck exists to close.
		var once sync.Once
		return func() (err error) {
			once.Do(func() {
				removeLockFile(path) // must happen while the lock is still held
				testHookBetweenUnlinkAndFunlock()
				if err = funlock(f); err != nil {
					_ = f.Close()
					return
				}
				err = f.Close()
			})
			return err
		}, nil
	}
}

// lockfileCurrent reports whether the locked fd still names path — false if
// a releasing holder unlinked it (and another acquirer possibly re-created
// it) between open and lock grant.
func lockfileCurrent(f *os.File, path string) (bool, error) {
	fi, err := f.Stat()
	if err != nil {
		return false, err
	}
	pi, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return os.SameFile(fi, pi), nil
}
