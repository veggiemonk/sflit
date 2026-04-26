package splitter

import (
	"bufio"
	"bytes"
	"go/ast"
	"go/build/constraint"
	"os"
	"strings"
)

func buildConstraintLines(path string) ([]string, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return buildConstraintLinesFromBytes(src), nil
}

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

func buildConstraintLinesFromBytes(src []byte) []string {
	var lines []string
	for _, line := range bytes.Split(src, []byte("\n")) {
		text := strings.TrimSpace(string(line))
		if text == "" {
			continue
		}
		if strings.HasPrefix(text, "package ") || text == "package" {
			break
		}
		if !strings.HasPrefix(text, "//") {
			break
		}
		if isBuildConstraintLine(text) {
			lines = append(lines, text)
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

func fileHasDotImport(file *ast.File) bool {
	for _, imp := range file.Imports {
		if imp.Name != nil && imp.Name.Name == "." {
			return true
		}
	}
	return false
}
