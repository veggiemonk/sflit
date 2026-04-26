package splitter

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"
)

// declKeys returns stable name keys for the JSON Matched report.
//
//	plain func:   ["Foo"]
//	method:       ["T.Foo"] (receiver base type)
//	type decl:    ["type T"]  (one entry per TypeSpec in the group)
//	var decl:     ["var X"]   (one entry per declared name, handles `var a, b = …`)
//	const decl:   ["const X"]
func declKeys(d ast.Decl) []string {
	switch x := d.(type) {
	case *ast.FuncDecl:
		if x.Recv == nil || len(x.Recv.List) == 0 {
			return []string{x.Name.Name}
		}
		recv := receiverBaseName(x.Recv.List[0].Type)
		if recv == "" {
			return nil
		}
		return []string{recv + "." + x.Name.Name}
	case *ast.GenDecl:
		var keys []string
		for _, s := range x.Specs {
			switch ss := s.(type) {
			case *ast.TypeSpec:
				keys = append(keys, "type "+ss.Name.Name)
			case *ast.ValueSpec:
				kind := "var"
				if x.Tok == token.CONST {
					kind = "const"
				}
				for _, n := range ss.Names {
					keys = append(keys, kind+" "+n.Name)
				}
			}
		}
		return keys
	}
	return nil
}

// collisionKeys returns names in the namespaces that matter for duplicate
// declarations in a Go package. Top-level funcs, types, vars, and consts share
// the package block, while methods are keyed by receiver type plus method name.
func collisionKeys(d ast.Decl) []string {
	switch x := d.(type) {
	case *ast.FuncDecl:
		if x.Recv == nil || len(x.Recv.List) == 0 {
			return []string{x.Name.Name}
		}
		recv := receiverBaseName(x.Recv.List[0].Type)
		if recv == "" {
			return nil
		}
		return []string{recv + "." + x.Name.Name}
	case *ast.GenDecl:
		var keys []string
		for _, s := range x.Specs {
			switch ss := s.(type) {
			case *ast.TypeSpec:
				keys = append(keys, ss.Name.Name)
			case *ast.ValueSpec:
				for _, n := range ss.Names {
					if n.Name == "_" {
						continue
					}
					keys = append(keys, n.Name)
				}
			}
		}
		return keys
	}
	return nil
}

func receiverBaseName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return receiverBaseName(t.X)
	case *ast.IndexExpr: // R[T]
		if id, ok := t.X.(*ast.Ident); ok {
			return id.Name
		}
	case *ast.IndexListExpr: // R[T, U]
		if id, ok := t.X.(*ast.Ident); ok {
			return id.Name
		}
	}
	return ""
}

func selectionSummary(cfg Config) string {
	parts := make([]string, 0, 2)
	if cfg.Receiver != "" {
		parts = append(parts, fmt.Sprintf("-receiver %q", cfg.Receiver))
	}
	if cfg.Regex != "" {
		parts = append(parts, fmt.Sprintf("-regex %q", cfg.Regex))
	}
	return strings.Join(parts, " ")
}

// validatePlan checks invariants before any file is written. origSink is the
// sink file as loaded from disk (may be nil); origSrc is the source file.
func validatePlan(plan Plan, origSink, origSrc *ast.File) error {
	// Empty selection: plan's sink has no decls appended beyond the original.
	if len(plan.SinkFile.Decls) == plan.OrigSinkDeclCount {
		selection := plan.Selection
		if selection == "" {
			selection = "selection criteria"
		}
		return fmt.Errorf("no declarations matched in %s for %s", plan.SrcPath, selection)
	}
	// Package mismatch.
	if origSink != nil && origSink.Name.Name != origSrc.Name.Name {
		return fmt.Errorf(
			"sink %s has package %q, but source %s has package %q",
			plan.SinkPath, origSink.Name.Name, plan.SrcPath, origSrc.Name.Name,
		)
	}
	if generated, err := isGeneratedFile(plan.SrcPath); err != nil {
		return err
	} else if generated {
		return fmt.Errorf("cannot split generated file %s: generated files should be changed at the generator source", plan.SrcPath)
	}
	if fileImportsC(origSrc) {
		return fmt.Errorf("cannot split cgo file %s: import \"C\" and its preamble are file-sensitive", plan.SrcPath)
	}
	if fileHasDotImport(origSrc) {
		return fmt.Errorf("cannot split file with dot imports %s: dot imports obscure dependencies; refactor to qualified imports first", plan.SrcPath)
	}
	if err := validateBuildConstraints(plan); err != nil {
		return err
	}
	// Collisions: Go package-namespace keys from the appended tail against keys
	// from the pre-existing head. This prevents writing invalid Go such as a
	// sink that already has `var Foo` receiving `func Foo`.
	existing := make(map[string]bool)
	for i := range plan.OrigSinkDeclCount {
		for _, k := range collisionKeys(plan.SinkFile.Decls[i]) {
			existing[k] = true
		}
	}
	for i := plan.OrigSinkDeclCount; i < len(plan.SinkFile.Decls); i++ {
		for _, k := range collisionKeys(plan.SinkFile.Decls[i]) {
			if existing[k] {
				return fmt.Errorf(
					"cannot write to %s: declaration %s already exists in sink",
					plan.SinkPath,
					k,
				)
			}
		}
	}
	return nil
}

func validateBuildConstraints(plan Plan) error {
	srcConstraints := buildConstraintLinesFromAST(plan.SrcFile)
	if plan.SinkIsNew {
		if len(srcConstraints) == 0 {
			return nil
		}
		return fmt.Errorf(
			"cannot move build-constrained declarations into sink with different build constraints: new sink without matching constraints for source %s and sink %s",
			plan.SrcPath,
			plan.SinkPath,
		)
	}

	sinkConstraints := buildConstraintLinesFromAST(plan.OrigSink)
	if sameStringSlice(srcConstraints, sinkConstraints) {
		return nil
	}
	if len(srcConstraints) == 0 {
		return fmt.Errorf(
			"cannot move declarations into sink with different build constraints: source %s is unconstrained but sink %s has build constraints",
			plan.SrcPath,
			plan.SinkPath,
		)
	}
	return fmt.Errorf(
		"cannot move build-constrained declarations into sink with different build constraints: source %s and sink %s have different build constraints",
		plan.SrcPath,
		plan.SinkPath,
	)
}
