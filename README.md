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
through `gofmt`, and imports are updated on both sides via `goimports`.

## Dependencies

`sflit` is a self-contained binary with no external runtime dependencies.
Import management (`goimports`) is compiled in via `golang.org/x/tools/imports` — no separate installation required.

Building from source requires **Go 1.26+**.

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
AST is re-parsed and reprinted through gofmt; imports updated via goimports.

Usage:
  sflit -source <file> -sink <file> [flags]

Flags:
  -source    string  Source Go file (required)
  -sink      string  Destination Go file, created if absent (required)
  -regex     string  Regex matched against declaration names
  -receiver  string  Receiver type name
  -move              Delete matched decls from source after writing (default: copy)
  -json              Print structured JSON result to stdout
  -debug             Print debug logs to stderr

Selection rules:
  -regex R              Any top-level decl whose name matches R — funcs,
                        methods (any receiver), vars, consts, types.
                        Grouped var/const/type blocks are split so only
                        the matching specs move; siblings stay behind.
  -receiver T           Type T and all its methods (bundled move).
  -receiver T -regex R  Only methods of T matching R (type stays).

At least one of -regex or -receiver is required.

Examples:
  # Copy functions matching a regex
  sflit -source big.go -regex '^Filter' -sink filter.go

  # Move a type and all its methods
  sflit -source big.go -receiver MyStruct -sink my_struct.go -move

  # Move specific methods
  sflit -source big.go -receiver MyStruct -regex '^Filter' -sink my_struct_filter.go -move

  # Undo: moved a func and build broke? Move it back
  sflit -source small.go -regex '^Filter' -sink big.go -move

Other:
  -v, -version   Print version information
  --tool-schema  Print JSON tool definition with examples (for agent integration)

Exit codes:
  0  Success
  1  Runtime error (collision, package mismatch, parse error)
  2  Flag/usage error
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

- On collision (a moved name already exists in the sink), `sflit` bails before writing.
- On package mismatch (sink's package differs from source's), `sflit` bails before writing.
- On move, source and sink are written together via temp-file + rename: a crash leaves both files valid.
- Doc comments and `//go:` directives attached to a decl travel with it.
