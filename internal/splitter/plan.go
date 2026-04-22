package splitter

import (
	"go/ast"
	"go/token"
)

type Plan struct {
	Fset              *token.FileSet
	SrcFile           *ast.File
	SinkFile          *ast.File
	SrcPath           string
	SinkPath          string
	OrigSinkDeclCount int // number of decls in sink before extracted were appended
	SinkIsNew         bool
	Move              bool
}

func buildPlan(
	fset *token.FileSet,
	srcPath, sinkPath string,
	src *ast.File,
	sink *ast.File, // may be nil when sink file does not yet exist
	extracted []Extracted,
	move bool,
) Plan {
	var sinkFile *ast.File
	sinkIsNew := sink == nil
	if sinkIsNew {
		sinkFile = &ast.File{
			Name:  ast.NewIdent(src.Name.Name),
			Decls: nil,
		}
	} else {
		sinkFile = sink
	}
	origSinkCount := 0
	if !sinkIsNew {
		origSinkCount = len(sinkFile.Decls)
	}
	for _, e := range extracted {
		sinkFile.Decls = append(sinkFile.Decls, e.Decl)
		if len(e.LeadComms) > 0 {
			sinkFile.Comments = append(sinkFile.Comments, e.LeadComms...)
		}
	}

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
		SrcPath:           srcPath,
		SinkPath:          sinkPath,
		SrcFile:           srcFile,
		SinkFile:          sinkFile,
		SinkIsNew:         sinkIsNew,
		Move:              move,
		OrigSinkDeclCount: origSinkCount,
	}
}
