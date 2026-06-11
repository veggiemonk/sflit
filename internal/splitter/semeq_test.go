package splitter

import (
	"strings"
	"testing"
)

func TestSemEqual_SameDeclSet(t *testing.T) {
	a := []string{"package p\nfunc Foo(){}\nfunc Bar(){}\n"}
	b := []string{"package p\nfunc Foo(){}\n", "package p\nfunc Bar(){}\n"}
	if err := SemEqual(a, b); err != nil {
		t.Fatalf("want equal, got %v", err)
	}
}

func TestSemEqual_DiffDeclSet(t *testing.T) {
	a := []string{"package p\nfunc Foo(){}\n"}
	b := []string{"package p\nfunc Bar(){}\n"}
	if err := SemEqual(a, b); err == nil {
		t.Fatal("want inequality")
	}
}

func TestSemEqual_Methods(t *testing.T) {
	a := []string{"package p\ntype R struct{}\nfunc (r R) Foo() {}\n"}
	b := []string{"package p\ntype R struct{}\n", "package p\nfunc (r R) Foo() {}\n"}
	if err := SemEqual(a, b); err != nil {
		t.Fatalf("want equal, got %v", err)
	}
}

// A decl landing in BOTH files after a move is the canonical splitter bug.
// The multiset comparison must reject it even though the key set is equal.
func TestSemEqual_DuplicateDecl(t *testing.T) {
	a := []string{"package p\nfunc Foo(){}\nfunc Bar(){}\n"}
	b := []string{"package p\nfunc Foo(){}\nfunc Bar(){}\n", "package p\nfunc Foo(){}\n"}
	err := SemEqual(a, b)
	if err == nil {
		t.Fatal("want duplicate-decl inequality")
	}
	if !strings.Contains(err.Error(), "Foo") {
		t.Fatalf("error should name the duplicated decl, got: %v", err)
	}
}

func TestSemEqual_ChangedBody(t *testing.T) {
	a := []string{"package p\nfunc Foo() int { return 1 }\n"}
	b := []string{"package p\nfunc Foo() int { return 2 }\n"}
	err := SemEqual(a, b)
	if err == nil {
		t.Fatal("want changed-body inequality")
	}
	if !strings.Contains(err.Error(), "Foo") {
		t.Fatalf("error should name the changed decl, got: %v", err)
	}
}

func TestSemEqual_ChangedSignature(t *testing.T) {
	a := []string{"package p\nfunc Foo(n int) {}\n"}
	b := []string{"package p\nfunc Foo(n string) {}\n"}
	if err := SemEqual(a, b); err == nil {
		t.Fatal("want changed-signature inequality")
	}
}

func TestSemEqual_LostDocComment(t *testing.T) {
	a := []string{"package p\n\n// Foo does foo.\nfunc Foo() {}\n"}
	b := []string{"package p\n\nfunc Foo() {}\n"}
	err := SemEqual(a, b)
	if err == nil {
		t.Fatal("want lost-doc-comment inequality")
	}
	if !strings.Contains(err.Error(), "Foo") {
		t.Fatalf("error should name the decl, got: %v", err)
	}
}

func TestSemEqual_LostInBodyComment(t *testing.T) {
	a := []string{"package p\nfunc Foo() {\n\t// load-bearing note\n\t_ = 1\n}\n"}
	b := []string{"package p\nfunc Foo() {\n\t_ = 1\n}\n"}
	if err := SemEqual(a, b); err == nil {
		t.Fatal("want lost-in-body-comment inequality")
	}
}

func TestSemEqual_PackageRenamed(t *testing.T) {
	a := []string{"package p\nfunc Foo() {}\n"}
	b := []string{"package q\nfunc Foo() {}\n"}
	if err := SemEqual(a, b); err == nil {
		t.Fatal("want package-name inequality")
	}
}

// Splitting a grouped decl is the tool's job: specs travel intact between
// groups and standalone decls. Grouping changes must NOT be inequality.
func TestSemEqual_PartialGroupSplit(t *testing.T) {
	a := []string{"package p\n\nvar (\n\t// a is a.\n\ta = 1\n\tb = 2\n)\n"}
	b := []string{
		"package p\n\nvar b = 2\n",
		"package p\n\n// a is a.\nvar a = 1\n",
	}
	if err := SemEqual(a, b); err != nil {
		t.Fatalf("want equal across group split, got %v", err)
	}
}

func TestSemEqual_GroupedConstSplit(t *testing.T) {
	a := []string{"package p\n\nconst (\n\tA = \"a\" // inline a\n\tB = \"b\"\n)\n"}
	b := []string{
		"package p\n\nconst B = \"b\"\n",
		"package p\n\nconst A = \"a\" // inline a\n",
	}
	if err := SemEqual(a, b); err != nil {
		t.Fatalf("want equal across const group split, got %v", err)
	}
}

// gofmt normalization and import churn are legitimate render effects.
func TestSemEqual_FormatAndImportChurn(t *testing.T) {
	a := []string{"package p\n\nimport \"fmt\"\n\nfunc Foo()    {fmt.Println( \"x\" )}\n"}
	b := []string{
		"package p\n",
		"package p\n\nimport \"fmt\"\n\nfunc Foo() { fmt.Println(\"x\") }\n",
	}
	if err := SemEqual(a, b); err != nil {
		t.Fatalf("want equal under gofmt drift and import move, got %v", err)
	}
}

func TestSemEqual_ParseError(t *testing.T) {
	if err := SemEqual([]string{"package p\nfunc {"}, []string{"package p\n"}); err == nil ||
		!strings.Contains(err.Error(), "before") {
		t.Fatal("want before-side parse error")
	}
	if err := SemEqual([]string{"package p\n"}, []string{"package p\nfunc {"}); err == nil ||
		!strings.Contains(err.Error(), "after") {
		t.Fatal("want after-side parse error")
	}
}
