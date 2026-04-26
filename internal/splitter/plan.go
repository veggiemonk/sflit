package splitter

import (
	"go/ast"
	"go/token"
	"sort"
)

// Plan holds the immutable inputs of one split operation, ready to render.
//
// SrcFile is the source AST after matched decls have been spliced out (when
// Move is true). It is rendered through Fset.
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
	Fset      *token.FileSet
	SinkFset  *token.FileSet
	SrcFile   *ast.File
	OrigSink  *ast.File
	MovedFile *ast.File
	// SinkFile is the union of OrigSink's decls and MovedFile's decls,
	// used by validatePlan for collision checks. It is NOT used for
	// rendering — the renderer handles OrigSink and MovedFile separately.
	SinkFile          *ast.File
	SrcPath           string
	SinkPath          string
	Selection         string
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

	srcFile := src
	if move {
		drop := make(map[ast.Decl]bool, len(extracted))
		for _, e := range extracted {
			drop[e.Decl] = true
		}
		srcFile.Decls = removeDecls(srcFile.Decls, drop)
	}

	return Plan{
		Fset:              fset,
		SinkFset:          sinkFset,
		SrcPath:           srcPath,
		SinkPath:          sinkPath,
		SrcFile:           srcFile,
		OrigSink:          sink,
		MovedFile:         moved,
		SinkFile:          sinkFile,
		SinkIsNew:         sinkIsNew,
		Move:              move,
		OrigSinkDeclCount: origCount,
	}
}
