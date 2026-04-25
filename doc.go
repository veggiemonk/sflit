// Command sflit is a semantic file splitter for Go.
//
// sflit moves or copies top-level Go declarations between files. The AST is
// re-parsed and reprinted through gofmt; imports are updated in written files.
//
// Usage:
//
//	sflit -source <file> -sink <file> [flags]
//
// Run "sflit -h" for the full flag list and selection rules.
//
// Examples:
//
//	# Copy functions matching a regex
//	sflit -source big.go -regex '^Filter' -sink filter.go
//
//	# Move a type and all its methods
//	sflit -source big.go -receiver MyStruct -sink my_struct.go -move
//
//	# Move only methods of a type matching a regex (type stays)
//	sflit -source big.go -receiver MyStruct -regex '^Filter' -sink my_struct_filter.go -move
//
// Exit codes: 0 success, 1 operation error (collision, package mismatch,
// parse error, no matches, write error), 2 flag/usage error.
//
// See [github.com/veggiemonk/sflit/internal/splitter] for the library API.
package main
