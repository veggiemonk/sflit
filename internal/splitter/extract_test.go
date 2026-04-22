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

func TestExtract_TrailingCommentTravels(t *testing.T) {
	fset, f := mustParse(t, `package p

type App struct{}

func (a App) M() {}

// trailing orphan
`)
	ms, _ := selectDecls(f, Config{Receiver: "App"})
	ex := extractMatches(fset, f, ms)
	// The last matched decl (M) should carry the trailing orphan comment.
	var tailLead []*ast.CommentGroup
	for _, e := range ex {
		if fd, ok := e.Decl.(*ast.FuncDecl); ok && fd.Name.Name == "M" {
			tailLead = e.LeadComms
		}
	}
	found := false
	for _, cg := range tailLead {
		if strings.Contains(cg.Text(), "trailing orphan") {
			found = true
		}
	}
	if !found {
		t.Fatalf("trailing orphan comment not attached to last matched decl")
	}
	for _, cg := range f.Comments {
		if strings.Contains(cg.Text(), "trailing orphan") {
			t.Fatalf("trailing orphan still present in source file.Comments")
		}
	}
}

func TestExtract_TrailingCommentStaysWhenTailNotMatched(t *testing.T) {
	fset, f := mustParse(t, `package p

type App struct{}

func (a App) M() {}

func Keep() {}

// comment trailing Keep
`)
	ms, _ := selectDecls(f, Config{Receiver: "App"})
	ex := extractMatches(fset, f, ms)
	// Keep is unmoved and is the last decl. The trailing comment must NOT
	// travel — it belongs to the unmoved tail.
	for _, e := range ex {
		for _, cg := range e.LeadComms {
			if strings.Contains(cg.Text(), "trailing Keep") {
				t.Fatalf("trailing comment after unmoved Keep was yanked")
			}
		}
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
