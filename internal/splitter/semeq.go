package splitter

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"sort"
	"strings"
)

// SemEqual returns nil iff the "before" and "after" file sets declare the
// same package-level program: same package name, and the same multiset of
// top-level declarations compared by gofmt-normalized text — doc comments,
// signatures, bodies (including in-body comments), and trailing inline
// comments included.
//
// Comparison is per spec, not per decl group, so the splitter's legitimate
// transformations are equal: regrouping (`var (a; b)` vs two standalone
// decls), gofmt drift, and import churn (import decls are goimports-managed
// and excluded). Everything else — a decl duplicated across files, a changed
// body, a dropped comment — is a mismatch. Multiset semantics make the
// canonical move bug, a declaration landing in both files, an inequality
// even though the key set is unchanged.
//
// Each argument is a slice of Go source strings. Used by tests to assert
// that a split/move preserved semantics.
func SemEqual(before, after []string) error {
	bPkg, b, err := collectUnits(before)
	if err != nil {
		return fmt.Errorf("before: %w", err)
	}
	aPkg, a, err := collectUnits(after)
	if err != nil {
		return fmt.Errorf("after: %w", err)
	}

	var diffs []string
	if bPkg != aPkg {
		diffs = append(diffs, fmt.Sprintf("package name differs: before %q, after %q", bPkg, aPkg))
	}
	keys := make([]string, 0, len(b))
	for k := range b {
		keys = append(keys, k)
	}
	for k := range a {
		if _, ok := b[k]; !ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		bt, at := b[k], a[k]
		if len(bt) != len(at) {
			diffs = append(diffs, fmt.Sprintf("%s: declared %d time(s) before, %d time(s) after", k, len(bt), len(at)))
			continue
		}
		for i := range bt {
			if bt[i] != at[i] {
				diffs = append(diffs, fmt.Sprintf("%s: declaration text differs\n-- before --\n%s\n-- after --\n%s", k, bt[i], at[i]))
				break
			}
		}
	}
	if len(diffs) == 0 {
		return nil
	}
	return fmt.Errorf("semantic mismatch:\n%s", strings.Join(diffs, "\n"))
}

// collectUnits parses each source and returns the package name plus a
// multiset of normalized declaration texts keyed by declKeys-style name
// keys. Texts per key are sorted so multisets compare positionally.
func collectUnits(srcs []string) (string, map[string][]string, error) {
	units := make(map[string][]string)
	pkg := ""
	for i, s := range srcs {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, fmt.Sprintf("src%d.go", i), s, parser.ParseComments)
		if err != nil {
			return "", nil, err
		}
		switch {
		case pkg == "":
			pkg = f.Name.Name
		case f.Name.Name != pkg:
			return "", nil, fmt.Errorf("mixed package names %q and %q", pkg, f.Name.Name)
		}
		for _, d := range f.Decls {
			if err := addDeclUnits(units, fset, s, d); err != nil {
				return "", nil, err
			}
		}
	}
	for k := range units {
		sort.Strings(units[k])
	}
	return pkg, units, nil
}

func addDeclUnits(units map[string][]string, fset *token.FileSet, src string, d ast.Decl) error {
	switch x := d.(type) {
	case *ast.FuncDecl:
		keys := declKeys(x)
		if len(keys) == 0 {
			return nil // unresolvable receiver: key-less, same as declKeys
		}
		start := x.Pos()
		if x.Doc != nil {
			start = x.Doc.Pos()
		}
		text, err := normalize(sliceSrc(fset, src, start, x.End()))
		if err != nil {
			return fmt.Errorf("%s: %w", keys[0], err)
		}
		units[keys[0]] = append(units[keys[0]], text)
	case *ast.GenDecl:
		if x.Tok == token.IMPORT {
			return nil // goimports-managed; legitimately differs across a split
		}
		for _, s := range x.Specs {
			keys := specKeys(x.Tok, s)
			if len(keys) == 0 {
				continue
			}
			doc, comment := specComments(s)
			if doc == nil && !x.Lparen.IsValid() {
				doc = x.Doc // ungrouped decl: the doc comment sits on the GenDecl
			}
			end := s.End()
			if comment != nil && comment.End() > end {
				end = comment.End()
			}
			// Wrap the spec in its own group so grouped and standalone
			// spellings of the same spec normalize identically. The doc
			// comment is sliced separately: in the ungrouped spelling it
			// sits above the keyword, outside the spec's range.
			body := sliceSrc(fset, src, s.Pos(), end)
			if doc != nil {
				body = sliceSrc(fset, src, doc.Pos(), doc.End()) + "\n" + body
			}
			wrapped := x.Tok.String() + " (\n" + body + "\n)"
			text, err := normalize(wrapped)
			if err != nil {
				return fmt.Errorf("%s: %w", keys[0], err)
			}
			for _, k := range keys {
				units[k] = append(units[k], text)
			}
		}
	}
	return nil
}

func specComments(s ast.Spec) (doc, comment *ast.CommentGroup) {
	switch ss := s.(type) {
	case *ast.TypeSpec:
		return ss.Doc, ss.Comment
	case *ast.ValueSpec:
		return ss.Doc, ss.Comment
	}
	return nil, nil
}

func sliceSrc(fset *token.FileSet, src string, from, to token.Pos) string {
	return src[fset.Position(from).Offset:fset.Position(to).Offset]
}

func normalize(text string) (string, error) {
	out, err := format.Source([]byte(text))
	if err != nil {
		return "", fmt.Errorf("normalize declaration: %w", err)
	}
	return string(out), nil
}
