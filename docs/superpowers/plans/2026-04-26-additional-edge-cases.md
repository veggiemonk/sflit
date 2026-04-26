# Additional Edge Cases Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `sflit` conservative around less-common but high-impact Go edge cases: blank identifier collisions, build constraints, generated files, cgo, and dot imports.

**Architecture:** Add validation after selection/extraction but before rendering/writing, because these checks need source-file metadata, sink-file metadata, and selected declarations. Keep selection behavior simple; use validator helpers in `internal/splitter/validate.go` and lightweight AST inspection helpers in a new focused file. CLI errors remain operation errors with exit code 1.

**Tech Stack:** Go parser/AST, `go/build/constraint` for build tags if useful, existing `testscript` harness, `go test ./...`.

---

## File Structure

- Modify: `internal/splitter/validate.go`
  - Ignore `_` in package namespace collision checks.
  - Add calls to new semantic-risk validators.
- Create: `internal/splitter/edge_cases.go`
  - Helpers for generated-file detection, build constraint extraction/comparison, cgo detection, dot import detection, and selected-decl identifier scanning.
- Create: `internal/splitter/edge_cases_test.go`
  - Unit tests for helpers without full CLI setup.
- Create: `internal/splitter/testdata/script/blank_identifier_collision.txt`
  - Verify multiple `var _ Interface = (*T)(nil)` assertions can coexist.
- Create: `internal/splitter/testdata/script/build_constraints_bail.txt`
  - Verify constrained source into unconstrained/differently constrained sink is rejected.
- Create: `internal/splitter/testdata/script/generated_source_bail.txt`
  - Verify generated source files are rejected.
- Create: `internal/splitter/testdata/script/cgo_bail.txt`
  - Verify cgo source files are rejected.
- Create: `internal/splitter/testdata/script/dot_import_bail.txt`
  - Verify dot-import dependent moves are rejected.
- Modify: `internal/splitter/help.go`, `README.md`, `internal/splitter/schema.go`, `doc.go`
  - Document additional conservative blocked cases.

---

### Task 1: Ignore blank identifier in collision detection

**Files:**
- Create: `internal/splitter/testdata/script/blank_identifier_collision.txt`
- Modify: `internal/splitter/validate.go`
- Modify: `internal/splitter/validate_test.go`

- [ ] **Step 1: Write the failing CLI regression test**

Create `internal/splitter/testdata/script/blank_identifier_collision.txt`:

```txt
# Blank identifier declarations do not bind package names, so interface
# assertions using var _ should not collide.

exec sflit -source big.go -sink sink.go -regex '^_$' -move
cmp sink.go expected_sink.go
cmp big.go expected_big.go

-- big.go --
package p

import "io"

type B struct{}

func (*B) Read([]byte) (int, error) { return 0, nil }

var _ io.Reader = (*B)(nil)
-- sink.go --
package p

import "fmt"

type A struct{}

func (*A) String() string { return "" }

var _ fmt.Stringer = (*A)(nil)
-- expected_big.go --
package p

type B struct{}

func (*B) Read([]byte) (int, error) { return 0, nil }
-- expected_sink.go --
package p

import (
	"fmt"
	"io"
)

type A struct{}

func (*A) String() string { return "" }

var _ fmt.Stringer = (*A)(nil)

var _ io.Reader = (*B)(nil)
```

- [ ] **Step 2: Run the test to verify it fails**

Run:

```sh
go test ./internal/splitter -run TestScripts/blank_identifier_collision -count=1
```

Expected: FAIL with collision on `_` if current collision logic still treats `_` as a package name.

- [ ] **Step 3: Update collision keys to skip `_`**

In `internal/splitter/validate.go`, change `collisionKeys` when iterating `*ast.ValueSpec` names:

```go
case *ast.ValueSpec:
	for _, n := range ss.Names {
		if n.Name == "_" {
			continue
		}
		keys = append(keys, n.Name)
	}
```

Do not change `declKeys`; JSON reporting should still report `var _` for moved assertions.

- [ ] **Step 4: Add unit test for collision keys**

In `internal/splitter/validate_test.go`, add:

```go
func TestValidate_BlankIdentifierDoesNotCollide(t *testing.T) {
	fset, src := mustParse(t, "package p\nvar _ interface{} = nil\n")
	_, sink := mustParse(t, "package p\nvar _ interface{} = nil\n")
	ms, _ := selectDecls(src, Config{Regex: "^_$"})
	ex := extractMatches(fset, src, ms)
	plan := buildPlan(fset, nil, "src.go", "sink.go", src, sink, ex, false)
	if err := validatePlan(plan, sink, src); err != nil {
		t.Fatalf("blank identifiers should not collide, got %v", err)
	}
}
```

- [ ] **Step 5: Verify**

Run:

```sh
go test ./internal/splitter -run 'TestValidate_BlankIdentifierDoesNotCollide|TestScripts/blank_identifier_collision' -count=1
```

Expected: PASS.

---

### Task 2: Block build constraint mismatches

**Files:**
- Create: `internal/splitter/edge_cases.go`
- Create: `internal/splitter/edge_cases_test.go`
- Create: `internal/splitter/testdata/script/build_constraints_bail.txt`
- Modify: `internal/splitter/validate.go`
- Modify: `internal/splitter/parse.go` if source bytes are not currently retained anywhere.

- [ ] **Step 1: Write the failing CLI regression test**

Create `internal/splitter/testdata/script/build_constraints_bail.txt`:

```txt
# Moving from a build-constrained source into an unconstrained sink is rejected;
# otherwise the declaration would build on platforms where it did not before.

! exec sflit -source linux.go -sink common.go -regex '^Platform$' -move
stderr 'cannot move build-constrained declarations into sink with different build constraints'
cmp linux.go expected_linux.go
cmp common.go expected_common.go

# Matching constraints are allowed.
exec sflit -source linux.go -sink linux_extra.go -regex '^Platform$' -move
cmp linux_extra.go expected_linux_extra.go

-- linux.go --
//go:build linux
// +build linux

package p

func Platform() string { return "linux" }
-- common.go --
package p

func Common() {}
-- linux_extra.go --
//go:build linux
// +build linux

package p

func Extra() {}
-- expected_linux.go --
//go:build linux
// +build linux

package p

func Platform() string { return "linux" }
-- expected_common.go --
package p

func Common() {}
-- expected_linux_extra.go --
//go:build linux
// +build linux

package p

func Extra() {}

func Platform() string { return "linux" }
```

- [ ] **Step 2: Run the test to verify it fails**

Run:

```sh
go test ./internal/splitter -run TestScripts/build_constraints_bail -count=1
```

Expected: FAIL because current code allows the constrained-to-unconstrained move.

- [ ] **Step 3: Add build constraint extraction helper**

Create `internal/splitter/edge_cases.go` with:

```go
package splitter

import (
	"bufio"
	"bytes"
	"os"
	"strings"
)

func buildConstraintLines(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return buildConstraintLinesFromBytes(data), nil
}

func buildConstraintLinesFromBytes(data []byte) []string {
	var out []string
	s := bufio.NewScanner(bytes.NewReader(data))
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "package ") {
			break
		}
		if strings.HasPrefix(line, "//go:build") || strings.HasPrefix(line, "// +build") {
			out = append(out, line)
		}
	}
	return out
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
```

- [ ] **Step 4: Add unit tests for constraint extraction**

Create `internal/splitter/edge_cases_test.go`:

```go
package splitter

import "testing"

func TestBuildConstraintLinesFromBytes(t *testing.T) {
	got := buildConstraintLinesFromBytes([]byte(`// Copyright
//go:build linux && amd64
// +build linux,amd64

package p
`))
	want := []string{"//go:build linux && amd64", "// +build linux,amd64"}
	if !sameStringSlice(got, want) {
		t.Fatalf("constraints = %#v, want %#v", got, want)
	}
}
```

- [ ] **Step 5: Validate source/sink build constraints before writing**

In `validatePlan` in `internal/splitter/validate.go`, after package mismatch and before collision checks, add:

```go
if err := validateBuildConstraints(plan); err != nil {
	return err
}
```

Add helper in `validate.go` or `edge_cases.go`:

```go
func validateBuildConstraints(plan Plan) error {
	srcConstraints, err := buildConstraintLines(plan.SrcPath)
	if err != nil {
		return err
	}
	if len(srcConstraints) == 0 {
		return nil
	}
	if plan.SinkIsNew {
		return fmt.Errorf("cannot move build-constrained declarations into new sink without matching build constraints: create sink with matching constraints first")
	}
	sinkConstraints, err := buildConstraintLines(plan.SinkPath)
	if err != nil {
		return err
	}
	if !sameStringSlice(srcConstraints, sinkConstraints) {
		return fmt.Errorf("cannot move build-constrained declarations into sink with different build constraints")
	}
	return nil
}
```

This conservative version rejects constrained source to new sink. A future enhancement can create a new sink with copied constraints, but this plan keeps behavior safe and simple.

- [ ] **Step 6: Verify**

Run:

```sh
go test ./internal/splitter -run 'TestBuildConstraintLinesFromBytes|TestScripts/build_constraints_bail' -count=1
```

Expected: PASS.

---

### Task 3: Block generated source files

**Files:**
- Modify: `internal/splitter/edge_cases.go`
- Modify: `internal/splitter/edge_cases_test.go`
- Create: `internal/splitter/testdata/script/generated_source_bail.txt`
- Modify: `internal/splitter/validate.go`

- [ ] **Step 1: Write the failing CLI regression test**

Create `internal/splitter/testdata/script/generated_source_bail.txt`:

```txt
# Generated files are rejected by default. Splitting them is usually overwritten
# by the next generator run.

! exec sflit -source generated.go -sink small.go -regex '^Generated$' -move
stderr 'cannot split generated file'
cmp generated.go expected_generated.go
! exists small.go

-- generated.go --
// Code generated by example; DO NOT EDIT.

package p

func Generated() {}
-- expected_generated.go --
// Code generated by example; DO NOT EDIT.

package p

func Generated() {}
```

- [ ] **Step 2: Run the test to verify it fails**

Run:

```sh
go test ./internal/splitter -run TestScripts/generated_source_bail -count=1
```

Expected: FAIL because current code allows generated source files.

- [ ] **Step 3: Add generated-file detection**

In `internal/splitter/edge_cases.go`, add:

```go
func isGeneratedFile(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
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
```

- [ ] **Step 4: Add unit test**

In `internal/splitter/edge_cases_test.go`, add:

```go
func TestIsGeneratedFileBytes(t *testing.T) {
	if !isGeneratedFileBytes([]byte("// Code generated by x; DO NOT EDIT.\npackage p\n")) {
		t.Fatal("generated marker not detected")
	}
	if isGeneratedFileBytes([]byte("// handwritten\npackage p\n")) {
		t.Fatal("handwritten file detected as generated")
	}
}
```

- [ ] **Step 5: Validate generated source before writing**

In `validatePlan`, add before build constraints:

```go
if generated, err := isGeneratedFile(plan.SrcPath); err != nil {
	return err
} else if generated {
	return fmt.Errorf("cannot split generated file %s: generated files should be changed at the generator source", plan.SrcPath)
}
```

- [ ] **Step 6: Verify**

Run:

```sh
go test ./internal/splitter -run 'TestIsGeneratedFileBytes|TestScripts/generated_source_bail' -count=1
```

Expected: PASS.

---

### Task 4: Block cgo source files conservatively

**Files:**
- Modify: `internal/splitter/edge_cases.go`
- Modify: `internal/splitter/edge_cases_test.go`
- Create: `internal/splitter/testdata/script/cgo_bail.txt`
- Modify: `internal/splitter/validate.go`

- [ ] **Step 1: Write the failing CLI regression test**

Create `internal/splitter/testdata/script/cgo_bail.txt`:

```txt
# cgo files are rejected conservatively. The preamble before import "C" is
# file-sensitive and should not be split mechanically.

! exec sflit -source cgo.go -sink small.go -regex '^UseC$' -move
stderr 'cannot split cgo file'
cmp cgo.go expected_cgo.go
! exists small.go

-- cgo.go --
package p

/*
#include <stdlib.h>
*/
import "C"

func UseC() int { return int(C.size_t(1)) }
-- expected_cgo.go --
package p

/*
#include <stdlib.h>
*/
import "C"

func UseC() int { return int(C.size_t(1)) }
```

- [ ] **Step 2: Run the test to verify it fails**

Run:

```sh
go test ./internal/splitter -run TestScripts/cgo_bail -count=1
```

Expected: FAIL because current code allows cgo source files.

- [ ] **Step 3: Add cgo detection helper**

In `internal/splitter/edge_cases.go`, add:

```go
func fileImportsC(file *ast.File) bool {
	for _, imp := range file.Imports {
		if imp.Path != nil && imp.Path.Value == "\"C\"" {
			return true
		}
	}
	return false
}
```

Add `go/ast` to imports.

- [ ] **Step 4: Add unit test**

In `internal/splitter/edge_cases_test.go`, add:

```go
func TestFileImportsC(t *testing.T) {
	_, f := mustParse(t, "package p\nimport \"C\"\nfunc UseC(){}\n")
	if !fileImportsC(f) {
		t.Fatal("import C not detected")
	}
	_, g := mustParse(t, "package p\nimport \"fmt\"\nfunc F(){ _ = fmt.Sprint(1) }\n")
	if fileImportsC(g) {
		t.Fatal("non-cgo file detected as cgo")
	}
}
```

- [ ] **Step 5: Validate cgo source before writing**

In `validatePlan`, add after generated-file check:

```go
if fileImportsC(origSrc) {
	return fmt.Errorf("cannot split cgo file %s: import \"C\" and its preamble are file-sensitive", plan.SrcPath)
}
```

If `validatePlan` currently receives `origSrc *ast.File`, use that parameter directly.

- [ ] **Step 6: Verify**

Run:

```sh
go test ./internal/splitter -run 'TestFileImportsC|TestScripts/cgo_bail' -count=1
```

Expected: PASS.

---

### Task 5: Block dot-import dependent moves

**Files:**
- Modify: `internal/splitter/edge_cases.go`
- Modify: `internal/splitter/edge_cases_test.go`
- Create: `internal/splitter/testdata/script/dot_import_bail.txt`
- Modify: `internal/splitter/validate.go`

- [ ] **Step 1: Write the failing CLI regression test**

Create `internal/splitter/testdata/script/dot_import_bail.txt`:

```txt
# Dot imports obscure where identifiers come from. goimports cannot reliably
# recreate them for moved declarations, so reject dot-import source files.

! exec sflit -source dot.go -sink small.go -regex '^Area$' -move
stderr 'cannot split file with dot imports'
cmp dot.go expected_dot.go
! exists small.go

-- dot.go --
package p

import . "math"

func Area(r float64) float64 { return Pi * r * r }
-- expected_dot.go --
package p

import . "math"

func Area(r float64) float64 { return Pi * r * r }
```

- [ ] **Step 2: Run the test to verify it fails**

Run:

```sh
go test ./internal/splitter -run TestScripts/dot_import_bail -count=1
```

Expected: FAIL because current code allows dot-import source files.

- [ ] **Step 3: Add dot import detection helper**

In `internal/splitter/edge_cases.go`, add:

```go
func fileHasDotImport(file *ast.File) bool {
	for _, imp := range file.Imports {
		if imp.Name != nil && imp.Name.Name == "." {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Add unit test**

In `internal/splitter/edge_cases_test.go`, add:

```go
func TestFileHasDotImport(t *testing.T) {
	_, f := mustParse(t, "package p\nimport . \"math\"\nfunc F(){ _ = Pi }\n")
	if !fileHasDotImport(f) {
		t.Fatal("dot import not detected")
	}
	_, g := mustParse(t, "package p\nimport m \"math\"\nfunc F(){ _ = m.Pi }\n")
	if fileHasDotImport(g) {
		t.Fatal("named import detected as dot import")
	}
}
```

- [ ] **Step 5: Validate dot imports before writing**

In `validatePlan`, add after cgo check:

```go
if fileHasDotImport(origSrc) {
	return fmt.Errorf("cannot split file with dot imports %s: dot imports obscure dependencies; refactor to qualified imports first", plan.SrcPath)
}
```

This plan blocks the entire source file if it has a dot import. A more precise future version can inspect selected declarations and allow moves that do not reference dot-imported names, but conservative blocking is safer and simpler.

- [ ] **Step 6: Verify**

Run:

```sh
go test ./internal/splitter -run 'TestFileHasDotImport|TestScripts/dot_import_bail' -count=1
```

Expected: PASS.

---

### Task 6: Update docs/help/schema for additional edge cases

**Files:**
- Modify: `internal/splitter/help.go`
- Modify: `README.md`
- Modify: `internal/splitter/schema.go`
- Modify: `doc.go`

- [ ] **Step 1: Update CLI help**

In `internal/splitter/help.go`, extend the blocked moves section with:

```md
  - Generated source files are rejected.
  - Files with build constraints can only move into sinks with identical
    build constraints.
  - Files using cgo import "C" or dot imports are rejected.
```

- [ ] **Step 2: Sync README**

Update the embedded help block in `README.md` to exactly match `help.go`.

Add to Guarantees:

```md
- `sflit` rejects generated files, cgo files, dot-import files, and build-constraint mismatches rather than guessing at file-sensitive semantics.
- Blank identifier declarations such as interface assertions do not collide with each other.
```

- [ ] **Step 3: Update tool schema**

In `internal/splitter/schema.go`, extend `blocked_moves` or add it if the core plan has not been implemented yet:

```go
"blocked_moves": []string{
	"generated source files",
	"moves from build-constrained source files into sinks with different or absent build constraints",
	"cgo source files using import C",
	"source files with dot imports",
},
```

If the core plan already added `blocked_moves`, append these entries rather than replacing the core entries.

- [ ] **Step 4: Update package docs**

In `doc.go`, add:

```go
// File-sensitive cases such as generated files, build constraints, cgo, and
// dot imports are rejected rather than rewritten heuristically.
```

- [ ] **Step 5: Verify docs and schema**

Run:

```sh
gofmt -w doc.go internal/splitter/help.go internal/splitter/schema.go internal/splitter/validate.go internal/splitter/edge_cases.go internal/splitter/edge_cases_test.go internal/splitter/validate_test.go
go test ./...
python3 - <<'PY'
from pathlib import Path
readme=Path('README.md').read_text()
start=readme.index('```md\n', readme.index('[embedmd]'))+len('```md\n')
end=readme.index('\n```', start)
block=readme[start:end]
help_text=Path('internal/splitter/help.go').read_text().split('const helpText = `',1)[1].split('`',1)[0].rstrip('\n')
assert block == help_text, 'README help block differs from help.go'
print('README help block matches help.go')
PY
go run . --tool-schema >/tmp/sflit-schema.json
python3 -m json.tool /tmp/sflit-schema.json >/dev/null
```

Expected: all commands pass.

---

### Task 7: Final full verification

**Files:**
- No source edits.

- [ ] **Step 1: Run all tests**

Run:

```sh
go test ./...
```

Expected: PASS.

- [ ] **Step 2: Run all new scripts together**

Run:

```sh
go test ./internal/splitter -run 'TestScripts/(blank_identifier_collision|build_constraints_bail|generated_source_bail|cgo_bail|dot_import_bail)' -count=1 -v
```

Expected: PASS for all listed scripts.

- [ ] **Step 3: Manual smoke test build-constraint rejection**

Run:

```sh
tmp=$(mktemp -d)
cat > "$tmp/linux.go" <<'EOF'
//go:build linux

package p
func Platform() {}
EOF
cat > "$tmp/common.go" <<'EOF'
package p
func Common() {}
EOF
! go run . -source "$tmp/linux.go" -sink "$tmp/common.go" -regex '^Platform$' -move
```

Expected: command fails with build-constraint mismatch.

- [ ] **Step 4: Manual smoke test blank identifier allowance**

Run:

```sh
tmp=$(mktemp -d)
cat > "$tmp/a.go" <<'EOF'
package p
var _ interface{} = nil
EOF
cat > "$tmp/b.go" <<'EOF'
package p
var _ interface{} = nil
EOF
go run . -source "$tmp/a.go" -sink "$tmp/b.go" -regex '^_$' -move
```

Expected: command succeeds.

- [ ] **Step 5: Check git diff for docs/help/schema consistency**

Run:

```sh
git diff -- README.md internal/splitter/help.go internal/splitter/schema.go doc.go
```

Expected: help text and README embedded block describe the same behavior; schema includes the same blocked cases.
