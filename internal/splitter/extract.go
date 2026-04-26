package splitter

import (
	"go/ast"
	"go/token"
)

// Extracted is one match staged for the sink: the decl plus every comment
// group that must travel with it (free-floating leads, attached docs, inline
// trailing comments, in-body free comments, and trailing-orphan comments
// when this is the last matched decl).
type Extracted struct {
	Decl      ast.Decl
	LeadComms []*ast.CommentGroup
}

// extractMatches detaches matched decls from file.Decls and gathers every
// comment group that travels with them. For each non-synthetic match, the
// span (prevDeclEnd, decl.End()] is swept so leading free-floating
// comments, attached doc comments, in-body comments, and trailing inline
// comments all come along. When the last decl(s) in the file are all
// matched, comments after the final unmoved decl (or package clause)
// travel with the last matched decl. For synthetic matches (specs spliced
// out of a partial GenDecl), spec-level Doc/Comment groups are captured
// directly from the spec nodes. Captured comments are removed from
// file.Comments so they never print twice from the source.
func extractMatches(_ *token.FileSet, file *ast.File, matches []Match) []Extracted {
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
		var owned []*ast.CommentGroup
		for _, cg := range file.Comments {
			if consumed[cg] {
				continue
			}
			if cg.End() > lower && cg.End() <= upper {
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
			out = append(out, Extracted{Decl: m.Decl, LeadComms: syntheticLeadComms[m.Decl]})
		}
	}
	for _, d := range file.Decls {
		if matchSet[d] {
			out = append(out, Extracted{Decl: d, LeadComms: perDecl[d]})
		}
	}

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

// commentsOfDecl gathers comment groups attached to a decl's AST nodes.
// Used for synthetic matches whose comments live in the source file's
// Comments slice — those cgs must travel to the sink and be removed
// from the source so they print exactly once.
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
