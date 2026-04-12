package splitter

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseSource(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.go")
	if err := os.WriteFile(filepath.Clean(p), []byte("package foo\nfunc Bar() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fset, file, err := parseGoFile(p)
	if err != nil {
		t.Fatalf("parseGoFile: %v", err)
	}
	if fset == nil || file == nil {
		t.Fatal("nil result")
	}
	if file.Name.Name != "foo" {
		t.Fatalf("pkg: %q", file.Name.Name)
	}
}

func TestParseSourceMissing(t *testing.T) {
	if _, _, err := parseGoFile("/nope/does/not/exist.go"); err == nil {
		t.Fatal("want error, got nil")
	}
}

func TestParseSourceSyntaxError(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bad.go")
	if err := os.WriteFile(filepath.Clean(p), []byte("package foo\nfunc Bar( {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := parseGoFile(p); err == nil {
		t.Fatal("want parse error")
	}
}
