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
  bounded by the new `-retries` flag (default 5). See ADR-0001.
- Sidecar lock files (`.<name>.sflit.lock`) are removed on release on unix;
  on windows they are left behind (best-effort platform) and are safe to
  ignore.
- `-json` output (and the library `Result`) includes `attempts`: how many
  pipeline attempts the run took. `1` means no commit-time conflict;
  higher values mean concurrent writers forced re-runs — observability
  for orchestrators fanning out parallel invocations (ADR-0001).

### Changed

- Help text, tool schema, and docs reworked around the canonical vocabulary:
  sflit is a declaration mover (split and merge are its two directions),
  partially selected grouped blocks are *narrowed*, and a move back is a
  *reversal*. Wording only; no behavior change.

### Fixed

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
