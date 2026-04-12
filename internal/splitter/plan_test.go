package splitter

import (
	"go/ast"
	"testing"
)

func declNames(f *ast.File) []string {
	var out []string
	for _, d := range f.Decls {
		switch x := d.(type) {
		case *ast.FuncDecl:
			out = append(out, x.Name.Name)
		case *ast.GenDecl:
			for _, s := range x.Specs {
				if ts, ok := s.(*ast.TypeSpec); ok {
					out = append(out, "type "+ts.Name.Name)
				}
			}
		}
	}
	return out
}

func TestBuildPlan_CopyNewSink(t *testing.T) {
	fset, src := mustParse(t, "package p\nfunc Foo(){}\nfunc Bar(){}\n")
	ms, _ := selectDecls(src, Config{Regex: "^Foo"})
	ex := extractMatches(fset, src, ms)
	plan := buildPlan(fset, "src.go", "sink.go", src, nil, ex, false)
	if !plan.SinkIsNew {
		t.Fatal("expected sink to be new")
	}
	if got := declNames(plan.SinkFile); len(got) != 1 || got[0] != "Foo" {
		t.Fatalf("sink decls = %v", got)
	}
	if got := declNames(plan.SrcFile); len(got) != 2 {
		t.Fatalf("copy must not modify src, got %v", got)
	}
}

func TestBuildPlan_MoveNewSink(t *testing.T) {
	fset, src := mustParse(t, "package p\nfunc Foo(){}\nfunc Bar(){}\n")
	ms, _ := selectDecls(src, Config{Regex: "^Foo"})
	ex := extractMatches(fset, src, ms)
	plan := buildPlan(fset, "src.go", "sink.go", src, nil, ex, true)
	if got := declNames(plan.SrcFile); len(got) != 1 || got[0] != "Bar" {
		t.Fatalf("src post-move = %v", got)
	}
	if got := declNames(plan.SinkFile); len(got) != 1 || got[0] != "Foo" {
		t.Fatalf("sink = %v", got)
	}
}

func TestBuildPlan_AppendToExistingSink(t *testing.T) {
	fset, src := mustParse(t, "package p\nfunc Foo(){}\n")
	_, sink := mustParse(t, "package p\nfunc Existing(){}\n")
	ms, _ := selectDecls(src, Config{Regex: "^Foo"})
	ex := extractMatches(fset, src, ms)
	plan := buildPlan(fset, "src.go", "sink.go", src, sink, ex, false)
	if plan.SinkIsNew {
		t.Fatal("sink should not be new")
	}
	got := declNames(plan.SinkFile)
	if len(got) != 2 || got[0] != "Existing" || got[1] != "Foo" {
		t.Fatalf("sink decls = %v", got)
	}
}
