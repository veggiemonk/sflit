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
// between the previous decl and each match. When the last decl(s) in the
// file are all matched, trailing comments between the final unmoved decl
// (or package clause) and EOF travel with the last matched decl — this
// prevents orphan comment groups from being stranded in the source.
// Captured comments are removed from file.Comments to prevent
// double-printing on the remaining source. Synthetic matches (constructed
// decls not in file.Decls) are passed through directly without comment
// extraction.
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

	// Trailing-comment pass: if the last decl(s) in file.Decls are all
	// matched, unconsumed comments positioned after the final UNMOVED decl
	// would be orphaned in the source. Attach them to the last matched
	// decl so they travel together.
	if lastMatched, ok := lastMatchedTailDecl(file.Decls, matchSet); ok {
		trailStart := lastMatched.End()
		for i := range out {
			if out[i].Decl != lastMatched {
				continue
			}
			for _, cg := range file.Comments {
				if consumed[cg] {
					continue
				}
				if cg.Pos() > trailStart {
					out[i].LeadComms = append(out[i].LeadComms, cg)
					consumed[cg] = true
				}
			}
			break
		}
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

// lastMatchedTailDecl returns the last decl in decls whose suffix (from
// that decl to the end of the slice) is entirely matched. It reports
// false when the final decl is not matched — in that case, trailing
// comments logically belong to the unmoved tail and should stay.
func lastMatchedTailDecl(decls []ast.Decl, matchSet map[ast.Decl]bool) (ast.Decl, bool) {
	if len(decls) == 0 {
		return nil, false
	}
	last := decls[len(decls)-1]
	if !matchSet[last] {
		return nil, false
	}
	return last, true
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
