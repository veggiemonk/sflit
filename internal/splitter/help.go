package splitter

import "io"

const helpText = `sflit - moves Go declarations between files

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
  - Source files using cgo import "C" are rejected.
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
     build-constraint mismatch, generated/cgo/dot-import source, parse error,
     no matches, write error, conflict retries exhausted)
  2  Flag/usage error (invalid flags or missing required arguments)
`

func printHelp(w io.Writer) {
	_, _ = io.WriteString(w, helpText)
}
