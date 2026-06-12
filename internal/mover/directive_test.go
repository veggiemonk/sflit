package mover

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Directive policy: declarations carrying //go:embed or //go:linkname are
// file- and directory-sensitive. Cross-directory moves are rejected
// (embed patterns are directory-relative, linkname binds package symbols);
// same-directory moves carry the directive's required blank import into
// the sink so the written file compiles.

func TestValidate_EmbedCrossDirRejected(t *testing.T) {
	fset, src := mustParse(t, `package p

import _ "embed"

//go:embed data.txt
var Data string
`)
	ms, _ := selectDecls(src, Config{Regex: "^Data$", Move: true})
	ex := extractMatches(fset, src, ms)
	op := buildRenderOp(fset, nil, "src.go", filepath.Join("otherdir", "sink.go"), src, nil, ex, true)
	err := validateRenderOp(op, nil, src)
	if err == nil || !strings.Contains(err.Error(), "go:embed") {
		t.Fatalf(
			"want //go:embed cross-directory rejection (embed patterns are directory-relative), got %v",
			err,
		)
	}
}

func TestValidate_LinknameCrossDirRejected(t *testing.T) {
	fset, src := mustParse(t, `package p

import _ "unsafe"

//go:linkname now runtime.nanotime
func now() int64
`)
	ms, _ := selectDecls(src, Config{Regex: "^now$", Move: true})
	ex := extractMatches(fset, src, ms)
	op := buildRenderOp(fset, nil, "src.go", filepath.Join("otherdir", "sink.go"), src, nil, ex, true)
	err := validateRenderOp(op, nil, src)
	if err == nil || !strings.Contains(err.Error(), "go:linkname") {
		t.Fatalf("want //go:linkname cross-directory rejection, got %v", err)
	}
}

func TestMove_EmbedSameDirCarriesBlankImport(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.go")
	b := filepath.Join(dir, "b.go")
	writeFile(t, filepath.Join(dir, "data.txt"), "hello")
	writeFile(t, a, `package p

import _ "embed"

//go:embed data.txt
var Data string

func Other() {}
`)
	if _, err := Run(Config{Source: a, Sink: b, Regex: "^Data$", Move: true}); err != nil {
		t.Fatal(err)
	}
	sink, err := os.ReadFile(b)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(sink), `_ "embed"`) {
		t.Fatalf("sink missing required blank import _ \"embed\":\n%s", sink)
	}
	if !strings.Contains(string(sink), "//go:embed data.txt") {
		t.Fatalf("//go:embed directive did not travel:\n%s", sink)
	}
	if err := TypeCheckFiles(a, b); err != nil {
		t.Fatal(err)
	}
}

func TestMove_LinknameSameDirCarriesBlankImport(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.go")
	b := filepath.Join(dir, "b.go")
	writeFile(t, a, `package p

import _ "unsafe"

//go:linkname now runtime.nanotime
func now() int64

func Other() int64 { return now() }
`)
	if _, err := Run(Config{Source: a, Sink: b, Regex: "^now$", Move: true}); err != nil {
		t.Fatal(err)
	}
	sink, err := os.ReadFile(b)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(sink), `_ "unsafe"`) {
		t.Fatalf("sink missing required blank import _ \"unsafe\":\n%s", sink)
	}
	if !strings.Contains(string(sink), "//go:linkname now runtime.nanotime") {
		t.Fatalf("//go:linkname directive did not travel:\n%s", sink)
	}
	if err := TypeCheckFiles(a, b); err != nil {
		t.Fatal(err)
	}
}
