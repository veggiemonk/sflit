// lock.go — per-file advisory lock around the commit window (ADR-0001).
//
// The lock is taken on a sidecar file, not the target itself: temp+rename
// swaps the target's inode, so a waiter flocking the target would hold a
// lock on a dead inode. The sidecar is never unlinked — removing a locked
// file lets a third process lock a fresh inode while a waiter still holds
// the old one, yielding two lock holders. The kernel releases the lock on
// process death, so crashes leave no stale locks (a leftover sidecar file
// is inert).
//
// Platform lock calls live in lock_unix.go / lock_windows.go, adapted from
// github.com/gofrs/flock (BSD-3-Clause, Copyright 2015 Tim Heckman,
// 2018-2024 The Gofrs) rather than imported.

package splitter

import (
	"os"
	"path/filepath"
)

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
	f, err := os.OpenFile(filepath.Clean(lockPath(target)), os.O_CREATE|os.O_RDONLY, 0o600)
	if err != nil {
		return nil, err
	}
	if err := flockExclusive(f); err != nil {
		_ = f.Close()
		return nil, err
	}
	return func() error {
		if err := funlock(f); err != nil {
			_ = f.Close()
			return err
		}
		return f.Close()
	}, nil
}
