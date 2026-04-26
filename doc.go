// Command sflit is a semantic file splitter for Go.
//
// sflit moves or copies top-level Go declarations between files. The AST is
// re-parsed and reprinted through gofmt; imports are updated in written files.
// Comments associated with moved declarations travel with them, including doc
// comments, //go: directives, leading comments, in-body comments, inline
// comments, and trailing orphan comments when the matched declaration is at the
// end of the file. Moves that can silently change semantics, such as init
// functions and partial implicit const blocks, are rejected. File-sensitive
// cases such as generated files, build constraints, cgo, and dot imports are
// rejected rather than rewritten heuristically.
//
// Usage:
//
//	sflit -source <file> -sink <file> [flags]
//
// Run "sflit -h" for the full flag list and selection rules.
//
// Examples:
//
//	# Copy declarations matching a regex
//	sflit -source big.go -regex '^Filter' -sink filter.go
//
//	# Move a type and all its methods
//	sflit -source big.go -receiver MyStruct -sink my_struct.go -move
//
//	# Move only methods of a type matching a regex (type stays)
//	sflit -source big.go -receiver MyStruct -regex '^Filter' -sink my_struct_filter.go -move
//
// Exit codes: 0 success, 1 operation error (collision, package mismatch,
// build-constraint mismatch, generated/cgo/dot-import source, parse error, no
// matches, write error), 2 flag/usage error.
//
// See [github.com/veggiemonk/sflit/internal/splitter] for the internal API.
package main
