package splitter

import "io"

const helpText = `sflit — semantic file splitter for Go

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
`

func printHelp(w io.Writer) {
	_, _ = io.WriteString(w, helpText)
}
