package splitter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun_EndToEnd_CopyRegex(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "big.go")
	sink := filepath.Join(dir, "small.go")
	_ = os.WriteFile(src, []byte(`package p

import "fmt"

func FilterA() { fmt.Println("a") }
func Other() {}
`), 0o600)

	cfg := Config{Source: src, Sink: sink, Regex: "^Filter"}
	if _, err := Run(cfg); err != nil {
		t.Fatalf("Run: %v", err)
	}
	srcOut, _ := os.ReadFile(filepath.Clean(src))
	if !strings.Contains(string(srcOut), "FilterA") || !strings.Contains(string(srcOut), "Other") {
		t.Fatalf("src modified on copy:\n%s", srcOut)
	}
	sinkOut, _ := os.ReadFile(filepath.Clean(sink))
	if !strings.Contains(string(sinkOut), "FilterA") {
		t.Fatalf("sink missing FilterA:\n%s", sinkOut)
	}
	if !strings.Contains(string(sinkOut), "package p") {
		t.Fatalf("sink missing package clause:\n%s", sinkOut)
	}
}

func TestRun_EndToEnd_MoveRegex(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "big.go")
	sink := filepath.Join(dir, "small.go")
	_ = os.WriteFile(src, []byte(`package p

import "fmt"

func FilterA() { fmt.Println("a") }
func Other() {}
`), 0o600)

	cfg := Config{Source: src, Sink: sink, Regex: "^Filter", Move: true}
	if _, err := Run(cfg); err != nil {
		t.Fatalf("Run: %v", err)
	}
	srcOut, _ := os.ReadFile(filepath.Clean(src))
	if strings.Contains(string(srcOut), "FilterA") {
		t.Fatalf("src still has FilterA after move:\n%s", srcOut)
	}
	if strings.Contains(string(srcOut), `"fmt"`) {
		t.Fatalf("src still imports fmt:\n%s", srcOut)
	}
	sinkOut, _ := os.ReadFile(filepath.Clean(sink))
	if !strings.Contains(string(sinkOut), "FilterA") || !strings.Contains(string(sinkOut), `"fmt"`) {
		t.Fatalf("sink missing FilterA or fmt import:\n%s", sinkOut)
	}
}

func TestRun_ReturnsResult(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "big.go")
	sink := filepath.Join(dir, "small.go")
	_ = os.WriteFile(src, []byte(`package p

import "fmt"

func FilterA() { fmt.Println("a") }
func FilterB() { fmt.Println("b") }
func Other() {}
`), 0o600)

	cfg := Config{Source: src, Sink: sink, Regex: "^Filter", Move: true}
	res, err := Run(cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Source != src {
		t.Errorf("Source = %q, want %q", res.Source, src)
	}
	if res.Sink != sink {
		t.Errorf("Sink = %q, want %q", res.Sink, sink)
	}
	if !res.Move {
		t.Error("Move should be true")
	}
	if len(res.Matched) != 2 || res.Matched[0] != "FilterA" || res.Matched[1] != "FilterB" {
		t.Errorf("Matched = %v, want [FilterA FilterB]", res.Matched)
	}
	if res.DeclarationsRemaining != 1 {
		t.Errorf("DeclarationsRemaining = %d, want 1", res.DeclarationsRemaining)
	}
}

func TestRun_CollisionBail(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.go")
	sink := filepath.Join(dir, "b.go")
	_ = os.WriteFile(src, []byte("package p\nfunc Foo(){}\n"), 0o600)
	_ = os.WriteFile(sink, []byte("package p\nfunc Foo(){}\n"), 0o600)
	srcBefore, _ := os.ReadFile(filepath.Clean(src))
	sinkBefore, _ := os.ReadFile(filepath.Clean(sink))

	_, err := Run(Config{Source: src, Sink: sink, Regex: "^Foo", Move: true})
	if err == nil || !strings.Contains(err.Error(), "collision") {
		t.Fatalf("want collision err, got %v", err)
	}
	srcAfter, _ := os.ReadFile(filepath.Clean(src))
	sinkAfter, _ := os.ReadFile(filepath.Clean(sink))
	if string(srcBefore) != string(srcAfter) {
		t.Fatalf("src modified after bail")
	}
	if string(sinkBefore) != string(sinkAfter) {
		t.Fatalf("sink modified after bail")
	}
}
