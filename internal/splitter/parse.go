package splitter

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
)

// parseGoFile parses a Go source file with comments attached to nodes.
// The returned FileSet must be used for all subsequent position lookups.
func parseGoFile(path string) (*token.FileSet, *ast.File, error) {
	fset := token.NewFileSet()
	file, err := parseGoFileInto(fset, path)
	if err != nil {
		return nil, nil, err
	}
	return fset, file, nil
}

// parseGoFileInto parses path into the given fset. Source and sink must share
// one fset so positions remain comparable when decls travel between them.
func parseGoFileInto(fset *token.FileSet, path string) (*ast.File, error) {
	src, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	file, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return file, nil
}

// parseGoFileIfExists is like parseGoFile but returns (nil, nil, nil) when
// the file does not exist. Any other error is returned. The sink uses its
// own FileSet so position-based line heuristics in go/printer remain
// per-file when sink and moved decls are rendered separately.
func parseGoFileIfExists(path string) (*token.FileSet, *ast.File, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("stat %s: %w", path, err)
	}
	return parseGoFile(path)
}
