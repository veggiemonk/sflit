//go:build windows

// lock_windows.go — LockFileEx calls behind acquireFileLock.
// Adapted from github.com/gofrs/flock flock_windows.go (BSD-3-Clause,
// Copyright 2015 Tim Heckman, 2018-2024 The Gofrs).

package splitter

import (
	"os"

	"golang.org/x/sys/windows"
)

// flockExclusive blocks until an exclusive lock is held on f. The lock
// covers a single byte at offset 0, which is all LockFileEx needs for a
// whole-file advisory convention; the sidecar file has no content.
func flockExclusive(f *os.File) error {
	return windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, new(windows.Overlapped))
}

// funlock releases the lock held on f.
func funlock(f *os.File) error {
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, new(windows.Overlapped))
}
