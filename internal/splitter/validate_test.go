package splitter

import (
	"strings"
	"testing"
)

func TestValidate_EmptySelection(t *testing.T) {
	fset, src := mustParse(t, "package p\nfunc Foo(){}\n")
	plan := buildPlan(fset, "src.go", "sink.go", src, nil, nil, false)
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
	plan := buildPlan(fset, "src.go", "sink.go", src, sink, ex, false)
	if err := validatePlan(plan, sink, src); err == nil || !strings.Contains(err.Error(), "package") {
		t.Fatalf("want package mismatch err, got %v", err)
	}
}

func TestValidate_Collision(t *testing.T) {
	fset, src := mustParse(t, "package p\nfunc Foo(){}\n")
	_, sink := mustParse(t, "package p\nfunc Foo(){}\n")
	ms, _ := selectDecls(src, Config{Regex: "^Foo"})
	ex := extractMatches(fset, src, ms)
	plan := buildPlan(fset, "src.go", "sink.go", src, sink, ex, false)
	if err := validatePlan(
		plan,
		sink,
		src,
	); err == nil ||
		!strings.Contains(err.Error(), "declaration Foo already exists in sink") {
		t.Fatalf("want collision err, got %v", err)
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
	plan := buildPlan(fset, "src.go", "sink.go", src, sink, ex, false)
	if err := validatePlan(plan, sink, src); err != nil {
		t.Fatalf("want nil, got %v", err)
	}
}
