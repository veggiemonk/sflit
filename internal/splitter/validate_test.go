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
	if err := validatePlan(plan, sink, src); err == nil ||
		!strings.Contains(err.Error(), `sink sink.go has package "q", but source src.go has package "p"`) {
		t.Fatalf("want package mismatch err, got %v", err)
	}
}

func TestValidate_NewSinkRejectsBuildConstrainedSource(t *testing.T) {
	fset, src := mustParse(t, "//go:build linux\n\npackage p\nfunc Foo(){}\n")
	ms, _ := selectDecls(src, Config{Regex: "^Foo", Move: true})
	ex := extractMatches(fset, src, ms)
	plan := buildPlan(fset, nil, "src.go", "sub/sink.go", src, nil, ex, true)
	err := validatePlan(plan, nil, src)
	if err == nil {
		t.Fatal("want build constraint err, got nil")
	}
	want := "cannot move build-constrained declarations into sink with different build constraints: new sink without matching constraints for source src.go and sink sub/sink.go"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func TestValidate_ImportAliasCollision(t *testing.T) {
	fset, src := mustParse(t, "package p\n\nimport f \"fmt\"\n\nfunc Foo() { f.Println() }\n")
	sinkFset, sink := mustParse(t, "package p\n\nimport f \"flag\"\n\nfunc Bar() { f.Arg(0) }\n")
	ms, _ := selectDecls(src, Config{Regex: "^Foo"})
	ex := extractMatches(fset, src, ms)
	plan := buildPlan(fset, sinkFset, "src.go", filepath.Join("other", "sink.go"), src, sink, ex, true)
	err := validatePlan(plan, sink, src)
	if err == nil || !strings.Contains(err.Error(), "same alias") {
		t.Fatalf("want alias-collision err (sink output could not compile), got %v", err)
	}
	if !strings.Contains(err.Error(), `"fmt"`) || !strings.Contains(err.Error(), `"flag"`) {
		t.Fatalf("error should name both paths bound to the alias: %v", err)
	}
}

func TestValidate_ImportAliasSamePathOK(t *testing.T) {
	fset, src := mustParse(t, "package p\n\nimport f \"fmt\"\n\nfunc Foo() { f.Println() }\n")
	sinkFset, sink := mustParse(t, "package p\n\nimport f \"fmt\"\n\nfunc Bar() { f.Println() }\n")
	ms, _ := selectDecls(src, Config{Regex: "^Foo"})
	ex := extractMatches(fset, src, ms)
	plan := buildPlan(fset, sinkFset, "src.go", filepath.Join("other", "sink.go"), src, sink, ex, true)
	if err := validatePlan(plan, sink, src); err != nil {
		t.Fatalf("same alias for the same path is not a collision: %v", err)
	}
}

// TestValidate_StrandedRefs covers the cross-directory stranding hole: the
// sink is a different package instance, so a moved declaration referencing
// a name that stays behind — or, on move, a remaining declaration
// referencing a name that leaves — yields output that cannot compile.
func TestValidate_StrandedRefs(t *testing.T) {
	crossDirSink := filepath.Join("otherdir", "sink.go")
	cases := []struct {
		name    string
		src     string
		regex   string
		sink    string
		wantErr string // empty = valid
		move    bool
	}{
		{
			name:    "moved func references staying helper",
			src:     "package p\n\nfunc helper() int { return 1 }\n\nfunc Foo() int { return helper() }\n",
			regex:   "^Foo$",
			sink:    crossDirSink,
			move:    true,
			wantErr: "helper",
		},
		{
			name:    "copy also strands sink references",
			src:     "package p\n\nfunc helper() int { return 1 }\n\nfunc Foo() int { return helper() }\n",
			regex:   "^Foo$",
			sink:    crossDirSink,
			move:    false,
			wantErr: "helper",
		},
		{
			name:    "remaining func references moved decl",
			src:     "package p\n\nfunc Moved() int { return 1 }\n\nfunc stays() int { return Moved() }\n",
			regex:   "^Moved$",
			sink:    crossDirSink,
			move:    true,
			wantErr: "Moved",
		},
		{
			name:  "self-contained move is fine",
			src:   "package p\n\nfunc helper() int { return 1 }\n\nfunc Foo() int { return 2 }\n",
			regex: "^Foo$",
			sink:  crossDirSink,
			move:  true,
		},
		{
			name:  "same-directory move keeps the package together",
			src:   "package p\n\nfunc helper() int { return 1 }\n\nfunc Foo() int { return helper() }\n",
			regex: "^Foo$",
			sink:  "sink.go",
			move:  true,
		},
		{
			name:  "local shadowing a top-level name is not a reference",
			src:   "package p\n\nfunc helper() int { return 1 }\n\nfunc Foo() int { helper := 2; return helper }\n",
			regex: "^Foo$",
			sink:  crossDirSink,
			move:  true,
		},
		{
			name:  "references travelling together are fine",
			src:   "package p\n\nfunc FooHelper() int { return 1 }\n\nfunc Foo() int { return FooHelper() }\n",
			regex: "^Foo",
			sink:  crossDirSink,
			move:  true,
		},
		{
			name:    "moved var references staying const",
			src:     "package p\n\nconst base = 10\n\nvar Limit = base * 2\n",
			regex:   "^Limit$",
			sink:    crossDirSink,
			move:    true,
			wantErr: "base",
		},
		// The parser resolves composite-literal keys against file scope even
		// when they are struct field names (go.dev/issue/45160). Field keys
		// must not count as references…
		{
			name:  "struct literal field key is not a reference",
			src:   "package p\n\nfunc helper() int { return 1 }\n\ntype T struct{ helper int }\n\nfunc Foo() T { return T{helper: 1} }\n",
			regex: "^(Foo|T)$",
			sink:  crossDirSink,
			move:  true,
		},
		{
			name:  "nested elided struct literal key is not a reference",
			src:   "package p\n\nfunc helper() int { return 1 }\n\ntype T struct{ helper int }\n\nfunc Foo() []T { return []T{{helper: 1}} }\n",
			regex: "^(Foo|T)$",
			sink:  crossDirSink,
			move:  true,
		},
		{
			name:  "imported-type struct literal key is not a reference",
			src:   "package p\n\nimport \"example.com/q\"\n\nfunc helper() int { return 1 }\n\nfunc Foo() q.T { return q.T{helper: 1} }\n",
			regex: "^Foo$",
			sink:  crossDirSink,
			move:  true,
		},
		// …while map keys and array indices are expressions and must keep
		// counting as references.
		{
			name:    "map literal key referencing a staying const is a reference",
			src:     "package p\n\nconst key = \"k\"\n\nfunc Foo() map[string]int { return map[string]int{key: 1} }\n",
			regex:   "^Foo$",
			sink:    crossDirSink,
			move:    true,
			wantErr: "key",
		},
		{
			name:    "array index key referencing a staying const is a reference",
			src:     "package p\n\nconst idx = 0\n\nfunc Foo() [4]int { return [4]int{idx: 1} }\n",
			regex:   "^Foo$",
			sink:    crossDirSink,
			move:    true,
			wantErr: "idx",
		},
		{
			name:    "file-local named map type key is a reference",
			src:     "package p\n\nconst key = \"k\"\n\ntype M map[string]int\n\nfunc Foo() M { return M{key: 1} }\n",
			regex:   "^(Foo|M)$",
			sink:    crossDirSink,
			move:    true,
			wantErr: "key",
		},
		{
			name:    "instantiated generic local map type key is a reference",
			src:     "package p\n\nconst key = \"k\"\n\ntype M[K comparable, V any] map[K]V\n\nfunc Foo() M[string, int] { return M[string, int]{key: 1} }\n",
			regex:   "^(Foo|M)$",
			sink:    crossDirSink,
			move:    true,
			wantErr: "key",
		},
		{
			name:    "elided pointer-to-map element key is a reference",
			src:     "package p\n\nconst key = \"k\"\n\ntype M map[string]int\n\nfunc Foo() []*M { return []*M{{key: 1}} }\n",
			regex:   "^(Foo|M)$",
			sink:    crossDirSink,
			move:    true,
			wantErr: "key",
		},
		{
			name:  "instantiated generic struct literal key is not a reference",
			src:   "package p\n\nfunc helper() int { return 1 }\n\ntype S[T any] struct{ helper T }\n\nfunc Foo() S[int] { return S[int]{helper: 1} }\n",
			regex: "^(Foo|S)$",
			sink:  crossDirSink,
			move:  true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fset, src := mustParse(t, tc.src)
			ms, err := selectDecls(src, Config{Regex: tc.regex})
			if err != nil {
				t.Fatal(err)
			}
			ex := extractMatches(fset, src, ms)
			plan := buildPlan(fset, nil, "src.go", tc.sink, src, nil, ex, tc.move)
			err = validatePlan(plan, nil, src)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("want valid plan, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("want stranded-reference err naming %q, got %v", tc.wantErr, err)
			}
		})
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

// Sink-side mirrors of the source file-class guards: a generated sink must
// not be appended to, and a dot-import sink defeats parse-level collision
// detection (a moved Pi would collide with math's dot-imported Pi only at
// compile time).
func TestValidate_GeneratedSinkRejected(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.go")
	b := filepath.Join(dir, "b.go")
	writeFile(t, a, "package p\n\nfunc Foo() {}\n")
	writeFile(t, b, "// Code generated by stub. DO NOT EDIT.\n\npackage p\n\nfunc Existing() {}\n")
	_, err := Run(Config{Source: a, Sink: b, Regex: "^Foo$", Move: true})
	if err == nil || !strings.Contains(err.Error(), "generated") {
		t.Fatalf("want generated-sink rejection, got %v", err)
	}
}

func TestValidate_DotImportSinkRejected(t *testing.T) {
	fset, src := mustParse(t, "package p\n\nfunc Pi() float64 { return 3.14 }\n")
	sinkFset, sink := mustParse(t, "package p\n\nimport . \"math\"\n\nfunc Tau() float64 { return Pi * 2 }\n")
	ms, _ := selectDecls(src, Config{Regex: "^Pi$", Move: true})
	ex := extractMatches(fset, src, ms)
	plan := buildPlan(fset, sinkFset, "src.go", "sink.go", src, sink, ex, true)
	err := validatePlan(plan, sink, src)
	if err == nil || !strings.Contains(err.Error(), "dot import") {
		t.Fatalf("want sink dot-import rejection, got %v", err)
	}
}

func TestValidate_CgoSinkRejected(t *testing.T) {
	fset, src := mustParse(t, "package p\nfunc Foo() {}\n")
	sinkFset, sink := mustParse(t, "package p\nimport \"C\"\nfunc Bar() {}\n")
	ms, _ := selectDecls(src, Config{Regex: "^Foo"})
	ex := extractMatches(fset, src, ms)
	plan := buildPlan(fset, sinkFset, "src.go", "sink/sink.go", src, sink, ex, true)
	err := validatePlan(plan, sink, src)
	if err == nil || !strings.Contains(err.Error(), "cannot write to cgo file") {
		t.Fatalf("want cgo-sink rejection, got %v", err)
	}
}

func TestValidate_GeneratedMarkerPastLine20(t *testing.T) {
	// Marker after 20+ comment lines — old checker accepted, ast.IsGenerated should reject
	var b strings.Builder
	for range 25 {
		b.WriteString("// comment line\n")
	}
	b.WriteString("// Code generated by stub. DO NOT EDIT.\n\n")
	src := b.String() + "package p\n\nfunc Generated() {}\n"
	fset, file := mustParse(t, src)
	ms, _ := selectDecls(file, Config{Regex: "^Generated"})
	ex := extractMatches(fset, file, ms)
	plan := buildPlan(fset, nil, "src.go", "sub/sink.go", file, nil, ex, true)
	err := validatePlan(plan, nil, file)
	if err == nil || !strings.Contains(err.Error(), "generated") {
		t.Fatalf("want generated-file rejection for late marker, got %v", err)
	}
}

func TestValidate_GeneratedMidSentenceNotRejected(t *testing.T) {
	// Contains "Code generated" and "DO NOT EDIT" but not as anchored marker
	src := "// Note: Code generated tools must add DO NOT EDIT. markers themselves\n\npackage p\n\nfunc Foo() {}\n"
	fset, file := mustParse(t, src)
	ms, _ := selectDecls(file, Config{Regex: "^Foo"})
	ex := extractMatches(fset, file, ms)
	plan := buildPlan(fset, nil, "src.go", "sub/sink.go", file, nil, ex, true)
	if err := validatePlan(plan, nil, file); err != nil {
		t.Fatalf("mid-sentence mention should not trigger generated rejection, got %v", err)
	}
}

func TestValidate_ImportAlias_SourceAliasVsSinkDecl(t *testing.T) {
	// Direction A: source aliases "fmt" as f, sink declares var f = 1
	fset, src := mustParse(t, "package p\n\nimport f \"fmt\"\n\nfunc Foo() { f.Println() }\n")
	sinkFset, sink := mustParse(t, "package p\n\nvar f = 1\n\nfunc Bar() {}\n")
	ms, _ := selectDecls(src, Config{Regex: "^Foo"})
	ex := extractMatches(fset, src, ms)
	plan := buildPlan(fset, sinkFset, "src.go", filepath.Join("other", "sink.go"), src, sink, ex, true)
	err := validatePlan(plan, sink, src)
	if err == nil || !strings.Contains(err.Error(), "package level") {
		t.Fatalf("want alias-vs-decl collision err, got %v", err)
	}
}

func TestValidate_ImportAlias_MovedDeclVsSinkAlias(t *testing.T) {
	// Direction B: sink imports "fmt" as foo, moved decl is func foo()
	fset, src := mustParse(t, "package p\n\nfunc foo() {}\n\nfunc Foo() { foo() }\n")
	sinkFset, sink := mustParse(t, "package p\n\nimport foo \"fmt\"\n\nfunc Bar() { foo.Println() }\n")
	ms, _ := selectDecls(src, Config{Regex: "^foo$"})
	ex := extractMatches(fset, src, ms)
	plan := buildPlan(fset, sinkFset, "src.go", filepath.Join("other", "sink.go"), src, sink, ex, true)
	err := validatePlan(plan, sink, src)
	if err == nil || !strings.Contains(err.Error(), "shadow") {
		t.Fatalf("want moved-decl-vs-sink-alias err, got %v", err)
	}
}
