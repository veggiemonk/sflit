package splitter

import (
	"bufio"
	"bytes"
	"go/ast"
	"go/build/constraint"
	"os"
	"strings"
)

func buildConstraintLinesFromAST(file *ast.File) []string {
	if file == nil {
		return nil
	}
	var lines []string
	for _, group := range file.Comments {
		if group.Pos() > file.Package {
			break
		}
		for _, comment := range group.List {
			text := strings.TrimSpace(comment.Text)
			if isBuildConstraintLine(text) {
				lines = append(lines, text)
			}
		}
	}
	return lines
}

func sameStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func isBuildConstraintLine(line string) bool {
	return constraint.IsGoBuild(line) || constraint.IsPlusBuild(line)
}

func isGeneratedFile(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return isGeneratedFileBytes(data), nil
}

func isGeneratedFileBytes(data []byte) bool {
	s := bufio.NewScanner(bytes.NewReader(data))
	for i := 0; i < 20 && s.Scan(); i++ {
		line := s.Text()
		if strings.Contains(line, "Code generated") && strings.Contains(line, "DO NOT EDIT") {
			return true
		}
		if strings.HasPrefix(strings.TrimSpace(line), "package ") {
			break
		}
	}
	return false
}

func fileImportsC(file *ast.File) bool {
	for _, imp := range file.Imports {
		if imp.Path != nil && imp.Path.Value == "\"C\"" {
			return true
		}
	}
	return false
}

// travellingDirectives reports whether any comment group travelling with
// the extracted declarations carries a //go:embed or //go:linkname
// directive. Both are file-sensitive: each requires a file-scoped blank
// import in the file carrying it (`_ "embed"` / `_ "unsafe"`), embed
// patterns resolve relative to the file's directory, and linkname binds a
// symbol of the file's package.
func travellingDirectives(extracted []Extracted) (embed, linkname bool) {
	for _, e := range extracted {
		for _, cg := range e.LeadComms {
			for _, c := range cg.List {
				switch {
				case isDirective(c.Text, "embed"):
					embed = true
				case isDirective(c.Text, "linkname"):
					linkname = true
				}
			}
		}
	}
	return embed, linkname
}

// isDirective reports whether text is the //go:<name> compiler directive —
// exact name match, so "//go:embedded" does not read as embed.
func isDirective(text, name string) bool {
	rest, ok := strings.CutPrefix(text, "//go:"+name)
	return ok && (rest == "" || rest[0] == ' ' || rest[0] == '\t')
}

func fileHasDotImport(file *ast.File) bool {
	for _, imp := range file.Imports {
		if imp.Name != nil && imp.Name.Name == "." {
			return true
		}
	}
	return false
}
