package splitter

import (
	"go/ast"
	"go/token"
)

type Extracted struct {
	Decl      ast.Decl
	LeadComms []*ast.CommentGroup
}

// extractMatches detaches matched decls from file.Decls and gathers any
// leading free-floating comments (including //go: directives) that sit
// between the previous decl and each match. Leading comments are removed
// from file.Comments to prevent double-printing on the remaining source.
// Synthetic matches (constructed decls not in file.Decls) are passed
// through directly without comment extraction.
func extractMatches(_ *token.FileSet, file *ast.File, matches []Match) []Extracted {
	if len(matches) == 0 {
		return nil
	}
	matchSet := make(map[ast.Decl]bool, len(matches))
	// Collect synthetic matches upfront; they don't live in file.Decls.
	var out []Extracted
	for _, m := range matches {
		if m.Synthetic {
			out = append(out, Extracted{Decl: m.Decl})
		} else {
			matchSet[m.Decl] = true
		}
	}

	// Determine the "previous decl end" for each decl in order.
	prevEnd := make(map[ast.Decl]token.Pos, len(file.Decls))
	prev := file.Package // bound start at package decl
	for _, d := range file.Decls {
		prevEnd[d] = prev
		prev = d.End()
	}

	consumed := make(map[*ast.CommentGroup]bool)
	for _, d := range file.Decls {
		if !matchSet[d] {
			continue
		}
		lowerBound := prevEnd[d]
		upperBound := d.Pos()
		var lead []*ast.CommentGroup
		for _, cg := range file.Comments {
			if consumed[cg] {
				continue
			}
			if cg.End() <= upperBound && cg.Pos() > lowerBound {
				// Exclude groups stdlib already attached as Doc (they'll print with the decl).
				if fd, ok := d.(*ast.FuncDecl); ok && cg == fd.Doc {
					consumed[cg] = true
					continue
				}
				if gd, ok := d.(*ast.GenDecl); ok && cg == gd.Doc {
					consumed[cg] = true
					continue
				}
				lead = append(lead, cg)
				consumed[cg] = true
			}
		}
		out = append(out, Extracted{Decl: d, LeadComms: lead})
	}

	// Remove consumed comment groups from file.Comments.
	if len(consumed) > 0 {
		kept := file.Comments[:0]
		for _, cg := range file.Comments {
			if !consumed[cg] {
				kept = append(kept, cg)
			}
		}
		file.Comments = kept
	}
	return out
}

// removeDecls returns a slice with the given decls filtered out, preserving order.
func removeDecls(all []ast.Decl, drop map[ast.Decl]bool) []ast.Decl {
	out := make([]ast.Decl, 0, len(all))
	for _, d := range all {
		if !drop[d] {
			out = append(out, d)
		}
	}
	return out
}
