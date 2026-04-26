package splitter

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/printer"
	"go/token"

	"golang.org/x/tools/imports"
)

// renderFiles produces the final source and sink bytes.
// The source is printed through plan.Fset and post-processed by goimports.
// The sink is rendered as two segments: any pre-existing sink content
// (printed through plan.SinkFset) followed by the moved-decls segment
// (printed through plan.Fset). Each segment is printed with its own
// FileSet so go/printer's per-file line-gap heuristics don't bleed across
// boundaries — that bleed is what made comments from a moved decl land
// before existing sink decls in the shared-fset prototype.
func renderFiles(plan Plan) ([]byte, []byte, error) {
	srcBytes, err := printAndFormat(plan.Fset, plan.SrcFile, plan.SrcPath)
	if err != nil {
		return nil, nil, fmt.Errorf("render source: %w", err)
	}

	movedBytes, err := printNode(plan.Fset, plan.MovedFile)
	if err != nil {
		return nil, nil, fmt.Errorf("render moved decls: %w", err)
	}

	var combined []byte
	if plan.SinkIsNew {
		combined = movedBytes
	} else {
		existing, err := printNode(plan.SinkFset, plan.OrigSink)
		if err != nil {
			return nil, nil, fmt.Errorf("render sink: %w", err)
		}
		movedBody := stripPackageClause(movedBytes)
		combined = make([]byte, 0, len(existing)+1+len(movedBody))
		combined = append(combined, existing...)
		combined = append(combined, '\n')
		combined = append(combined, movedBody...)
	}

	sinkBytes, err := imports.Process(plan.SinkPath, combined, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("imports.Process sink: %w", err)
	}
	return srcBytes, sinkBytes, nil
}

func printAndFormat(fset *token.FileSet, file *ast.File, path string) ([]byte, error) {
	out, err := printNode(fset, file)
	if err != nil {
		return nil, err
	}
	return imports.Process(path, out, nil)
}

func printNode(fset *token.FileSet, file *ast.File) ([]byte, error) {
	var buf bytes.Buffer
	cfg := &printer.Config{Mode: printer.UseSpaces | printer.TabIndent, Tabwidth: 8}
	if err := cfg.Fprint(&buf, fset, file); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// stripPackageClause removes the leading `package X` line from a printed
// Go file. The moved-decls segment is rendered as a standalone *ast.File
// to preserve its associated comments; when concatenated after an
// existing sink, the duplicate package clause must be stripped.
func stripPackageClause(b []byte) []byte {
	i := bytes.Index(b, []byte("package "))
	if i < 0 {
		return b
	}
	nl := bytes.IndexByte(b[i:], '\n')
	if nl < 0 {
		return nil
	}
	rest := b[i+nl+1:]
	for len(rest) > 0 && rest[0] == '\n' {
		rest = rest[1:]
	}
	return rest
}
