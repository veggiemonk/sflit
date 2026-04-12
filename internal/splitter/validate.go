package splitter

import (
	"fmt"
	"go/ast"
)

// declKeys returns stable name keys for collision detection.
//
//	plain func:   ["Foo"]
//	method:       ["T.Foo"] (receiver base type)
//	type decl:    ["type T"] (one entry per TypeSpec in the group)
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
			if ts, ok := s.(*ast.TypeSpec); ok {
				keys = append(keys, "type "+ts.Name.Name)
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

// validatePlan checks invariants before any file is written. origSink is the
// sink file as loaded from disk (may be nil); origSrc is the source file.
func validatePlan(plan Plan, origSink, origSrc *ast.File) error {
	// Empty selection: plan's sink has no decls appended beyond the original.
	if len(plan.SinkFile.Decls) == plan.OrigSinkDeclCount {
		return fmt.Errorf("no decls matched the selection criteria")
	}
	// Package mismatch.
	if origSink != nil && origSink.Name.Name != origSrc.Name.Name {
		return fmt.Errorf(
			"sink package %q does not match source package %q",
			origSink.Name.Name, origSrc.Name.Name,
		)
	}
	// Collisions: keys from the appended tail against keys from the pre-existing head.
	existing := make(map[string]bool)
	for i := 0; i < plan.OrigSinkDeclCount; i++ {
		for _, k := range declKeys(plan.SinkFile.Decls[i]) {
			existing[k] = true
		}
	}
	for i := plan.OrigSinkDeclCount; i < len(plan.SinkFile.Decls); i++ {
		for _, k := range declKeys(plan.SinkFile.Decls[i]) {
			if existing[k] {
				return fmt.Errorf("name collision in sink: %s already exists", k)
			}
		}
	}
	return nil
}
