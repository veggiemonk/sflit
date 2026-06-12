// write.go — owns the commit seam: the lock–verify–rename atom (ADR-0001).

package splitter

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// errConflict reports that a file changed, appeared, or vanished between
// parse and commit. Run retries the whole pipeline on this error; nothing
// has been written when it is returned.
var errConflict = errors.New("conflict: file changed since parse")

// commit owns the atomicity of a write against concurrent sflit runs and
// other writers. It locks every snapshotted file's sidecar in sorted-path
// order (rules out deadlock between runs locking the same pair), re-reads
// each pre-image and compares hashes, and only then renames temp files into
// place. Temp files must be created before locking: writeTempFile may
// MkdirAll the target directory, and the critical section stays at a
// re-read plus renames. An empty commit (no snapshots) verifies nothing and
// takes no locks; it degrades to plain temp+rename.
//
// verifyOnly holds pre-images the rendered bytes were derived from but that
// this commit never writes (the source, in copy mode). verify checks snaps
// AND verifyOnly; lockAll locks only snaps. The lock guards the write
// window; copy never writes the source, so no source sidecar is created.
// A lock-free re-read+hash verify stays serializable: if the source changes
// after verify but before the sink rename, the copy is equivalent to one
// that completed entirely before that change.
type commit struct {
	snaps      []fileSnapshot
	verifyOnly []fileSnapshot
}

// lockAll acquires the sidecar lock of every snapshot path in sorted order
// and returns one release for all of them. The order must agree across
// processes regardless of how each spelled the paths (cwd-relative vs
// absolute), so paths are canonicalized before sorting; exact-string dedup
// keeps aliased snapshots of one file from self-deadlocking on a second fd.
// Symlink aliases are not resolved (filepath.EvalSymlinks fails on
// not-yet-existing sinks).
//
// On case-insensitive volumes (macOS APFS default) two differently-spelled
// paths can name the same sidecar inode. canonicalLockOrder sorts by the
// case-folded primary key so both spellings sort to the same position
// relative to all other paths (no AB-BA deadlock). Within a folded-equal
// run, lockAll probes each new path's sidecar via os.SameFile against every
// lock already held in that run; if the sidecar is the same file, the lock
// is already held and the new path is skipped. The probe is race-free: only
// the holder unlinks a sidecar, and we ARE the holder of the compared lock,
// so its inode binding is stable. The probe fd is closed immediately
// (never funlocked — that would unlink the shared sidecar under the held lock).
func (c commit) lockAll() (release func(), err error) {
	paths, err := canonicalLockOrder(c.snaps)
	if err != nil {
		return nil, err
	}
	type heldLock struct {
		unlock func() error
		info   fs.FileInfo
		path   string
	}
	held := make([]heldLock, 0, len(paths))
	release = func() {
		for i := len(held) - 1; i >= 0; i-- {
			_ = held[i].unlock()
		}
	}
	for i, p := range paths {
		// Within a folded-equal run, check whether this path's sidecar is
		// already held (case-insensitive alias of an earlier path). The
		// comparison walks held, not paths: a skipped alias never enters
		// held, so the two lists do not stay index-aligned.
		if i > 0 && strings.EqualFold(p, paths[i-1]) {
			lp := lockPath(p)
			//nolint:gosec // probe for identity dedup; mode matches acquireFileLockInfo
			probe, probeErr := os.OpenFile(filepath.Clean(lp), os.O_CREATE|os.O_RDONLY, 0o644)
			if probeErr == nil {
				probeInfo, statErr := probe.Stat()
				_ = probe.Close() // never funlock — that would unlink the shared sidecar
				if statErr == nil {
					alreadyHeld := false
					// The current folded-equal run is the tail of held: sorting
					// groups folded-equal paths adjacently and held appends in
					// acquisition order.
					for k := len(held) - 1; k >= 0 && strings.EqualFold(held[k].path, p); k-- {
						if os.SameFile(probeInfo, held[k].info) {
							alreadyHeld = true
							break
						}
					}
					if alreadyHeld {
						continue
					}
				}
			}
		}
		unlock, info, lockErr := acquireFileLockInfo(p)
		if lockErr != nil {
			release()
			return nil, fmt.Errorf("lock %s: %w", p, lockErr)
		}
		held = append(held, heldLock{unlock: unlock, info: info, path: p})
	}
	return release, nil
}

// canonicalLockOrder returns the lock-acquisition sequence for snaps:
// absolute paths, sorted by (strings.ToLower(abs), abs), deduplicated by
// exact string. Canonicalizing before sorting is what makes the order agree
// across processes that spelled the same files differently (cwd-relative vs
// absolute) — sorting the raw spellings reintroduces the e0ab1ed AB-BA
// deadlock.
//
// The case-folded primary key closes a second AB-BA variant on
// case-insensitive volumes (macOS APFS default): two runs spelling the same
// two files with different case ('B' vs 'b') sort them in opposite byte-wise
// orders and deadlock in untimed flock. Sorting by (ToLower, raw) gives one
// global total order on every platform: on case-insensitive volumes the two
// spellings of one file order identically vs everything else; on
// case-sensitive volumes genuinely distinct case-differing files get a
// deterministic order from the raw tiebreak. ToLower is not APFS's exact
// folding, but any fold that all processes compute identically yields a
// consistent order — exactness is irrelevant.
func canonicalLockOrder(snaps []fileSnapshot) ([]string, error) {
	paths := make([]string, 0, len(snaps))
	for _, s := range snaps {
		p, err := filepath.Abs(s.path)
		if err != nil {
			return nil, fmt.Errorf("lock %s: %w", s.path, err)
		}
		paths = append(paths, p)
	}
	sort.Slice(paths, func(i, j int) bool {
		li, lj := strings.ToLower(paths[i]), strings.ToLower(paths[j])
		if li != lj {
			return li < lj
		}
		return paths[i] < paths[j]
	})
	return slices.Compact(paths), nil
}

// verify re-reads every snapshot and compares against the pre-image.
// It checks both snaps (the files being written, under lock) and verifyOnly
// (files derived from but not written, e.g. the source in copy mode).
// Must run under lockAll.
func (c commit) verify() error {
	for _, s := range slices.Concat(c.snaps, c.verifyOnly) {
		data, err := os.ReadFile(filepath.Clean(s.path))
		switch {
		case errors.Is(err, fs.ErrNotExist):
			if s.exists {
				return fmt.Errorf("%s deleted since parse: %w", s.path, errConflict)
			}
		case err != nil:
			return fmt.Errorf("verify %s: %w", s.path, err)
		case !s.exists:
			return fmt.Errorf("%s created since parse: %w", s.path, errConflict)
		case sha256.Sum256(data) != s.sum:
			return fmt.Errorf("%s changed since parse: %w", s.path, errConflict)
		}
	}
	return nil
}

// commitEntry is one file to be written by the commit window helper.
// label appears in staging error messages; wrapRenameErr decorates a rename
// failure (a func, not a format string — paths may contain % signs).
type commitEntry struct {
	wrapRenameErr func(error) error
	label         string
	path          string
	data          []byte
}

// runCommitWindow is the shared lock→verify→rename atom for both writePair
// and writeSingle. It stages temps (one per entry, in order), acquires all
// locks, verifies all pre-images, then — under the lock — stats each rename
// target to pick the committed mode (existing file keeps its current mode;
// new file gets 0644), chmods the temp, and renames. Any failure at any
// stage cleans up remaining temps and any directory subtrees that were
// created by writeTempFile for this call.
//
// The ordered-entry shape is the anticipated interface for batch/plan mode
// (ADR-0001 Option E), which generalises the commit atom from 2 files to
// N+1. writePair and writeSingle are thin wrappers that exist to preserve
// the pinned error strings; the window logic lives here exactly once.
func (c commit) runCommitWindow(entries []commitEntry) error {
	type staged struct {
		tmp        string
		createdDir string
		entry      commitEntry
	}
	stagedList := make([]staged, 0, len(entries))

	// cleanupOne removes a temp and any dirs this entry's writeTempFile created.
	cleanupOne := func(s staged) {
		_ = os.Remove(filepath.Clean(s.tmp))
		if s.createdDir != "" {
			removeCreatedDirTree(filepath.Dir(s.entry.path), filepath.Dir(s.createdDir))
		}
	}
	cleanupAll := func() {
		for _, s := range stagedList {
			cleanupOne(s)
		}
	}

	for _, e := range entries {
		tmp, createdDir, err := writeTempFile(e.path, e.data)
		if err != nil {
			// The failed entry never reaches stagedList, but writeTempFile may
			// already have created its directory tree — roll that back too.
			if createdDir != "" {
				removeCreatedDirTree(filepath.Dir(e.path), filepath.Dir(createdDir))
			}
			cleanupAll()
			return fmt.Errorf("write %s temp: %w", e.label, err)
		}
		stagedList = append(stagedList, staged{entry: e, tmp: tmp, createdDir: createdDir})
	}

	release, err := c.lockAll()
	if err != nil {
		cleanupAll()
		return err
	}

	if err := c.verify(); err != nil {
		// Release the lock BEFORE cleanup: on unix, release unlinks the sidecar
		// from the target's directory. If we cleaned first, os.Remove on the
		// target dir would fail (non-empty: sidecar still present), leaving an
		// orphan directory tree. Release is idempotent so the defer below is safe.
		release()
		cleanupAll()
		return err
	}
	// Success path: release after all renames complete.
	defer release()

	// Under the lock: stat each rename target to pick the correct mode, chmod
	// the temp to that mode, then rename. Mode is sampled here — not in
	// writeTempFile — so a chmod between staging and locking is not silently
	// reverted by the rename.
	for i, s := range stagedList {
		mode := fs.FileMode(0o644)
		if info, statErr := os.Stat(filepath.Clean(s.entry.path)); statErr == nil {
			mode = info.Mode().Perm()
		}
		if err := os.Chmod(filepath.Clean(s.tmp), mode); err != nil {
			// Clean remaining temps; already-renamed files are committed.
			for _, rem := range stagedList[i:] {
				cleanupOne(rem)
			}
			return err
		}
		if err := os.Rename(s.tmp, s.entry.path); err != nil {
			cleanupOne(s)
			for _, rem := range stagedList[i+1:] {
				cleanupOne(rem)
			}
			return s.entry.wrapRenameErr(err)
		}
	}
	return nil
}

// removeCreatedDirTree removes empty directories starting at deepest and
// climbing up to but not including stopAt (the first ancestor that existed
// before the commit staged its temps). Each os.Remove call only deletes an
// empty directory, so this can never destroy user content — it merely
// reverses the MkdirAll that writeTempFile performed. On any Remove failure
// (non-empty dir, permissions, already gone) the walk stops.
func removeCreatedDirTree(deepest, stopAt string) {
	for d := deepest; d != stopAt && filepath.Dir(d) != d; d = filepath.Dir(d) {
		if err := os.Remove(d); err != nil {
			return
		}
	}
}

// writePair commits both files: each goes to a temp file in the same
// directory, fsynced; then, under the locks and only if every snapshot
// still verifies, both are renamed into place. Sink is renamed first so
// that if the source rename fails the user has duplicates but no data loss.
// If any step fails, written temps are cleaned up.
func (c commit) writePair(srcPath string, srcBytes []byte, sinkPath string, sinkBytes []byte) error {
	// Sink first in the rename order: on src rename failure the error names
	// the committed sink so the user can recover manually.
	return c.runCommitWindow([]commitEntry{
		{label: "sink", path: sinkPath, data: sinkBytes, wrapRenameErr: func(err error) error {
			return fmt.Errorf("rename sink: %w", err)
		}},
		{label: "src", path: srcPath, data: srcBytes, wrapRenameErr: func(err error) error {
			return fmt.Errorf("rename src (sink already committed at %s): %w", sinkPath, err)
		}},
	})
}

// writeSingle commits one file via temp+rename under the same lock–verify
// atom. verifyOnly snapshots (the source pre-image in copy mode) are
// verified lock-free: the lock guards the write window only; copy never
// writes the source, so no source sidecar is created or opened.
func (c commit) writeSingle(path string, data []byte) error {
	return c.runCommitWindow([]commitEntry{
		{label: "sink", path: path, data: data, wrapRenameErr: func(err error) error { return err }},
	})
}

// writeTempFile stages a temp file for finalPath in the same directory,
// fsynced. It creates parent directories with MkdirAll if needed, and
// returns the path to the created temp and the first ancestor directory
// that was created (empty string if no dirs were created — they already
// existed). The caller uses createdDir to roll back orphan directories on
// failure.
//
// The temp is created at CreateTemp's default 0600; the commit window
// (runCommitWindow) sets the final mode under the lock, after verify, so a
// chmod racing the staging cannot be silently reverted by the rename.
func writeTempFile(finalPath string, data []byte) (tmp, createdDir string, err error) {
	dir := filepath.Dir(finalPath)
	base := filepath.Base(finalPath)
	createdDir = firstMissingAncestor(dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}
	f, err := os.CreateTemp(dir, base+".tmp*")
	if err != nil {
		return "", createdDir, err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return "", createdDir, err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return "", createdDir, err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(f.Name())
		return "", createdDir, err
	}
	return f.Name(), createdDir, nil
}

// firstMissingAncestor walks up the directory tree starting at dir and
// returns the deepest path whose parent exists but which does not exist
// itself. Returns "" if dir already exists (no ancestor needs to be created).
func firstMissingAncestor(dir string) string {
	if _, err := os.Stat(dir); err == nil {
		return "" // already exists
	}
	missing := dir
	for {
		parent := filepath.Dir(missing)
		if parent == missing {
			return missing // reached root
		}
		if _, err := os.Stat(parent); err == nil {
			return missing // parent exists, missing is the first gap
		}
		missing = parent
	}
}
