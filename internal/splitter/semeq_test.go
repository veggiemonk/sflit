package splitter

import "testing"

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
