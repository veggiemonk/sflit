package splitter

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
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
		t.Errorf("only %d runs observed a conflict, want >= %d — the optimistic-concurrency path was not exercised", contended, want)
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

// TestRunGaveUpReportsAttempts pins Result.Attempts on the exhausted-retries
// path: a run that gave up burned retries+1 attempts, and the Result must
// say so — the field exists for orchestrator observability, and the give-up
// case is where it matters most. The hook mutates the source before every
// commit, so every attempt conflicts.
func TestRunGaveUpReportsAttempts(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "big.go")
	if err := os.WriteFile(src, []byte("package foo\n\nfunc FilterA() int { return 0 }\n\nfunc Other() int { return 2 }\n"), 0o600); err != nil {
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
			content := fmt.Sprintf("package foo\n\nfunc FilterA() int { return %d }\n\nfunc Other() int { return 2 }\n", n)
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
