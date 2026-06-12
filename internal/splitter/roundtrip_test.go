package splitter

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readBack reads a written output file, failing the test on error so a
// missing file surfaces as itself rather than as a confusing oracle diff.
func readBack(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestRoundTrip_MoveAndBack(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.go")
	b := filepath.Join(dir, "b.go")
	original := `package p

import "fmt"

// FilterA prints a.
func FilterA() {
	// in-body note
	fmt.Println("a")
}

// FilterB prints b.
func FilterB() { fmt.Println("b") }

// Keeper stays in a.go while its Filter method travels.
type Keeper struct{ n int }

// FilterCount reports n.
func (k Keeper) FilterCount() int { return k.n }

func Other() int { return Keeper{n: 1}.FilterCount() }
`
	writeFile(t, a, original)

	// move FilterA, FilterB, Keeper.FilterCount from a.go to b.go
	if _, err := Run(Config{Source: a, Sink: b, Regex: "^Filter", Move: true}); err != nil {
		t.Fatal(err)
	}
	typeCheckDir(t, dir)
	if err := SemEqual([]string{original}, []string{readBack(t, a), readBack(t, b)}); err != nil {
		t.Fatalf("after move: %v", err)
	}

	// move them back from b.go to a.go
	if _, err := Run(Config{Source: b, Sink: a, Regex: "^Filter", Move: true}); err != nil {
		t.Fatal(err)
	}
	typeCheckDir(t, dir)
	if err := SemEqual([]string{original}, []string{readBack(t, a), readBack(t, b)}); err != nil {
		t.Fatalf("after move back: %v", err)
	}
}

// A partial split of a grouped var: the moved spec leaves its group, comes
// back as a standalone decl, and the spec-level oracle must see equality.
func TestRoundTrip_GroupedVarPartial(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.go")
	b := filepath.Join(dir, "b.go")
	original := `package p

var (
	// limit caps things.
	limit = 8
	name  = "x" // inline note
)

func Other() int { return limit + len(name) }
`
	writeFile(t, a, original)

	if _, err := Run(Config{Source: a, Sink: b, Regex: "^limit$", Move: true}); err != nil {
		t.Fatal(err)
	}
	typeCheckDir(t, dir)
	if err := SemEqual([]string{original}, []string{readBack(t, a), readBack(t, b)}); err != nil {
		t.Fatalf("after move: %v", err)
	}

	if _, err := Run(Config{Source: b, Sink: a, Regex: "^limit$", Move: true}); err != nil {
		t.Fatal(err)
	}
	typeCheckDir(t, dir)
	if err := SemEqual([]string{original}, []string{readBack(t, a), readBack(t, b)}); err != nil {
		t.Fatalf("after move back: %v", err)
	}
}

// A moved decl that uses an aliased import: goimports cannot infer the
// alias from the identifier, so the rendered sink must carry the import.
func TestRun_AliasedImport_NewSink(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.go")
	b := filepath.Join(dir, "b.go")
	original := `package p

import f "fmt"

// FilterA prints a.
func FilterA() { f.Println("a") }

func Other() { f.Println("o") }
`
	writeFile(t, a, original)

	if _, err := Run(Config{Source: a, Sink: b, Regex: "^FilterA$", Move: true}); err != nil {
		t.Fatal(err)
	}
	typeCheckDir(t, dir)
	if err := SemEqual([]string{original}, []string{readBack(t, a), readBack(t, b)}); err != nil {
		t.Fatalf("after move: %v", err)
	}
}

// Copy into a different directory: the sink has no sibling files, so
// goimports cannot learn the alias from the package — the render itself
// must carry the named import or the written sink does not compile.
func TestRun_AliasedImport_CopyNewDir(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o750); err != nil {
		t.Fatal(err)
	}
	a := filepath.Join(dir, "a.go")
	b := filepath.Join(sub, "b.go")
	writeFile(t, a, `package p

import f "fmt"

// FilterA prints a.
func FilterA() { f.Println("a") }

func Other() { f.Println("o") }
`)

	if _, err := Run(Config{Source: a, Sink: b, Regex: "^FilterA$"}); err != nil {
		t.Fatal(err)
	}
	typeCheckDir(t, dir)
	typeCheckDir(t, sub)
	if err := SemEqual(
		[]string{"package p\n\nimport f \"fmt\"\n\n// FilterA prints a.\nfunc FilterA() { f.Println(\"a\") }\n"},
		[]string{readBack(t, b)},
	); err != nil {
		t.Fatalf("sink content: %v", err)
	}
}

func TestRun_AliasedImport_ExistingSink(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.go")
	b := filepath.Join(dir, "b.go")
	srcOriginal := `package p

import f "fmt"

// FilterA prints a.
func FilterA() { f.Println("a") }

func Other() { f.Println("o") }
`
	sinkOriginal := `package p

// Existing stays put.
func Existing() {}
`
	writeFile(t, a, srcOriginal)
	writeFile(t, b, sinkOriginal)

	if _, err := Run(Config{Source: a, Sink: b, Regex: "^FilterA$", Move: true}); err != nil {
		t.Fatal(err)
	}
	typeCheckDir(t, dir)
	if err := SemEqual(
		[]string{srcOriginal, sinkOriginal},
		[]string{readBack(t, a), readBack(t, b)},
	); err != nil {
		t.Fatalf("after move: %v", err)
	}
}

// TestMove_MixedSyntheticAndFuncKeepDocComments mixes both match kinds: a
// func (non-synthetic) declared before a partially narrowed var group
// (synthetic). The synthetic match must not be emitted ahead of the func,
// or go/printer flushes the func's doc comment before the var prints.
func TestMove_MixedSyntheticAndFuncKeepDocComments(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.go")
	b := filepath.Join(dir, "b.go")
	writeFile(t, a, `package p

// DocA documents DocA.
func DocA() int { return 1 }

var (
	// travels documents travels.
	travels = 8
	stays   = 9
)

func Other() int { return stays }
`)
	if _, err := Run(Config{Source: a, Sink: b, Regex: "^(DocA|travels)$", Move: true}); err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, b, nil, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	var doca *ast.FuncDecl
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == "DocA" {
			doca = fd
		}
	}
	if doca == nil {
		t.Fatal("DocA not in sink")
	}
	if doca.Doc == nil || !strings.Contains(doca.Doc.Text(), "DocA documents DocA") {
		t.Fatalf("DocA lost its doc comment in the sink (decls rendered out of position order); sink:\n%s", readBack(t, b))
	}
	if err := TypeCheckFiles(a, b); err != nil {
		t.Fatal(err)
	}
}
