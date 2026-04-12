package splitter

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWritePair_BothSucceed(t *testing.T) {
	dir := t.TempDir()
	s := filepath.Join(dir, "a.go")
	k := filepath.Join(dir, "b.go")
	if err := writePair(s, []byte("A"), k, []byte("B")); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Clean(s))
	if string(got) != "A" {
		t.Fatalf("src: %q", got)
	}
	got, _ = os.ReadFile(filepath.Clean(k))
	if string(got) != "B" {
		t.Fatalf("sink: %q", got)
	}
}

func TestWritePair_NewSink(t *testing.T) {
	dir := t.TempDir()
	s := filepath.Join(dir, "a.go")
	_ = os.WriteFile(s, []byte("old"), 0o600)
	k := filepath.Join(dir, "new.go") // does not exist
	if err := writePair(s, []byte("A"), k, []byte("B")); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Clean(k))
	if string(got) != "B" {
		t.Fatalf("sink: %q", got)
	}
}
