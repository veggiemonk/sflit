// lock_multiprocess_test.go — converts ADR-0001's two load-bearing lock
// claims from asserted to tested. flock excludes per open-file-description,
// so in-process goroutine tests cannot exercise either claim: release on
// process death needs a process to die, and the e0ab1ed AB-BA deadlock
// needs two processes with different working directories. Children are the
// sflit-lockhold / sflit-lockstress commands registered in TestMain
// (scripttest.Main puts them on PATH as copies of this test binary).

package splitter_test

import (
	"bufio"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// startLockHold launches sflit-lockhold on target and returns once the
// child reports LOCKED. The returned stdin pipe keeps the child holding the
// lock until it is closed (graceful release) or the process is killed.
func startLockHold(ctx context.Context, t *testing.T, target string) (*exec.Cmd, io.WriteCloser) {
	t.Helper()
	cmd := exec.CommandContext(ctx, "sflit-lockhold", target)
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sflit-lockhold: %v", err)
	}
	// Blocks until the child holds the lock. If it never acquires, the
	// context deadline kills the child and Scan returns false.
	sc := bufio.NewScanner(stdout)
	if !sc.Scan() || sc.Text() != "LOCKED" {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("child never reported LOCKED (ctx err: %v)", ctx.Err())
	}
	return cmd, stdin
}

// TestLockReleasedOnProcessDeath pins the claim that made flock win over
// O_EXCL lockfiles in ADR-0001: the kernel releases the lock when the
// holding process dies, so a crash leaves no stale lock and the next
// process acquires without any manual cleanup. A child is SIGKILLed while
// holding the lock — no userspace release runs — and a second child must
// then acquire within the deadline, with the dead holder's sidecar file
// still sitting on disk.
func TestLockReleasedOnProcessDeath(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	target := filepath.Join(t.TempDir(), "a.go")
	sidecar := filepath.Join(filepath.Dir(target), "."+filepath.Base(target)+".sflit.lock")

	holder, holderStdin := startLockHold(ctx, t, target)
	defer holderStdin.Close() //nolint:errcheck // child is killed below

	// SIGKILL while the lock is held: the child gets no chance to release
	// or unlink anything.
	if err := holder.Process.Kill(); err != nil {
		t.Fatalf("kill holder: %v", err)
	}
	_ = holder.Wait() // reap; exits non-zero by signal

	// The dead holder's sidecar is still on disk — whatever the second
	// acquirer achieves, it is not thanks to userspace cleanup.
	if _, err := os.Stat(sidecar); err != nil {
		t.Fatalf("stale sidecar should remain after SIGKILL: stat err = %v", err)
	}

	// The kernel released the flock with the dead process's fd table, so a
	// fresh process must acquire. If the lock were stale, this blocks until
	// the context deadline and fails.
	second, secondStdin := startLockHold(ctx, t, target)
	if err := secondStdin.Close(); err != nil {
		t.Fatalf("close second stdin: %v", err)
	}
	if err := second.Wait(); err != nil {
		t.Fatalf("second holder release: %v", err)
	}

	// And the release path still works post-recovery: on unix the sidecar
	// is unlinked again (windows keeps it by design, ADR-0001 Amendment 1).
	_, err := os.Stat(sidecar)
	if runtime.GOOS == "windows" {
		if err != nil {
			t.Fatalf("sidecar must remain on windows: %v", err)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("sidecar must be unlinked after graceful release: stat err = %v", err)
	}
}

// TestLockNoCrossProcessDeadlock regresses the e0ab1ed AB-BA deadlock:
// lock-acquisition order must agree across processes regardless of how
// each spelled the paths. Two children loop the real commit.lockAll over
// the same two files, each from its own working directory with
// cwd-relative spellings whose *raw* sort orders are opposite — only
// canonicalization before sorting keeps the acquisition order shared. If
// ordering regresses, the children deadlock holding one lock each and the
// context deadline fails the test.
func TestLockNoCrossProcessDeadlock(t *testing.T) {
	work := t.TempDir()
	adir := filepath.Join(work, "a")
	bdir := filepath.Join(work, "b")
	for _, d := range []string{adir, bdir} {
		if err := os.Mkdir(d, 0o750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	// From adir: "../b/g.go" sorts before "f.go". From bdir: "../a/f.go"
	// sorts before "g.go". Raw spellings would lock in opposite orders.
	const iterations = "300"
	fromA := exec.CommandContext(ctx, "sflit-lockstress", iterations, "f.go", filepath.Join("..", "b", "g.go"))
	fromA.Dir = adir
	fromB := exec.CommandContext(ctx, "sflit-lockstress", iterations, "g.go", filepath.Join("..", "a", "f.go"))
	fromB.Dir = bdir

	for _, cmd := range []*exec.Cmd{fromA, fromB} {
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			t.Fatalf("start sflit-lockstress: %v", err)
		}
	}
	for _, cmd := range []*exec.Cmd{fromA, fromB} {
		if err := cmd.Wait(); err != nil {
			t.Fatalf(
				"sflit-lockstress from %s: %v (ctx err: %v — deadline means deadlock)",
				cmd.Dir,
				err,
				ctx.Err(),
			)
		}
	}
}
