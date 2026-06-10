package splitter

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
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
				switch ss := s.(type) {
				case *ast.TypeSpec:
					out = append(out, "type "+ss.Name.Name)
				case *ast.ValueSpec:
					kind := "var"
					if d.Tok == token.CONST {
						kind = "const"
					}
					for _, n := range ss.Names {
						out = append(out, kind+" "+n.Name)
					}
				}
			}
		}
	}
	return out
}

func TestSelectRegexOnly_MatchesAllKinds(t *testing.T) {
	// regex-only mode matches funcs, methods, vars, consts, and types
	// by name. Prior behaviour (regex-only = free funcs only) forced
	// callers to issue N invocations for a mixed split; the new
	// semantic collapses it to one.
	_, f := mustParse(t, `package p

func FilterFunc() {}
func other() {}
func (r *R) FilterMethod() {} // method — now matches regex-only
type R struct{}

var FilterVar = 1
const FilterConst = 2
type FilterType struct{}
`)
	cfg := Config{Regex: "^Filter"}
	ms, err := selectDecls(f, cfg)
	if err != nil {
		t.Fatal(err)
	}
	got := matchNames(ms)
	want := map[string]bool{
		"FilterFunc":        true,
		"FilterMethod":      true,
		"var FilterVar":     true,
		"const FilterConst": true,
		"type FilterType":   true,
	}
	if len(got) != len(want) {
		t.Fatalf("got %v want keys %v", got, want)
	}
	for _, g := range got {
		if !want[g] {
			t.Fatalf("unexpected %q in %v", g, got)
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

func TestSelectRegexOnly_InitCopyRejected(t *testing.T) {
	// Copying init into a same-package sink duplicates it: Go allows
	// multiple init funcs, so it compiles but runs init twice.
	_, f := mustParse(t, "package p\nfunc init(){}\n")
	_, err := selectDecls(f, Config{Regex: "^init$"})
	if err == nil || !strings.Contains(err.Error(), "cannot split init function") {
		t.Fatalf("got err %v, want cannot split init function", err)
	}
}

func TestSelectRegexOnly_InitMoveRejected(t *testing.T) {
	_, f := mustParse(t, "package p\nfunc init(){}\n")
	_, err := selectDecls(f, Config{Regex: "^init$", Move: true})
	if err == nil || !strings.Contains(err.Error(), "cannot split init function") {
		t.Fatalf("got err %v, want cannot split init function", err)
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
	// Also verify Helper still exists in the original file.Decls; selection
	// is read-only on the source.
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

func TestSelectValueSpecs_AllowsPartialExplicitConstBlock(t *testing.T) {
	_, f := mustParse(t, `package p

const (
	A = "x"
	B = "y"
	C = "z"
)
`)
	matches, err := selectDecls(f, Config{Regex: "^B$", Move: true})
	if err != nil {
		t.Fatalf("selectDecls returned error: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("len(matches) = %d, want 1", len(matches))
	}
	if got := declKeys(matches[0].Decl); len(got) != 1 || got[0] != "const B" {
		t.Fatalf("declKeys = %v, want [const B]", got)
	}
}

func TestSelectValueSpecs_RejectsPartialIotaExpressionConstBlock(t *testing.T) {
	_, f := mustParse(t, `package p

const (
	A = 1 << iota
	B
)
`)
	_, err := selectDecls(f, Config{Regex: "^A$", Move: true})
	if err == nil || !strings.Contains(err.Error(), "cannot partially split iota const block") {
		t.Fatalf("got err %v, want iota const partial split rejection", err)
	}
}

func TestSelectValueSpecs_RejectsPartialIotaConstCopy(t *testing.T) {
	// Copy mode used to bypass the guard and emit `const B` — invalid Go.
	_, f := mustParse(t, `package p

const (
	A = iota
	B
	C
)
`)
	_, err := selectDecls(f, Config{Regex: "^B$"})
	if err == nil || !strings.Contains(err.Error(), "cannot partially split iota const block") {
		t.Fatalf("got err %v, want iota const partial split rejection", err)
	}
}

func TestSelectValueSpecs_AllowsWholeIotaExpressionConstBlock(t *testing.T) {
	_, f := mustParse(t, `package p

const (
	A = 1 << iota
	B
)
`)
	matches, err := selectDecls(f, Config{Regex: "^(A|B)$", Move: true})
	if err != nil {
		t.Fatalf("selectDecls returned error: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("len(matches) = %d, want 1", len(matches))
	}
	if matches[0].Synthetic {
		t.Fatal("whole iota block move returned synthetic match")
	}
	if got := declKeys(matches[0].Decl); strings.Join(got, ",") != "const A,const B" {
		t.Fatalf("declKeys = %v, want [const A const B]", got)
	}
}

func TestSelectValueSpecs_RejectsPartialMultiNameConstWithoutOneToOneValues(t *testing.T) {
	_, f := mustParse(t, `package p

const a, b = 1
const x, y = "same", "same"
`)
	// The guard protects the sink's validity, so it applies in copy mode too.
	_, err := selectDecls(f, Config{Regex: "^a$"})
	if err == nil || !strings.Contains(err.Error(), "cannot partially split multi-name value spec") {
		t.Fatalf("want multi-name rejection in copy mode, got %v", err)
	}

	_, f = mustParse(t, `package p

const a, b = 1
const x, y = "same", "same"
`)
	_, err = selectDecls(f, Config{Regex: "^a$", Move: true})
	if err == nil || !strings.Contains(err.Error(), "cannot partially split multi-name value spec") {
		t.Fatalf("want multi-name rejection, got %v", err)
	}
}

func TestSelectValueSpecs_RejectsPartialMultiNameImplicitConstBlock(t *testing.T) {
	_, f := mustParse(t, `package p

const (
	A, B = 1, 2
	C, D
)
`)
	_, err := selectDecls(f, Config{Regex: "^(A|C|D)$", Move: true})
	if err == nil ||
		!strings.Contains(err.Error(), "cannot partially split const block with implicit expressions") {
		t.Fatalf("got err %v, want implicit const partial split rejection", err)
	}
}

func TestSelectValueSpecs_RejectsPartialImplicitConstCopy(t *testing.T) {
	// Copy mode used to bypass the guard and emit `const B` — invalid Go.
	_, f := mustParse(t, `package p

const (
	A = "x"
	B
	C
)
`)
	_, err := selectDecls(f, Config{Regex: "^B$"})
	if err == nil ||
		!strings.Contains(err.Error(), "cannot partially split const block with implicit expressions") {
		t.Fatalf("got err %v, want implicit const partial split rejection", err)
	}
}

// Selection must be pure: it describes what to take without touching the
// source AST, in both copy and move mode. The move-time splice happens in
// Plan.applyMove, after validation.
func TestSelectDecls_PureOnPartialTypeGroup(t *testing.T) {
	for _, move := range []bool{false, true} {
		_, f := mustParse(t, `package p

type (
	Helper struct{}
	Target struct{}
)
`)
		ms, err := selectDecls(f, Config{Regex: "^Target$", Move: move})
		if err != nil {
			t.Fatal(err)
		}
		if len(ms) != 1 || !ms[0].Synthetic || ms[0].Origin == nil {
			t.Fatalf("move=%v: want one synthetic match with origin, got %+v", move, ms)
		}
		gd := f.Decls[0].(*ast.GenDecl)
		if len(gd.Specs) != 2 {
			t.Fatalf("move=%v: selectDecls mutated source group: %d specs remain, want 2",
				move, len(gd.Specs))
		}
	}
}

func TestSelectDecls_PureOnPartialMultiNameValueSpec(t *testing.T) {
	_, f := mustParse(t, `package p

var a, b = 1, 2
`)
	ms, err := selectDecls(f, Config{Regex: "^a$", Move: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 || !ms[0].Synthetic || ms[0].Origin == nil {
		t.Fatalf("want one synthetic match with origin, got %+v", ms)
	}
	vs := f.Decls[0].(*ast.GenDecl).Specs[0].(*ast.ValueSpec)
	if len(vs.Names) != 2 || len(vs.Values) != 2 {
		t.Fatalf("selectDecls mutated source value spec: names=%d values=%d, want 2/2",
			len(vs.Names), len(vs.Values))
	}
}
