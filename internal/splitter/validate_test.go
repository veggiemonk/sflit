package splitter

import (
	"go/ast"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestValidate_EmptySelection(t *testing.T) {
	fset, src := mustParse(t, "package p\nfunc Foo(){}\n")
	plan := buildPlan(fset, nil, "src.go", "sink.go", src, nil, nil, false)
	if err := validatePlan(
		plan,
		nil,
		src,
	); err == nil ||
		!strings.Contains(err.Error(), "no declarations matched in src.go") {
		t.Fatalf("want empty-selection err, got %v", err)
	}
}

func TestValidate_PackageMismatch(t *testing.T) {
	fset, src := mustParse(t, "package p\nfunc Foo(){}\n")
	_, sink := mustParse(t, "package q\nfunc Bar(){}\n")
	ms, _ := selectDecls(src, Config{Regex: "^Foo"})
	ex := extractMatches(fset, src, ms)
	plan := buildPlan(fset, nil, "src.go", "sink.go", src, sink, ex, false)
	if err := validatePlan(plan, sink, src); err == nil || !strings.Contains(err.Error(), "package") {
		t.Fatalf("want package mismatch err, got %v", err)
	}
}

func TestValidate_Collision(t *testing.T) {
	fset, src := mustParse(t, "package p\nfunc Foo(){}\n")
	_, sink := mustParse(t, "package p\nfunc Foo(){}\n")
	ms, _ := selectDecls(src, Config{Regex: "^Foo"})
	ex := extractMatches(fset, src, ms)
	plan := buildPlan(fset, nil, "src.go", "sub/sink.go", src, sink, ex, false)
	if err := validatePlan(
		plan,
		sink,
		src,
	); err == nil ||
		!strings.Contains(err.Error(), "declaration Foo already exists in sink") {
		t.Fatalf("want collision err, got %v", err)
	}
}

func TestValidate_BlankIdentifierDoesNotCollide(t *testing.T) {
	fset, src := mustParse(t, "package p\nvar _ interface{} = nil\n")
	_, sink := mustParse(t, "package p\nvar _ interface{} = nil\n")
	ms, _ := selectDecls(src, Config{Regex: "^_$"})
	ex := extractMatches(fset, src, ms)
	plan := buildPlan(fset, nil, "src.go", "sub/sink.go", src, sink, ex, false)
	if err := validatePlan(plan, sink, src); err != nil {
		t.Fatalf("blank identifiers should not collide, got %v", err)
	}
}

func TestCollisionKeys_ParenthesizedReceiver(t *testing.T) {
	// Legal but rare: a parenthesized receiver type must still produce a
	// collision key, or duplicate methods slip past validatePlan.
	_, f := mustParse(t, "package p\nfunc (r (*T)) M() {}\n")
	got := collisionKeys(f.Decls[0])
	want := []string{"T.M"}
	if !slices.Equal(got, want) {
		t.Fatalf("collisionKeys: got %v want %v", got, want)
	}
}

func TestValidate_CollisionUsesGoPackageNamespace(t *testing.T) {
	tests := []struct {
		name string
		src  string
		sink string
	}{
		{name: "func collides with var", src: "package p\nfunc Foo(){}\n", sink: "package p\nvar Foo = 1\n"},
		{
			name: "func collides with const",
			src:  "package p\nfunc Foo(){}\n",
			sink: "package p\nconst Foo = 1\n",
		},
		{
			name: "func collides with type",
			src:  "package p\nfunc Foo(){}\n",
			sink: "package p\ntype Foo struct{}\n",
		},
		{
			name: "type collides with func",
			src:  "package p\ntype Foo struct{}\n",
			sink: "package p\nfunc Foo(){}\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fset, src := mustParse(t, tt.src)
			_, sink := mustParse(t, tt.sink)
			ms, _ := selectDecls(src, Config{Regex: "^Foo"})
			ex := extractMatches(fset, src, ms)
			plan := buildPlan(fset, nil, "src.go", "sub/sink.go", src, sink, ex, false)
			if err := validatePlan(plan, sink, src); err == nil ||
				!strings.Contains(err.Error(), "declaration Foo already exists in sink") {
				t.Fatalf("want package namespace collision err, got %v", err)
			}
		})
	}
}

func TestSelectionSummary(t *testing.T) {
	tests := []struct {
		name string
		want string
		cfg  Config
	}{
		{name: "regex", cfg: Config{Regex: "Nope"}, want: `-regex "Nope"`},
		{name: "receiver", cfg: Config{Receiver: "App"}, want: `-receiver "App"`},
		{
			name: "receiver regex",
			cfg:  Config{Receiver: "App", Regex: "^Init$"},
			want: `-receiver "App" -regex "^Init$"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := selectionSummary(tt.cfg); got != tt.want {
				t.Fatalf("selectionSummary() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidate_OK(t *testing.T) {
	fset, src := mustParse(t, "package p\nfunc Foo(){}\n")
	_, sink := mustParse(t, "package p\nfunc Bar(){}\n")
	ms, _ := selectDecls(src, Config{Regex: "^Foo"})
	ex := extractMatches(fset, src, ms)
	plan := buildPlan(fset, nil, "src.go", "sub/sink.go", src, sink, ex, false)
	if err := validatePlan(plan, sink, src); err != nil {
		t.Fatalf("want nil, got %v", err)
	}
}

func TestValidate_SameDirCopyRejected(t *testing.T) {
	fset, src := mustParse(t, "package p\nfunc Foo(){}\n")
	ms, _ := selectDecls(src, Config{Regex: "^Foo"})
	ex := extractMatches(fset, src, ms)
	plan := buildPlan(fset, nil, "src.go", "sink.go", src, nil, ex, false)
	err := validatePlan(plan, nil, src)
	if err == nil {
		t.Fatal("want same-directory copy err, got nil")
	}
	for _, want := range []string{"cannot copy within the same directory", "-move"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q missing %q", err, want)
		}
	}
}

func TestValidate_SameDirCopyRejected_RelativeVsAbsoluteSpelling(t *testing.T) {
	fset, src := mustParse(t, "package p\nfunc Foo(){}\n")
	ms, _ := selectDecls(src, Config{Regex: "^Foo"})
	ex := extractMatches(fset, src, ms)
	abs, err := filepath.Abs("sink.go")
	if err != nil {
		t.Fatal(err)
	}
	plan := buildPlan(fset, nil, "./src.go", abs, src, nil, ex, false)
	if err := validatePlan(plan, nil, src); err == nil ||
		!strings.Contains(err.Error(), "cannot copy within the same directory") {
		t.Fatalf("want same-directory copy err for mixed path spellings, got %v", err)
	}
}

func TestValidate_SameDirMoveAllowed(t *testing.T) {
	fset, src := mustParse(t, "package p\nfunc Foo(){}\nfunc Bar(){}\n")
	ms, _ := selectDecls(src, Config{Regex: "^Foo"})
	ex := extractMatches(fset, src, ms)
	plan := buildPlan(fset, nil, "src.go", "sink.go", src, nil, ex, true)
	if err := validatePlan(plan, nil, src); err != nil {
		t.Fatalf("same-directory move should be allowed, got %v", err)
	}
}

func TestValidate_CrossDirCopyAllowed(t *testing.T) {
	fset, src := mustParse(t, "package p\nfunc Foo(){}\n")
	ms, _ := selectDecls(src, Config{Regex: "^Foo"})
	ex := extractMatches(fset, src, ms)
	plan := buildPlan(fset, nil, "src.go", "sub/sink.go", src, nil, ex, false)
	if err := validatePlan(plan, nil, src); err != nil {
		t.Fatalf("cross-directory copy should be allowed, got %v", err)
	}
}

// A validation failure must leave the parsed source AST exactly as parsed:
// no decl drops, no spec splices, no comment consumption. This pins the
// pipeline ordering (select/extract/buildPlan are read-only; mutation only
// happens in applyMove, which Run calls after validatePlan succeeds).
func TestValidate_FailureLeavesSourceUntouched(t *testing.T) {
	fset, src := mustParse(t, `package p

type (
	Helper struct{}
	Target struct{} // travels with Target
)
`)
	_, sink := mustParse(t, "package p\ntype Target struct{}\n")
	ms, err := selectDecls(src, Config{Regex: "^Target$", Move: true})
	if err != nil {
		t.Fatal(err)
	}
	ex := extractMatches(fset, src, ms)
	plan := buildPlan(fset, nil, "src.go", "sink.go", src, sink, ex, true)
	if err := validatePlan(plan, sink, src); err == nil ||
		!strings.Contains(err.Error(), "already exists in sink") {
		t.Fatalf("want collision err, got %v", err)
	}
	// Validation failed before applyMove: source must be pristine.
	gd := src.Decls[0].(*ast.GenDecl)
	if len(gd.Specs) != 2 {
		t.Fatalf("failed validation left source group spliced: %d specs", len(gd.Specs))
	}
	if len(src.Comments) != 1 {
		t.Fatalf("failed validation consumed source comments: %d groups remain, want 1", len(src.Comments))
	}
}
