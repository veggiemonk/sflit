package splitter

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRoundTrip_MoveAndBack(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.go")
	b := filepath.Join(dir, "b.go")
	original := `package p

import "fmt"

func FilterA() { fmt.Println("a") }
func FilterB() { fmt.Println("b") }
func Other()   {}
`
	if err := os.WriteFile(a, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	// move FilterA,FilterB from a.go to b.go
	if _, err := Run(Config{Source: a, Sink: b, Regex: "^Filter", Move: true}); err != nil {
		t.Fatal(err)
	}
	// move them back from b.go to a.go
	if _, err := Run(Config{Source: b, Sink: a, Regex: "^Filter", Move: true}); err != nil {
		t.Fatal(err)
	}
	final, _ := os.ReadFile(filepath.Clean(a))
	if err := SemEqual([]string{original}, []string{string(final)}); err != nil {
		t.Fatalf("round-trip semantic mismatch: %v\nfinal:\n%s", err, final)
	}
}
