# Sflit

The go file splitter. 

> File boundaries are not important in Go and a tool to split files sounds questionable.

The use cases for splitting files are:
1. token efficient
2. for the agent to read and have more context by the file name.

Refactoring a file of more than 5000 lines consumed so many tokens and doing it manually was pretty painful. 
It is not about code quality or taste, just pure efficiency and speed.

Therefore this tool exist.

`sflit` moves or copies top-level Go declarations between files with
**semantic accuracy** (not byte-for-byte): the AST is re-parsed and reprinted
through `gofmt`, and imports are updated in written files.

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
sflit — semantic file splitter for Go

Moves or copies top-level Go declarations between files.
AST is re-parsed and reprinted through gofmt; imports are updated in written files.

Usage:
  sflit -source <file> -sink <file> [flags]

Flags:
  -source    string  Source Go file (required)
  -sink      string  Destination Go file; created if absent, re-rendered if present (required)
  -regex     string  Regex matched against declaration names
  -receiver  string  Receiver type name
  -move              Delete matched decls from source after writing (default: copy)
  -json              Print structured JSON result to stdout
  -debug             Print debug logs to stderr

Selection rules:
  -regex R              Any top-level decl whose name matches R — funcs,
                        methods (matched by method name only, any receiver),
                        vars, consts, types. Grouped var/const/type blocks
                        are split so only the matching specs are selected;
                        siblings stay behind.
  -receiver T           Type T if present and all its methods (copy by default; move with -move).
  -receiver T -regex R  Only methods of T matching R (type stays).

At least one of -regex or -receiver is required.

Blocked moves:
  - init functions are rejected because moving them may change package
    initialization order.
  - Partial moves from const blocks with iota or implicit expressions are
    rejected; move the whole block or refactor constants manually first.
  - Partial moves from multi-name var/const specs are rejected unless each
    name has a corresponding explicit value.
  - Generated source files are rejected.
  - Files with build constraints can only move into sinks with identical
    build constraints.
  - Files using cgo import "C" or dot imports are rejected.

Comments:
  Comments associated with moved declarations travel with them, including
  doc comments, //go: directives, free-floating lead comments, in-body
  comments, inline spec/statement comments, and trailing orphan comments
  when the matched declaration is at the end of the file.

Examples:
  # Copy declarations matching a regex
  sflit -source big.go -regex '^Filter' -sink filter.go

  # Move a type and all its methods
  sflit -source big.go -receiver MyStruct -sink my_struct.go -move

  # Move specific methods
  sflit -source big.go -receiver MyStruct -regex '^Filter' -sink my_struct_filter.go -move

  # Undo: moved a func and build broke? Move it back
  sflit -source small.go -regex '^Filter' -sink big.go -move

Other:
  -v, -version, --version  Print version information
  --tool-schema  Print JSON tool definition with examples (for agent integration)

Exit codes:
  0  Success
  1  Operation error (collision, package mismatch, parse error, no matches, write error)
  2  Flag/usage error (invalid flags or missing required arguments)
```


### Agent integration

`sflit --tool-schema` emits a JSON tool definition (name, description,
parameters, selection rules, worked examples, exit codes) suitable for
LLM tool-use loops. Pipe it straight into your agent's tool registry:

```sh
sflit --tool-schema | jq .
```

See [`internal/splitter/schema.go`](internal/splitter/schema.go) for the
schema source.

### Guarantees

- On collision (a selected Go package-namespace name already exists in the sink), `sflit` bails before writing.
- On package mismatch (sink's package differs from source's), `sflit` bails before writing.
- On copy, only the sink is written; on move, source and sink are written via temp-file + rename.
- `sflit` rejects moves that are likely to change semantics silently: `init` functions, partial `iota`/implicit const blocks, and unsafe partial multi-name value specs.
- `sflit` rejects generated files, cgo files, dot-import files, and build-constraint mismatches rather than guessing at file-sensitive semantics.
- Blank identifier declarations such as interface assertions do not collide with each other.
- Comments associated with moved declarations travel with them: doc comments, `//go:` directives, free-floating lead comments, in-body comments, inline spec/statement comments, and trailing orphan comments when the matched declaration is at the end of the file.
