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

## Reject implicit-name import collisions (alias vs unaliased)

- **Kind:** deferred improvement (needs a decision on the heuristic).
- **Context:** `validateImportAliases` (`internal/splitter/validate.go`) only compares *named* imports on both sides. Four adjacent holes still write or produce broken sinks. Import-vs-import: (1) sink has `import "fmt"`, source has `import fmt "other/path"` — the carried named import redeclares `fmt` and the sink does not compile; (2) source has unaliased `import "fmt"` and the sink binds alias `fmt` to a different path — the moved code's `fmt.X` silently resolves to the *wrong package* (type error at best, silent miscompile if APIs overlap), because goimports sees the ident as already satisfied. Found in the deep-review pass of `e46c38c`. Decl-vs-unaliased-import (confirmed and reproduced in the 2026-06-12 verification review, cross-directory only — same-dir pre-states don't compile): (3) a moved decl named like a sink unaliased import's package (`func fmt() int` moved into a sink using `fmt.Println`) — goimports prunes the now-shadowed import and the written sink does not compile, exit 0; (4) the mirror direction, a sink decl shadowing the moved code's unaliased-import usage.
- **Why deferred:** detecting all four requires guessing the implicit package name of an unaliased import at parse level (no type info). `path.Base` is the goimports heuristic, but it false-positives when the package name differs from the path base (`go-foo` → `foo`, `yaml.v3` → `yaml`). Whether a rare false rejection is acceptable to close a wrote-non-compiling-output hole is a product call — the dot-import precedent says yes, but it should be decided, not slipped in.
- **Acceptance:** all four directions (sink-unaliased vs source-alias, source-unaliased vs sink-alias, moved-decl vs sink-unaliased-import, sink-decl vs source-unaliased-import-usage) are rejected with a message naming both sides, a red test exists for each, and the heuristic's false-positive class is documented in help/schema like the file-local stranded-refs caveat.

## Unicode-normalization aliases reopen the AB-BA lock deadlock

- **Kind:** blocked (needs a decision: dependency vs accepted gap).
- **Context:** `canonicalLockOrder` (`internal/splitter/write.go`) sorts by `(strings.ToLower(abs), abs)`. APFS is normalization-insensitive: NFC and NFD spellings of `café.go` name the same file but are neither exact-equal (no `slices.Compact` dedup) nor `EqualFold`-equal (the `lockAll` `os.SameFile` probe never fires). Demonstrated in the 2026-06-12 verification review: with a third path sorting between the two encodings, two processes lock the same pair in opposite orders and block forever in untimed flock — the same family the case-folding fix closed. The `write.go` comment "gives one global total order on every platform" is false on normalization-insensitive volumes.
- **Why deferred:** a correct shared fold needs Unicode normalization (`golang.org/x/text/unicode/norm`) — a new dependency, and copying NFC tables is not realistic. Scenario requires non-ASCII filenames reaching two concurrent runs in opposite spellings; consequence is a hang (kill releases flock; no data loss).
- **Acceptance:** a decision is recorded (add the dependency and NFC-normalize the sort key + dedup probe, or document the gap in ADR-0001 and the `canonicalLockOrder` comment); if fixed, a regression test pins identical lock order for NFC/NFD spellings.

## Make written-implies-locked+verified structural in the commit atom

- **Kind:** deferred improvement.
- **Context:** the commit atom is three parallel lists synced only by call-site convention: `commit.snaps` (locked+verified), `commit.verifyOnly` (verified), and `runCommitWindow`'s entries (written). Nothing checks entries ⊆ snaps — a mismatched call site writes a file outside the ADR-0001 lock+hash atom with no signal. `runOnce` (`internal/splitter/splitter.go`) also branches twice on `!cfg.Move` (commit construction, then writeSingle/writePair choice). Confirmed workable in the 2026-06-12 verification review: fold `fileSnapshot` into `commitEntry` (locks/verify derived from entries; `verifyOnly` stays a separate lock-free list), collapsing the duplicated branch.
- **Why deferred:** structural refactor of the commit seam; batch/plan mode (above) generalizes the same atom to N+1 entries and should land on the folded shape — do it as that work's first step.
- **Acceptance:** a written path that lacks a snapshot is impossible by construction (or fails loudly); `runOnce` classifies move-vs-copy once; full suite green.

## Commit-seam and validation cleanups (2026-06-12 verification review)

- **Kind:** deferred improvement.
- **Context:** three confirmed duplications. (1) `lockAll`'s case-alias probe (`internal/splitter/write.go`) re-opens the sidecar with flags/mode copy-pasted from `acquireFileLockInfo`, synced only by a nolint comment; a `sidecarAlreadyHeld` helper belongs in `lock.go` (which already hosts `acquireFileLockInfo` for this caller) and flattens the 6-deep probe nesting. (2) `validateImportAliases` Direction A hand-rolls package-level name extraction — a near-verbatim copy of `collisionKeys` that already diverges on blank handling — and Direction B filters methods by sniffing `strings.Contains(k, ".")` on the serialized key format; Direction A should consume `collisionKeys` (behavior-identical, verified) with a structural method filter. (3) the named-imports collection loop is copy-pasted at `validate.go` ×2 and `render.go` (`carryNamedImports`) — one `sourceNamedImports(f *ast.File)` helper; render's doc comment explicitly relies on validation mirroring its predicate.
- **Why deferred:** quality-only; out of scope for the verification pass that confirmed them.
- **Acceptance:** one sidecar-open site, one package-level-name rule, one named-imports predicate; full suite green.

## New sink could inherit the source's build constraints instead of rejecting

- **Kind:** deferred improvement (needs a decision).
- **Context:** `validateBuildConstraints` rejects moving from a build-constrained source into a *new* sink ("new sink without matching constraints"), pinned by `TestValidate_NewSinkRejectsBuildConstrainedSource`. But sflit writes the new sink from scratch — it could copy the source's `//go:build` lines into it and proceed, which is almost always what the user wants when splitting a `_linux.go` file.
- **Why deferred:** behavior change to a released rejection; interacts with filename-implied constraints (`_linux.go`, `_test.go` suffixes) which the constraint-line copy would not cover. Surfaced while pinning the branch in the test-improvement batch; the pin records current behavior, not desired behavior.
- **Acceptance:** a decision is recorded (keep rejecting vs inherit); if inherit: new sinks receive the source's constraint lines, the rejection branch only fires for existing sinks, help/schema/CHANGELOG updated, golden refreshed.

## Unify applyMove's travel classification with Plan.travelSet

- **Kind:** deferred improvement.
- **Context:** the 2026-06-12 cleanup batch extracted `Plan.travelSet()` (plan.go) and `validateNoStrandedRefs` now consumes it, but `applyMove` still runs its own index-based classification loop: `trimValueSpec` needs positional indices into `ValueSpec.Names`/`Values`, while `travelSet` returns name-keyed maps. The classification switch (`e.Origin == nil` / `o.Names == nil` / narrowed) therefore still exists in two shapes.
- **Why deferred:** full unification means reworking `applyMove`'s drop bookkeeping around indices derived from the shared helper; ADR-0001's batch mode will rework `applyMove` anyway — do both in one pass.
- **Acceptance:** one classification switch; `applyMove` derives its index maps from the shared helper (or the helper returns indices); full suite stays green.

## Remaining findings from the 2026-06 multi-agent quality review

- **Kind:** deferred improvement (explicitly scoped out of the agreed fix sequence — "only if the user asks / time permits").
- **Context:** the 70-agent review confirmed 65 findings; the four agreed batches (correctness, CLI/write/retries, doc pipeline, glossary sweep) landed on `docs/adr-0001-parallel-editing`. The rest, with repros and per-finding recommendations, live in the review artifact: `~/.claude/projects/-Users-julien-perso-sflit/d4f94d76-fb73-494f-b478-8a5d428d50f7/tool-results/bvz4m2ki0.txt`. Highlights: symlink handling in the commit atom and lock identity (`write.go`), a FuzzSplit target wiring the existing TypeCheckFiles + declKeys oracles, splitting 600-line `validate.go` with sflit itself (declkeys.go + strandedrefs.go), `slog.DiscardHandler`/`slices.Equal` cleanups, help-to-stdout and `-h` exit codes, JSON error payloads for `-json`, copy-mode wasted source render, unexporting pipeline internals (Plan/Match/Extracted/SemEqual), a `GOOS=windows go build` CI gate (vet fails on the testscript dep), dropping ratchet (redundant with renovate digest pinning), THIRD_PARTY notices for the vendored flock code, and the swallowed `enc.Encode` error.
- **Why deferred:** out of the approved scope for this pass; several need decisions (symlink policy, JSON error contract, package rename).
- **Acceptance:** each item either fixed with a red test, or explicitly rejected with a reason recorded here.
