# Core Block Edge Cases Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reject the highest-risk declaration moves that can silently change Go semantics: `init` functions, partial const blocks with implicit expressions, and unsafe partial multi-name var/const specs.

**Architecture:** Keep selection-time safety checks in `internal/splitter/select.go`, because selection is where grouped declarations are split and source AST mutation happens. Return ordinary operation errors from selection so the CLI exits with code 1 before rendering or writing. Add script tests for CLI behavior and unit tests for focused selector edge cases.

**Tech Stack:** Go AST (`go/ast`, `go/token`), existing `testscript` harness, `go test ./...`.

---

## File Structure

- Modify: `internal/splitter/select.go`
  - Add rejection checks for `init`, implicit const blocks, and unsafe multi-name value specs.
  - Keep existing allowed paths unchanged.
- Modify: `internal/splitter/select_test.go`
  - Add focused unit tests for helper-level behavior where exact AST mutation matters.
- Create: `internal/splitter/testdata/script/block_init.txt`
  - CLI regression: moving `init` fails and source/sink are unchanged.
- Create: `internal/splitter/testdata/script/block_implicit_const_partial.txt`
  - CLI regression: moving part of an implicit const block fails.
- Create: `internal/splitter/testdata/script/block_unsafe_multiname_value_partial.txt`
  - CLI regression: partial split of `var a, b = f()` and `var a, b T` fails.
- Modify: `internal/splitter/help.go`
  - Document the new blocked cases.
- Modify: `README.md`
  - Keep embedded help block in sync with `help.go`; update guarantees.
- Modify: `internal/splitter/schema.go`
  - Add blocked-case notes to tool schema.
- Modify: `doc.go`
  - Add short package-doc note.

---

### Task 1: Block moving `init` functions

**Files:**
- Create: `internal/splitter/testdata/script/block_init.txt`
- Modify: `internal/splitter/select.go`

- [ ] **Step 1: Write the failing CLI regression test**

Create `internal/splitter/testdata/script/block_init.txt`:

```txt
# Moving init functions is rejected because file movement can change package
# initialization order.

! exec sflit -source big.go -sink small.go -regex '^init$' -move
stderr 'cannot move init function'
cmp big.go expected_big.go
! exists small.go

-- big.go --
package p

var Seen []string

func init() {
	Seen = append(Seen, "big")
}

func Keep() {}
-- expected_big.go --
package p

var Seen []string

func init() {
	Seen = append(Seen, "big")
}

func Keep() {}
```

- [ ] **Step 2: Run the test to verify it fails**

Run:

```sh
go test ./internal/splitter -run TestScripts/block_init -count=1
```

Expected: FAIL with `unexpected success` because current selector allows `func init`.

- [ ] **Step 3: Implement the minimal selector rejection**

In `internal/splitter/select.go`, inside the `case *ast.FuncDecl:` branch in `selectDecls`, add this guard before appending a matched function/method:

```go
func rejectInitMove(fn *ast.FuncDecl) error {
	if fn.Recv == nil && fn.Name != nil && fn.Name.Name == "init" {
		return fmt.Errorf("cannot move init function: init order may change; refactor init body into a named function and move that instead")
	}
	return nil
}
```

Then call it in every selection mode that can match a function:

```go
if re.MatchString(x.Name.Name) {
	if err := rejectInitMove(x); err != nil {
		return nil, err
	}
	kind := KindFunc
	if isMethod {
		kind = KindMethod
	}
	out = append(out, Match{Decl: x, Kind: kind})
}
```

For receiver-only and receiver+regex method paths, `rejectInitMove` will return nil because methods have receivers. Keeping the call only in regex-only is sufficient today, but using the helper consistently makes future selection changes safer.

- [ ] **Step 4: Verify the init test passes**

Run:

```sh
go test ./internal/splitter -run TestScripts/block_init -count=1
```

Expected: PASS.

- [ ] **Step 5: Verify existing selector tests still pass**

Run:

```sh
go test ./internal/splitter -run 'TestSelect|TestScripts/(regex|receiver|new_sink|json)' -count=1
```

Expected: PASS.

---

### Task 2: Block partial const blocks with implicit expressions

**Files:**
- Create: `internal/splitter/testdata/script/block_implicit_const_partial.txt`
- Modify: `internal/splitter/select.go`
- Modify: `internal/splitter/select_test.go`

- [ ] **Step 1: Write the failing CLI regression test**

Create `internal/splitter/testdata/script/block_implicit_const_partial.txt`:

```txt
# Partial moves from const blocks with implicit expressions are rejected.
# B and C inherit A's expression, so moving only B would be misleading.

! exec sflit -source big.go -sink small.go -regex '^B$' -move
stderr 'cannot partially move const block with implicit expressions'
cmp big.go expected_big.go
! exists small.go

# Moving the whole implicit const block is allowed.
exec sflit -source big.go -sink all.go -regex '^(A|B|C)$' -move
cmp all.go expected_all.go

-- big.go --
package p

const (
	A = "x"
	B
	C
)
-- expected_big.go --
package p

const (
	A = "x"
	B
	C
)
-- expected_all.go --
package p

const (
	A = "x"
	B
	C
)
```

Note: The script first checks failure leaves `big.go` unchanged. The second command starts from the same script work directory after the failed command, so `big.go` is still intact.

- [ ] **Step 2: Run the test to verify it fails**

Run:

```sh
go test ./internal/splitter -run TestScripts/block_implicit_const_partial -count=1
```

Expected: FAIL with `unexpected success` for the partial move.

- [ ] **Step 3: Add helper to detect implicit const expressions**

In `internal/splitter/select.go`, add:

```go
func rejectPartialImplicitConstMove(gd *ast.GenDecl, re *regexp.Regexp) error {
	if gd.Tok != token.CONST || len(gd.Specs) == 0 {
		return nil
	}

	totalSpecs := 0
	matchedSpecs := 0
	hasImplicit := false
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
			// Partial multi-name specs are handled by the unsafe multi-name check.
			matchedSpecs++
		}
	}

	if !hasImplicit || matchedSpecs == 0 || matchedSpecs == totalSpecs {
		return nil
	}
	return fmt.Errorf("cannot partially move const block with implicit expressions: move the whole block or make each const expression explicit")
}
```

Also add small helpers near value-spec selection:

```go
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
```

Call the new rejection at the start of `selectValueSpecs`, after the `iota` rejection:

```go
if err := rejectPartialImplicitConstMove(gd, re); err != nil {
	return nil, err
}
```

- [ ] **Step 4: Verify the implicit const test passes**

Run:

```sh
go test ./internal/splitter -run TestScripts/block_implicit_const_partial -count=1
```

Expected: PASS.

- [ ] **Step 5: Add a unit test for explicit const blocks staying allowed**

In `internal/splitter/select_test.go`, add:

```go
func TestSelectValueSpecs_AllowsPartialExplicitConstBlock(t *testing.T) {
	_, f := mustParse(t, `package p

const (
	A = "x"
	B = "y"
	C = "z"
)
`)
	matches, err := selectDecls(f, Config{Regex: "^B$"})
	if err != nil {
		t.Fatalf("selectDecls returned error: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("len(matches) = %d, want 1", len(matches))
	}
	if got := declKeys(matches[0].Decl); len(got) != 1 || got[0] != "const B" {
		t.Fatalf("declKeys = %v, want [const B]", got)
	}
}
```

- [ ] **Step 6: Run focused selector tests**

Run:

```sh
go test ./internal/splitter -run 'TestSelectValueSpecs|TestScripts/block_implicit_const_partial' -count=1
```

Expected: PASS.

---

### Task 3: Block unsafe partial multi-name var/const specs

**Files:**
- Create: `internal/splitter/testdata/script/block_unsafe_multiname_value_partial.txt`
- Modify: `internal/splitter/select.go`
- Modify: `internal/splitter/select_test.go`

- [ ] **Step 1: Write the failing CLI regression test**

Create `internal/splitter/testdata/script/block_unsafe_multiname_value_partial.txt`:

```txt
# Partial splits of multi-name value specs are allowed only when values map
# one-to-one to names. Calls and zero-value declarations are rejected.

! exec sflit -source call.go -sink call_small.go -regex '^a$' -move
stderr 'cannot partially move multi-name value spec'
cmp call.go expected_call.go
! exists call_small.go

! exec sflit -source zero.go -sink zero_small.go -regex '^x$' -move
stderr 'cannot partially move multi-name value spec'
cmp zero.go expected_zero.go
! exists zero_small.go

# One-to-one values are still allowed.
exec sflit -source explicit.go -sink explicit_small.go -regex '^m$' -move
cmp explicit.go expected_explicit.go
cmp explicit_small.go expected_explicit_small.go

-- call.go --
package p

func pair() (int, int) { return 1, 2 }

var a, b = pair()
-- expected_call.go --
package p

func pair() (int, int) { return 1, 2 }

var a, b = pair()
-- zero.go --
package p

var x, y int
-- expected_zero.go --
package p

var x, y int
-- explicit.go --
package p

var m, n = 1, 2
-- expected_explicit.go --
package p

var n = 2
-- expected_explicit_small.go --
package p

var m = 1
```

- [ ] **Step 2: Run the test to verify it fails**

Run:

```sh
go test ./internal/splitter -run TestScripts/block_unsafe_multiname_value_partial -count=1
```

Expected: FAIL with `unexpected success` for at least `call.go`.

- [ ] **Step 3: Implement unsafe multi-name rejection**

In `internal/splitter/select.go`, add:

```go
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
			return fmt.Errorf("cannot partially move multi-name value spec: %d names share %d values; split the declaration manually first", len(vs.Names), len(vs.Values))
		}
	}
	return nil
}
```

Call it near the top of `selectValueSpecs`, after iota and implicit const checks:

```go
if err := rejectUnsafePartialMultiNameValueSpec(gd, re); err != nil {
	return nil, err
}
```

- [ ] **Step 4: Verify the multi-name script passes**

Run:

```sh
go test ./internal/splitter -run TestScripts/block_unsafe_multiname_value_partial -count=1
```

Expected: PASS.

- [ ] **Step 5: Add unit coverage for same-rule const specs**

In `internal/splitter/select_test.go`, add:

```go
func TestSelectValueSpecs_RejectsPartialMultiNameConstWithoutOneToOneValues(t *testing.T) {
	_, f := mustParse(t, `package p

const a, b = 1, 2
const x, y = "same", "same"
`)
	_, err := selectDecls(f, Config{Regex: "^a$"})
	if err == nil || !strings.Contains(err.Error(), "cannot partially move multi-name value spec") {
		t.Fatalf("want multi-name rejection, got %v", err)
	}
}
```

If the test imports do not already include `strings`, add it to the import block.

- [ ] **Step 6: Run focused and full tests**

Run:

```sh
go test ./internal/splitter -run 'TestSelectValueSpecs|TestScripts/block_unsafe_multiname_value_partial' -count=1
go test ./...
```

Expected: both PASS.

---

### Task 4: Update docs/help/schema for core blocked cases

**Files:**
- Modify: `internal/splitter/help.go`
- Modify: `README.md`
- Modify: `internal/splitter/schema.go`
- Modify: `doc.go`

- [ ] **Step 1: Update CLI help**

In `internal/splitter/help.go`, add a section after `Iota const blocks:` or rename it to broader `Blocked moves:`:

```md
Blocked moves:
  - init functions are rejected because moving them may change package
    initialization order.
  - Partial moves from const blocks with iota or implicit expressions are
    rejected; move the whole block or refactor constants manually first.
  - Partial moves from multi-name var/const specs are rejected unless each
    name has a corresponding explicit value.
```

Keep line lengths close to the existing help style.

- [ ] **Step 2: Sync README embedded help block**

Update the `[embedmd]` block in `README.md` to exactly match `helpText` from `internal/splitter/help.go`.

Also update Guarantees:

```md
- `sflit` rejects moves that are likely to change semantics silently: `init` functions, partial `iota`/implicit const blocks, and unsafe partial multi-name value specs.
```

- [ ] **Step 3: Update tool schema**

In `internal/splitter/schema.go`, extend the schema map with:

```go
"blocked_moves": []string{
	"init functions, because moving them may change package initialization order",
	"partial moves from iota const blocks",
	"partial moves from const blocks with implicit expressions",
	"partial moves from multi-name var/const specs unless values map one-to-one to names",
},
```

Also update the top-level description to mention conservative rejection of semantic-risk moves.

- [ ] **Step 4: Update package docs**

In `doc.go`, add one sentence to the package comment:

```go
// Moves that can silently change semantics, such as init functions and partial
// implicit const blocks, are rejected.
```

- [ ] **Step 5: Verify docs and schema**

Run:

```sh
gofmt -w doc.go internal/splitter/help.go internal/splitter/schema.go internal/splitter/select.go internal/splitter/select_test.go
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

Expected: all commands pass, README block matches help, schema JSON is valid.

---

### Task 5: Manual smoke test the final behavior

**Files:**
- No source edits.

- [ ] **Step 1: Smoke test `init` rejection**

Run:

```sh
tmp=$(mktemp -d)
cat > "$tmp/big.go" <<'EOF'
package p
func init() {}
EOF
! go run . -source "$tmp/big.go" -sink "$tmp/small.go" -regex '^init$' -move
! test -e "$tmp/small.go"
```

Expected: command fails and `small.go` does not exist.

- [ ] **Step 2: Smoke test whole const block remains allowed**

Run:

```sh
tmp=$(mktemp -d)
cat > "$tmp/big.go" <<'EOF'
package p
const (
	A = "x"
	B
	C
)
EOF
go run . -source "$tmp/big.go" -sink "$tmp/small.go" -regex '^(A|B|C)$' -move -json
cat "$tmp/small.go"
```

Expected: command succeeds and sink contains the whole const block.

- [ ] **Step 3: Smoke test unsafe multi-name rejection**

Run:

```sh
tmp=$(mktemp -d)
cat > "$tmp/big.go" <<'EOF'
package p
func pair() (int, int) { return 1, 2 }
var a, b = pair()
EOF
! go run . -source "$tmp/big.go" -sink "$tmp/small.go" -regex '^a$' -move
! test -e "$tmp/small.go"
```

Expected: command fails and `small.go` does not exist.

- [ ] **Step 4: Final verification**

Run:

```sh
go test ./...
```

Expected: PASS.
