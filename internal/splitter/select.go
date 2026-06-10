package splitter

import (
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"regexp"
	"slices"
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
	Origin    *SpecOrigin
	Kind      MatchKind
	Synthetic bool
}

// SpecOrigin points back into the source AST for a synthetic match: the
// group GenDecl in file.Decls and the spec inside it that the synthetic
// was split from. Names lists the matched name indices when only some
// names of a multi-name ValueSpec matched; nil means the whole spec moves.
type SpecOrigin struct {
	Decl  *ast.GenDecl
	Spec  ast.Spec
	Names []int // nil = whole spec; else matched name indices within the ValueSpec
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
					if err := rejectInitSplit(x); err != nil {
						return nil, err
					}
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
			matches, err := selectGenDecl(x, cfg, re)
			if err != nil {
				return nil, err
			}
			out = append(out, matches...)
		}
	}
	return out, nil
}

// rejectInitSplit applies in both copy and move mode: moving init may change
// package initialization order, and copying duplicates it — Go allows
// multiple init funcs, so the package compiles but runs init twice.
func rejectInitSplit(fn *ast.FuncDecl) error {
	if fn.Recv == nil && fn.Name != nil && fn.Name.Name == "init" {
		return errors.New(
			"cannot split init function: copying duplicates it and moving may change init order; refactor init body into a named function and split that instead",
		)
	}
	return nil
}

// selectGenDecl picks specs from a type/var/const GenDecl according to cfg.
// Returns a []Match. Grouped decls with partial matches are split: synthetic
// single-spec GenDecls are emitted for the sink, each carrying a SpecOrigin
// so the move-time splice can drop the spec from the source group after
// validation (Plan.applyMove). The source AST is never mutated here.
func selectGenDecl(gd *ast.GenDecl, cfg Config, re *regexp.Regexp) ([]Match, error) {
	switch gd.Tok {
	case token.TYPE:
		return selectTypeSpecs(gd, cfg, re), nil
	case token.VAR, token.CONST:
		if re == nil {
			// Receiver-only mode never matches vars/consts.
			return nil, nil
		}
		return selectValueSpecs(gd, re)
	}
	return nil, nil
}

// selectTypeSpecs handles `type (...)` groups. Two paths:
//   - receiver-only: pick TypeSpec whose name == cfg.Receiver (existing
//     behaviour, preserves the "type T and its methods" bundle).
//   - regex-only: pick every TypeSpec whose name matches cfg.Regex.
func selectTypeSpecs(gd *ast.GenDecl, cfg Config, re *regexp.Regexp) []Match {
	var out []Match
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
	// Partial match → split. Build synthetics for matched specs; the
	// source group stays untouched and the splice is deferred to
	// Plan.applyMove via Origin.
	for _, i := range matchIdx {
		s := gd.Specs[i]
		syn := &ast.GenDecl{Tok: gd.Tok, Specs: []ast.Spec{s}}
		out = append(out, Match{
			Decl:      syn,
			Kind:      KindTypeDecl,
			Synthetic: true,
			Origin:    &SpecOrigin{Decl: gd, Spec: s},
		})
	}
	return out
}

// selectValueSpecs picks ValueSpecs from a var/const block whose names
// match the regex. Grouped declarations are split spec-by-spec; a
// ValueSpec declaring multiple names (e.g. `var a, b = 1, 2`) is split
// name-by-name into synthetic single-name specs. Three outcomes per
// GenDecl:
//  1. no names match → return nil;
//  2. every spec+name matches → return the whole gd as a single match
//     so Plan.applyMove strips it from the source entirely on move;
//  3. partial match → emit synthetic per-name/per-spec matches, each
//     carrying a SpecOrigin describing the move-time splice.
//
// The source AST is never mutated here; mutation is deferred to
// Plan.applyMove, after validation, and only on move.
func selectValueSpecs(gd *ast.GenDecl, re *regexp.Regexp) ([]Match, error) {
	// The guards below apply in both copy and move mode: they protect the
	// sink's validity (a partially copied implicit spec renders as invalid
	// Go) and package semantics, not just the source.
	if isIotaConstBlock(gd) {
		if err := rejectPartialIotaConstSplit(gd, re); err != nil {
			return nil, err
		}
	}
	if err := rejectPartialImplicitConstSplit(gd, re); err != nil {
		return nil, err
	}
	if err := rejectUnsafePartialMultiNameValueSpec(gd, re); err != nil {
		return nil, err
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
		return nil, nil
	}
	if allMatch {
		return []Match{{Decl: gd, Kind: KindValueDecl}}, nil
	}

	var out []Match
	for _, pm := range per {
		if pm.spec == nil || len(pm.names) == 0 {
			// Non-ValueSpec or untouched ValueSpec — nothing to take.
			continue
		}
		if pm.fullyIn {
			syn := &ast.GenDecl{Tok: gd.Tok, Specs: []ast.Spec{pm.spec}}
			out = append(out, Match{
				Decl:      syn,
				Kind:      KindValueDecl,
				Synthetic: true,
				Origin:    &SpecOrigin{Decl: gd, Spec: pm.spec},
			})
			continue
		}
		// Partial name match within a multi-name ValueSpec. Split each
		// taken name into its own synthetic single-name spec (sharing the
		// original Ident/Type/Value nodes); the surviving names are
		// trimmed from the original spec by Plan.applyMove on move.
		for _, j := range pm.names {
			split := &ast.ValueSpec{
				Names: []*ast.Ident{pm.spec.Names[j]},
				Type:  pm.spec.Type,
			}
			if j < len(pm.spec.Values) {
				split.Values = []ast.Expr{pm.spec.Values[j]}
			}
			syn := &ast.GenDecl{Tok: gd.Tok, Specs: []ast.Spec{split}}
			out = append(out, Match{
				Decl:      syn,
				Kind:      KindValueDecl,
				Synthetic: true,
				Origin:    &SpecOrigin{Decl: gd, Spec: pm.spec, Names: []int{j}},
			})
		}
	}
	return out, nil
}

func rejectUnsafePartialMultiNameValueSpec(gd *ast.GenDecl, re *regexp.Regexp) error {
	for _, s := range gd.Specs {
		vs, ok := s.(*ast.ValueSpec)
		if !ok || len(vs.Names) <= 1 {
			continue
		}
		matched := valueSpecMatchedNameCount(vs, re)
		if matched == 0 || matched == len(vs.Names) {
			continue
		}
		if len(vs.Values) != len(vs.Names) {
			return fmt.Errorf(
				"cannot partially split multi-name value spec: %d names share %d values; split the declaration manually first",
				len(vs.Names),
				len(vs.Values),
			)
		}
	}
	return nil
}

func valueSpecMatchedNameCount(vs *ast.ValueSpec, re *regexp.Regexp) int {
	count := 0
	for _, n := range vs.Names {
		if re.MatchString(n.Name) {
			count++
		}
	}
	return count
}

func valueSpecFullyMatched(vs *ast.ValueSpec, re *regexp.Regexp) bool {
	return len(vs.Names) > 0 && valueSpecMatchedNameCount(vs, re) == len(vs.Names)
}

func valueSpecPartiallyMatched(vs *ast.ValueSpec, re *regexp.Regexp) bool {
	count := valueSpecMatchedNameCount(vs, re)
	return count > 0 && count < len(vs.Names)
}

// isIotaConstBlock reports whether gd is a const block with any value
// expression containing the predeclared identifier `iota`. Subsequent specs in
// such a block may implicitly inherit the iota chain, so partial splits are
// rejected; callers must select the whole block or refactor manually.
func isIotaConstBlock(gd *ast.GenDecl) bool {
	if gd.Tok != token.CONST || len(gd.Specs) == 0 {
		return false
	}
	for _, s := range gd.Specs {
		vs, ok := s.(*ast.ValueSpec)
		if !ok {
			continue
		}
		if slices.ContainsFunc(vs.Values, exprContainsIota) {
			return true
		}
	}
	return false
}

func exprContainsIota(expr ast.Expr) bool {
	contains := false
	ast.Inspect(expr, func(n ast.Node) bool {
		if contains {
			return false
		}
		id, ok := n.(*ast.Ident)
		if ok && id.Name == "iota" {
			contains = true
			return false
		}
		return true
	})
	return contains
}

func rejectPartialIotaConstSplit(gd *ast.GenDecl, re *regexp.Regexp) error {
	totalNames := 0
	matchedNames := 0
	for _, s := range gd.Specs {
		vs, ok := s.(*ast.ValueSpec)
		if !ok {
			continue
		}
		for _, n := range vs.Names {
			totalNames++
			if re.MatchString(n.Name) {
				matchedNames++
			}
		}
	}
	if matchedNames == 0 || matchedNames == totalNames {
		return nil
	}
	return fmt.Errorf(
		"cannot partially split iota const block: selection matches %d of %d names; select the whole block or refactor it manually",
		matchedNames,
		totalNames,
	)
}

func rejectPartialImplicitConstSplit(gd *ast.GenDecl, re *regexp.Regexp) error {
	if gd.Tok != token.CONST || len(gd.Specs) == 0 {
		return nil
	}

	totalSpecs := 0
	matchedSpecs := 0
	hasImplicit := false
	hasPartialSpec := false
	for _, s := range gd.Specs {
		vs, ok := s.(*ast.ValueSpec)
		if !ok {
			continue
		}
		totalSpecs++
		if len(vs.Values) == 0 {
			hasImplicit = true
		}
		if valueSpecFullyMatched(vs, re) {
			matchedSpecs++
		} else if valueSpecPartiallyMatched(vs, re) {
			matchedSpecs++
			hasPartialSpec = true
		}
	}

	if !hasImplicit || matchedSpecs == 0 || (matchedSpecs == totalSpecs && !hasPartialSpec) {
		return nil
	}
	return errors.New(
		"cannot partially split const block with implicit expressions: select the whole block or make each const expression explicit",
	)
}
