//go:build unix

// lock_unix.go — flock(2) calls behind acquireFileLock.
// Adapted from github.com/gofrs/flock flock_unix.go (BSD-3-Clause,
// Copyright 2015 Tim Heckman, 2018-2024 The Gofrs).

package mover

import (
	"errors"
	"os"
	"syscall"
)

// flockRetry calls syscall.Flock with how, retrying on EINTR (a signal
// can interrupt a blocking flock; the correct response is to retry with
// the same arguments — the lock state is unchanged on EINTR).
func flockRetry(f *os.File, how int) error {
	for {
		err := syscall.Flock(int(f.Fd()), how)
		if !errors.Is(err, syscall.EINTR) {
			return err
		}
	}
}

// flockExclusive blocks until an exclusive lock is held on f.
func flockExclusive(f *os.File) error { return flockRetry(f, syscall.LOCK_EX) }

// funlock releases the lock held on f. Closing f would also release it;
// the explicit unlock keeps the unix and windows paths symmetric.
func funlock(f *os.File) error { return flockRetry(f, syscall.LOCK_UN) }

// removeLockFile unlinks the sidecar; the caller must still hold the lock
// (only the holder may unlink — acquirers detect the swap via os.SameFile).
// Failure is ignored: the sidecar then merely remains, which is inert.
func removeLockFile(path string) {
	_ = os.Remove(path)
}
