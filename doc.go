// Command sflit moves or copies top-level Go declarations between Go files.
//
// sflit moves or copies top-level Go declarations between files through the
// AST, and refuses any operation that could change what the program means.
// Files are re-parsed and reprinted through gofmt; imports are updated in
// written files. Comments associated with moved declarations travel with
// them, including doc comments, //go: directives, leading comments, in-body
// comments, inline comments, and trailing orphan comments when the matched
// declaration is at the end of the file. Grouped var/const/type blocks are
// narrowed — matching specs travel, siblings stay. Operations that can
// silently change semantics, such as init functions and partial implicit
// const blocks, are rejected. File-sensitive cases such as generated files,
// build constraints, cgo, and dot imports are rejected rather than rewritten
// heuristically. Copying (the default, without -move) into the source's own
// directory is rejected: the source keeps the declarations, so the package
// would gain duplicates; use -move for same-directory splits, or copy into a
// different directory.
//
// Usage:
//
//	sflit -source <file> -sink <file> [flags]
//
// Run "sflit -h" for the full flag list and selection rules.
//
// Examples:
//
//	# Split a file: move declarations matching a regex into a new file
//	sflit -source big.go -regex '^Filter' -sink filter.go -move
//
//	# Copy declarations into another directory
//	sflit -source big.go -regex '^Filter' -sink otherpkg/filter.go
//
//	# Move a type and all its methods
//	sflit -source big.go -receiver MyStruct -sink my_struct.go -move
//
//	# Move only methods of a type matching a regex (type stays)
//	sflit -source big.go -receiver MyStruct -regex '^Filter' -sink my_struct_filter.go -move
//
// Concurrent invocations on the same files are safe with no external
// coordination: each run verifies pre-image hashes under a short per-file
// lock at commit and re-runs on conflict, bounded by -retries (ADR-0001).
//
// Exit codes: 0 success, 1 operation error, 2 flag/usage error. Run
// "sflit -h" for the authoritative list of exit-1 causes and blocked
// operations.
//
// See [github.com/veggiemonk/sflit/internal/splitter] for the internal API.
package main
