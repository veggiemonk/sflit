package splitter

import (
	"go/ast"
	"go/token"
	"regexp"
	"strconv"
)

type MatchKind int

const (
	KindFunc MatchKind = iota
	KindMethod
	KindTypeDecl
	KindValueDecl
)

type Match struct {
	Decl      ast.Decl
	Kind      MatchKind
	Synthetic bool // true when Decl was constructed and is not in file.Decls
}

// selectDecls picks top-level declarations from file based on cfg:
//
//   - cfg.Regex alone: matches any top-level decl by NAME — funcs,
//     methods, vars, consts, types. Grouped var/const/type decls are
//     split so only the matching specs are selected.
//   - cfg.Receiver alone: matches the named type plus every method
//     whose receiver's base type equals Receiver. Grouped type decls
//     are split so sibling types stay in place.
//   - cfg.Receiver + cfg.Regex: matches only methods of Receiver
//     whose name matches Regex. The type itself is not moved.
//
// Prior versions of this function skipped methods silently when
// cfg.Regex was set without cfg.Receiver, forcing callers to issue a
// follow-up invocation. The single-name-namespace semantic above is
// backwards-incompatible but collapses the common agent workflow
// (move everything named X) into one call.
func selectDecls(file *ast.File, cfg Config) ([]Match, error) {
	var re *regexp.Regexp
	if cfg.Regex != "" {
		r, err := regexp.Compile(cfg.Regex)
		if err != nil {
			return nil, err
		}
		re = r
	}
	var out []Match
	for _, d := range file.Decls {
		switch x := d.(type) {
		case *ast.FuncDecl:
			isMethod := x.Recv != nil
			switch {
			case cfg.Receiver == "" && re != nil:
				// regex-only: match funcs AND methods by name.
				if re.MatchString(x.Name.Name) {
					kind := KindFunc
					if isMethod {
						kind = KindMethod
					}
					out = append(out, Match{Decl: x, Kind: kind})
				}
			case cfg.Receiver != "" && re == nil:
				// receiver-only: every method of that receiver.
				if isMethod && receiverBaseName(x.Recv.List[0].Type) == cfg.Receiver {
					out = append(out, Match{Decl: x, Kind: KindMethod})
				}
			case cfg.Receiver != "" && re != nil:
				// receiver + regex: matching methods only.
				if isMethod && receiverBaseName(x.Recv.List[0].Type) == cfg.Receiver &&
					re.MatchString(x.Name.Name) {
					out = append(out, Match{Decl: x, Kind: KindMethod})
				}
			}
		case *ast.GenDecl:
			out = append(out, selectGenDecl(x, cfg, re)...)
		}
	}
	return out, nil
}

// selectGenDecl picks specs from a type/var/const GenDecl according to cfg.
// Returns a []Match. Grouped decls with partial matches are split: the
// source-side GenDecl is mutated to drop matched specs, and synthetic
// single-spec GenDecls are emitted for the sink.
func selectGenDecl(gd *ast.GenDecl, cfg Config, re *regexp.Regexp) []Match {
	switch gd.Tok {
	case token.TYPE:
		return selectTypeSpecs(gd, cfg, re)
	case token.VAR, token.CONST:
		if re == nil {
			// Receiver-only mode never matches vars/consts.
			return nil
		}
		return selectValueSpecs(gd, re)
	}
	return nil
}

// selectTypeSpecs handles `type (...)` groups. Two paths:
//   - receiver-only: pick TypeSpec whose name == cfg.Receiver (existing
//     behaviour, preserves the "type T and its methods" bundle).
//   - regex-only: pick every TypeSpec whose name matches cfg.Regex.
func selectTypeSpecs(gd *ast.GenDecl, cfg Config, re *regexp.Regexp) []Match {
	var out []Match
	// Walk specs backwards so mutation-indices stay valid when we splice.
	matchIdx := make([]int, 0, len(gd.Specs))
	for i, s := range gd.Specs {
		ts, ok := s.(*ast.TypeSpec)
		if !ok {
			continue
		}
		switch {
		case cfg.Receiver != "" && re == nil:
			if ts.Name.Name == cfg.Receiver {
				matchIdx = append(matchIdx, i)
			}
		case cfg.Receiver == "" && re != nil:
			if re.MatchString(ts.Name.Name) {
				matchIdx = append(matchIdx, i)
			}
		case cfg.Receiver != "" && re != nil:
			// receiver + regex mode doesn't move the type; it's filter-only for methods.
			return nil
		}
	}
	if len(matchIdx) == 0 {
		return nil
	}
	if len(matchIdx) == len(gd.Specs) {
		// All specs match → move the whole group decl.
		out = append(out, Match{Decl: gd, Kind: KindTypeDecl})
		return out
	}
	// Partial match → split. Build synthetics for matched specs, splice them out.
	kept := gd.Specs[:0]
	matched := make(map[int]bool, len(matchIdx))
	for _, i := range matchIdx {
		matched[i] = true
	}
	for i, s := range gd.Specs {
		if matched[i] {
			syn := &ast.GenDecl{Tok: gd.Tok, Specs: []ast.Spec{s}}
			out = append(out, Match{Decl: syn, Kind: KindTypeDecl, Synthetic: true})
			continue
		}
		kept = append(kept, s)
	}
	gd.Specs = kept
	return out
}

// selectValueSpecs picks ValueSpecs from a var/const block whose names
// match the regex. Grouped declarations are split spec-by-spec; a
// ValueSpec declaring multiple names (e.g. `var a, b = 1, 2`) is split
// name-by-name into synthetic single-name specs. Three outcomes per
// GenDecl:
//  1. no names match → return nil, source untouched;
//  2. every spec+name matches → return the whole gd as a single match
//     so removeDecls strips it from the source entirely;
//  3. partial match → emit synthetic per-name/per-spec matches; mutate
//     the source gd to drop moved specs/names.
func selectValueSpecs(gd *ast.GenDecl, re *regexp.Regexp) []Match {
	if isIotaConstBlock(gd) {
		if m, ok := selectIotaConstSpecs(gd, re); ok {
			return m
		}
	}
	type specMatch struct {
		spec    *ast.ValueSpec
		names   []int // indices of matching Names
		fullyIn bool  // true when every Name in this spec matched
	}
	var per []specMatch
	anyMatch := false
	allMatch := true
	for _, s := range gd.Specs {
		vs, ok := s.(*ast.ValueSpec)
		if !ok {
			allMatch = false
			per = append(per, specMatch{})
			continue
		}
		var nameIdx []int
		for j, n := range vs.Names {
			if re.MatchString(n.Name) {
				nameIdx = append(nameIdx, j)
			}
		}
		if len(nameIdx) > 0 {
			anyMatch = true
		}
		fullyIn := len(nameIdx) == len(vs.Names) && len(nameIdx) > 0
		if !fullyIn {
			allMatch = false
		}
		per = append(per, specMatch{spec: vs, names: nameIdx, fullyIn: fullyIn})
	}
	if !anyMatch {
		return nil
	}
	if allMatch {
		return []Match{{Decl: gd, Kind: KindValueDecl}}
	}

	var out []Match
	kept := gd.Specs[:0]
	for i, pm := range per {
		if pm.spec == nil || len(pm.names) == 0 {
			// Non-ValueSpec or untouched ValueSpec — keep as-is.
			kept = append(kept, gd.Specs[i])
			continue
		}
		if pm.fullyIn {
			syn := &ast.GenDecl{Tok: gd.Tok, Specs: []ast.Spec{pm.spec}}
			out = append(out, Match{Decl: syn, Kind: KindValueDecl, Synthetic: true})
			continue
		}
		// Partial name match within a multi-name ValueSpec. Split each
		// moved name into its own synthetic; retain surviving names in
		// the original spec (which stays in kept).
		moved := make(map[int]bool, len(pm.names))
		for _, j := range pm.names {
			moved[j] = true
			split := &ast.ValueSpec{
				Names: []*ast.Ident{pm.spec.Names[j]},
				Type:  pm.spec.Type,
			}
			if j < len(pm.spec.Values) {
				split.Values = []ast.Expr{pm.spec.Values[j]}
			}
			syn := &ast.GenDecl{Tok: gd.Tok, Specs: []ast.Spec{split}}
			out = append(out, Match{Decl: syn, Kind: KindValueDecl, Synthetic: true})
		}
		keptNames := pm.spec.Names[:0]
		var keptValues []ast.Expr
		for j, n := range pm.spec.Names {
			if moved[j] {
				continue
			}
			keptNames = append(keptNames, n)
			if j < len(pm.spec.Values) {
				keptValues = append(keptValues, pm.spec.Values[j])
			}
		}
		pm.spec.Names = keptNames
		pm.spec.Values = keptValues
		kept = append(kept, pm.spec)
	}
	gd.Specs = kept
	return out
}

// isIotaConstBlock reports whether gd is a const block whose first
// ValueSpec uses `iota` as its initializer. Subsequent specs in such a
// block implicitly inherit the iota chain, so partial moves must
// preserve spec offsets to keep semantic values stable.
func isIotaConstBlock(gd *ast.GenDecl) bool {
	if gd.Tok != token.CONST || len(gd.Specs) == 0 {
		return false
	}
	vs0, ok := gd.Specs[0].(*ast.ValueSpec)
	if !ok || len(vs0.Values) != 1 {
		return false
	}
	id, ok := vs0.Values[0].(*ast.Ident)
	return ok && id.Name == "iota"
}

// selectIotaConstSpecs handles a partial move out of an iota'd const
// block. It returns (matches, true) when the call was handled here; a
// false `ok` falls back to the generic selectValueSpecs path.
//
// Strategy:
//   - Each moved spec materialises in the destination as an explicit
//     `Name Type = literalIotaValue` so the destination compiles without
//     reproducing the iota chain.
//   - In the source, every moved spec is replaced by a `_` placeholder so
//     the iota offsets of surviving specs are preserved verbatim.
//   - When the anchor (index 0, the spec carrying `= iota`) is moved,
//     leading `_` placeholders are stripped and the type+iota initializer
//     is hoisted onto the first surviving spec — there is no surviving
//     spec to act as anchor otherwise, and leading `_`s can collapse
//     because the first spec defaults to iota=0 either way.
//
// All-match and no-match cases fall through with ok=false so the generic
// path can pick them up unchanged (whole-gd move / no-op).
func selectIotaConstSpecs(gd *ast.GenDecl, re *regexp.Regexp) ([]Match, bool) {
	moved := make([]bool, len(gd.Specs))
	movedCount, totalSpecs := 0, 0
	for i, s := range gd.Specs {
		vs, ok := s.(*ast.ValueSpec)
		if !ok {
			return nil, false
		}
		totalSpecs++
		if len(vs.Names) == 1 && re.MatchString(vs.Names[0].Name) {
			moved[i] = true
			movedCount++
		}
	}
	if movedCount == 0 || movedCount == totalSpecs {
		return nil, false
	}

	anchor, ok := gd.Specs[0].(*ast.ValueSpec)
	if !ok {
		return nil, false
	}
	anchorType := anchor.Type

	destSpecs := make([]ast.Spec, 0, movedCount)
	for i, s := range gd.Specs {
		if !moved[i] {
			continue
		}
		vs, ok := s.(*ast.ValueSpec)
		if !ok {
			return nil, false
		}
		destSpecs = append(destSpecs, &ast.ValueSpec{
			Names:   vs.Names,
			Type:    cloneTypeExpr(anchorType),
			Values:  []ast.Expr{&ast.BasicLit{Kind: token.INT, Value: strconv.Itoa(i)}},
			Doc:     vs.Doc,
			Comment: vs.Comment,
		})
	}
	synth := &ast.GenDecl{Tok: token.CONST, Specs: destSpecs}
	if len(destSpecs) > 1 {
		// Lparen/Rparen must be non-zero for the printer to emit the
		// parenthesised form. Concrete positions don't matter — the
		// synthetic renders via its own segment in renderFiles.
		synth.Lparen = token.Pos(1)
		synth.Rparen = token.Pos(2)
	}

	kept := make([]ast.Spec, 0, len(gd.Specs))
	for i, s := range gd.Specs {
		if moved[i] {
			kept = append(kept, &ast.ValueSpec{Names: []*ast.Ident{ast.NewIdent("_")}})
			continue
		}
		kept = append(kept, s)
	}
	if moved[0] {
		firstReal := -1
		for i, s := range kept {
			vs, ok := s.(*ast.ValueSpec)
			if !ok {
				continue
			}
			if !isPlaceholder(vs) {
				firstReal = i
				break
			}
		}
		if firstReal >= 0 {
			kept = kept[firstReal:]
			head, ok := kept[0].(*ast.ValueSpec)
			if !ok {
				return nil, false
			}
			head.Type = anchorType
			head.Values = []ast.Expr{ast.NewIdent("iota")}
		} else {
			kept = nil
		}
	}
	gd.Specs = kept

	// Zero positions on the mutated gd so go/printer uses default spacing
	// between specs: blank lines from the original layout would otherwise
	// re-appear at each placeholder boundary, since `_` placeholders have
	// no source position and stripped leading specs leave a Pos gap to
	// Lparen.
	resetGenDeclPositions(gd)

	return []Match{{Decl: synth, Kind: KindValueDecl, Synthetic: true}}, true
}

func resetGenDeclPositions(gd *ast.GenDecl) {
	gd.Lparen = token.NoPos
	gd.Rparen = token.NoPos
	for _, s := range gd.Specs {
		vs, ok := s.(*ast.ValueSpec)
		if !ok {
			continue
		}
		for _, n := range vs.Names {
			n.NamePos = token.NoPos
		}
	}
}

func isPlaceholder(vs *ast.ValueSpec) bool {
	return len(vs.Names) == 1 && vs.Names[0].Name == "_" && vs.Type == nil && len(vs.Values) == 0
}

func cloneTypeExpr(e ast.Expr) ast.Expr {
	switch x := e.(type) {
	case nil:
		return nil
	case *ast.Ident:
		return ast.NewIdent(x.Name)
	}
	return e
}
