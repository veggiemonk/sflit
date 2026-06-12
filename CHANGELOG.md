# Changelog

All notable changes to sflit are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Optimistic concurrency for parallel invocations: it is safe to fan out N
  concurrent runs on the same files with no external coordination. Each run
  records SHA-256 pre-image snapshots at parse, verifies them under a short
  per-file lock at commit, and re-runs against fresh content on conflict —
  bounded by the new `-retries` flag (default 16). See ADR-0001.
- Sidecar lock files (`.<name>.sflit.lock`) are removed on release on unix;
  on windows they are left behind (best-effort platform) and are safe to
  ignore.
- `-json` output (and the library `Result`) includes `attempts`: how many
  pipeline attempts the run took. `1` means no commit-time conflict;
  higher values mean concurrent writers forced re-runs — observability
  for orchestrators fanning out parallel invocations (ADR-0001).
- Splits into a different directory (a different package) are now rejected
  when they would tear package-internal references apart: a moved
  declaration referencing a top-level name that stays behind in the source
  file, or — on move — a remaining declaration referencing a name that
  moves away. Previously sflit wrote files that did not compile. The check
  is file-local; references involving sibling files of the source package
  are not seen.
- Splits into a sink that imports a different path under an alias the
  source also uses are now rejected; the carried named import would have
  declared the alias twice and the sink would not compile.

### Changed

- Help text, tool schema, and docs reworked around the canonical vocabulary:
  sflit is a declaration mover (split and merge are its two directions),
  partially selected grouped blocks are *narrowed*, and a move back is a
  *reversal*. Wording only; no behavior change.

### Fixed

- `--tool-schema` now documents the `-debug` flag (it was missing) and shows
  the `attempts` field in its example outputs; the documented `retries`
  default is tied to the implementation constant instead of duplicating it.
- Lock acquisition order is canonicalized to prevent deadlock between
  concurrent runs; collision keys handle parenthesized receivers.
- A doc comment on a spec split out of a grouped `var`/`const`/`type` block
  now prints above the keyword in the sink. Previously the keyword rendered
  on its own line with the comment wedged between keyword and spec —
  syntactically valid but not gofmt-stable, and the doc comment was demoted
  to a floating comment.
- Named imports (`f "fmt"`) now travel with moved declarations. goimports
  cannot infer an alias from the identifier, so a split into a directory with
  no sibling files previously wrote a sink that referenced the alias without
  importing it and did not compile.
- Copy mode no longer creates or locks a sidecar next to the source, so
  copying out of read-only trees (Go module cache, 0555 checkouts) works
  and source directories see no transient lock dotfiles. The source
  pre-image is still verified at commit (ADR-0001 Amendment 2).
- A source import alias colliding with a sink package-level declaration —
  in either direction — is rejected before writing. Previously sflit
  exited 0 and wrote a sink that did not compile (goimports pruned the
  carried import as shadowed) or silently rebound the name.
- Generated-file detection follows the official line-anchored convention
  (`go/ast.IsGenerated`) on the already-parsed AST: markers below line 20
  are now honored, mid-sentence mentions no longer false-positive, and the
  check cannot race a concurrent writer by re-reading the file from disk.
- Moving declarations into an existing cgo sink is rejected (`import "C"`
  and its preamble are file-sensitive); previously only cgo sources were.
- Lock acquisition order is case-folded, so concurrent runs spelling the
  same files with different case cannot AB-BA deadlock on case-insensitive
  filesystems (macOS, Windows); case-aliased spellings of one file dedup
  by file identity instead of by string.
- Sidecar lock files are created mode 0644 instead of 0600, so a sidecar
  left behind by a crashed run does not lock other users out of the file.
- A failed run removes the directories it created for a new sink instead
  of leaving an empty package-less directory behind.
- Source and sink naming the same file — exactly (`-source a.go -sink a.go`)
  or via a case-aliased spelling (`A.go` on macOS/Windows) — is rejected in
  both modes. Previously a move whose selection escaped the collision check
  (blank-identifier declarations like `var _ io.Reader = …`) exited 0 and
  silently deleted the declaration: the rewritten source overwrote the
  just-committed sink.
- The same-directory copy guard compares directories by file identity, not
  by path bytes, so a case-aliased directory spelling (`PKG/` vs `pkg/`)
  can no longer slip a duplicate declaration into the package; the same
  identity test keeps same-directory moves classified correctly.
- A failure inside the commit window (chmod or rename) releases the file
  locks before rolling back created directories; previously the sidecar
  still inside the new sink directory made the rollback stop, orphaning
  the directory tree the run had created.
- A commit-window failure after the sink was already renamed into place
  now always names the committed sink in the error, including the chmod
  step between the two renames; previously that path returned the bare
  chmod error and a degraded move (decls duplicated in both files) gave
  no recovery breadcrumb.
- Directories created before a failed `MkdirAll` (disk full, permission
  change mid-walk) are rolled back; previously that one staging failure
  path skipped the rollback and leaked the partial tree.
- A concurrent run's conflict rollback deleting a freshly shared sink
  directory between another run's `MkdirAll` and temp creation now
  classifies as a conflict and is retried; previously it surfaced as a
  hard exit 1, breaking fan-out-without-coordination for new-directory
  sinks.
- Help and tool schema document that cgo files are rejected as source or
  as existing sink; the prose previously claimed the cgo rule was
  source-only while the code rejected sinks too.
- A `chmod` racing the commit window is preserved: the committed file's
  mode is sampled under the lock, not before it.
- `SemEqual` (the test oracle) now detects a dropped or added doc comment
  on a grouped `var`/`const`/`type` block; previously group-level docs
  were invisible to the comparison.

## [0.5.0] - 2026-06-10

### Changed

- **Breaking:** `--tool-schema` output renames `blocked_moves` to
  `blocked_splits`.

### Fixed

- Semantic guards (init functions, iota blocks, multi-name specs) now apply
  to copy mode as well, not just move.
- Same-directory copy is rejected before writing: the source keeps the
  declarations, so the package would gain duplicate names and stop
  compiling.
- Extraction no longer steals comments owned by neighboring declarations.

### Added

- goreleaser release pipeline and govulncheck in CI; Renovate for
  dependency updates.

## [0.4.0] - 2026-04-26

### Added

- Blocked splits — operations rejected before any write because they could
  silently change semantics or produce invalid Go:
  - `init` functions (moving may change package initialization order;
    copying duplicates init so it runs twice).
  - Narrowing of const blocks with iota or implicit expressions.
  - Partial selection of multi-name var/const specs unless each name has a
    corresponding explicit value.
  - Generated sources, cgo files, dot-import files, and build-constraint
    mismatches between source and sink.
- Blank-identifier declarations (e.g. interface assertions) do not collide
  with each other.

### Fixed

- iota const block detection.

## [0.3.0] - 2026-04-26

### Fixed

- Doc comments, in-body comments, and iota chains are preserved across
  moves.

## [0.2.1] - 2026-04-25

Earliest tagged release. The fixes below came out of the first field test:
moving a 1208-line file into 11 files (see
[docs/splitting-a-real-file.md](docs/splitting-a-real-file.md)).

### Changed

- **Breaking:** `-regex` matches any top-level declaration by name — funcs,
  methods (by method name, any receiver), vars, consts, and types.
  Previously methods and top-level vars were silently skipped, and selecting
  free functions plus methods required two invocations. Grouped
  var/const/type blocks are narrowed spec-by-spec; multi-name specs
  (`var a, b = 1, 2`) name-by-name.

### Fixed

- Trailing orphan comments after the last moved declaration travel with it
  instead of being stranded in the source.

[Unreleased]: https://github.com/veggiemonk/sflit/compare/v0.5.0...HEAD
[0.5.0]: https://github.com/veggiemonk/sflit/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/veggiemonk/sflit/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/veggiemonk/sflit/compare/v0.2.1...v0.3.0
[0.2.1]: https://github.com/veggiemonk/sflit/releases/tag/v0.2.1
