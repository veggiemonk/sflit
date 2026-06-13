# ADR-0002: Batch mode takes a JSON plan — one source, N sinks, one N+1-file commit

Date: 2026-06-13

## Status

Proposed. Builds on ADR-0001 (Option E there, deferred to this ADR); blocked
behind the commit-atom fold tracked in TODO.md ("Make written-implies-locked+verified
structural in the commit atom"). Paired with ADR-0003 (`analyze`): its
go-api-style map emits selection keys, which this ADR's plan consumes via the
`keys` selector — so `analyze` should land before that selector ships.

## Context

ADR-0001 chose optimistic concurrency (Option D) for *many brains with no
global plan* — independently fanned-out agents — and deferred Option E,
batch/plan mode, for the other orchestration shape: *one brain with a complete
plan*. This ADR designs Option E.

The motivating shape is real and measured. The app.go experiment
(`docs/splitting-a-real-file.md`) split a 1208-line file into 11 feature
files (141 lines left in the source) using ~15 sequential single-shot
invocations. Every invocation re-parsed and re-rendered the shrinking source,
and — the dominant cost for the primary consumer, an LLM tool-use loop — every
invocation was a full tool-call round trip: emit command, wait, read result,
emit the next. An orchestrator that already knows the whole decomposition
pays N round trips for what is logically one transaction.

Batch mode collapses that to one call: parse the source once, validate that
all selections are disjoint **statically** (before anything is written), and
commit every sink plus the rewritten source as one atomic N+1-file write.
Contention-free by construction within the invocation; still wrapped in
ADR-0001's hash-verify + retry because the agent's own editor or a sibling
single-shot sflit can write mid-batch.

Forces:

- The CLI contract is published to agents via `--tool-schema`
  (`internal/mover/schema.go`); whatever plan format we pick becomes published
  surface that agents are prompted with and must emit reliably.
- Selection is a small closed vocabulary — `regex`, `receiver`,
  `receiver+regex` (`internal/mover/select.go`). A plan entry mostly needs
  repetition of that, plus one addition: ADR-0003's `analyze` emits explicit
  `declKeys`, so the plan gains a `keys` selector that consumes them directly,
  closing the analyze → plan → execute loop without regex round-tripping.
- The commit atom exists (`commit`, `runCommitWindow` in
  `internal/mover/write.go`) and already stages temps, locks sidecars in
  canonical order, verifies pre-image hashes, and renames in sequence.
  Generalizing it from 2 entries to N+1 is the intended reuse (ADR-0001,
  "How the two integrate").
- One source, one sink, single flags is the current `Config` shape; batch
  must not disturb the single-shot contract (exit 0/1/2, JSON result shape
  per call).

What triggered deciding now: the TODO.md entry's open item — "Needs its own
plan-format design" — is the only unresolved piece between ADR-0001's
integration sketch and an implementable feature.

## Decision

`sflit -plan <path>` reads a JSON plan (`-plan -` reads it from stdin),
applies N splits from one source in a single process, and commits all N sinks
plus the rewritten source in one generalized ADR-0001 commit atom. All other
behavior (retry on conflict, exit codes, `-json`, `-debug`) is unchanged.

### Plan format

```json
{
  "source": "app.go",
  "move": true,
  "splits": [
    { "sink": "keys.go",   "regex": "^key|^defaultKeyMap$" },
    { "sink": "view.go",   "receiver": "App", "regex": "^View$|^render" },
    { "sink": "stream.go", "receiver": "StreamState" },
    { "sink": "io.go",     "keys": ["readConfig", "App.flush", "errClosed"] }
  ]
}
```

- **One source per plan.** `source` is a single file. Each `splits[i]` entry
  is `{sink, <selector>}` where `<selector>` is exactly one of three forms,
  carrying the single-shot selection semantics (same narrowing of grouped
  specs, validated per entry as `Config.Validate` does today):
  - `regex` — name pattern, regex must compile;
  - `receiver` (optionally with `regex`) — receiver type, optionally narrowed;
  - `keys` — an explicit list of `declKeys` (`Foo`, `T.Foo`, `type T`,
    `var X`), each of which must resolve to exactly one declaration in the
    source.

  An entry must use **exactly one** selector form: `keys` is mutually
  exclusive with `regex`/`receiver` (mixing them is a usage error, exit 2),
  and at least one form must be present. The `keys` form is the precise,
  round-trip-from-`analyze` selector — the LLM names the exact declarations
  ADR-0003's map showed it, with no regex to author or mis-anchor.
- **`move` is plan-level**, not per-entry. Mixing copy and move entries
  against one source makes the source's post-state depend on entry
  interleaving; all-copy or all-move keeps the transaction's meaning
  order-independent.
- **Strict decoding.** Unknown fields are a usage error
  (`json.Decoder.DisallowUnknownFields`). The plan is written by an LLM; a
  typo'd `"recevier"` must fail loudly at exit 2, not silently select
  nothing. No `version` field: additions are gated by the strict decoder
  (an old binary rejects a newer plan loudly), and the schema's source of
  truth is `--tool-schema`.
- **Runtime knobs stay flags.** `-retries`, `-json`, `-debug` describe the
  run, not the split; they are not duplicated into the plan. `-plan` is
  mutually exclusive with `-source`/`-sink`/`-regex`/`-receiver`/`-move`
  (usage error, exit 2).

### Static disjointness validation

Before anything is written, after parsing the source once:

1. Run selection for every entry against the same AST. For `regex`/`receiver`
   entries this is the existing matcher; for `keys` entries it is a direct
   lookup of each listed key in the source's `declKeys` set.
2. Build each entry's matched-declaration key set (the existing `declKeys`
   vocabulary: `Foo`, `T.Foo`, `type T`, `var X`, `const X`).
3. Any key matched by two entries is an error naming both entries and the
   key — in copy mode too (overlapping copies are coherent in principle but
   rejected in v1; see revisit triggers).
4. Any entry matching nothing is the existing "no declarations matched"
   error, attributed to its entry. For a `keys` entry, a listed key that
   resolves to no declaration is a sharper, separately reported "unknown key
   `<key>`" error — the LLM gave an exact name, so an exact miss (typo, stale
   map) is named exactly rather than folded into "no matches".
5. Duplicate sinks across entries are rejected (one entry per sink; union
   selections belong in one entry's regex). `sink == source` is rejected.
6. Per-sink validation is the existing `validateRenderOp` pass (duplicate
   decls in sink after merge, import-alias collisions, build constraints,
   stranded refs), each attributed to its entry.

Every plan error is reported as `plan entry N (sink <path>): <existing
message>` so the orchestrator can fix one entry without re-deriving which
selection misfired.

### Commit and retry

One commit atom with N+1 entries: all sinks first (plan order), source last
— the same "failure leaves duplicates, never data loss" ordering as
`writePair`, generalized. Locks are acquired over all N+1 sidecar paths in
`canonicalLockOrder`; all pre-image hashes (source + every existing sink)
verify under the locks; then renames run in sequence. In copy mode the
source pre-image moves to `verifyOnly` exactly as Amendment 2 prescribes —
the lock set is the sinks only.

The whole plan is one optimistic transaction: any `errConflict` re-runs the
entire pipeline against fresh content inside the existing `Run` retry loop
(`internal/mover/mover.go`), bounded by `-retries`. This is well-defined for
the same reason as in ADR-0001 — selection is semantic, so re-selection
against changed content either converges or surfaces a defined error.

### Output

With `-json`, the success result generalizes the single-shot `Result`:

```json
{
  "source": "app.go",
  "move": true,
  "attempts": 1,
  "declarations_remaining": 9,
  "splits": [
    { "sink": "keys.go", "matched": ["keyMap", "defaultKeyMap"] },
    { "sink": "view.go", "matched": ["App.View", "App.renderHeader"] }
  ]
}
```

Exit codes unchanged: 0 success, 1 operation error, 2 usage/plan-shape error.
`--tool-schema` gains a `plan` section documenting the format, the
disjointness rule, and a worked example.

### Options considered

The *whether* was decided in ADR-0001 (Option E, deferred, complementary to
D). The decisions here are the plan transport/format and the plan scope.

#### Transport and format

| Option | Agent emits it reliably | Shell-safe for regexes | New surface to maintain | Validation story |
|---|---|---|---|---|
| A. JSON plan file / stdin — **chosen** | yes (native) | yes (no quoting) | one schema in `--tool-schema` | strict decode, exit 2 |
| B. Repeated flag groups (`-split 'sink=…,regex=…'`) | poorly | **no** (regex meta vs delimiter) | ad-hoc mini-grammar | hand-rolled |
| C. rf-style command script | moderately | partially | a DSL + parser | hand-rolled |
| D. YAML recipe (OpenRewrite-style) | moderately | yes | YAML dependency + schema | needs yaml lib |

**Option A — JSON file, `-plan -` for stdin. Chosen.** The consumer is an
LLM tool loop that already speaks JSON in both directions (`-json` out,
`--tool-schema` describing inputs); a JSON plan closes the loop — the
orchestrator can generate the plan as a structured tool-call argument with
schema validation on its side too. `encoding/json` strict decoding gives
exact, loud failure on malformed plans with zero new dependencies. Stdin
support (`-`) lets harnesses avoid temp-file lifecycle entirely.

**Option B — repeated flag groups.** No file, smallest-looking surface.
Rejected: selection regexes contain every character a `key=value,key=value`
mini-grammar would want as a delimiter, so this becomes an escaping scheme —
a worse format with a worse parser, and `flag` offers no help with grouped
repetition. Also the hardest shape for an agent to emit without quoting
mistakes.

**Option C — command script, rf-style.** [rsc.io/rf](https://pkg.go.dev/rsc.io/rf)
applies a script of refactoring commands (`mv T.Field T.field`, one per
line) and proves the shape works for human-driven batch refactoring.
Rejected: it is a DSL — a parser, a quoting story, and an error-reporting
layer we'd own — and its strength (human terseness in an editor buffer) is
not our consumer. An agent gains nothing from terseness and loses the
machine-checkable schema.

**Option D — YAML recipes.** [OpenRewrite](https://docs.openrewrite.org/reference/yaml-format-reference)
demonstrates declarative YAML at much larger scale (composable recipes,
framework migrations). Rejected for sflit: that power solves a problem we
don't have (a plan is a flat list of selections, not a recipe graph), and it
costs a YAML dependency — against the repo's "a little copying is better
than a little dependency" stance — plus YAML's well-known footguns as an
LLM emission target. [ast-grep](https://ast-grep.github.io/) and
[comby](https://comby.dev/) rule files are the same trade at smaller scale:
right for pattern-rewrite engines, oversized for a split list.

A note on precedent for the semantics rather than the syntax:
[JSON:API atomic operations](https://jsonapi.org/ext/atomic/) is the same
contract — an array of operations that "completely succeed or fail
together" — and Terraform's plan/apply popularized the noun. The format
decision is local; the all-or-nothing semantics are the established pattern.

#### Selector forms: regex, receiver, and keys

The plan inherits `regex` and `receiver` unchanged — they are the published
single-shot vocabulary and an LLM that reasons about a file in patterns
("everything matching `^render`") should keep expressing it that way. The
addition is `keys`, an explicit `declKeys` list, and the case for it is the
ADR-0003 round trip: `analyze` hands the LLM a map whose every line *is* a
key, so the most precise plan it can write is "move exactly these keys."
Forcing that through a regex means authoring a pattern that re-matches a set
the LLM already has by name — pure opportunity for a mis-anchored `^View`
that also grabs `ViewModel`, or an alternation that silently widens. `keys`
removes the guess: exact names in, exact decls out, and an exact "unknown
key" error when a name is stale.

Rejected alternative — **keys only, drop regex/receiver**: tempting for a
single contract, but it discards the case where the LLM legitimately wants a
pattern (a large or still-changing decl set it would rather match than
enumerate), and it makes `receiver`-based clustering — sflit's most natural
"move a type and its methods" gesture — verbose. The three coexist; an entry
picks one.

Rejected alternative — **a fourth `kinds` selector** (move all `var`s, all
`type`s): no evidence agents want kind-bucketed files, and `analyze` already
exposes kind per line if a future plan wants to filter on it. Not added
speculatively.

#### Plan scope: one source vs many

**One source per plan — chosen.** The disjointness check is a per-source
property (selections over one AST); the failure attribution is per-entry;
the commit is N+1 files around one source rewrite. This is exactly the
experiment's shape. Multi-source plans buy nothing the orchestrator can't
get by issuing one plan per source — plans against different sources touch
disjoint lock sets and don't interact (ADR-0001, "they compose without
knowing which scenario they're in") — while costing cross-source error
attribution, a larger lock set per commit, and a bigger blast radius for one
conflicting file forcing the whole transaction to retry. If a real need
appears, the format extends compatibly (a top-level `plans: []` wrapper)
rather than redesigning entries.

### Sequencing

Per TODO.md, two deferred items are scheduled as this work's opening moves,
in order:

1. **Fold `fileSnapshot` into `commitEntry`** ("written-implies-locked+verified
   structural") — batch generalizes the atom to N+1 entries and must land on
   the folded shape, where a written path without a verified snapshot is
   impossible by construction.
2. **Unify `applyMove`'s classification with `renderOp.travelSet`** — batch
   reworks `applyMove` to apply N selections' drops to one source AST in a
   single pass; do the unification in the same rework instead of twice.

Then: plan decode + validation, selection fan-out over one AST, N-sink
render, generalized commit, testscript coverage (including a plan-level
conflict-retry case via the existing `testHookBeforeCommit` seam), and the
`--tool-schema` `plan` section.

## Consequences

Easier:

- An orchestrator with a complete decomposition pays one tool round trip and
  one parse instead of ~15 (the app.go experiment becomes a single call);
  disjointness mistakes in its plan surface as one structured exit-2 error
  before anything is written, instead of as mid-sequence surprises.
- The atomic N+1 commit means a half-applied split cannot exist on disk:
  either every sink and the rewritten source land, or nothing does (modulo
  the documented "failure leaves duplicates, never data loss" rename
  ordering, now stated once for N files).
- Batch composes with ADR-0001 unchanged: two agents issuing plans against
  the same source serialize on the commit lock and converge via retry;
  single-shot and batch invocations interleave safely.

Harder / costs:

- A second published input surface. The plan schema joins the flag set as
  contract; selection features shared by both (regex/receiver) must be
  documented in `--tool-schema` for each. The `keys` selector is deliberately
  plan-only — it earns its place by pairing with `analyze`'s batch-oriented
  map, whereas a single-shot caller authoring one selection by hand is well
  served by regex/receiver — so the two surfaces are no longer identical
  vocabularies, which the schema must make clear.
- The commit atom's lock set grows from ≤2 to N+1 sidecars. Lock acquisition
  stays deadlock-free by canonical ordering, but ADR-0001's revisit trigger
  ("multi-file operations… re-examine whether a single per-directory lock is
  simpler than N ordered locks") activates with this feature — it should be
  answered during implementation, not after.
- Retry amplification: the optimistic transaction now spans N+1 pre-images,
  so the conflict surface (and the cost of each retry — full N-sink
  re-render) grows with plan size. At sflit file sizes this is still
  milliseconds; it matters only under sustained contention, which the
  one-brain shape makes unlikely by design.
- Plan-level `move` and one-entry-per-sink are deliberate v1 rigidities;
  both are relaxable later but are rejections an agent will occasionally
  hit.
- Error attribution machinery (entry index + sink in every validation error)
  threads through validation paths that today assume one sink.

### Revisit triggers

- If telemetry/usage shows orchestrators always pre-plan and the single-shot
  fan-out path goes unused, ADR-0001's standing trigger fires: consider
  simplifying retry machinery toward Option B semantics.
- If agents repeatedly hit the duplicate-sink rejection with plausible
  union-selection plans, allow multiple entries per sink (merge selections,
  attribute errors to all contributing entries).
- If a real workflow needs overlapping selections in copy mode (the same
  decl fanned into two sinks), relax the disjointness check to move mode
  only — the static check already has both modes' information.
- If multi-source transactions are requested with a concrete need one-plan-
  per-source can't serve, extend with a top-level `plans: []` wrapper.
- If N+1 ordered sidecar locks prove awkward in implementation or operation
  (lock-count limits, slow acquire under contention), switch to the
  per-directory lock ADR-0001 anticipated.
- If a selection need lands that none of the three forms (`regex`,
  `receiver`, `keys`) can express — e.g. kind-bucketed splits, or
  cross-cutting predicates over `analyze` annotations — that is the moment to
  design plan-format v2, not to bolt fields onto entries ad hoc.
