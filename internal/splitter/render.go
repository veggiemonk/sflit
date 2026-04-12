package splitter

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/printer"
	"go/token"

	"golang.org/x/tools/imports"
)

// renderFiles prints both ASTs and runs imports.Process on each.
// Returns (srcBytes, sinkBytes, err).
func renderFiles(plan Plan) ([]byte, []byte, error) {
	srcBytes, err := printAndFormat(plan.Fset, plan.SrcFile, plan.SrcPath)
	if err != nil {
		return nil, nil, fmt.Errorf("render source: %w", err)
	}
	sinkBytes, err := printAndFormat(plan.Fset, plan.SinkFile, plan.SinkPath)
	if err != nil {
		return nil, nil, fmt.Errorf("render sink: %w", err)
	}
	return srcBytes, sinkBytes, nil
}

func printAndFormat(fset *token.FileSet, file *ast.File, path string) ([]byte, error) {
	var buf bytes.Buffer
	cfg := &printer.Config{Mode: printer.UseSpaces | printer.TabIndent, Tabwidth: 8}
	if err := cfg.Fprint(&buf, fset, file); err != nil {
		return nil, err
	}
	out, err := imports.Process(path, buf.Bytes(), nil)
	if err != nil {
		return nil, fmt.Errorf("imports.Process: %w", err)
	}
	return out, nil
}
