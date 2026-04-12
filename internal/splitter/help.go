package splitter

import "io"

const helpText = `sflit — semantic file splitter for Go

Moves or copies top-level Go declarations between files.
AST is re-parsed and reprinted through gofmt; imports updated via goimports.

Usage:
  sflit -source <file> -sink <file> [flags]

Flags:
  -source    string  Source Go file (required)
  -sink      string  Destination Go file, created if absent (required)
  -regex     string  Regex matched against function/method names
  -receiver  string  Receiver type name
  -move              Delete matched decls from source after writing (default: copy)
  -json              Print structured JSON result to stdout
  -debug             Print debug logs to stderr

Selection rules:
  -regex R              Functions whose name matches R
  -receiver T           Type T and all its methods
  -receiver T -regex R  Only methods of T matching R (type stays)

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
`

func printHelp(w io.Writer) {
	_, _ = io.WriteString(w, helpText)
}
