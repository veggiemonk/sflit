// lockproc_test.go — child-process entry points for the multi-process lock
// tests (ADR-0001 safety claims). Exported so testscript_test.go can
// register them as scripttest.Main commands; like TypeCheckFiles, they live
// in an in-package test file to reach the unexported lock seam while
// staying out of the shipped library.

package splitter

import (
	"fmt"
	"io"
)

// LockHoldMain implements the sflit-lockhold command: acquire the sidecar
// lock on target, print LOCKED so the parent knows the lock is held, then
// hold it until stdin reaches EOF (or the process is killed — the
// release-on-death tests SIGKILL it here, while the lock is held).
func LockHoldMain(target string, stdin io.Reader, stdout, stderr io.Writer) int {
	release, err := acquireFileLock(target)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintln(stdout, "LOCKED")
	_, _ = io.Copy(io.Discard, stdin)
	if err := release(); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

// LockStressMain implements the sflit-lockstress command: iterations times,
// acquire every path's sidecar lock through the real commit.lockAll (so the
// canonical cross-process ordering is what's exercised) and release. Paths
// are taken as spelled — the deadlock test spells the same two files
// cwd-relative from different working directories so their raw sort orders
// are opposite.
func LockStressMain(iterations int, paths []string, stderr io.Writer) int {
	snaps := make([]fileSnapshot, len(paths))
	for i, p := range paths {
		snaps[i] = fileSnapshot{path: p}
	}
	c := commit{snaps: snaps}
	for range iterations {
		release, err := c.lockAll()
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		release()
	}
	return 0
}
