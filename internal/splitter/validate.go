package splitter

import (
	"fmt"
	"go/ast"
	"go/token"
	"path/filepath"
	"strconv"
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
			keys = append(keys, specKeys(x.Tok, s)...)
		}
		return keys
	}
	return nil
}

// specKeys returns the declKeys-style name keys for one spec of a GenDecl
// with the given keyword token.
func specKeys(tok token.Token, s ast.Spec) []string {
	switch ss := s.(type) {
	case *ast.TypeSpec:
		return []string{"type " + ss.Name.Name}
	case *ast.ValueSpec:
		kind := "var"
		if tok == token.CONST {
			kind = "const"
		}
		keys := make([]string, 0, len(ss.Names))
		for _, n := range ss.Names {
			keys = append(keys, kind+" "+n.Name)
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
	case *ast.ParenExpr: // (T), (*T)
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
	// Same-directory copy: the source keeps every selected declaration, so a
	// sink in the same directory (same package) would gain duplicates and the
	// package would no longer compile. Copying is only valid into a different
	// directory (same package name, different package).
	if !plan.Move {
		same, err := sameDir(plan.SrcPath, plan.SinkPath)
		if err != nil {
			return err
		}
		if same {
			return fmt.Errorf(
				"cannot copy within the same directory: source %s and sink %s are in the same package, so the copied declarations would duplicate the originals and the package would no longer compile; use -move to move them, or copy into a different directory",
				plan.SrcPath,
				plan.SinkPath,
			)
		}
	}
	if generated, err := isGeneratedFile(plan.SrcPath); err != nil {
		return err
	} else if generated {
		return fmt.Errorf(
			"cannot split generated file %s: generated files should be changed at the generator source",
			plan.SrcPath,
		)
	}
	if fileImportsC(origSrc) {
		return fmt.Errorf(
			"cannot split cgo file %s: import \"C\" and its preamble are file-sensitive",
			plan.SrcPath,
		)
	}
	if fileHasDotImport(origSrc) {
		return fmt.Errorf(
			"cannot split file with dot imports %s: dot imports obscure dependencies; refactor to qualified imports first",
			plan.SrcPath,
		)
	}
	if err := validateBuildConstraints(plan); err != nil {
		return err
	}
	if err := validateImportAliases(plan, origSink, origSrc); err != nil {
		return err
	}
	if err := validateNoStrandedRefs(plan); err != nil {
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

// sameDir reports whether two file paths resolve to the same parent
// directory. Comparison is on cleaned absolute paths, so relative and
// absolute spellings of one directory match; symlinked aliases of a
// directory are not resolved.
func sameDir(a, b string) (bool, error) {
	da, err := filepath.Abs(filepath.Dir(filepath.Clean(a)))
	if err != nil {
		return false, fmt.Errorf("resolve directory of %s: %w", a, err)
	}
	db, err := filepath.Abs(filepath.Dir(filepath.Clean(b)))
	if err != nil {
		return false, fmt.Errorf("resolve directory of %s: %w", b, err)
	}
	return da == db, nil
}

// validateImportAliases rejects a split whose sink already binds an alias
// the source binds to a different path: carryNamedImports re-adds the
// source's named imports to the combined sink, so the written file would
// declare one alias twice and not compile. Like the dot-import rule, the
// check is file-wide rather than per moved declaration — conservative, but
// it fails before anything is written.
func validateImportAliases(plan Plan, origSink, origSrc *ast.File) error {
	if origSink == nil {
		return nil
	}
	sinkPaths := make(map[string]string, len(origSink.Imports))
	for _, imp := range origSink.Imports {
		name, path, err := namedImport(imp)
		if err != nil {
			return err
		}
		if name != "" {
			sinkPaths[name] = path
		}
	}
	for _, imp := range origSrc.Imports {
		name, path, err := namedImport(imp)
		if err != nil {
			return err
		}
		if name == "" {
			continue
		}
		if sinkPath, ok := sinkPaths[name]; ok && sinkPath != path {
			return fmt.Errorf(
				"cannot write to %s: source %s imports %q as %s but the sink imports %q under the same alias; rename one of the imports first",
				plan.SinkPath, plan.SrcPath, path, name, sinkPath,
			)
		}
	}
	return nil
}

// namedImport returns the alias and unquoted path of imp; name is "" for
// unaliased, blank, and dot imports.
func namedImport(imp *ast.ImportSpec) (name, path string, err error) {
	if imp.Name == nil || imp.Name.Name == "_" || imp.Name.Name == "." {
		return "", "", nil
	}
	path, err = strconv.Unquote(imp.Path.Value)
	if err != nil {
		return "", "", fmt.Errorf("import path %s: %w", imp.Path.Value, err)
	}
	return imp.Name.Name, path, nil
}

// validateNoStrandedRefs rejects cross-directory splits that would tear
// package-internal references apart: the sink lives in a different
// directory, hence a different package instance, so a moved declaration
// referencing a top-level name that stays behind — or, on move, a staying
// declaration referencing a name that leaves — produces files that cannot
// compile. Resolution is parser-level and file-local: locals that shadow a
// top-level name resolve to the local and are not flagged, and references
// from or to sibling files of the source package are invisible to this
// check (non-goal: whole-package analysis needs go/types and the full
// directory).
func validateNoStrandedRefs(plan Plan) error {
	same, err := sameDir(plan.SrcPath, plan.SinkPath)
	if err != nil {
		return err
	}
	if same || len(plan.extracted) == 0 {
		return nil
	}

	// Top-level declaration nodes of the source file; idents resolve to
	// these via ast.Object.Decl. ImportSpecs are excluded — imports are
	// carried and re-resolved by goimports, never stranded.
	topLevel := make(map[any]bool)
	for _, d := range plan.SrcFile.Decls {
		switch x := d.(type) {
		case *ast.FuncDecl:
			topLevel[x] = true
		case *ast.GenDecl:
			if x.Tok == token.IMPORT {
				continue
			}
			for _, s := range x.Specs {
				topLevel[s] = true
			}
		}
	}

	// What travels with the selection: whole funcs, whole specs, or single
	// names of a partially split multi-name spec (mirrors applyMove's
	// classification of the same Extracted entries).
	travelling := make(map[any]bool, len(plan.extracted))
	travellingNames := make(map[*ast.ValueSpec]map[string]bool)
	for _, e := range plan.extracted {
		o := e.Origin
		switch {
		case o == nil:
			if gd, ok := e.Decl.(*ast.GenDecl); ok {
				for _, s := range gd.Specs {
					travelling[s] = true
				}
				continue
			}
			travelling[e.Decl] = true
		case o.Names == nil:
			travelling[o.Spec] = true
		default:
			vs, ok := o.Spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			if travellingNames[vs] == nil {
				travellingNames[vs] = make(map[string]bool, len(o.Names))
			}
			for _, j := range o.Names {
				travellingNames[vs][vs.Names[j].Name] = true
			}
		}
	}

	// classify reports whether id resolves to a top-level name of the
	// source file, and whether that name travels with the selection.
	classify := func(id *ast.Ident) (isTopLevel, travels bool) {
		if id.Obj == nil || id.Obj.Decl == nil {
			return false, false
		}
		decl := id.Obj.Decl
		if !topLevel[decl] {
			return false, false
		}
		if travelling[decl] {
			return true, true
		}
		if vs, ok := decl.(*ast.ValueSpec); ok && travellingNames[vs] != nil {
			return true, travellingNames[vs][id.Name]
		}
		return true, false
	}

	// refs collects, deduplicated, the top-level names n references whose
	// travel direction matches travels.
	refs := func(n ast.Node, travels bool) []string {
		var out []string
		seen := make(map[string]bool)
		ast.Inspect(n, func(node ast.Node) bool {
			id, ok := node.(*ast.Ident)
			if !ok {
				return true
			}
			if isTop, t := classify(id); isTop && t == travels && !seen[id.Name] {
				seen[id.Name] = true
				out = append(out, id.Name)
			}
			return true
		})
		return out
	}

	// Forward: a moved declaration must not reference names staying behind.
	for _, e := range plan.extracted {
		if stranded := refs(e.Decl, false); len(stranded) > 0 {
			return fmt.Errorf(
				"cannot split %s into different directory %s: moved declaration %s references %s, which stays behind in the source package; move them together or refactor first",
				plan.SrcPath, plan.SinkPath,
				strings.Join(declKeys(e.Decl), ", "), strings.Join(stranded, ", "),
			)
		}
	}

	// Reverse, move only (a copy removes nothing): a staying declaration
	// must not reference names that leave.
	if !plan.Move {
		return nil
	}
	torn := func(n ast.Node, keys []string) error {
		if away := refs(n, true); len(away) > 0 {
			return fmt.Errorf(
				"cannot move out of %s into different directory %s: remaining declaration %s references %s, which would move away; move them together or refactor first",
				plan.SrcPath, plan.SinkPath,
				strings.Join(keys, ", "), strings.Join(away, ", "),
			)
		}
		return nil
	}
	for _, d := range plan.SrcFile.Decls {
		switch x := d.(type) {
		case *ast.FuncDecl:
			if travelling[x] {
				continue
			}
			if err := torn(x, declKeys(x)); err != nil {
				return err
			}
		case *ast.GenDecl:
			if x.Tok == token.IMPORT {
				continue
			}
			for _, s := range x.Specs {
				if travelling[s] {
					continue
				}
				if vs, ok := s.(*ast.ValueSpec); ok && travellingNames[vs] != nil {
					// Partially split spec: only the kept names' values
					// (1:1 with names — the shared-value case is rejected
					// upstream) and the shared type stay behind.
					if vs.Type != nil {
						if err := torn(vs.Type, specKeys(x.Tok, vs)); err != nil {
							return err
						}
					}
					for j, n := range vs.Names {
						if travellingNames[vs][n.Name] || j >= len(vs.Values) {
							continue
						}
						if err := torn(vs.Values[j], []string{n.Name}); err != nil {
							return err
						}
					}
					continue
				}
				if err := torn(s, specKeys(x.Tok, s)); err != nil {
					return err
				}
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
