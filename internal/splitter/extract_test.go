package splitter

import (
	"go/ast"
	"strings"
	"testing"
)

func TestExtract_DocCommentAttached(t *testing.T) {
	fset, f := mustParse(t, `package p

// Foo does the thing.
func Foo() {}
`)
	ms, _ := selectDecls(f, Config{Regex: "^Foo"})
	ex := extractMatches(fset, f, ms)
	if len(ex) != 1 {
		t.Fatalf("want 1 extracted, got %d", len(ex))
	}
	fd := ex[0].Decl.(*ast.FuncDecl)
	if fd.Doc == nil || !strings.Contains(fd.Doc.Text(), "Foo does the thing") {
		t.Fatalf("doc comment not preserved: %+v", fd.Doc)
	}
}

func TestExtract_DirectiveAbove(t *testing.T) {
	fset, f := mustParse(t, `package p

//go:noinline
func Foo() {}
`)
	ms, _ := selectDecls(f, Config{Regex: "^Foo"})
	ex := extractMatches(fset, f, ms)
	if len(ex) != 1 {
		t.Fatalf("want 1, got %d", len(ex))
	}
	// The directive will attach as Doc since it sits immediately above.
	if ex[0].Decl.(*ast.FuncDecl).Doc == nil && len(ex[0].LeadComms) == 0 {
		t.Fatalf("//go:noinline directive lost")
	}
}

func TestExtract_FreeFloatingAbove(t *testing.T) {
	fset, f := mustParse(t, `package p

func Prev() {}

// Section header comment, not attached to Foo.

func Foo() {}
`)
	ms, _ := selectDecls(f, Config{Regex: "^Foo"})
	ex := extractMatches(fset, f, ms)
	if len(ex) != 1 {
		t.Fatalf("want 1, got %d", len(ex))
	}
	found := false
	for _, cg := range ex[0].LeadComms {
		if strings.Contains(cg.Text(), "Section header") {
			found = true
		}
	}
	if !found && ex[0].Decl.(*ast.FuncDecl).Doc == nil {
		t.Fatalf("free-floating comment above decl not captured")
	}
}
