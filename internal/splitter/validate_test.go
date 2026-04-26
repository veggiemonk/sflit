package splitter

import (
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
	plan := buildPlan(fset, nil, "src.go", "sink.go", src, sink, ex, false)
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
	plan := buildPlan(fset, nil, "src.go", "sink.go", src, sink, ex, false)
	if err := validatePlan(plan, sink, src); err != nil {
		t.Fatalf("blank identifiers should not collide, got %v", err)
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
			plan := buildPlan(fset, nil, "src.go", "sink.go", src, sink, ex, false)
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
	plan := buildPlan(fset, nil, "src.go", "sink.go", src, sink, ex, false)
	if err := validatePlan(plan, sink, src); err != nil {
		t.Fatalf("want nil, got %v", err)
	}
}
