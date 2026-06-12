package splitter

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestConcurrentFanOut is ADR-0001's acceptance scenario: N concurrent
// -move invocations with disjoint regexes on one source, no external
// coordination. Every declaration must exist exactly once across the
// package afterwards — the silent-corruption failure mode was a lost
// update resurrecting moved declarations in the source.
func TestConcurrentFanOut(t *testing.T) {
	groups := []string{"Filter", "Render", "Store", "Parse", "Build", "Merge", "Split", "Clean"}
	dir := t.TempDir()
	src := filepath.Join(dir, "big.go")

	var sb strings.Builder
	sb.WriteString("package foo\n")
	want := map[string]int{"KeepA": 0, "KeepB": 0}
	for _, g := range groups {
		for _, s := range []string{"A", "B"} {
			fmt.Fprintf(&sb, "\nfunc %s%s() int { return 1 }\n", g, s)
			want[g+s] = 0
		}
	}
	sb.WriteString("\nfunc KeepA() int { return 1 }\n\nfunc KeepB() int { return 1 }\n")
	if err := os.WriteFile(src, []byte(sb.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	// First-attempt barrier: no run may commit until every run has parsed
	// the original source. The first committer then invalidates all other
	// snapshots, so at least len(groups)-1 runs MUST observe a conflict —
	// contention is guaranteed even on serialized hardware, where the test
	// would otherwise silently degrade to exercising the sequential path.
	var parseBarrier sync.WaitGroup
	parseBarrier.Add(len(groups))
	results := make([]Result, len(groups))
	var wg sync.WaitGroup
	for i, g := range groups {
		wg.Go(func() {
			firstAttempt := true
			// If this run dies before its first commit, release its barrier
			// slot so the surviving runs fail their assertions instead of
			// deadlocking in parseBarrier.Wait until the test timeout.
			defer func() {
				if firstAttempt {
					firstAttempt = false
					parseBarrier.Done()
				}
			}()
			res, err := Run(Config{
				Source: src,
				Sink:   filepath.Join(dir, strings.ToLower(g)+".go"),
				Regex:  "^" + g,
				Move:   true,
				// Worst case every commit invalidates all in-flight
				// runs, so rounds can reach len(groups); leave headroom
				// over the default bound.
				Retries: 4 * len(groups),
				testHookBeforeCommit: func() {
					if firstAttempt {
						firstAttempt = false
						parseBarrier.Done()
						parseBarrier.Wait()
					}
				},
			})
			if err != nil {
				t.Errorf("group %s: %v", g, err)
			}
			results[i] = res
		})
	}
	wg.Wait()

	contended := 0
	for i, res := range results {
		if res.Attempts >= 2 {
			contended++
		} else if res.Attempts < 1 {
			t.Errorf("group %s: attempts = %d, want >= 1", groups[i], res.Attempts)
		}
	}
	if want := len(groups) - 1; contended < want {
		t.Errorf(
			"only %d runs observed a conflict, want >= %d — the optimistic-concurrency path was not exercised",
			contended,
			want,
		)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	fset := token.NewFileSet()
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, 0)
		if err != nil {
			t.Fatalf("package corrupted, %s does not parse: %v", e.Name(), err)
		}
		if file.Name.Name != "foo" {
			t.Errorf("%s: package %q", e.Name(), file.Name.Name)
		}
		for _, d := range file.Decls {
			for _, key := range declKeys(d) {
				got[key]++
			}
		}
	}
	for name, n := range got {
		if n != 1 {
			t.Errorf("declaration %s exists %d times, want exactly 1", name, n)
		}
	}
	for name := range want {
		if got[name] == 0 {
			t.Errorf("declaration %s lost", name)
		}
	}
	if len(got) != len(want) {
		t.Errorf("declaration count: got %d want %d (%v)", len(got), len(want), got)
	}
}

// TestConcurrentSameSink drives fully overlapping lock sets: every run
// moves a different group from one source into the SAME sink, so all of
// them contend on both sidecar locks and every commit rewrites the file the
// others are about to append to. The disjoint-sink fan-out never stresses
// ordered two-lock acquisition with identical pairs; this does.
func TestConcurrentSameSink(t *testing.T) {
	groups := []string{"Filter", "Render", "Store", "Parse"}
	dir := t.TempDir()
	src := filepath.Join(dir, "big.go")
	sink := filepath.Join(dir, "moved.go")

	var sb strings.Builder
	sb.WriteString("package foo\n")
	for _, g := range groups {
		fmt.Fprintf(&sb, "\nfunc %s() int { return 1 }\n", g)
	}
	sb.WriteString("\nfunc Keep() int { return 1 }\n")
	if err := os.WriteFile(src, []byte(sb.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	var parseBarrier sync.WaitGroup
	parseBarrier.Add(len(groups))
	results := make([]Result, len(groups))
	var wg sync.WaitGroup
	for i, g := range groups {
		wg.Go(func() {
			firstAttempt := true
			// See TestConcurrentFanOut: a run dying pre-commit must not
			// deadlock the barrier.
			defer func() {
				if firstAttempt {
					firstAttempt = false
					parseBarrier.Done()
				}
			}()
			res, err := Run(Config{
				Source:  src,
				Sink:    sink,
				Regex:   "^" + g + "$",
				Move:    true,
				Retries: 4 * len(groups),
				testHookBeforeCommit: func() {
					if firstAttempt {
						firstAttempt = false
						parseBarrier.Done()
						parseBarrier.Wait()
					}
				},
			})
			if err != nil {
				t.Errorf("group %s: %v", g, err)
			}
			results[i] = res
		})
	}
	wg.Wait()

	contended := 0
	for _, res := range results {
		if res.Attempts >= 2 {
			contended++
		}
	}
	if want := len(groups) - 1; contended < want {
		t.Errorf("only %d runs observed a conflict, want >= %d", contended, want)
	}

	fset := token.NewFileSet()
	counts := map[string]int{}
	for _, path := range []string{src, sink} {
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("%s does not parse: %v", path, err)
		}
		for _, d := range file.Decls {
			for _, key := range declKeys(d) {
				counts[key]++
			}
		}
	}
	sinkFile, err := parser.ParseFile(fset, sink, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	inSink := map[string]bool{}
	for _, d := range sinkFile.Decls {
		for _, key := range declKeys(d) {
			inSink[key] = true
		}
	}
	for _, g := range groups {
		if counts[g] != 1 {
			t.Errorf("declaration %s exists %d times, want exactly 1", g, counts[g])
		}
		if !inSink[g] {
			t.Errorf("declaration %s did not land in the shared sink", g)
		}
	}
	if counts["Keep"] != 1 || inSink["Keep"] {
		t.Errorf("Keep must stay in source exactly once (count %d, inSink %v)", counts["Keep"], inSink["Keep"])
	}
}

// TestCommitPicksModeUnderLock pins that the mode of the committed file
// reflects a chmod that lands between writeTemp's stat and the rename.
// writeTemp stats the target before locking and chmods the temp to that
// mode; if a chmod races in after that stat but before the rename, the
// rename overwrites the target with the old mode — silently reverting it.
// The fix: stat-under-lock inside the commit window, Chmod the temp, then
// rename.
//
// The test calls commit.writePair directly (not via Run) so the goroutine
// enters writeTemp → lockAll without passing through the pre-commit hook
// which fires before writeTemp. The sidecar is held externally so the
// goroutine blocks after staging the temp (writeTemp done, stat done,
// temp chmoded to 0644). We then chmod the target to 0600 and release.
// Without the fix the committed file is 0644; with the fix it is 0600.
func TestCommitPicksModeUnderLock(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod semantics differ on windows")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "a.go")
	sink := filepath.Join(dir, "b.go")

	// Pre-create both files. src has a snapshot so verify passes.
	snap := snapshotted(t, src, "src-content")
	if err := os.WriteFile(filepath.Clean(sink), []byte("sink-content"), 0o644); err != nil {
		t.Fatal(err)
	}
	sinkSnap := newFileSnapshot(sink, []byte("sink-content"))

	// Hold the sink sidecar so the commit blocks in lockAll after writeTemp.
	releaseSink, err := acquireFileLock(sink)
	if err != nil {
		t.Fatalf("hold sink lock: %v", err)
	}

	blocked := make(chan struct{})
	done := make(chan struct{})
	var commitErr error
	go func() {
		defer close(done)
		// Signal just before entering the commit so the main goroutine knows
		// writeTemp has run and the temp already carries the pre-chmod mode.
		close(blocked)
		c := commit{snaps: []fileSnapshot{snap, sinkSnap}}
		commitErr = c.writePair(src, []byte("new-src"), sink, []byte("new-sink"))
	}()

	<-blocked
	// Give the goroutine time to reach lockAll (writeTemp completes fast;
	// 50 ms is ample for any scheduler latency without introducing a race).
	time.Sleep(50 * time.Millisecond)

	// chmod after writeTemp has sampled 0644 and chmoded the temp to 0644.
	if err := os.Chmod(sink, 0o600); err != nil {
		t.Fatalf("chmod sink: %v", err)
	}
	if err := releaseSink(); err != nil {
		t.Fatalf("release sink lock: %v", err)
	}
	<-done
	if commitErr != nil {
		t.Fatalf("writePair: %v", commitErr)
	}

	info, err := os.Stat(sink)
	if err != nil {
		t.Fatalf("stat sink: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf(
			"committed sink mode = %o, want 0600 — mode must be sampled under the lock, not before it",
			got,
		)
	}
}

// TestCommitBlocksOnHeldLock pins the commit/lock interaction
// deterministically, with no reliance on scheduler parallelism: while the
// sink's sidecar lock is held externally, a run that has rendered and is
// entering commit must block; once the lock is released it must complete
// without ever having observed a conflict.
func TestCommitBlocksOnHeldLock(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "big.go")
	sink := filepath.Join(dir, "filter.go")
	content := "package foo\n\nfunc FilterA() int { return 1 }\n\nfunc Other() int { return 2 }\n"
	if err := os.WriteFile(src, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	releaseSink, err := acquireFileLock(sink)
	if err != nil {
		t.Fatalf("hold sink lock: %v", err)
	}

	reachedCommit := make(chan struct{})
	var once sync.Once
	done := make(chan struct{})
	var res Result
	var runErr error
	go func() {
		defer close(done)
		res, runErr = Run(Config{
			Source: src,
			Sink:   sink,
			Regex:  "^Filter",
			Move:   true,
			testHookBeforeCommit: func() {
				once.Do(func() { close(reachedCommit) })
			},
		})
	}()

	<-reachedCommit
	// The run is past render, heading into lockAll on the held sidecar.
	// Without the lock it completes in single-digit milliseconds, so a
	// bounded wait distinguishes blocking from racing ahead.
	select {
	case <-done:
		t.Fatal("commit completed while the sink lock was held externally")
	case <-time.After(200 * time.Millisecond):
	}

	if err := releaseSink(); err != nil {
		t.Fatalf("release sink lock: %v", err)
	}
	<-done
	if runErr != nil {
		t.Fatalf("Run after release: %v", runErr)
	}
	if res.Attempts != 1 {
		t.Errorf("attempts = %d, want 1 (blocking is not a conflict)", res.Attempts)
	}
	if _, err := os.Stat(sink); err != nil {
		t.Fatalf("sink not written after release: %v", err)
	}
}

// TestOrphanSinkDirCleanedOnFailure pins that a new directory tree created by
// writeTemp (for a sink whose parent dirs did not yet exist) is removed on every
// failure path. A leaked directory is a go-build hazard: "go build ./..." sees a
// package-less directory and may error. The hook mutates the source on every
// attempt so all Retries+1 attempts conflict; after giving up, the sink dir
// must not exist.
func TestOrphanSinkDirCleanedOnFailure(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "big.go")
	// sink lives two levels deep — neither "sub" nor "sub/new" exists yet.
	sink := filepath.Join(dir, "sub", "new", "x.go")
	if err := os.WriteFile(
		src,
		[]byte("package foo\n\nfunc FilterA() int { return 0 }\n\nfunc Other() int { return 2 }\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	n := 0
	_, err := Run(Config{
		Source:  src,
		Sink:    sink,
		Regex:   "^Filter",
		Retries: 1,
		testHookBeforeCommit: func() {
			n++
			content := fmt.Sprintf(
				"package foo\n\nfunc FilterA() int { return %d }\n\nfunc Other() int { return 2 }\n",
				n,
			)
			if werr := os.WriteFile(src, []byte(content), 0o600); werr != nil {
				t.Error(werr)
			}
		},
	})
	if err == nil || !strings.Contains(err.Error(), "gave up") {
		t.Fatalf("want gave-up error, got %v", err)
	}

	// The first missing ancestor of sink was dir/sub — it must not exist.
	if _, statErr := os.Stat(filepath.Join(dir, "sub")); !os.IsNotExist(statErr) {
		t.Errorf("orphan sink dir left behind after failed commit: %v", statErr)
	}
}

// waitForGlob polls until pattern matches at least one path and returns the
// first match. Used to observe a staged temp from outside the committing
// goroutine without a hook inside the staging loop.
func waitForGlob(t *testing.T, pattern string) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		if len(matches) > 0 {
			return matches[0]
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s to appear", pattern)
	return ""
}

// TestOrphanSinkDirCleanedOnChmodFailure pins the release-before-cleanup
// order on the in-window failure paths (chmod/rename), mirroring the verify
// path: the sidecar lock lives in the sink's directory, so cleaning up while
// the lock is still held makes removeCreatedDirTree fail ENOTEMPTY on the
// sidecar and orphans the freshly created tree — the deferred release
// unlinks the sidecar too late. The src sidecar is held externally so
// writePair parks in lockAll after staging; deleting the staged sink temp
// out from under it makes the in-window chmod fail ENOENT and drives the
// cleanup path. TestOrphanSinkDirCleanedOnFailure covers only the
// verify-failure path.
func TestOrphanSinkDirCleanedOnChmodFailure(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.go")
	// sink lives two levels deep — neither "sub" nor "sub/new" exists yet.
	sink := filepath.Join(dir, "sub", "new", "x.go")
	snap := snapshotted(t, src, "src-content")

	// Hold the src sidecar (a.go sorts first) so the commit blocks in lockAll
	// after both temps are staged.
	releaseSrc, err := acquireFileLock(src)
	if err != nil {
		t.Fatalf("hold src lock: %v", err)
	}

	done := make(chan struct{})
	var commitErr error
	go func() {
		defer close(done)
		c := commit{snaps: []fileSnapshot{snap, {path: sink}}}
		commitErr = c.writePair(src, []byte("new-src"), sink, []byte("new-sink"))
	}()

	// writePair stages temps before locking; the held src sidecar keeps it
	// parked in lockAll, so the staged sink temp can be deleted race-free.
	sinkTmp := waitForGlob(t, filepath.Join(dir, "sub", "new", "x.go.tmp*"))
	if rmErr := os.Remove(sinkTmp); rmErr != nil {
		t.Fatalf("remove staged sink temp: %v", rmErr)
	}
	if relErr := releaseSrc(); relErr != nil {
		t.Fatalf("release src lock: %v", relErr)
	}
	<-done

	if commitErr == nil {
		t.Fatal("want in-window chmod failure, got nil")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "sub")); !os.IsNotExist(statErr) {
		t.Errorf("orphan sink dir left behind after in-window failure: stat err %v", statErr)
	}
	noTempLitter(t, dir)
}

// TestWritePair_ChmodFailureNamesCommittedSink pins the breadcrumb on the
// post-sink-commit chmod path: once the sink rename has landed, ANY later
// failure must say what already committed — in move mode the declarations
// now exist in both files and a bare chmod error gives no hint that a re-run
// will collide on the sink. The old writePair had no failure point between
// the two renames; the per-entry chmod under the lock introduced one. Same
// forced schedule as TestOrphanSinkDirCleanedOnChmodFailure, but deleting
// the staged SRC temp so entry 1's chmod fails after entry 0 (sink) renamed.
func TestWritePair_ChmodFailureNamesCommittedSink(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.go")
	sink := filepath.Join(dir, "b.go")
	snap := snapshotted(t, src, "src-content")

	releaseSrc, err := acquireFileLock(src)
	if err != nil {
		t.Fatalf("hold src lock: %v", err)
	}

	done := make(chan struct{})
	var commitErr error
	go func() {
		defer close(done)
		c := commit{snaps: []fileSnapshot{snap, {path: sink}}}
		commitErr = c.writePair(src, []byte("new-src"), sink, []byte("new-sink"))
	}()

	srcTmp := waitForGlob(t, filepath.Join(dir, "a.go.tmp*"))
	if rmErr := os.Remove(srcTmp); rmErr != nil {
		t.Fatalf("remove staged src temp: %v", rmErr)
	}
	if relErr := releaseSrc(); relErr != nil {
		t.Fatalf("release src lock: %v", relErr)
	}
	<-done

	if commitErr == nil {
		t.Fatal("want chmod failure after sink rename, got nil")
	}
	if !strings.Contains(commitErr.Error(), "chmod src temp") {
		t.Errorf("error should name the failing step: %v", commitErr)
	}
	if !strings.Contains(commitErr.Error(), "sink already committed at "+sink) {
		t.Errorf("error must flag the committed sink for manual recovery: %v", commitErr)
	}
	got, readErr := os.ReadFile(filepath.Clean(sink))
	if readErr != nil || string(got) != "new-sink" {
		t.Errorf("sink must stay committed when the src chmod fails: %q (err %v)", got, readErr)
	}
	gotSrc, readErr := os.ReadFile(filepath.Clean(src))
	if readErr != nil || string(gotSrc) != "src-content" {
		t.Errorf("src must be untouched when its chmod fails: %q (err %v)", gotSrc, readErr)
	}
	noTempLitter(t, dir)
}

// TestRunGaveUpReportsAttempts pins Result.Attempts on the exhausted-retries
// path: a run that gave up burned retries+1 attempts, and the Result must
// say so — the field exists for orchestrator observability, and the give-up
// case is where it matters most. The hook mutates the source before every
// commit, so every attempt conflicts.
func TestRunGaveUpReportsAttempts(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "big.go")
	if err := os.WriteFile(
		src,
		[]byte("package foo\n\nfunc FilterA() int { return 0 }\n\nfunc Other() int { return 2 }\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	n := 0
	res, err := Run(Config{
		Source:  src,
		Sink:    filepath.Join(dir, "filter.go"),
		Regex:   "^Filter",
		Move:    true,
		Retries: 1,
		testHookBeforeCommit: func() {
			n++
			content := fmt.Sprintf(
				"package foo\n\nfunc FilterA() int { return %d }\n\nfunc Other() int { return 2 }\n",
				n,
			)
			if werr := os.WriteFile(src, []byte(content), 0o600); werr != nil {
				t.Error(werr)
			}
		},
	})
	if err == nil || !strings.Contains(err.Error(), "gave up after 2 attempts") {
		t.Fatalf("want gave-up error after 2 attempts, got %v", err)
	}
	if res.Attempts != 2 {
		t.Errorf("attempts = %d, want 2 on the gave-up result", res.Attempts)
	}
}
