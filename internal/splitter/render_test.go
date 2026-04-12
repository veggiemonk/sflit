package splitter

import (
	"strings"
	"testing"
)

func TestRender_AddsImportToNewSink(t *testing.T) {
	fset, src := mustParse(t, `package p

import "fmt"

func Foo() { fmt.Println("hi") }
func Bar() {}
`)
	ms, _ := selectDecls(src, Config{Regex: "^Foo"})
	ex := extractMatches(fset, src, ms)
	plan := buildPlan(fset, "src.go", "sink.go", src, nil, ex, false)
	srcBytes, sinkBytes, err := renderFiles(plan)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(string(sinkBytes), `"fmt"`) {
		t.Fatalf("sink missing fmt import:\n%s", sinkBytes)
	}
	if !strings.Contains(string(sinkBytes), "Foo") {
		t.Fatalf("sink missing Foo:\n%s", sinkBytes)
	}
	if !strings.Contains(string(srcBytes), "Foo") {
		t.Fatalf("copy must leave src with Foo:\n%s", srcBytes)
	}
}

func TestRender_RemovesUnusedImportOnMove(t *testing.T) {
	fset, src := mustParse(t, `package p

import "fmt"

func Foo() { fmt.Println("hi") }
func Bar() {}
`)
	ms, _ := selectDecls(src, Config{Regex: "^Foo"})
	ex := extractMatches(fset, src, ms)
	plan := buildPlan(fset, "src.go", "sink.go", src, nil, ex, true /*move*/)
	srcBytes, _, err := renderFiles(plan)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(string(srcBytes), `"fmt"`) {
		t.Fatalf("src still imports fmt after moving its only user:\n%s", srcBytes)
	}
	if strings.Contains(string(srcBytes), "Foo") {
		t.Fatalf("src still has Foo after move:\n%s", srcBytes)
	}
}
