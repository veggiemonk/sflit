package splitter

import (
	"crypto/sha256"
	"go/ast"
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
	fset, file, _, err := parseGoFile(p)
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
	if _, _, _, err := parseGoFile("/nope/does/not/exist.go"); err == nil {
		t.Fatal("want error, got nil")
	}
}

func TestParseSourceSyntaxError(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bad.go")
	if err := os.WriteFile(filepath.Clean(p), []byte("package foo\nfunc Bar( {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := parseGoFile(p); err == nil {
		t.Fatal("want parse error")
	}
}

func TestParseSnapshotHash(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.go")
	content := []byte("package foo\nfunc Bar() {}\n")
	if err := os.WriteFile(filepath.Clean(p), content, 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, snap, err := parseGoFile(p)
	if err != nil {
		t.Fatalf("parseGoFile: %v", err)
	}
	if !snap.exists {
		t.Fatal("snapshot of existing file must have exists=true")
	}
	if snap.path != p {
		t.Fatalf("path: got %q want %q", snap.path, p)
	}
	if want := sha256.Sum256(content); snap.sum != want {
		t.Fatalf("sum mismatch: got %x want %x", snap.sum, want)
	}
}

func TestParseSnapshotMissingSink(t *testing.T) {
	p := filepath.Join(t.TempDir(), "absent.go")
	fset, file, snap, err := parseGoFileIfExists(p)
	if err != nil {
		t.Fatalf("parseGoFileIfExists: %v", err)
	}
	if fset != nil || file != nil {
		t.Fatal("missing file must yield nil ASTs")
	}
	if snap.exists {
		t.Fatal("snapshot of missing file must have exists=false")
	}
	if snap.path != p {
		t.Fatalf("path: got %q want %q", snap.path, p)
	}
}

func TestParse_ObjectResolutionEnabled(t *testing.T) {
	// validateNoStrandedRefs depends on id.Obj != nil for package-level references.
	// This test guards against parser.SkipObjectResolution being added to parseGoFile.
	src := "package p\n\nfunc helper() {}\n\nfunc Foo() { helper() }\n"
	dir := t.TempDir()
	path := filepath.Join(dir, "p.go")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	_, file, _, err := parseGoFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Find the call to helper() inside Foo and check Obj is set.
	fn := file.Decls[1].(*ast.FuncDecl) // Foo
	call := fn.Body.List[0].(*ast.ExprStmt).X.(*ast.CallExpr)
	id := call.Fun.(*ast.Ident)
	if id.Obj == nil {
		t.Fatal(
			"parseGoFile returned file with Obj=nil for local reference;" +
				" validateNoStrandedRefs would silently miss it" +
				" — do not add parser.SkipObjectResolution",
		)
	}
}
