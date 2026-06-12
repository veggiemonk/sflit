package splitter

import (
	"go/ast"
	"go/token"
	"sort"
)

// Plan holds the inputs of one split operation, ready to render.
//
// SrcFile is the source AST exactly as parsed. buildPlan never mutates it;
// on move, matched decls/specs and travelling comments are spliced out by
// [Plan.applyMove], which Run calls only after validatePlan succeeds — a
// validation failure therefore leaves the parsed source untouched, and a
// copy never mutates the source at all. SrcFile is rendered through Fset.
//
// MovedFile is a synthetic *ast.File containing only the extracted decls
// and the comment groups travelling with them. It is rendered through Fset
// (matching the source). The package clause is stripped during rendering
// when the sink already exists, so the moved decls append cleanly after
// OrigSink's content.
//
// OrigSink is the sink AST as parsed from disk (nil when SinkIsNew). It
// keeps its own SinkFset because go/printer's line-gap heuristics are
// per-file: rendering OrigSink and MovedFile through different FileSets
// avoids the cross-file misordering that arises when both share one fset.
type Plan struct {
	Fset              *token.FileSet
	SinkFset          *token.FileSet
	SrcFile           *ast.File
	OrigSink          *ast.File
	MovedFile         *ast.File
	SinkFile          *ast.File
	SrcPath           string
	SinkPath          string
	Selection         string
	extracted         []Extracted
	OrigSinkDeclCount int
	SinkIsNew         bool
	Move              bool
}

func buildPlan(
	fset *token.FileSet,
	sinkFset *token.FileSet,
	srcPath, sinkPath string,
	src *ast.File,
	sink *ast.File, // may be nil when sink file does not yet exist
	extracted []Extracted,
	move bool,
) Plan {
	sinkIsNew := sink == nil

	moved := &ast.File{Name: ast.NewIdent(src.Name.Name)}
	for _, e := range extracted {
		moved.Decls = append(moved.Decls, e.Decl)
		if len(e.LeadComms) > 0 {
			moved.Comments = append(moved.Comments, e.LeadComms...)
		}
	}
	// go/printer interleaves comments with nodes strictly by position, so
	// decls must be in source position order like the comments below —
	// extraction emits synthetic (group-narrowed) matches first regardless
	// of where their group sits in the source. syntheticGenDecl anchors its
	// TokPos at spec.Pos()-1, so position sort is well-defined.
	if len(moved.Decls) > 1 {
		sort.SliceStable(moved.Decls, func(i, j int) bool {
			return moved.Decls[i].Pos() < moved.Decls[j].Pos()
		})
	}
	if len(moved.Comments) > 1 {
		sort.SliceStable(moved.Comments, func(i, j int) bool {
			return moved.Comments[i].Pos() < moved.Comments[j].Pos()
		})
	}

	// SinkFile is the merged view used only for validation (collision /
	// empty-selection checks). The renderer never consumes it.
	var sinkFile *ast.File
	origCount := 0
	if sinkIsNew {
		sinkFile = &ast.File{Name: ast.NewIdent(src.Name.Name)}
	} else {
		sinkFile = &ast.File{
			Name:  sink.Name,
			Decls: append([]ast.Decl(nil), sink.Decls...),
		}
		origCount = len(sink.Decls)
	}
	sinkFile.Decls = append(sinkFile.Decls, moved.Decls...)

	return Plan{
		Fset:              fset,
		SinkFset:          sinkFset,
		SrcPath:           srcPath,
		SinkPath:          sinkPath,
		SrcFile:           src,
		OrigSink:          sink,
		MovedFile:         moved,
		SinkFile:          sinkFile,
		SinkIsNew:         sinkIsNew,
		Move:              move,
		OrigSinkDeclCount: origCount,
		extracted:         extracted,
	}
}

// opVerb names the operation for user-facing messages: move or copy, the
// glossary's only operation verbs (UBIQUITOUS_LANGUAGE.md).
func (p Plan) opVerb() string {
	if p.Move {
		return "move"
	}
	return "copy"
}

// applyMove commits the move to the source AST. It is the only place in
// the pipeline that mutates the parsed source:
//
//   - whole matched decls are removed from SrcFile.Decls;
//   - synthetic matches are spliced out of their origin group: the spec is
//     dropped, or — for a partially matched multi-name ValueSpec — only the
//     matched names (and their 1:1 values) are trimmed;
//   - comment groups travelling to the sink are removed from
//     SrcFile.Comments so they never print twice.
//
// No-op on copy: the sink still receives the synthetic split-out specs,
// but the source AST handed to rendering keeps everything. Run calls
// applyMove after validatePlan, so a validation failure leaves the parsed
// source untouched.
func (p *Plan) applyMove() {
	if !p.Move {
		return
	}
	dropDecls := make(map[ast.Decl]bool, len(p.extracted))
	dropSpecs := make(map[ast.Spec]bool)
	dropNames := make(map[*ast.ValueSpec]map[int]bool)
	editedGroups := make(map[*ast.GenDecl]bool)
	consumed := make(map[*ast.CommentGroup]bool)
	for _, e := range p.extracted {
		for _, cg := range e.LeadComms {
			consumed[cg] = true
		}
		o := e.Origin
		if o == nil {
			dropDecls[e.Decl] = true
			continue
		}
		editedGroups[o.Decl] = true
		if o.Names == nil {
			dropSpecs[o.Spec] = true
			continue
		}
		vs, ok := o.Spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		if dropNames[vs] == nil {
			dropNames[vs] = make(map[int]bool, len(o.Names))
		}
		for _, j := range o.Names {
			dropNames[vs][j] = true
		}
	}

	for gd := range editedGroups {
		kept := gd.Specs[:0]
		for _, s := range gd.Specs {
			if dropSpecs[s] {
				continue
			}
			if vs, ok := s.(*ast.ValueSpec); ok && dropNames[vs] != nil {
				trimValueSpec(vs, dropNames[vs])
			}
			kept = append(kept, s)
		}
		gd.Specs = kept
		if len(gd.Specs) == 0 {
			// Defensive: selection emits a whole-decl match when every
			// spec matches, so a narrowed group always keeps >= 1 spec.
			dropDecls[gd] = true
		}
	}

	if len(dropDecls) > 0 {
		p.SrcFile.Decls = removeDecls(p.SrcFile.Decls, dropDecls)
	}
	if len(consumed) > 0 {
		kept := p.SrcFile.Comments[:0]
		for _, cg := range p.SrcFile.Comments {
			if !consumed[cg] {
				kept = append(kept, cg)
			}
		}
		p.SrcFile.Comments = kept
	}
}

// trimValueSpec removes the names at the given indices from vs, keeping
// values aligned 1:1 with surviving names. Selection guarantees the unsafe
// shared-value case (len(Values) != len(Names)) is rejected before a move
// reaches this point.
func trimValueSpec(vs *ast.ValueSpec, drop map[int]bool) {
	keptNames := vs.Names[:0]
	var keptValues []ast.Expr
	for j, n := range vs.Names {
		if drop[j] {
			continue
		}
		keptNames = append(keptNames, n)
		if j < len(vs.Values) {
			keptValues = append(keptValues, vs.Values[j])
		}
	}
	vs.Names = keptNames
	vs.Values = keptValues
}
