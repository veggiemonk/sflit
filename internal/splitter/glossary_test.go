package splitter

import (
	"os"
	"strings"
	"testing"
)

// TestGlossary_NoForbiddenPhrasing pins UBIQUITOUS_LANGUAGE.md's usage
// rules onto the user-facing surfaces: copy and move are the only
// operation verbs (never "split X into Y"), and a partially selected
// grouped block is narrowed, never "partially split". A new rejection
// message reintroducing the forbidden phrasing fails here.
func TestGlossary_NoForbiddenPhrasing(t *testing.T) {
	forbidden := []string{
		"cannot split",
		"partially split",
		"partial split",
		"Partial split",
	}
	for _, file := range []string{"select.go", "validate.go", "help.go", "schema.go", "splitter.go"} {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, phrase := range forbidden {
			if strings.Contains(string(data), phrase) {
				t.Errorf("%s uses forbidden glossary phrasing %q: operations are move/copy, partial group selection is narrowing", file, phrase)
			}
		}
	}
}
