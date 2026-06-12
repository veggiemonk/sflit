package splitter

import (
	"fmt"
	"go/ast"
	"go/token"
	"os"
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
	// Identity: a file cannot be its own sink. On move, commit renames the
	// rendered sink in and then the rendered source — which no longer holds
	// the selected declarations — over it, so they vanish; on copy, the sink
	// would duplicate every selected declaration. Guarded before the
	// mode-specific checks below.
	sameF, err := sameFile(plan.SrcPath, plan.SinkPath)
	if err != nil {
		return err
	}
	if sameF {
		return fmt.Errorf(
			"source %s and sink %s are the same file: a file cannot be its own sink; choose a different sink",
			plan.SrcPath,
			plan.SinkPath,
		)
	}
	// Compute sameDir once; used for the copy guard below and threaded into
	// validateDirectives and validateNoStrandedRefs to avoid redundant calls.
	same, err := sameDir(plan.SrcPath, plan.SinkPath)
	if err != nil {
		return err
	}
	// Same-directory copy: the source keeps every selected declaration, so a
	// sink in the same directory (same package) would gain duplicates and the
	// package would no longer compile. Copying is only valid into a different
	// directory (same package name, different package).
	if !plan.Move && same {
		return fmt.Errorf(
			"cannot copy within the same directory: source %s and sink %s are in the same package, so the copied declarations would duplicate the originals and the package would no longer compile; use -move to move them, or copy into a different directory",
			plan.SrcPath,
			plan.SinkPath,
		)
	}
	// Per-file-role guards: generated, cgo, and dot-import checks apply to
	// both source and sink with role-appropriate messages. Appending to a
	// generated or cgo file is unsafe; a dot-import sink defeats the
	// parse-level collision check below.
	type fileRole struct {
		file *ast.File
		path string
		role string // "source" or "sink"
	}
	roles := []fileRole{{origSrc, plan.SrcPath, "source"}}
	if origSink != nil {
		roles = append(roles, fileRole{origSink, plan.SinkPath, "sink"})
	}
	for _, r := range roles {
		if ast.IsGenerated(r.file) {
			if r.role == "source" {
				return fmt.Errorf(
					"cannot %s declarations out of generated file %s: generated files should be changed at the generator source",
					plan.opVerb(),
					r.path,
				)
			}
			return fmt.Errorf(
				"cannot write to generated file %s: generated files should be changed at the generator source",
				r.path,
			)
		}
		if fileImportsC(r.file) {
			if r.role == "source" {
				return fmt.Errorf(
					"cannot %s declarations out of cgo file %s: import \"C\" and its preamble are file-sensitive",
					plan.opVerb(),
					r.path,
				)
			}
			return fmt.Errorf(
				"cannot write to cgo file %s: import \"C\" and its preamble are file-sensitive",
				r.path,
			)
		}
		if fileHasDotImport(r.file) {
			if r.role == "source" {
				return fmt.Errorf(
					"cannot %s declarations out of %s: source has dot imports, which obscure dependencies; refactor to qualified imports first",
					plan.opVerb(),
					r.path,
				)
			}
			return fmt.Errorf(
				"cannot write to %s: sink has dot imports, which obscure dependencies and defeat collision detection; refactor to qualified imports first",
				r.path,
			)
		}
	}
	if err := validateDirectives(plan, same); err != nil {
		return err
	}
	if err := validateBuildConstraints(plan); err != nil {
		return err
	}
	if err := validateImportAliases(plan, origSink, origSrc); err != nil {
		return err
	}
	if err := validateNoStrandedRefs(plan, same); err != nil {
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

// validateDirectives rejects cross-directory operations on declarations
// carrying //go:embed or //go:linkname: embed patterns resolve relative to
// the directory of the file containing the directive, so the operation
// would compile green yet silently embed different files; linkname binds a
// symbol of the package the declaration lives in. Same-directory moves are
// allowed — rendering carries the directive's required blank import into
// the sink (see requiredBlankImports).
// sameDirHint is the precomputed sameDir result from validatePlan.
func validateDirectives(plan Plan, sameDirHint bool) error {
	embed, linkname := travellingDirectives(plan.extracted)
	if !embed && !linkname {
		return nil
	}
	if sameDirHint {
		return nil
	}
	if embed {
		return fmt.Errorf(
			"cannot %s declarations carrying //go:embed into a different directory %s: embed patterns are directory-relative, so the embedded files would silently change; move within the source directory or relocate the embedded files first",
			plan.opVerb(),
			plan.SinkPath,
		)
	}
	return fmt.Errorf(
		"cannot %s declarations carrying //go:linkname into a different directory %s: the directive binds a symbol of the source package; move within the source directory or refactor first",
		plan.opVerb(),
		plan.SinkPath,
	)
}

// sameFile reports whether the source and sink paths name one file.
// Byte-equal cleaned absolute paths match without touching the filesystem;
// otherwise both paths are stat-ed so that case-aliased spellings on a
// case-insensitive volume, normalization aliases, and hardlinks compare
// equal via os.SameFile. A sink that cannot be stat-ed (typically: it does
// not exist yet) cannot be the already-parsed source; the source is stat-ed
// only after the sink resolves, so the unit-test convention of placeholder
// paths never reaches it.
func sameFile(source, sink string) (bool, error) {
	src, err := filepath.Abs(filepath.Clean(source))
	if err != nil {
		return false, fmt.Errorf("resolve %s: %w", source, err)
	}
	snk, err := filepath.Abs(filepath.Clean(sink))
	if err != nil {
		return false, fmt.Errorf("resolve %s: %w", sink, err)
	}
	if src == snk {
		return true, nil
	}
	sinkInfo, err := os.Stat(snk)
	if err != nil {
		return false, nil //nolint:nilerr // a sink that cannot be stat-ed cannot be the existing source
	}
	srcInfo, err := os.Stat(src)
	if err != nil {
		return false, fmt.Errorf("stat source %s: %w", source, err)
	}
	return os.SameFile(srcInfo, sinkInfo), nil
}

// sameDir reports whether two file paths resolve to the same parent
// directory. Byte-equal cleaned absolute paths match without touching the
// filesystem, so relative and absolute spellings of one directory compare
// equal; otherwise both directories are stat-ed so that case-aliased
// spellings on a case-insensitive volume, normalization aliases, and
// symlinked aliases (os.Stat follows symlinks) compare equal via
// os.SameFile. A sink directory that cannot be stat-ed (typically: not
// created yet) cannot be the source's existing directory.
func sameDir(a, b string) (bool, error) {
	da, err := filepath.Abs(filepath.Dir(filepath.Clean(a)))
	if err != nil {
		return false, fmt.Errorf("resolve directory of %s: %w", a, err)
	}
	db, err := filepath.Abs(filepath.Dir(filepath.Clean(b)))
	if err != nil {
		return false, fmt.Errorf("resolve directory of %s: %w", b, err)
	}
	if da == db {
		return true, nil
	}
	dbInfo, err := os.Stat(db)
	if err != nil {
		return false, nil //nolint:nilerr // a sink directory that cannot be stat-ed cannot be the source's existing directory
	}
	daInfo, err := os.Stat(da)
	if err != nil {
		return false, fmt.Errorf("stat directory of %s: %w", a, err)
	}
	return os.SameFile(daInfo, dbInfo), nil
}

// validateImportAliases rejects a split whose sink already binds an alias
// the source binds to a different path: carryNamedImports re-adds the
// source's named imports to the combined sink, so the written file would
// declare one alias twice and not compile. Like the dot-import rule, the
// check is file-wide rather than per moved declaration — conservative, but
// it fails before anything is written.
//
// It also checks two new directions:
//   - Direction A (source alias vs sink decl): if a source named-import alias
//     equals a package-level declared name in the sink, the carried import
//     would be pruned by goimports as shadowed, breaking the sink.
//   - Direction B (moved decl vs sink alias): if a moved declaration's name
//     equals a sink named-import alias, the declaration would shadow the
//     alias in the combined sink.
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
				plan.SinkPath,
				plan.SrcPath,
				path,
				name,
				sinkPath,
			)
		}
	}

	// Direction A: source named-import alias vs sink package-level declaration.
	// Collect sink package-level declared names (funcs without receivers,
	// types, vars, consts — methods do NOT bind package-level names).
	sinkDeclNames := make(map[string]bool)
	for _, d := range origSink.Decls {
		switch x := d.(type) {
		case *ast.FuncDecl:
			if x.Recv == nil || len(x.Recv.List) == 0 {
				sinkDeclNames[x.Name.Name] = true
			}
		case *ast.GenDecl:
			if x.Tok == token.IMPORT {
				continue
			}
			for _, s := range x.Specs {
				switch ss := s.(type) {
				case *ast.TypeSpec:
					sinkDeclNames[ss.Name.Name] = true
				case *ast.ValueSpec:
					for _, n := range ss.Names {
						if n.Name != "_" {
							sinkDeclNames[n.Name] = true
						}
					}
				}
			}
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
		if sinkDeclNames[name] {
			return fmt.Errorf(
				"cannot write to %s: source %s imports %q as %s but the sink declares %s at package level; rename the import alias or the sink declaration first",
				plan.SinkPath,
				plan.SrcPath,
				path,
				name,
				name,
			)
		}
	}

	// Direction B: moved declaration name vs sink named-import alias.
	// Collect names of declarations appended to the sink (the tail beyond OrigSinkDeclCount).
	for i := plan.OrigSinkDeclCount; i < len(plan.SinkFile.Decls); i++ {
		for _, k := range collisionKeys(plan.SinkFile.Decls[i]) {
			if strings.Contains(k, ".") {
				// Methods are keyed as "Recv.Method" — not a package-level name.
				continue
			}
			if path, ok := sinkPaths[k]; ok {
				return fmt.Errorf(
					"cannot write to %s: moved declaration %s would shadow the sink's import alias %s (path %q); rename the declaration or the import alias first",
					plan.SinkPath,
					k,
					k,
					path,
				)
			}
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
// sameDirHint is the precomputed sameDir result from validatePlan.
func validateNoStrandedRefs(plan Plan, sameDirHint bool) error {
	if sameDirHint || len(plan.extracted) == 0 {
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
	// names of a narrowed multi-name spec (mirrors applyMove's classification).
	travelling, travellingNames := plan.travelSet()

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
		skip := fieldNameKeys(n)
		ast.Inspect(n, func(node ast.Node) bool {
			id, ok := node.(*ast.Ident)
			if !ok || skip[id] {
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
				"cannot %s declarations from %s into a different directory %s: %s references %s, which stays behind in the source package; move them together or refactor first",
				plan.opVerb(),
				plan.SrcPath,
				plan.SinkPath,
				strings.Join(declKeys(e.Decl), ", "),
				strings.Join(stranded, ", "),
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
				plan.SrcPath,
				plan.SinkPath,
				strings.Join(keys, ", "),
				strings.Join(away, ", "),
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
					// Narrowed spec: only the kept names' values
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

// fieldNameKeys returns the bare-ident composite-literal keys under n that
// are struct field names, not expressions: the parser resolves T{helper: 1}
// keys against file scope (go.dev/issue/45160), so without skipping them the
// reference walk sees phantom references to same-file top-level names. Keys
// of map and array literals ARE expressions and are not collected. A
// literal's kind is judged syntactically, chasing file-local type
// declarations; types this file cannot resolve (imported, sibling-file) are
// treated as structs — phantom rejections are the worse failure here, and
// an unjudgeable map key joins the documented file-local blind spots.
func fieldNameKeys(n ast.Node) map[*ast.Ident]bool {
	skip := make(map[*ast.Ident]bool)
	ast.Inspect(n, func(node ast.Node) bool {
		// Nil-typed nested literals are reached through their typed
		// ancestor, which knows their element type.
		if lit, ok := node.(*ast.CompositeLit); ok && lit.Type != nil {
			markFieldNameKeys(lit, lit.Type, skip)
		}
		return true
	})
	return skip
}

// markFieldNameKeys records lit's field-name keys into skip, recursing into
// nil-typed (elided) nested literals with the key/element type inherited
// from typ.
func markFieldNameKeys(lit *ast.CompositeLit, typ ast.Expr, skip map[*ast.Ident]bool) {
	var keyType, elemType ast.Expr
	structLike := false
	switch t := underlyingLocalType(typ).(type) {
	case *ast.MapType:
		keyType, elemType = t.Key, t.Value
	case *ast.ArrayType:
		elemType = t.Elt
	default:
		structLike = true
	}
	for _, el := range lit.Elts {
		val := el
		if kv, ok := el.(*ast.KeyValueExpr); ok {
			val = kv.Value
			switch {
			case structLike:
				if id, ok := kv.Key.(*ast.Ident); ok {
					skip[id] = true
				}
			default:
				if klit, ok := kv.Key.(*ast.CompositeLit); ok && klit.Type == nil {
					markFieldNameKeys(klit, keyType, skip)
				}
			}
		}
		if vlit, ok := val.(*ast.CompositeLit); ok && vlit.Type == nil {
			markFieldNameKeys(vlit, elemType, skip)
		}
	}
}

// underlyingLocalType chases parentheses, generic instantiations, pointers,
// and type declarations resolvable in this file (including aliases) to the
// syntactic underlying type; anything it cannot resolve file-locally is
// returned as-is. Pointers occur only as inherited element types of elided
// literals ([]*M{{…}}); a spelled literal type is never a pointer. Bounded
// because parser objects can be cyclic on invalid input.
func underlyingLocalType(typ ast.Expr) ast.Expr {
	for range 64 {
		switch t := typ.(type) {
		case *ast.ParenExpr:
			typ = t.X
		case *ast.StarExpr:
			typ = t.X
		case *ast.IndexExpr: // M[T]
			typ = t.X
		case *ast.IndexListExpr: // M[T, U]
			typ = t.X
		case *ast.Ident:
			if t.Obj == nil {
				return typ
			}
			ts, ok := t.Obj.Decl.(*ast.TypeSpec)
			if !ok {
				return typ
			}
			typ = ts.Type
		default:
			return typ
		}
	}
	return typ
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
