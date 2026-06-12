# Sflit

Move Go declarations between files without changing what your program means.

sflit moves or copies top-level Go declarations between files through the
AST, and refuses any operation that could change what the program means.
It is a declaration mover, and **split** (one file → many) and **merge**
(many → one) are its two directions. The output has **semantic accuracy**,
not byte-for-byte fidelity: the AST is re-parsed and reprinted through
`gofmt`, and imports are updated in written files.

> File boundaries are not important in Go and a tool to split files sounds questionable.

Fair. Then a file grew past 5000 lines. Refactoring it consumed so many
tokens and doing it manually was pretty painful. It is not about code
quality or taste, just pure efficiency and speed. Therefore this tool exists.

Here is what moving declarations buys you:

1. **Agent-efficient codebases.** After a split, an agent reads only the
   file that matters. Small files make string-matching edits reliable, and
   when an agent changes one file, the rest of the package stays cached.
   The filenames become a map of the package.
2. **Parallel editing without contention.** A 5000-line file is a
   serialization point: two agents — or an agent and a human — editing it
   collide with merge conflicts and stomped edits. After a split, work on
   disjoint features touches disjoint files.
3. **Reviewable pure-move commits.** Semantic accuracy plus blocked splits
   mean a move can land as a commit that is verifiably behavior-free. The
   reviewer checks the partition, not the code; the behavioral change lands
   separately.
4. **Merging and reorganizing.** Because sflit is a declaration mover, the
   reverse direction works too: merge over-split files back into one,
   move a stray declaration to the file where it belongs, or re-partition
   a package by feature. A move that turned out wrong is fixed by a
   reversal — the same move with source and sink swapped.
5. **Test-file parity.** `_test.go` files move the same way, so the test
   layout can mirror the source layout — `foo.go` and `foo_test.go` stay
   aligned.
6. **Enforcing a file-size policy.** When the team rule says files over N
   lines must be split, sflit is the remediation: the linter flags, sflit
   moves, nothing changes meaning.

For a worked example — a 1208-line file moved into 11 files, tests
mirrored to match — see
[docs/splitting-a-real-file.md](docs/splitting-a-real-file.md).

## Dependencies

`sflit` is a self-contained binary with no external runtime dependencies.
Import management (`goimports`) is compiled in via `golang.org/x/tools/imports` — no separate installation required.

Building from source requires **Go 1.26.2+**.

## Installation

```sh
go install github.com/veggiemonk/sflit@latest
```

Or clone and build locally:

```sh
git clone https://github.com/veggiemonk/sflit.git
cd sflit
make build      # produces ./sflit
make install    # installs to $GOPATH/bin
```

## Usage
[embedmd]:# (TOOL.md)
```md
sflit - moves Go declarations between files

Moves or copies top-level Go declarations between files through the AST,
and refuses any operation that could change what the program means.
Files are re-parsed and reprinted through gofmt; imports are updated in written files.

Usage:
  sflit -source <file> -sink <file> [flags]

Flags:
  -source    string  Source Go file (required)
  -sink      string  Destination Go file; created if absent, re-rendered if present (required)
  -regex     string  Regex matched against declaration names
  -receiver  string  Receiver type name
  -move              Delete matched decls from source after writing (default: copy)
  -retries   int     Max re-runs after a concurrent-write conflict
                     (default: 16; 0 or negative uses the default — retry
                     cannot be disabled). Fanning out more than ~16
                     concurrent movers on one file needs -retries >= N.
  -json              Print structured JSON result to stdout
  -debug             Print debug logs to stderr

Selection rules:
  -regex R              Any top-level decl whose name matches R — funcs,
                        methods (matched by method name only, any receiver),
                        vars, consts, types. Grouped var/const/type blocks
                        are narrowed — matching specs travel, siblings
                        stay behind.
  -receiver T           Type T if present and all its methods (copy by default; move with -move).
  -receiver T -regex R  Only methods of T matching R (type stays).

At least one of -regex or -receiver is required.

Blocked splits (copy and move alike):
  - init functions are rejected: moving them may change package
    initialization order, and copying duplicates init so it runs twice.
  - Narrowing a const block with iota or implicit expressions is blocked;
    select the whole block or refactor the constants manually first.
  - Narrowing a multi-name var/const spec is blocked unless each name has
    a corresponding explicit value.
  - Generated files are rejected, as source or as existing sink: generated
    files should be changed at the generator source.
  - Files with build constraints can only move into sinks with identical
    build constraints.
  - Files using cgo import "C" are rejected, as source or as existing
    sink: import "C" and its preamble are file-sensitive.
  - Dot imports are rejected in both source and sink: they obscure
    dependencies and defeat collision detection.
  - Declarations carrying //go:embed or //go:linkname cannot move or copy
    into a different directory: embed patterns are directory-relative and
    linkname binds a symbol of the source package. Same-directory moves
    carry the directive's required blank import (_ "embed" / _ "unsafe")
    into the sink.
  - Copying (without -move) into a sink in the source's own directory is
    rejected: the source keeps the declarations, so the package would gain
    duplicates and stop compiling. Use -move, or copy into a different
    directory.
  - Splits into a different directory (a different package) are rejected
    when a moved declaration references a top-level name that stays behind
    in the source file, or a remaining declaration references a name that
    moves away: either file would stop compiling. Move the declarations
    together or refactor first. (File-local check: references involving
    sibling files of the source are not seen.)
  - Splits into a sink that imports a different path under an alias the
    source also uses are rejected; rename one of the imports first.

Concurrency:
  Safe to fan out N concurrent invocations on the same files with no
  external coordination. Each run hashes source and sink at parse and
  verifies them under a short per-file lock at commit; if another writer
  (sflit or not) changed a file in between, the run re-runs against the
  fresh content, up to -retries times. Sidecar lock files
  (.<name>.sflit.lock) are removed on release; on windows they are left
  behind (best-effort platform) and are safe to ignore.

Comments:
  Comments associated with moved declarations travel with them, including
  doc comments, //go: directives, free-floating lead comments, in-body
  comments, inline spec/statement comments, and trailing orphan comments
  when the matched declaration is at the end of the file.

Examples:
  # Split a file: move declarations matching a regex into a new file
  sflit -source big.go -regex '^Filter' -sink filter.go -move

  # Copy declarations into another directory
  # (copying within the same directory would duplicate them; use -move)
  sflit -source big.go -regex '^Filter' -sink otherpkg/filter.go

  # Move a type and all its methods
  sflit -source big.go -receiver MyStruct -sink my_struct.go -move

  # Move specific methods
  sflit -source big.go -receiver MyStruct -regex '^Filter' -sink my_struct_filter.go -move

  # Reversal: moved a declaration and regret it? Swap source and sink to move it back
  sflit -source small.go -regex '^Filter' -sink big.go -move

Other:
  -v, -version, --version  Print version information
  --tool-schema  Print JSON tool definition with examples (for agent integration)

Exit codes:
  0  Success
  1  Operation error (collision, package mismatch, same-directory copy,
     build-constraint mismatch, generated/cgo/dot-import file, parse error,
     no matches, write error, conflict retries exhausted)
  2  Flag/usage error (invalid flags or missing required arguments)
```


### Agent integration

`sflit --tool-schema` emits a JSON tool definition (name, description,
parameters, selection rules, worked examples, exit codes) suitable for
LLM tool-use loops. Pipe it straight into your agent's tool registry:

```sh
sflit --tool-schema | jq .
```

See [`internal/mover/schema.go`](internal/mover/schema.go) for the
schema source.

### Guarantees

- On collision (a selected Go package-namespace name already exists in the sink), `sflit` bails before writing.
- On package mismatch (sink's package differs from source's), `sflit` bails before writing.
- On copy, only the sink is written; on move, source and sink are written via temp-file + rename.
- Concurrent invocations on the same files are safe without external coordination — fan out N agents freely. Each run hashes source and sink at parse and verifies both under a short per-file lock at commit; a conflicting write (by sflit or any other tool) triggers a re-run against the fresh content, up to `-retries` times (default 16; 0 or negative uses the default — retry cannot be disabled). See [ADR-0001](docs/adr/0001-optimistic-concurrency-for-parallel-edits.md). Sidecar lock files (`.<name>.sflit.lock`) are removed on release; on windows (best-effort platform) they are left behind and are safe to ignore or gitignore.
- Copying (the default, without `-move`) into the source's own directory is rejected before writing: the source keeps every selected declaration, so the package would gain duplicate names and stop compiling. Use `-move` for same-directory splits; copy targets a sink in a different directory.
- A copy or move that could silently change semantics or produce invalid Go is a blocked split — rejected before any write: `init` functions, narrowing of `iota`/implicit const blocks, narrowing of unsafe multi-name value specs, cross-directory operations that would strand package-internal references (file-local check: sibling files of the source are not seen), sinks importing a different path under an alias the source also uses, and cross-directory operations on declarations carrying `//go:embed` or `//go:linkname`.
- Otherwise, a selector that matches only part of a grouped var/const/type block narrows it: the matching specs travel, the siblings stay in the source.
- `sflit` rejects generated files (source or sink), cgo files, dot-import files (source or sink), and build-constraint mismatches rather than guessing at file-sensitive semantics.
- Blank identifier declarations such as interface assertions do not collide with each other.
- Comments associated with moved declarations travel with them: doc comments, `//go:` directives, free-floating lead comments, in-body comments, inline spec/statement comments, and trailing orphan comments when the matched declaration is at the end of the file.
