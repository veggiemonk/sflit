# TODO

Deferred-work ledger. Items ordered by priority — top is next. When work cannot be completed (out of scope, blocked, needs a decision, deferred improvement), append an entry here. Never silently drop incomplete work.

Entry format:

```
## <title>
- **Kind:** out of scope, blocked, needs a decision or deferred improvement.
- **Context:** what / where
- **Why deferred:** reason
- **Acceptance:** how we'll know it's done
```

---

## Lock release-order invariant has no test seam

- **Kind:** deferred improvement.
- **Context:** `lock.go:71-79` — sidecar unlink must happen *while the flock is still held*; swapping unlink/funlock reopens the two-winners ABA race the acquire-side recheck (`lockfileCurrent`) exists to close. Mutation testing (2026-06-10 review) showed the swapped order survives 5/5 full-suite `-race` runs — the invariant exists only as a comment.
- **Why deferred:** needs a deterministic test seam (hook between unlink and funlock, or retry-counter instrumentation in `lockfileCurrent`) — design work, not a quick assert. Found during test-suite review; out of scope of the structural-oracle fix.
- **Acceptance:** a test fails when the release order in `releaseFileLock` is swapped; `lockfileCurrent` branches (ErrNotExist, recreated-inode, same-inode) have deterministic unit tests; double-`release()` idempotency is pinned.

## Multi-process lock coverage (ADR-0001 safety claims)

- **Kind:** deferred improvement.
- **Context:** ADR-0001's two load-bearing claims — flock release-on-process-death (why flock beat O_EXCL lockfiles) and cross-process canonical lock ordering (the e0ab1ed cwd-relative AB-BA deadlock requires two processes with different cwds) — are tested only in-process, where both hold trivially. flock excludes per open-file-description, so goroutine tests cannot exercise either claim.
- **Why deferred:** needs exec-based test infrastructure (re-exec the test binary or a testscript with backgrounded concurrent invocations, child killed while holding the lock). Found during test-suite review.
- **Acceptance:** a test kills a child process holding the lock and asserts a second process acquires without manual cleanup; concurrent multi-process moves over overlapping lock sets complete without deadlock.

## Concurrent fan-out test has no contention assertion

- **Kind:** deferred improvement.
- **Context:** `concurrent_test.go:19` `TestConcurrentFanOut` is the *only* test that fails when `lockAll` is removed from commit (mutation-verified), and it never asserts contention occurred — on serialized CI hardware it silently degrades to testing the sequential path. All sinks are disjoint, so ordered two-lock acquisition with overlapping pairs is never stressed under real concurrency.
- **Why deferred:** needs a retry/conflict-observed counter seam plus a deterministic blocked-commit test (externally hold a sidecar lock, assert `writePair` blocks until release). Found during test-suite review.
- **Acceptance:** fan-out test asserts ≥1 conflict/retry occurred; a deterministic test holds the lock externally and observes commit blocking; a concurrent same-sink scenario (fully overlapping lock sets) exists.

## writePair crash-safety branches untested

- **Kind:** deferred improvement.
- **Context:** `write.go:118-126` — the ADR-0001 atomicity story: sink-rename fails → source untouched; src-rename fails after sink committed → "duplicates but no data loss" error naming the sink. Zero coverage; a reordering that renames source first would pass the suite. Also untested: temp-file litter after conflict/error (`cleanup`, write.go:103-106), `verify`'s non-ENOENT branch (write.go:78-79 — a permission error must not masquerade as a retryable conflict), `lockAll` partial-acquire rollback (write.go:59-62).
- **Why deferred:** found during test-suite review; out of scope of the structural-oracle fix.
- **Acceptance:** tests force each rename failure (e.g. sink path is a directory) and assert the documented invariant; a conflict path asserts no `*.tmp*` litter remains; a permission-denied re-read is asserted not-`errConflict`.

## Pin agent-facing surfaces with golden files

- **Kind:** deferred improvement.
- **Context:** help text (`sflit -h`, 92 lines — agents are routed through it), `--tool-schema`, and the `-json` payload are the product's contract surfaces and are pinned only by substring/regex fragments (`help.txt` pins 5 lines of 92; `tool_schema.txt` greps key names; `result_test.go` round-trips through the same struct so tag renames pass). `testdata/golden/` was scaffolded for this and holds only a `.gitkeep`. Known undetected drift: the `debug` flag is absent from the tool schema; `retries.default: 5` in schema.go duplicates `defaultRetries` with no tie.
- **Why deferred:** found during test-suite review; sizable batch of golden fixtures plus schema-vs-FlagSet drift tests.
- **Acceptance:** `cmp`-against-golden for help, tool schema, and full `-json` success payload; a drift test cross-checks schema properties/defaults/exit-codes against the FlagSet, `defaultRetries`, and `RunCLI`'s exit mapping.

## Small unguarded contracts and vacuous tests

- **Kind:** deferred improvement.
- **Context:** batch of one-line regression holes from the test-suite review: `Config.retries()` clamp (0/negative → 5) untested anywhere; `UsageError` type never asserted with `errors.As` (exit-2 mapping keys on it, text-only pins survive a type regression); version flags (`-v`/`-version`/`--version`) never executed and `TestHelpListsAllVersionFlags` is vacuous (`Contains("-v")` matches inside `"--version"`); `TestValidate_PackageMismatch` passes with the check deleted (sibling error also contains "package"); idempotent re-run (same `-move` twice → exit 1) untested; unknown flag → exit 2 untested; cross-directory *move* stranding unexported source-package references is neither validated nor tested; `buildConstraintLinesFromBytes` is dead code with the best table test in its file while the live `buildConstraintLinesFromAST` has zero unit tests (and `validateBuildConstraints`' `SinkIsNew` branch is untested everywhere); `Match.Kind` is produced but consumed by nothing (untestable — delete or give it a consumer); `parseGoFileInto`'s shared-fset contract is vestigial; `carryNamedImports` does not detect an alias collision (sink already imports a different path under the same alias — output won't compile; validate should reject it like dot imports).
- **Why deferred:** found during test-suite review; individually small, batched to keep the structural-oracle fix focused.
- **Acceptance:** each listed hole has a failing-then-green test or the dead code is removed; `TestValidate_PackageMismatch` asserts the specific mismatch message.

## Batch/plan mode (one invocation, N splits)

- **Kind:** deferred improvement.
- **Context:** ADR-0001 (`docs/adr/0001-optimistic-concurrency-for-parallel-edits.md`) Option E: `sflit -plan plan.json` parses the source once, validates all selections are disjoint, writes every sink + source in one process. Contention-free by construction; most token-efficient shape for an orchestrator that pre-plans a whole split.
- **Why deferred:** complementary to, not a substitute for, the optimistic-concurrency design — it doesn't cover independently fanned-out agents, which is the actual contention scenario. Builds on ADR-0001's commit atom (lock → verify hashes → rename, generalized from 2 files to N+1), so it must land after it. Needs its own plan-format design.
- **Acceptance:** a plan file format is specified, `sflit -plan` applies N splits atomically with disjointness validation, testscript coverage exists, and `--tool-schema` documents it.

## Wire CHANGELOG.md into the release workflow

- **Kind:** deferred improvement.
- **Context:** `CHANGELOG.md` exists (Keep-a-Changelog, populated retroactively v0.2.1–v0.5.0 + Unreleased). The release pipeline (goreleaser) does not yet consume it or enforce that tagged releases promote the Unreleased section.
- **Why deferred:** needs a decision on mechanism (goreleaser changelog config vs. a tag-time check that Unreleased is non-empty and gets renamed to the version).
- **Acceptance:** tagging a release promotes the Unreleased section to that version heading, and the release notes are generated from it.
