package mover

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
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

// TestCanonicalLockOrderCwdIndependent pins the property the e0ab1ed
// deadlock fix rests on, deterministically: two processes spelling the same
// two files cwd-relative from different working directories — raw spellings
// sorting in opposite orders — must still compute the same acquisition
// sequence. t.Chdir stands in for the second process; the real two-process
// version is TestLockNoCrossProcessDeadlock.
func TestCanonicalLockOrderCwdIndependent(t *testing.T) {
	work := t.TempDir()
	adir := filepath.Join(work, "a")
	bdir := filepath.Join(work, "b")
	for _, d := range []string{adir, bdir} {
		if err := os.Mkdir(d, 0o750); err != nil {
			t.Fatal(err)
		}
	}

	// From a: "../b/g.go" < "f.go". From b: "../a/f.go" < "g.go". Sorting
	// the raw spellings would acquire in opposite orders.
	t.Chdir(adir)
	fromA, err := canonicalLockOrder([]fileSnapshot{{path: "f.go"}, {path: filepath.Join("..", "b", "g.go")}})
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(bdir)
	fromB, err := canonicalLockOrder([]fileSnapshot{{path: "g.go"}, {path: filepath.Join("..", "a", "f.go")}})
	if err != nil {
		t.Fatal(err)
	}

	if len(fromA) != 2 {
		t.Fatalf("want 2 lock paths, got %v", fromA)
	}
	if !slices.Equal(fromA, fromB) {
		t.Fatalf("lock order depends on cwd spelling:\n  from a: %v\n  from b: %v", fromA, fromB)
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
// the rendered sink was derived from a stale parse. The source snapshot
// lives in verifyOnly (not snaps) — mirroring production: copy locks only
// the sink sidecar and verifies the source lock-free.
func TestCommitSingle_VerifiesSourceSnapshot(t *testing.T) {
	dir := t.TempDir()
	s := filepath.Join(dir, "a.go")
	k := filepath.Join(dir, "sub", "b.go")
	snap := snapshotted(t, s, "original")
	if err := os.WriteFile(s, []byte("mutated"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := commit{snaps: []fileSnapshot{{path: k}}, verifyOnly: []fileSnapshot{snap}}
	if err := c.writeSingle(k, []byte("copied")); !errors.Is(err, errConflict) {
		t.Fatalf("want errConflict, got %v", err)
	}
	if _, err := os.Stat(k); !os.IsNotExist(err) {
		t.Fatal("conflicting copy must not create sink")
	}
}

// noTempLitter asserts no temp file survived in dir: every failure path in
// the commit atom must clean up after itself, or aborted runs accumulate
// big.go.tmp* turds next to the sources.
func noTempLitter(t *testing.T, dir string) {
	t.Helper()
	litter, err := filepath.Glob(filepath.Join(dir, "*.tmp*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(litter) > 0 {
		t.Errorf("temp-file litter left behind: %v", litter)
	}
}

// TestWritePair_SinkRenameFails pins half of ADR-0001's atomicity story:
// the sink is renamed first, so when that rename fails the source has not
// been touched — no declarations were removed without landing anywhere.
// The sink path being an existing directory makes the rename fail.
func TestWritePair_SinkRenameFails(t *testing.T) {
	dir := t.TempDir()
	s := filepath.Join(dir, "a.go")
	if err := os.WriteFile(s, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	k := filepath.Join(dir, "sinkdir")
	if err := os.Mkdir(k, 0o750); err != nil {
		t.Fatal(err)
	}

	err := (commit{}).writePair(s, []byte("rendered"), k, []byte("moved"))
	if err == nil {
		t.Fatal("want sink-rename error, got nil")
	}
	if !strings.Contains(err.Error(), "rename sink") {
		t.Errorf("error should name the failing step: %v", err)
	}
	got, readErr := os.ReadFile(filepath.Clean(s))
	if readErr != nil || string(got) != "original" {
		t.Errorf("source must be untouched when sink rename fails: %q (err %v)", got, readErr)
	}
	noTempLitter(t, dir)
}

// TestWritePair_SrcRenameFails pins the other half: the sink committed but
// the source rename failed, so the user has duplicates but no data loss,
// and the error must say so and name the sink that already holds the
// declarations. The source path being a directory makes its rename fail.
func TestWritePair_SrcRenameFails(t *testing.T) {
	dir := t.TempDir()
	s := filepath.Join(dir, "srcdir")
	if err := os.Mkdir(s, 0o750); err != nil {
		t.Fatal(err)
	}
	k := filepath.Join(dir, "b.go")

	err := (commit{}).writePair(s, []byte("rendered"), k, []byte("moved"))
	if err == nil {
		t.Fatal("want src-rename error, got nil")
	}
	if !strings.Contains(err.Error(), "sink already committed at "+k) {
		t.Errorf("error must flag the committed sink for manual recovery: %v", err)
	}
	got, readErr := os.ReadFile(filepath.Clean(k))
	if readErr != nil || string(got) != "moved" {
		t.Errorf("sink must stay committed when src rename fails: %q (err %v)", got, readErr)
	}
	noTempLitter(t, dir)
	noTempLitter(t, s) // srcdir is where the src temp was created
}

// TestCommitConflict_NoTempLitter: a conflicting commit writes nothing —
// including the temp files it staged before taking the locks.
func TestCommitConflict_NoTempLitter(t *testing.T) {
	dir := t.TempDir()
	s := filepath.Join(dir, "a.go")
	k := filepath.Join(dir, "b.go")
	snap := snapshotted(t, s, "original")
	if err := os.WriteFile(s, []byte("mutated"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := commit{snaps: []fileSnapshot{snap, {path: k}}}
	if err := c.writePair(s, []byte("rendered"), k, []byte("moved")); !errors.Is(err, errConflict) {
		t.Fatalf("want errConflict, got %v", err)
	}
	noTempLitter(t, dir)

	// Copy mode: source in verifyOnly, only sink locked.
	cc := commit{snaps: []fileSnapshot{{path: k}}, verifyOnly: []fileSnapshot{snap}}
	if err := cc.writeSingle(k, []byte("copied")); !errors.Is(err, errConflict) {
		t.Fatalf("writeSingle: want errConflict, got %v", err)
	}
	noTempLitter(t, dir)
}

// TestStaging_MkdirAllFailureRollsBackCreatedDirs pins writeTempFile's
// createdDir contract on the MkdirAll failure path: when MkdirAll creates
// some ancestors and then fails (here: a leaf directory name over the
// 255-byte filesystem limit, so a/ and a/b/ are created before the leaf
// mkdir fails), the partially created tree must be rolled back, not leaked —
// a package-less directory is a `go build ./...` hazard. Regresses the
// `return "", "", err` that dropped createdDir, and the rollback walk
// stopping at the first Remove failure (the rollback starts at a path deeper
// than what MkdirAll actually created, so it must skip-and-climb).
func TestStaging_MkdirAllFailureRollsBackCreatedDirs(t *testing.T) {
	dir := t.TempDir()
	leaf := strings.Repeat("d", 300) // over every mainstream FS name limit
	sink := filepath.Join(dir, "a", "b", leaf, "x.go")

	err := (commit{}).writeSingle(sink, []byte("x"))
	if err == nil {
		t.Skip("filesystem accepts 300-byte directory names; recipe does not apply")
	}
	if !strings.Contains(err.Error(), "write sink temp") {
		t.Errorf("error should name the staging step: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "a")); !os.IsNotExist(statErr) {
		t.Errorf("partially created sink dir tree leaked after MkdirAll failure: stat err %v", statErr)
	}
}

// TestStagingVanishedDirClassifiesAsConflict pins the staging retry gate:
// writeTempFile MkdirAlls the target directory immediately before creating
// the temp, so fs.ErrNotExist from staging can only mean a concurrent run
// removed the directory in between (its conflict rollback removing a
// created-dir tree). That race is transient — a re-run re-stages and
// re-creates the directory — so it must classify as errConflict for Run's
// retry gate instead of a hard exit 1, or the fan-out-without-coordination
// guarantee (ADR-0001) breaks on a race a bare re-run would absorb. Any
// other staging failure stays non-retryable.
func TestStagingVanishedDirClassifiesAsConflict(t *testing.T) {
	vanished := wrapStageErr("sink", &fs.PathError{Op: "open", Path: "sub/x.go.tmp1", Err: fs.ErrNotExist})
	if !errors.Is(vanished, errConflict) {
		t.Errorf("vanished-dir staging failure must be retryable (errConflict): %v", vanished)
	}
	if !errors.Is(vanished, fs.ErrNotExist) {
		t.Errorf("classification must preserve the underlying cause: %v", vanished)
	}
	if !strings.Contains(vanished.Error(), "write sink temp") {
		t.Errorf("descriptive staging text lost: %v", vanished)
	}
	if other := wrapStageErr("sink", fs.ErrPermission); errors.Is(other, errConflict) {
		t.Errorf("non-ENOENT staging failure must not masquerade as a retryable conflict: %v", other)
	}
}

// TestVerify_ReadErrorIsNotConflict pins verify's non-ENOENT branch: a
// pre-image that cannot be re-read (here: permission denied) is an
// operational error, not a retryable conflict — retrying cannot fix EACCES,
// and masquerading would burn the retry budget hiding the real cause.
func TestVerify_ReadErrorIsNotConflict(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod 0 does not block reads on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root reads through permission bits")
	}
	dir := t.TempDir()
	s := filepath.Join(dir, "a.go")
	snap := snapshotted(t, s, "original")
	if err := os.Chmod(s, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(s, 0o600) })

	err := (commit{snaps: []fileSnapshot{snap}}).verify()
	if err == nil {
		t.Fatal("want read error, got nil")
	}
	if errors.Is(err, errConflict) {
		t.Fatalf("permission error must not masquerade as a retryable conflict: %v", err)
	}
	if !errors.Is(err, fs.ErrPermission) {
		t.Errorf("want wrapped fs.ErrPermission, got %v", err)
	}
}

// TestLockAllPartialAcquireRollback: when a later sidecar lock cannot be
// acquired, the locks already taken must be released — otherwise an
// aborted commit leaves the first file locked until process exit. The
// second path's directory is read-only so its sidecar cannot be created.
func TestLockAllPartialAcquireRollback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("read-only directory bits do not block file creation on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root creates through permission bits")
	}
	dir := t.TempDir()
	a := filepath.Join(dir, "a.go")
	rodir := filepath.Join(dir, "ro")
	if err := os.Mkdir(rodir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(rodir, 0o700) })
	b := filepath.Join(rodir, "b.go") // sorts after a.go: acquired second

	c := commit{snaps: []fileSnapshot{{path: a}, {path: b}}}
	if _, err := c.lockAll(); err == nil {
		t.Fatal("want lock error for read-only sidecar dir, got nil")
	}

	// a.go's lock must have been rolled back: a fresh acquire on it must
	// not block (locks exclude between fds, so a leak blocks forever).
	acquired := make(chan struct{})
	go func() {
		release, err := acquireFileLock(a)
		if err != nil {
			t.Error(err)
			return
		}
		close(acquired)
		_ = release()
	}()
	select {
	case <-acquired:
	case <-time.After(10 * time.Second):
		t.Fatal("first lock leaked: partial acquire was not rolled back")
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

// TestCommit_PreservesFileModes pins the commit atom's identity contract:
// temp+rename must not replace a file's permission bits with os.CreateTemp's
// 0600, and a new sink must get a regular 0644, not a private file.
func TestCommit_PreservesFileModes(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.go")
	b := filepath.Join(dir, "b.go")
	writeFile(t, a, "package p\n\nfunc Foo() {}\n\nfunc Bar() {}\n")
	if err := os.Chmod(a, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(Config{Source: a, Sink: b, Regex: "^Foo$", Move: true}); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]os.FileMode{a: 0o644, b: 0o644} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Errorf("%s mode = %o, want %o", filepath.Base(path), got, want)
		}
	}

	// A pre-existing sink keeps its own mode on rewrite.
	c := filepath.Join(dir, "c.go")
	writeFile(t, c, "package p\n\nfunc Existing() {}\n")
	if err := os.Chmod(c, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(Config{Source: b, Sink: c, Regex: "^Foo$", Move: true}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(c)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("pre-existing sink mode = %o, want it preserved as 600", got)
	}
}

// TestCommit_NewDirGroupTraversable pins that directories created for a new
// sink are usable: group read without group execute (the old 0o740) lets the
// group list the directory but not stat or open anything inside it.
func TestCommit_NewDirGroupTraversable(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.go")
	b := filepath.Join(dir, "sub", "b.go")
	writeFile(t, a, "package p\n\nfunc Foo() {}\n\nfunc Bar() {}\n")
	if _, err := Run(Config{Source: a, Sink: b, Regex: "^Foo$", Move: true}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Dir(b))
	if err != nil {
		t.Fatal(err)
	}
	perm := info.Mode().Perm()
	if perm&0o700 != 0o700 {
		t.Errorf("dir mode = %o: owner must keep rwx", perm)
	}
	if perm&0o040 != 0 && perm&0o010 == 0 {
		t.Errorf("dir mode = %o: group can read the listing but not traverse", perm)
	}
}

// TestCanonicalLockOrderCaseFolded pins the case-folding sort key: two
// different case-spellings of the same path prefix must produce an identical
// acquisition sequence so case-insensitive volumes (macOS APFS default)
// cannot AB-BA deadlock. The test is pure (no FS access) — it proves the
// sort logic regardless of whether the runtime FS is case-insensitive.
func TestCanonicalLockOrderCaseFolded(t *testing.T) {
	dir := t.TempDir()

	// snaps1 and snaps2 use opposite case for the b.go entry. On a
	// case-insensitive FS these name the same file; with a byte-wise sort
	// 'B' < 'a', so without folding they yield opposite sequences.
	bUpper := filepath.Join(dir, "B.go")
	bLower := filepath.Join(dir, "b.go")
	a := filepath.Join(dir, "a.go")

	snaps1 := []fileSnapshot{{path: bUpper}, {path: a}}
	snaps2 := []fileSnapshot{{path: bLower}, {path: a}}

	order1, err := canonicalLockOrder(snaps1)
	if err != nil {
		t.Fatal(err)
	}
	order2, err := canonicalLockOrder(snaps2)
	if err != nil {
		t.Fatal(err)
	}
	// Both sequences must have the same length.
	if len(order1) != len(order2) {
		t.Fatalf("different lengths: %v vs %v", order1, order2)
	}
	// The folded primary keys must agree in order (a-before-b in both).
	for i := range order1 {
		if !strings.EqualFold(order1[i], order2[i]) {
			t.Errorf(
				"lock order differs at position %d: %q vs %q — "+
					"case-aliased paths must produce the same acquisition sequence",
				i, order1[i], order2[i],
			)
		}
	}
}

// TestLockAllCaseInsensitiveDedup pins the identity-dedup path on a
// case-insensitive FS: lockAll given both "a.go" and "A.go" (same inode)
// must complete without self-deadlocking. Skips if t.TempDir is case-sensitive.
func TestLockAllCaseInsensitiveDedup(t *testing.T) {
	dir := t.TempDir()
	// Probe whether the FS is case-insensitive by writing "probe.go" and
	// stat-ing "PROBE.GO": SameFile iff case-insensitive.
	probeA := filepath.Join(dir, "probe.go")
	if err := os.WriteFile(probeA, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	infoA, err := os.Stat(probeA)
	if err != nil {
		t.Fatal(err)
	}
	probeB := filepath.Join(dir, "PROBE.GO")
	infoB, err := os.Stat(probeB)
	if err != nil {
		// ENOENT on a case-sensitive FS — skip.
		t.Skipf("FS is case-sensitive (stat %q: %v) — case-insensitive volumes only", probeB, err)
	}
	if !os.SameFile(infoA, infoB) {
		t.Skip("FS is case-sensitive — case-insensitive volumes only")
	}

	// All spellings name the same inode. lockAll must not self-deadlock.
	// Three spellings, not two: with more aliases than acquired locks the
	// dedup walk must index the held-lock list, not the sorted path list —
	// a paths-indexed walk reads past the held list and panics here.
	p := filepath.Join(dir, "a.go")
	pUpper := filepath.Join(dir, "A.go")
	pUpper2 := filepath.Join(dir, "A.GO")
	c := commit{snaps: []fileSnapshot{{path: p}, {path: pUpper}, {path: pUpper2}}}
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
		t.Fatal("deadlock: case-aliased snapshot paths must collapse to one lock on case-insensitive FS")
	}
}
