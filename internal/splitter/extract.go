package splitter

import (
	"go/ast"
	"go/token"
)

// Extracted is one match staged for the sink: the decl plus every comment
// group that must travel with it (free-floating leads, attached docs, inline
// trailing comments, in-body free comments, and trailing-orphan comments
// when this is the last matched decl). Origin is carried over from the
// Match so Plan.applyMove can splice synthetic specs out of the source
// group on move.
type Extracted struct {
	Decl      ast.Decl
	Origin    *SpecOrigin
	LeadComms []*ast.CommentGroup
}

// extractMatches gathers, for each matched decl, every comment group that
// travels with it to the sink. For each non-synthetic match, the span
// (prevDeclEnd, decl.End()] is swept so leading free-floating comments,
// attached doc comments, in-body comments, and trailing inline comments
// all come along. A comment group starting on the same line as a decl's
// End() belongs to that decl: the sweep skips groups on the previous
// boundary's last line (they stay with the unmoved decl or package
// clause) and extends to groups on the matched decl's own last line.
// When the last decl(s) in the file are all matched, comments after the
// final unmoved decl (or package clause) travel with the last matched
// decl. For synthetic matches (specs split out of a partial GenDecl),
// spec-level Doc/Comment groups are captured directly from the spec
// nodes.
//
// extractMatches is read-only on file: captured comments are removed from
// file.Comments by Plan.applyMove (move only, after validation) so they
// never print twice from a moved-from source.
func extractMatches(fset *token.FileSet, file *ast.File, matches []Match) []Extracted {
	if len(matches) == 0 {
		return nil
	}
	matchSet := make(map[ast.Decl]bool, len(matches))
	consumed := make(map[*ast.CommentGroup]bool)

	syntheticLeadComms := make(map[ast.Decl][]*ast.CommentGroup)
	for _, m := range matches {
		if m.Synthetic {
			cs := commentsOfDecl(m.Decl)
			for _, cg := range cs {
				consumed[cg] = true
			}
			syntheticLeadComms[m.Decl] = cs
		} else {
			matchSet[m.Decl] = true
		}
	}

	prevEnd := make(map[ast.Decl]token.Pos, len(file.Decls))
	prev := file.Package
	for _, d := range file.Decls {
		prevEnd[d] = prev
		prev = d.End()
	}

	perDecl := make(map[ast.Decl][]*ast.CommentGroup, len(matchSet))
	for _, d := range file.Decls {
		if !matchSet[d] {
			continue
		}
		lower := prevEnd[d]
		upper := d.End()
		// PositionFor with adjusted=false: layout decisions are about bytes
		// on disk, and //line directives renumber the adjusted view, making
		// physically distinct lines compare equal.
		lowerLine := fset.PositionFor(lower, false).Line
		upperLine := fset.PositionFor(upper, false).Line
		var owned []*ast.CommentGroup
		for _, cg := range file.Comments {
			if consumed[cg] {
				continue
			}
			if cg.End() <= lower {
				continue
			}
			cgLine := fset.PositionFor(cg.Pos(), false).Line
			if cgLine == lowerLine {
				// Same-line trailing comment of the previous boundary
				// (unmoved decl or package clause): it stays behind.
				continue
			}
			if cg.End() <= upper || cgLine == upperLine {
				owned = append(owned, cg)
				consumed[cg] = true
			}
		}
		perDecl[d] = owned
	}

	if lastMatched, ok := lastMatchedTailDecl(file.Decls, matchSet); ok {
		trailStart := lastMatched.End()
		for _, cg := range file.Comments {
			if consumed[cg] {
				continue
			}
			if cg.Pos() > trailStart {
				perDecl[lastMatched] = append(perDecl[lastMatched], cg)
				consumed[cg] = true
			}
		}
	}

	out := make([]Extracted, 0, len(matches))
	for _, m := range matches {
		if m.Synthetic {
			out = append(out, Extracted{
				Decl:      m.Decl,
				LeadComms: syntheticLeadComms[m.Decl],
				Origin:    m.Origin,
			})
		}
	}
	for _, d := range file.Decls {
		if matchSet[d] {
			out = append(out, Extracted{Decl: d, LeadComms: perDecl[d]})
		}
	}
	return out
}

// commentsOfDecl gathers comment groups attached to a decl's AST nodes.
// Used for synthetic matches whose comments live in the source file's
// Comments slice — those cgs must travel to the sink and, on move, be
// removed from the source (Plan.applyMove) so they print exactly once.
func commentsOfDecl(d ast.Decl) []*ast.CommentGroup {
	var out []*ast.CommentGroup
	switch x := d.(type) {
	case *ast.FuncDecl:
		if x.Doc != nil {
			out = append(out, x.Doc)
		}
	case *ast.GenDecl:
		if x.Doc != nil {
			out = append(out, x.Doc)
		}
		for _, s := range x.Specs {
			switch ss := s.(type) {
			case *ast.ValueSpec:
				if ss.Doc != nil {
					out = append(out, ss.Doc)
				}
				if ss.Comment != nil {
					out = append(out, ss.Comment)
				}
			case *ast.TypeSpec:
				if ss.Doc != nil {
					out = append(out, ss.Doc)
				}
				if ss.Comment != nil {
					out = append(out, ss.Comment)
				}
			case *ast.ImportSpec:
				if ss.Doc != nil {
					out = append(out, ss.Doc)
				}
				if ss.Comment != nil {
					out = append(out, ss.Comment)
				}
			}
		}
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
