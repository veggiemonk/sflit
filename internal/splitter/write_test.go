package splitter

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestWritePair_BothSucceed(t *testing.T) {
	dir := t.TempDir()
	s := filepath.Join(dir, "a.go")
	k := filepath.Join(dir, "b.go")
	if err := (commit{}).writePair(s, []byte("A"), k, []byte("B")); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Clean(s))
	if string(got) != "A" {
		t.Fatalf("src: %q", got)
	}
	got, _ = os.ReadFile(filepath.Clean(k))
	if string(got) != "B" {
		t.Fatalf("sink: %q", got)
	}
}

func TestWritePair_NewSink(t *testing.T) {
	dir := t.TempDir()
	s := filepath.Join(dir, "a.go")
	_ = os.WriteFile(s, []byte("old"), 0o600)
	k := filepath.Join(dir, "new.go") // does not exist
	if err := (commit{}).writePair(s, []byte("A"), k, []byte("B")); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Clean(k))
	if string(got) != "B" {
		t.Fatalf("sink: %q", got)
	}
}

// TestLockAllCanonicalOrdering regresses an AB-BA deadlock: lock order must
// be process-independent, so lockAll has to canonicalize paths before
// sorting. Here the same two files are spelled so the raw strings sort in
// opposite orders ("z/.." places the a.go alias lexically after b.go);
// without canonicalization the two committers lock in opposite order and
// block forever in flock, which has no deadlock detection.
func TestLockAllCanonicalOrdering(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.go")
	b := filepath.Join(dir, "b.go")
	aAlias := filepath.FromSlash(dir + "/z/../a.go")
	c1 := commit{snaps: []fileSnapshot{{path: a}, {path: b}}}
	c2 := commit{snaps: []fileSnapshot{{path: b}, {path: aAlias}}}
	done := make(chan struct{})
	go func() {
		defer close(done)
		var wg sync.WaitGroup
		for _, c := range []commit{c1, c2} {
			wg.Go(func() {
				for range 200 {
					release, err := c.lockAll()
					if err != nil {
						t.Error(err)
						return
					}
					release()
				}
			})
		}
		wg.Wait()
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("deadlock: lockAll lock order differs across path spellings")
	}
}

// TestLockAllDuplicatePaths regresses a self-deadlock: locks exclude between
// file descriptors, so two snapshots naming the same file must collapse to
// one sidecar lock instead of blocking forever on a second fd.
func TestLockAllDuplicatePaths(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.go")
	c := commit{snaps: []fileSnapshot{{path: p}, {path: p}}}
	done := make(chan struct{})
	go func() {
		defer close(done)
		release, err := c.lockAll()
		if err != nil {
			t.Error(err)
			return
		}
		release()
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("deadlock: duplicate snapshot paths must collapse to one lock")
	}
}

// snapshotted writes a file and returns its pre-image snapshot, as if it had
// just been parsed.
func snapshotted(t *testing.T, path, content string) fileSnapshot {
	t.Helper()
	if err := os.WriteFile(filepath.Clean(path), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return newFileSnapshot(path, []byte(content))
}

func TestCommitConflict_SourceChanged(t *testing.T) {
	dir := t.TempDir()
	s := filepath.Join(dir, "a.go")
	k := filepath.Join(dir, "b.go")
	snap := snapshotted(t, s, "original")
	// Another writer lands between parse and commit.
	if err := os.WriteFile(s, []byte("mutated"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := commit{snaps: []fileSnapshot{snap, {path: k}}}
	err := c.writePair(s, []byte("rendered"), k, []byte("moved"))
	if !errors.Is(err, errConflict) {
		t.Fatalf("want errConflict, got %v", err)
	}
	got, _ := os.ReadFile(filepath.Clean(s))
	if string(got) != "mutated" {
		t.Fatalf("conflicting commit must write nothing; src: %q", got)
	}
	if _, err := os.Stat(k); !os.IsNotExist(err) {
		t.Fatal("conflicting commit must not create sink")
	}
}

func TestCommitConflict_SinkAppeared(t *testing.T) {
	dir := t.TempDir()
	s := filepath.Join(dir, "a.go")
	k := filepath.Join(dir, "b.go")
	snap := snapshotted(t, s, "original")
	// Sink was absent at parse but a sibling run created it since.
	if err := os.WriteFile(k, []byte("surprise"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := commit{snaps: []fileSnapshot{snap, {path: k}}}
	err := c.writePair(s, []byte("rendered"), k, []byte("moved"))
	if !errors.Is(err, errConflict) {
		t.Fatalf("want errConflict, got %v", err)
	}
	got, _ := os.ReadFile(filepath.Clean(k))
	if string(got) != "surprise" {
		t.Fatalf("sink clobbered: %q", got)
	}
}

func TestCommitConflict_SourceDeleted(t *testing.T) {
	dir := t.TempDir()
	s := filepath.Join(dir, "a.go")
	k := filepath.Join(dir, "b.go")
	snap := snapshotted(t, s, "original")
	if err := os.Remove(s); err != nil {
		t.Fatal(err)
	}
	c := commit{snaps: []fileSnapshot{snap, {path: k}}}
	if err := c.writePair(s, []byte("rendered"), k, []byte("moved")); !errors.Is(err, errConflict) {
		t.Fatalf("want errConflict, got %v", err)
	}
}

func TestCommitClean_WritesBoth(t *testing.T) {
	dir := t.TempDir()
	s := filepath.Join(dir, "a.go")
	k := filepath.Join(dir, "b.go")
	snap := snapshotted(t, s, "original")
	c := commit{snaps: []fileSnapshot{snap, {path: k}}}
	if err := c.writePair(s, []byte("rendered"), k, []byte("moved")); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Clean(s))
	if string(got) != "rendered" {
		t.Fatalf("src: %q", got)
	}
	got, _ = os.ReadFile(filepath.Clean(k))
	if string(got) != "moved" {
		t.Fatalf("sink: %q", got)
	}
}

// Copy mode writes only the sink, but a mutated source still conflicts:
// the rendered sink was derived from a stale parse.
func TestCommitSingle_VerifiesSourceSnapshot(t *testing.T) {
	dir := t.TempDir()
	s := filepath.Join(dir, "a.go")
	k := filepath.Join(dir, "sub", "b.go")
	snap := snapshotted(t, s, "original")
	if err := os.WriteFile(s, []byte("mutated"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := commit{snaps: []fileSnapshot{snap, {path: k}}}
	if err := c.writeSingle(k, []byte("copied")); !errors.Is(err, errConflict) {
		t.Fatalf("want errConflict, got %v", err)
	}
	if _, err := os.Stat(k); !os.IsNotExist(err) {
		t.Fatal("conflicting copy must not create sink")
	}
}

func TestCommitSingle_Clean(t *testing.T) {
	dir := t.TempDir()
	s := filepath.Join(dir, "a.go")
	k := filepath.Join(dir, "sub", "b.go")
	snap := snapshotted(t, s, "original")
	c := commit{snaps: []fileSnapshot{snap, {path: k}}}
	if err := c.writeSingle(k, []byte("copied")); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Clean(k))
	if string(got) != "copied" {
		t.Fatalf("sink: %q", got)
	}
}
