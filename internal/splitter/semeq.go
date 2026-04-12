package splitter

import (
	"fmt"
	"go/parser"
	"go/token"
	"sort"
)

// SemEqual returns nil iff the union of top-level declarations (by key)
// across the "before" and "after" file sets is identical. Each argument
// is a slice of Go source strings. Used by tests to assert that a
// split/move preserved semantics.
func SemEqual(before, after []string) error {
	b, err := collectKeys(before)
	if err != nil {
		return fmt.Errorf("before: %w", err)
	}
	a, err := collectKeys(after)
	if err != nil {
		return fmt.Errorf("after: %w", err)
	}
	if len(b) != len(a) {
		return fmt.Errorf("decl count differs: before=%d after=%d\nbefore=%v\nafter=%v", len(b), len(a), b, a)
	}
	for i := range b {
		if b[i] != a[i] {
			return fmt.Errorf("decl key differs at %d: %q vs %q\nbefore=%v\nafter=%v", i, b[i], a[i], b, a)
		}
	}
	return nil
}

func collectKeys(srcs []string) ([]string, error) {
	fset := token.NewFileSet()
	var keys []string
	for i, s := range srcs {
		f, err := parser.ParseFile(fset, fmt.Sprintf("src%d.go", i), s, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		for _, d := range f.Decls {
			keys = append(keys, declKeys(d)...)
		}
	}
	sort.Strings(keys)
	return dedup(keys), nil
}

func dedup(s []string) []string {
	if len(s) == 0 {
		return s
	}
	out := s[:1]
	for _, x := range s[1:] {
		if x != out[len(out)-1] {
			out = append(out, x)
		}
	}
	return out
}
