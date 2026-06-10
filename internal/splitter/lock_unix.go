//go:build unix

// lock_unix.go — flock(2) calls behind acquireFileLock.
// Adapted from github.com/gofrs/flock flock_unix.go (BSD-3-Clause,
// Copyright 2015 Tim Heckman, 2018-2024 The Gofrs).

package splitter

import (
	"errors"
	"os"
	"syscall"
)

// flockExclusive blocks until an exclusive lock is held on f.
func flockExclusive(f *os.File) error {
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
		if !errors.Is(err, syscall.EINTR) {
			return err
		}
	}
}

// funlock releases the lock held on f. Closing f would also release it;
// the explicit unlock keeps the unix and windows paths symmetric.
func funlock(f *os.File) error {
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		if !errors.Is(err, syscall.EINTR) {
			return err
		}
	}
}
