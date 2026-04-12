package splitter

import (
	"fmt"
	"os"
	"path/filepath"
)

// writePair writes both files atomically: each goes to a temp file in the
// same directory, fsynced, then renamed. Renames happen after both temps
// are ready. If any step fails, written temps are cleaned up.
func writePair(srcPath string, srcBytes []byte, sinkPath string, sinkBytes []byte) error {
	srcTmp, err := writeTemp(srcPath, srcBytes)
	if err != nil {
		return fmt.Errorf("write src temp: %w", err)
	}
	sinkTmp, err := writeTemp(sinkPath, sinkBytes)
	if err != nil {
		_ = os.Remove(filepath.Clean(srcTmp))
		return fmt.Errorf("write sink temp: %w", err)
	}
	// Sink first: if this fails, source is untouched.
	if err := os.Rename(sinkTmp, sinkPath); err != nil {
		_ = os.Remove(filepath.Clean(srcTmp))
		_ = os.Remove(filepath.Clean(sinkTmp))
		return fmt.Errorf("rename sink: %w", err)
	}
	// Source second: if this fails, user has duplicates but no data loss.
	if err := os.Rename(srcTmp, srcPath); err != nil {
		_ = os.Remove(srcTmp)
		return fmt.Errorf("rename src (sink already committed at %s): %w", sinkPath, err)
	}
	return nil
}

// writeSingle writes one file atomically via temp+rename.
func writeSingle(path string, data []byte) error {
	tmp, err := writeTemp(path, data)
	if err != nil {
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
