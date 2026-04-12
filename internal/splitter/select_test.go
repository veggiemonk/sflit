package splitter

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func mustParse(t *testing.T, src string) (*token.FileSet, *ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "t.go", src, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	return fset, f
}

func matchNames(ms []Match) []string {
	var out []string
	for _, m := range ms {
		switch d := m.Decl.(type) {
		case *ast.FuncDecl:
			out = append(out, d.Name.Name)
		case *ast.GenDecl:
			for _, s := range d.Specs {
				if ts, ok := s.(*ast.TypeSpec); ok {
					out = append(out, "type "+ts.Name.Name)
				}
			}
		}
	}
	return out
}

func TestSelectRegexOnly_PlainFuncs(t *testing.T) {
	_, f := mustParse(t, `package p

func FilterA() {}
func FilterB() {}
func other() {}
func (r *R) FilterC() {} // method, must NOT match regex-only mode
`)
	cfg := Config{Regex: "^Filter"}
	ms, err := selectDecls(f, cfg)
	if err != nil {
		t.Fatal(err)
	}
	got := matchNames(ms)
	want := []string{"FilterA", "FilterB"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestSelectRegexOnly_NoMatch(t *testing.T) {
	_, f := mustParse(t, "package p\nfunc Foo(){}\n")
	ms, err := selectDecls(f, Config{Regex: "^Bar"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 0 {
		t.Fatalf("want 0 matches, got %d", len(ms))
	}
}

func TestSelectReceiverOnly(t *testing.T) {
	_, f := mustParse(t, `package p

type R struct{ X int }
func (r R) A() {}
func (r *R) B() {}
func (q *Q) C() {} // different receiver
type Q struct{}
func Plain() {}
`)
	ms, err := selectDecls(f, Config{Receiver: "R"})
	if err != nil {
		t.Fatal(err)
	}
	got := matchNames(ms)
	want := map[string]bool{"type R": true, "A": true, "B": true}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for _, g := range got {
		if !want[g] {
			t.Fatalf("unexpected %q in %v", g, got)
		}
	}
}

func TestSelectReceiverOnly_GroupedTypeDecl(t *testing.T) {
	_, f := mustParse(t, `package p

type (
	Helper struct{}
	MyStruct struct{ X int }
)

func (m MyStruct) Foo() {}
`)
	ms, err := selectDecls(f, Config{Receiver: "MyStruct"})
	if err != nil {
		t.Fatal(err)
	}
	got := matchNames(ms)
	// Want: "type MyStruct", "Foo" — NOT "type Helper"
	want := map[string]bool{"type MyStruct": true, "Foo": true}
	if len(got) != len(want) {
		t.Fatalf("got %v want keys %v", got, want)
	}
	for _, g := range got {
		if !want[g] {
			t.Fatalf("unexpected %q in %v", g, got)
		}
	}
	// Also verify Helper still exists in the original file.Decls after mutation.
	var helperFound bool
	for _, d := range f.Decls {
		if gd, ok := d.(*ast.GenDecl); ok {
			for _, s := range gd.Specs {
				if ts, ok := s.(*ast.TypeSpec); ok && ts.Name.Name == "Helper" {
					helperFound = true
				}
			}
		}
	}
	if !helperFound {
		t.Fatal("Helper type was removed from source after splitting MyStruct out")
	}
}

func TestSelectReceiverOnly_Generic(t *testing.T) {
	_, f := mustParse(t, `package p

type R[T any] struct{ v T }

func (r R[T]) A() {}
func (r *R[T]) B() {}
func (r *R[T]) C() {}
`)
	ms, err := selectDecls(f, Config{Receiver: "R"})
	if err != nil {
		t.Fatal(err)
	}
	// Expect: type R, A, B, C (4 matches)
	got := matchNames(ms)
	if len(got) != 4 {
		t.Fatalf("want 4 matches, got %v", got)
	}
}

func TestSelectReceiverAndRegex(t *testing.T) {
	_, f := mustParse(t, `package p

type R struct{}
func (r R) FilterA() {}
func (r R) FilterB() {}
func (r R) Other() {}
`)
	ms, err := selectDecls(f, Config{Receiver: "R", Regex: "^Filter"})
	if err != nil {
		t.Fatal(err)
	}
	got := matchNames(ms)
	if len(got) != 2 {
		t.Fatalf("want 2, got %v", got)
	}
	for _, g := range got {
		if g != "FilterA" && g != "FilterB" {
			t.Fatalf("unexpected %q", g)
		}
	}
}
