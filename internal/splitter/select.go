package splitter

import (
	"go/ast"
	"regexp"
)

type MatchKind int

const (
	KindFunc MatchKind = iota
	KindMethod
	KindTypeDecl
)

type Match struct {
	Decl      ast.Decl
	Kind      MatchKind
	Synthetic bool // true when Decl was constructed and is not in file.Decls
}

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
			case cfg.Receiver == "" && cfg.Regex != "":
				// regex-only: plain funcs by name
				if !isMethod && re.MatchString(x.Name.Name) {
					out = append(out, Match{Decl: x, Kind: KindFunc})
				}
			case cfg.Receiver != "" && cfg.Regex == "":
				// receiver-only: every method of that receiver
				if isMethod && receiverBaseName(x.Recv.List[0].Type) == cfg.Receiver {
					out = append(out, Match{Decl: x, Kind: KindMethod})
				}
			case cfg.Receiver != "" && cfg.Regex != "":
				// receiver + regex: matching methods only
				if isMethod && receiverBaseName(x.Recv.List[0].Type) == cfg.Receiver && re.MatchString(x.Name.Name) {
					out = append(out, Match{Decl: x, Kind: KindMethod})
				}
			}
		case *ast.GenDecl:
			// Only for receiver-only mode: pick up `type T ...`.
			if cfg.Receiver != "" && cfg.Regex == "" && x.Tok.String() == "type" {
				for i, s := range x.Specs {
					ts, ok := s.(*ast.TypeSpec)
					if !ok || ts.Name.Name != cfg.Receiver {
						continue
					}
					if len(x.Specs) == 1 {
						out = append(out, Match{Decl: x, Kind: KindTypeDecl})
					} else {
						// Split: build a standalone GenDecl containing only this TypeSpec.
						synthetic := &ast.GenDecl{
							Tok:   x.Tok,
							Specs: []ast.Spec{ts},
						}
						// Remove this spec from the original group so move mode
						// leaves sibling types in place.
						x.Specs = append(x.Specs[:i], x.Specs[i+1:]...)
						out = append(out, Match{Decl: synthetic, Kind: KindTypeDecl, Synthetic: true})
					}
					break
				}
			}
		}
	}
	return out, nil
}
