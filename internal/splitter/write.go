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
)

// errConflict reports that a file changed, appeared, or vanished between
// parse and commit. Run retries the whole pipeline on this error; nothing
// has been written when it is returned.
var errConflict = errors.New("conflict: file changed since parse")

// commit owns the atomicity of a write against concurrent sflit runs and
// other writers. It locks every snapshotted file's sidecar in sorted-path
// order (rules out deadlock between runs locking the same pair), re-reads
// each pre-image and compares hashes, and only then renames temp files into
// place. Temp files must be created before locking: writeTemp may MkdirAll
// the target directory, and the critical section stays at a re-read plus
// renames. An empty commit (no snapshots) verifies nothing and takes no
// locks; it degrades to plain temp+rename.
type commit struct {
	snaps []fileSnapshot
}

// lockAll acquires the sidecar lock of every snapshot path in sorted order
// and returns one release for all of them. The order must agree across
// processes regardless of how each spelled the paths (cwd-relative vs
// absolute), so paths are canonicalized before sorting; deduping keeps
// aliased snapshots of one file from self-deadlocking on a second fd of
// the same sidecar. Symlink aliases are not resolved
// (filepath.EvalSymlinks fails on not-yet-existing sinks).
func (c commit) lockAll() (release func(), err error) {
	paths := make([]string, 0, len(c.snaps))
	for _, s := range c.snaps {
		p, err := filepath.Abs(s.path)
		if err != nil {
			return nil, fmt.Errorf("lock %s: %w", s.path, err)
		}
		paths = append(paths, p)
	}
	sort.Strings(paths)
	paths = slices.Compact(paths)
	releases := make([]func() error, 0, len(paths))
	release = func() {
		for i := len(releases) - 1; i >= 0; i-- {
			_ = releases[i]()
		}
	}
	for _, p := range paths {
		unlock, err := acquireFileLock(p)
		if err != nil {
			release()
			return nil, fmt.Errorf("lock %s: %w", p, err)
		}
		releases = append(releases, unlock)
	}
	return release, nil
}

// verify re-reads every snapshot and compares against the pre-image.
// Must run under lockAll.
func (c commit) verify() error {
	for _, s := range c.snaps {
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

// writePair commits both files: each goes to a temp file in the same
// directory, fsynced; then, under the locks and only if every snapshot
// still verifies, both are renamed into place. Renames happen after both
// temps are ready. If any step fails, written temps are cleaned up.
func (c commit) writePair(srcPath string, srcBytes []byte, sinkPath string, sinkBytes []byte) error {
	srcTmp, err := writeTemp(srcPath, srcBytes)
	if err != nil {
		return fmt.Errorf("write src temp: %w", err)
	}
	sinkTmp, err := writeTemp(sinkPath, sinkBytes)
	if err != nil {
		_ = os.Remove(filepath.Clean(srcTmp))
		return fmt.Errorf("write sink temp: %w", err)
	}
	cleanup := func() {
		_ = os.Remove(filepath.Clean(srcTmp))
		_ = os.Remove(filepath.Clean(sinkTmp))
	}
	release, err := c.lockAll()
	if err != nil {
		cleanup()
		return err
	}
	defer release()
	if err := c.verify(); err != nil {
		cleanup()
		return err
	}
	// Sink first: if this fails, source is untouched.
	if err := os.Rename(sinkTmp, sinkPath); err != nil {
		cleanup()
		return fmt.Errorf("rename sink: %w", err)
	}
	// Source second: if this fails, user has duplicates but no data loss.
	if err := os.Rename(srcTmp, srcPath); err != nil {
		_ = os.Remove(srcTmp)
		return fmt.Errorf("rename src (sink already committed at %s): %w", sinkPath, err)
	}
	return nil
}

// writeSingle commits one file via temp+rename under the same lock–verify
// atom. Snapshots of files that are not being written (the source, in copy
// mode) are still verified: the rendered bytes were derived from them.
func (c commit) writeSingle(path string, data []byte) error {
	tmp, err := writeTemp(path, data)
	if err != nil {
		return err
	}
	release, err := c.lockAll()
	if err != nil {
		_ = os.Remove(filepath.Clean(tmp))
		return err
	}
	defer release()
	if err := c.verify(); err != nil {
		_ = os.Remove(filepath.Clean(tmp))
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func writeTemp(finalPath string, data []byte) (string, error) {
	dir := filepath.Dir(finalPath)
	base := filepath.Base(finalPath)
	if err := os.MkdirAll(dir, 0o740); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(dir, base+".tmp*")
	if err != nil {
		return "", err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return "", err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}
