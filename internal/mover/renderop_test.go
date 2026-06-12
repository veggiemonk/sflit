package mover

import (
	"go/ast"
	"go/token"
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

func TestBuildRenderOp_CopyNewSink(t *testing.T) {
	fset, src := mustParse(t, "package p\nfunc Foo(){}\nfunc Bar(){}\n")
	ms, _ := selectDecls(src, Config{Regex: "^Foo"})
	ex := extractMatches(fset, src, ms)
	op := buildRenderOp(fset, nil, "src.go", "sink.go", src, nil, ex, false)
	if !op.SinkIsNew {
		t.Fatal("expected sink to be new")
	}
	if got := declNames(op.SinkFile); len(got) != 1 || got[0] != "Foo" {
		t.Fatalf("sink decls = %v", got)
	}
	if got := declNames(op.SrcFile); len(got) != 2 {
		t.Fatalf("copy must not modify src, got %v", got)
	}
}

func TestBuildRenderOp_MoveNewSink(t *testing.T) {
	fset, src := mustParse(t, "package p\nfunc Foo(){}\nfunc Bar(){}\n")
	ms, _ := selectDecls(src, Config{Regex: "^Foo"})
	ex := extractMatches(fset, src, ms)
	op := buildRenderOp(fset, nil, "src.go", "sink.go", src, nil, ex, true)
	// buildRenderOp is read-only on the source; the splice happens in
	// applyMove, after validation.
	if got := declNames(op.SrcFile); len(got) != 2 {
		t.Fatalf("buildRenderOp mutated src before applyMove: %v", got)
	}
	op.applyMove()
	if got := declNames(op.SrcFile); len(got) != 1 || got[0] != "Bar" {
		t.Fatalf("src post-move = %v", got)
	}
	if got := declNames(op.SinkFile); len(got) != 1 || got[0] != "Foo" {
		t.Fatalf("sink = %v", got)
	}
}

func TestBuildRenderOp_AppendToExistingSink(t *testing.T) {
	fset, src := mustParse(t, "package p\nfunc Foo(){}\n")
	_, sink := mustParse(t, "package p\nfunc Existing(){}\n")
	ms, _ := selectDecls(src, Config{Regex: "^Foo"})
	ex := extractMatches(fset, src, ms)
	op := buildRenderOp(fset, nil, "src.go", "sink.go", src, sink, ex, false)
	if op.SinkIsNew {
		t.Fatal("sink should not be new")
	}
	got := declNames(op.SinkFile)
	if len(got) != 2 || got[0] != "Existing" || got[1] != "Foo" {
		t.Fatalf("sink decls = %v", got)
	}
}

// The subtle core of the split pipeline: on a partial group match the sink
// always receives the split-out synthetic specs (copy AND move), but the
// source AST handed to rendering loses its specs only on move, and only
// once applyMove commits the op after validation.
func TestApplyMove_PartialTypeGroup(t *testing.T) {
	const input = `package p

type (
	Helper struct{}
	Target struct{}
)
`
	t.Run("copy keeps source intact", func(t *testing.T) {
		fset, src := mustParse(t, input)
		ms, err := selectDecls(src, Config{Regex: "^Target$"})
		if err != nil {
			t.Fatal(err)
		}
		ex := extractMatches(fset, src, ms)
		op := buildRenderOp(fset, nil, "src.go", "sink.go", src, nil, ex, false)
		op.applyMove() // no-op on copy
		gd := op.SrcFile.Decls[0].(*ast.GenDecl)
		if len(gd.Specs) != 2 {
			t.Fatalf("copy spliced source group: %d specs remain, want 2", len(gd.Specs))
		}
		if got := declNames(op.SinkFile); len(got) != 1 || got[0] != "type Target" {
			t.Fatalf("sink decls = %v, want [type Target]", got)
		}
	})
	t.Run("move splices source at applyMove", func(t *testing.T) {
		fset, src := mustParse(t, input)
		ms, err := selectDecls(src, Config{Regex: "^Target$", Move: true})
		if err != nil {
			t.Fatal(err)
		}
		ex := extractMatches(fset, src, ms)
		op := buildRenderOp(fset, nil, "src.go", "sink.go", src, nil, ex, true)
		gd := op.SrcFile.Decls[0].(*ast.GenDecl)
		if len(gd.Specs) != 2 {
			t.Fatalf("source group spliced before applyMove: %d specs", len(gd.Specs))
		}
		op.applyMove()
		if len(gd.Specs) != 1 || gd.Specs[0].(*ast.TypeSpec).Name.Name != "Helper" {
			t.Fatalf("source group after move = %v, want only Helper", gd.Specs)
		}
		if got := declNames(op.SinkFile); len(got) != 1 || got[0] != "type Target" {
			t.Fatalf("sink decls = %v, want [type Target]", got)
		}
	})
}

func TestApplyMove_TrimsPartialMultiNameValueSpec(t *testing.T) {
	fset, src := mustParse(t, "package p\n\nvar a, b = 1, 2\n")
	ms, err := selectDecls(src, Config{Regex: "^a$", Move: true})
	if err != nil {
		t.Fatal(err)
	}
	ex := extractMatches(fset, src, ms)
	op := buildRenderOp(fset, nil, "src.go", "sink.go", src, nil, ex, true)
	op.applyMove()
	vs := op.SrcFile.Decls[0].(*ast.GenDecl).Specs[0].(*ast.ValueSpec)
	if len(vs.Names) != 1 || vs.Names[0].Name != "b" || len(vs.Values) != 1 {
		t.Fatalf("source spec after move: names=%v values=%d, want [b]/1", vs.Names, len(vs.Values))
	}
	// The synthetic for the sink still carries `a` with its value.
	syn := op.MovedFile.Decls[0].(*ast.GenDecl).Specs[0].(*ast.ValueSpec)
	if len(syn.Names) != 1 || syn.Names[0].Name != "a" || len(syn.Values) != 1 {
		t.Fatalf("sink spec = names %v values %d, want [a]/1", syn.Names, len(syn.Values))
	}
}

// TestBuildRenderOp_OrdersMovedDeclsBySourcePosition pins the ordering invariant
// behind doc-comment attachment: go/printer interleaves comments with nodes
// strictly by position, so a synthetic (group-narrowed) match originating
// later in the source than a non-synthetic match must not be emitted first.
func TestBuildRenderOp_OrdersMovedDeclsBySourcePosition(t *testing.T) {
	fset, src := mustParse(t, `package p

// DocA documents DocA.
func DocA() int { return 1 }

var (
	// travels documents travels.
	travels = 8
	stays   = 9
)
`)
	ms, _ := selectDecls(src, Config{Regex: "^(DocA|travels)$", Move: true})
	ex := extractMatches(fset, src, ms)
	op := buildRenderOp(fset, nil, "src.go", "sink.go", src, nil, ex, true)
	if len(op.MovedFile.Decls) != 2 {
		t.Fatalf("want 2 moved decls, got %d", len(op.MovedFile.Decls))
	}
	last := token.NoPos
	for i, d := range op.MovedFile.Decls {
		if d.Pos() < last {
			t.Fatalf(
				"MovedFile.Decls[%d] at %v precedes its predecessor at %v: decls out of source position order",
				i,
				d.Pos(),
				last,
			)
		}
		last = d.Pos()
	}
}
