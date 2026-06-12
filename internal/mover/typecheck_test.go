package mover

import (
	"errors"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TypeCheckFiles parses the given .go files as one package and runs the
// go/types checker over the set. It is the compile oracle for split output:
// SemEqual proves no declaration was lost or corrupted, the type check
// proves the surviving files still form a valid package (imports resolved,
// no duplicate or stranded identifiers). Exported so the external
// testscript package can expose it as the `gotypecheck` script command.
func TypeCheckFiles(paths ...string) error {
	if len(paths) == 0 {
		return errors.New("typecheck: no Go files")
	}
	fset := token.NewFileSet()
	files := make([]*ast.File, 0, len(paths))
	for _, p := range paths {
		f, err := parser.ParseFile(fset, filepath.Clean(p), nil, parser.SkipObjectResolution)
		if err != nil {
			return fmt.Errorf("typecheck: %w", err)
		}
		files = append(files, f)
	}
	conf := types.Config{Importer: importer.ForCompiler(fset, "source", nil)}
	if _, err := conf.Check(files[0].Name.Name, fset, files, nil); err != nil {
		return fmt.Errorf("typecheck: %w", err)
	}
	return nil
}

// TypeCheckDir type-checks every .go file in dir as one package.
func TypeCheckDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("typecheck %s: %w", dir, err)
	}
	var paths []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		paths = append(paths, filepath.Join(dir, e.Name()))
	}
	if len(paths) == 0 {
		return fmt.Errorf("typecheck %s: no Go files", dir)
	}
	return TypeCheckFiles(paths...)
}

func typeCheckDir(t *testing.T, dir string) {
	t.Helper()
	if err := TypeCheckDir(dir); err != nil {
		t.Fatalf("split output does not type-check: %v", err)
	}
}

func TestTypeCheckDir_DetectsBrokenPackage(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.go"), "package p\n\nfunc Foo() { undeclared() }\n")
	if err := TypeCheckDir(dir); err == nil {
		t.Fatal("want type error for undeclared identifier")
	}
}

func TestTypeCheckDir_DetectsDuplicateAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.go"), "package p\n\nfunc Foo() {}\n")
	writeFile(t, filepath.Join(dir, "b.go"), "package p\n\nfunc Foo() {}\n")
	if err := TypeCheckDir(dir); err == nil {
		t.Fatal("want redeclaration error across files")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
