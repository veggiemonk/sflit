package splitter

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// TestConcurrentFanOut is ADR-0001's acceptance scenario: N concurrent
// -move invocations with disjoint regexes on one source, no external
// coordination. Every declaration must exist exactly once across the
// package afterwards — the silent-corruption failure mode was a lost
// update resurrecting moved declarations in the source.
func TestConcurrentFanOut(t *testing.T) {
	groups := []string{"Filter", "Render", "Store", "Parse", "Build", "Merge", "Split", "Clean"}
	dir := t.TempDir()
	src := filepath.Join(dir, "big.go")

	var sb strings.Builder
	sb.WriteString("package foo\n")
	want := map[string]int{"KeepA": 0, "KeepB": 0}
	for _, g := range groups {
		for _, s := range []string{"A", "B"} {
			fmt.Fprintf(&sb, "\nfunc %s%s() int { return 1 }\n", g, s)
			want[g+s] = 0
		}
	}
	sb.WriteString("\nfunc KeepA() int { return 1 }\n\nfunc KeepB() int { return 1 }\n")
	if err := os.WriteFile(src, []byte(sb.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for _, g := range groups {
		wg.Go(func() {
			_, err := Run(Config{
				Source: src,
				Sink:   filepath.Join(dir, strings.ToLower(g)+".go"),
				Regex:  "^" + g,
				Move:   true,
				// Worst case every commit invalidates all in-flight
				// runs, so rounds can reach len(groups); leave headroom
				// over the default bound of 5.
				Retries: 4 * len(groups),
			})
			if err != nil {
				t.Errorf("group %s: %v", g, err)
			}
		})
	}
	wg.Wait()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	fset := token.NewFileSet()
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, 0)
		if err != nil {
			t.Fatalf("package corrupted, %s does not parse: %v", e.Name(), err)
		}
		if file.Name.Name != "foo" {
			t.Errorf("%s: package %q", e.Name(), file.Name.Name)
		}
		for _, d := range file.Decls {
			for _, key := range declKeys(d) {
				got[key]++
			}
		}
	}
	for name, n := range got {
		if n != 1 {
			t.Errorf("declaration %s exists %d times, want exactly 1", name, n)
		}
	}
	for name := range want {
		if got[name] == 0 {
			t.Errorf("declaration %s lost", name)
		}
	}
	if len(got) != len(want) {
		t.Errorf("declaration count: got %d want %d (%v)", len(got), len(want), got)
	}
}
