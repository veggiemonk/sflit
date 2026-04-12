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
	src, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", path, err)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return fset, file, nil
}

// parseGoFileIfExists is like parseGoFile but returns (nil,nil,nil) if the
// file does not exist. Any other error is returned.
func parseGoFileIfExists(path string) (*token.FileSet, *ast.File, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	return parseGoFile(path)
}
