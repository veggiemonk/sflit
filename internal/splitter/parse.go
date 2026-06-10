package splitter

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
)

// fileSnapshot records the content hash of the exact bytes a parse ran on.
// The commit step re-reads the file and compares sums to detect concurrent
// writers (ADR-0001). exists=false means the file was absent at parse time
// and must still be absent at commit.
type fileSnapshot struct {
	path   string
	sum    [sha256.Size]byte
	exists bool
}

func newFileSnapshot(path string, data []byte) fileSnapshot {
	return fileSnapshot{path: path, sum: sha256.Sum256(data), exists: true}
}

// parseGoFile parses a Go source file with comments attached to nodes.
// The returned FileSet must be used for all subsequent position lookups.
// The snapshot captures the bytes that were parsed, for commit-time verify.
func parseGoFile(path string) (*token.FileSet, *ast.File, fileSnapshot, error) {
	fset := token.NewFileSet()
	file, snap, err := parseGoFileInto(fset, path)
	if err != nil {
		return nil, nil, fileSnapshot{}, err
	}
	return fset, file, snap, nil
}

// parseGoFileInto parses path into the given fset. Source and sink must share
// one fset so positions remain comparable when decls travel between them.
// Hash and AST come from one read so the snapshot matches what was parsed.
func parseGoFileInto(fset *token.FileSet, path string) (*ast.File, fileSnapshot, error) {
	src, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fileSnapshot{}, fmt.Errorf("read %s: %w", path, err)
	}
	file, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return nil, fileSnapshot{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return file, newFileSnapshot(path, src), nil
}

// parseGoFileIfExists is like parseGoFile but returns nil ASTs and an
// exists=false snapshot when the file does not exist. Any other error is
// returned. The sink uses its own FileSet so position-based line heuristics
// in go/printer remain per-file when sink and moved decls are rendered
// separately.
func parseGoFileIfExists(path string) (*token.FileSet, *ast.File, fileSnapshot, error) {
	fset, file, snap, err := parseGoFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil, fileSnapshot{path: path}, nil
		}
		return nil, nil, fileSnapshot{}, err
	}
	return fset, file, snap, nil
}
