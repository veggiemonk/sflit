# ADR-0003: `analyze` emits a go-api-style text map (including unexported decls) for LLM split planning

Date: 2026-06-13

## Status

Proposed. Pairs with ADR-0002 (batch/plan mode): `analyze` is the read
primitive that feeds the plan that ADR-0002's batch commit executes. Motivates
a `keys:` selector in the plan format (ADR-0002 revisit trigger), so it should
land before that selector.

## Context

sflit is the concurrency-safe **write** primitive for moving top-level
declarations between files of one package (ADR-0001 for the commit atom,
ADR-0002 for atomic N-split batches). The intended operator is an LLM
orchestrator. Its workflow is:

1. The user asks the LLM to split a large Go file.
2. The LLM needs a structural map of the file/package to decide the split.
3. The LLM writes a plan.
4. The LLM calls sflit to execute the plan atomically.

Step 2 has no tool. Today the LLM's only options are to **read the whole
file** into context — expensive on a 5000-line file, impossible to do
repeatedly across a package without blowing the budget — or to **guess** from
filenames. Reading also misses the hazards that make a split *wrong* rather
than merely ugly: splitting a `const (… iota …)` block silently changes
values; moving an `init()` reorders package initialization; moving a
build-tagged decl into an untagged sink breaks the build. An LLM skimming
1208 lines reliably overlooks these; a parser does not.

So sflit has a write primitive and no read primitive. This ADR adds the read
primitive: a command that produces a dense, structural map of what is in the
file, what references what, and what cannot be moved — sized to fit a tool
result, not the whole source.

Forces:

- **The consumer is an LLM, not a parser.** The output is *read by the model*
  to make a judgment, so it should optimize for model reading (token density,
  a grammar the model already speaks fluently), not for machine parsing.
- **Same-package moves are reference-preserving** (ADR-0001 background): a
  top-level name is visible package-wide regardless of which file holds it.
  So the genuine correctness hazards of a split are *file-local* — imports,
  build constraints, iota grouping, init order — not type resolution. The map
  must surface exactly those.
- **sflit is syntactic and local, by deliberate choice** (no type checker —
  it conflicts with the optimistic-concurrency model and pulls sflit onto
  gopls/rf's turf). `analyze` must hold that line: AST only, no package load,
  no `go/types`.
- **Don't overlap with standard tools.** gopls (LSP, interactive, web UI) and
  `go tool api` (exported surface, type-canonicalized) exist. `analyze` earns
  its place only by being the headless, agent-native, *includes-unexported*,
  *carries-the-dependency-graph* map that neither provides.
- The output is a published contract, like `--tool-schema`; every field is
  tokens on every line forever.

What triggers deciding now: ADR-0002 specified the executor but assumed the
LLM already has a plan. Closing the loop needs the sensor that produces the
plan's raw material — and the sensor's output vocabulary must match the
executor's selection vocabulary, so the two must be designed together.

## Decision

Add `sflit -analyze <target>` (a `.go` file or a directory/package). It parses
the target (read-only — no locks, no commit; orthogonal to ADR-0001's write
machinery) and prints a **go-api-style text map as Markdown** to stdout,
including **all declarations, exported and unexported**.

### The data-direction principle

This ADR establishes a rule that also governs ADR-0002:

> Data flowing **to** the LLM is human-readable text. Data flowing **from**
> the LLM into sflit (the plan) is strict JSON.

`analyze` output is read by the model → go-api-style text. The plan
(ADR-0002) is written by the model and validated by sflit's strict decoder →
JSON. It is the `git log` (readable out) / `kubectl apply -f` (structured in)
split. The direction of travel picks the format.

### `analyze` is a sensor, not a planner

`analyze` reports facts: what declarations exist, what references what, what
cannot be moved. It does **not** cluster declarations into proposed files or
name sinks — that is taste, and taste is the LLM's job. Keeping `analyze`
judgment-free keeps it deterministic and keeps sflit off gopls's
clustering/SCC-UI turf.

### Output format (the contract)

A Markdown document. The line grammar is the `go tool api` grammar
([api/README](https://github.com/golang/go/blob/master/api/README)) —
one declaration per line — with the per-line `pkg <path>,` prefix specialized
to the **source file** (the granularity the splitter operates at), and split-
relevant annotations appended as trailing tokens (the api format's `#nnnnn` /
`//deprecated` precedent for trailing metadata).

```markdown
# analyze app.go

main — 1208 lines, 47 decls (12 exported, 35 unexported)

## Declarations

app.go, type App struct  @42,78  refs:keyMap,streamState,Config
app.go, method (App) Init() tea.Cmd  @95,110  refs:streamState
app.go, method (App) Update(tea.Msg) (App, tea.Cmd)  @112,200  refs:App.handleKey,keyMap
app.go, method (App) View() string  @202,290  uses:lipgloss  refs:App.renderHeader,headerStyle
app.go, method (App) renderHeader() string  @292,320  uses:lipgloss  refs:headerStyle
app.go, func newApp(Config) App  @30,40  refs:App,defaultKeyMap
app.go, type keyMap struct  @462,480
app.go, var headerStyle lipgloss.Style  @497,500  uses:lipgloss
app.go, const C  @10,14  iota-group:C,D

## Receivers

App           Init View Update renderHeader handleKey   (5)
streamState   Next Done                                  (2)

## Constraints

iota   C,D @10,14        splitting the const block changes iota values
init   init @900,930     runs in file+source order; moving reorders package init
build  probeLinux @1100  //go:build linux — sink must carry the tag

## Cycles (cannot be separated)

App.Update ↔ App.handleKey
```

Rules:

- **Unexported declarations are included, first-class.** This is the decisive
  difference from `go tool api` and `go doc` (both exported-only). A split
  moves whole files' worth of *internal* structure; omitting unexported names
  would hide the majority of a typical file and make the map useless for
  planning. Lowercase names render identically to exported ones.
- **Syntactic signatures, from the AST — not type-canonicalized.** `go tool
  api` runs `go/types` and normalizes (`[]byte` → `[]uint8`). `analyze` has no
  type checker by design, so it emits signatures as written in source. For an
  LLM this is *better* (it matches what the model sees in the file) and it
  keeps `analyze` consistent with sflit being syntactic and local.
- **Per-line prefix is the file.** In single-file mode it is constant (and
  also stated in the header); in package mode (`-analyze ./pkg`) it is
  load-bearing — it tells the LLM which file each decl currently lives in,
  which is exactly what a package rebalance needs.
- **Source-position order within each file.** Deterministic (stable for a
  given input → cache-friendly and diffable) and mirrors reading the file
  top-to-bottom.
- **Two axes, visibly separated.** `refs:` (intra-package top-level keys a
  decl references) is the **cohesion** signal — it tells the LLM what clusters
  naturally, so the resulting files are sensible. `## Constraints` is the
  **correctness** signal — the file-local hazards that make a split invalid or
  semantics-changing. Same-package moves never strand at the type level, so
  these are genuinely different questions: refs for a *good* split, constraints
  for a *valid* one.
- **`refs:` is resolved syntactically** — walk each decl's AST idents, match
  against the package-level name set. Same documented-heuristic class as
  ADR-0001's stranded-refs caveat (a shadowing local or a field selector can
  produce a false edge). Acceptable: refs drive clustering judgment, where
  approximately-right is fine; they are not a correctness gate.
- **`uses:` (imports a decl needs) is advisory.** The executor computes
  carried imports at commit (ADR-0001/0002); `analyze` surfaces them only so
  the LLM can anticipate which sink pulls in a heavy import. Source of truth
  stays the executor.
- **`## Cycles` lists strongly-connected components of size > 1** as a neutral
  structural fact ("mutually recursive → cannot be cleanly separated"), framed
  as a constraint, never a suggestion. Often empty; that is fine.
- **Keys round-trip to selectors.** `method (App) View()` → sflit key
  `App.View`; `func newApp` → `newApp`; `type keyMap` → `keyMap`. One
  derivation rule (strip receiver `*`, drop params). This is what lets the LLM
  turn the map directly into a plan, and is the forcing function for a `keys:`
  selector in ADR-0002's plan format.

### Options considered

| Option | LLM-readable | Token cost | Includes unexported | Carries dep-graph + hazards | Headless / no extra deps | Overlaps standard tools |
|---|---|---|---|---|---|---|
| A. go-api-style Markdown text — **chosen** | high (native grammar) | low | yes | yes | yes (AST only) | no |
| B. JSON structural dump | medium | **high** (repeated keys) | yes | yes | yes | no |
| C. `go doc` / `go tool api` (exported API) | high | low | **no** | no | yes | **yes** |
| D. No tool — LLM reads the file | n/a | **highest** at scale | yes | **no** (hazards missed) | n/a | no |
| E. Reuse gopls `splitpkg` graph | low (web UI/LSP) | n/a | partial | yes (exported graph) | **no** (LSP, type checker) | **yes** |

**Option A — go-api-style Markdown. Chosen.** One decl per line in the
grammar the model already reads fluently, dense (no repeated JSON keys),
diffable (before/after-split verification falls out), deterministic. Extends
the api format minimally with the annotations a splitter needs. AST-only, no
new dependency.

**Option B — JSON structural dump.** This was the prior proposal (superseded
here). Rejected for output: JSON repeats field keys on every record, so it is
markedly more tokens for identical information, and an AST-shaped object is not
the model's native dialect. JSON is retained where it belongs — the *plan*
input (ADR-0002), per the data-direction principle.

**Option C — `go doc` / `go tool api`.** Already exists, already terse,
already the canonical Go signature notation. Rejected as the *whole* answer
because both are **exported-only** and neither carries a dependency graph or
hazards — fatal for split planning, where the unexported internals are most of
the file and the edges are the point. `analyze` borrows C's *grammar and
discipline* (one-per-line, sorted, signature-native) while inverting its
audience (restructurer, not consumer) and adding what it omits.

**Option D — let the LLM read the file.** Zero new surface. Fine for a small
file. Rejected as the general answer: it scales linearly in tokens (a
5000-line file or a whole-package rebalance does not fit), it is
non-deterministic (the model re-derives structure each time), and it misses
the deterministic hazards (iota, init order, build tags) that a parser catches
for free. `analyze` earns its keep precisely at scale and on hazards; for tiny
files, reading directly remains reasonable and `analyze` is optional.

**Option E — reuse gopls `splitpkg`.** gopls already computes a symbol
dependency graph for its split-planning web UI (validating that the graph is
the right artifact). Rejected as the mechanism: it is an LSP server with a
browser UI, not a headless stdout primitive; it requires the type checker;
its graph is package/exported-oriented; and adopting it would put sflit
squarely on a standard tool's path. `analyze` is the headless, single-file-
friendly, includes-unexported, syntactic counterpart that emits sflit's own
selection vocabulary so the output round-trips into the executor.

### How it integrates

- **Shared vocabulary is the spine.** `analyze` emits keys in the exact
  `declKeys` form sflit's selector consumes (`Foo`, `T.Foo`, `type T`,
  `var X`). Sensor output and executor input speak one language.
- **Closes the loop with ADR-0002.** Because `analyze` emits explicit keys,
  the most precise plan an LLM can write is "move exactly these keys" — which
  is the `keys:` plan selector flagged as a revisit trigger in ADR-0002. Build
  `analyze` first; the `keys:` selector follows from it.
- **Orthogonal to ADR-0001.** `analyze` is read-only: no sidecar locks, no
  hash-verify, no commit window. It cannot conflict with concurrent writers
  because it never writes; a map taken against a file mid-edit is simply a map
  of that moment, and the plan it informs is validated and committed under
  ADR-0001/0002's machinery regardless.

## Consequences

Easier:

- An LLM can plan a split of a 5000-line file (or rebalance a package) from a
  ~150-line structural map instead of the full source — the difference between
  fitting in a tool result and blowing the context budget.
- The deterministic hazards an LLM overlooks (iota value shifts, init
  ordering, build-tag splits, mutual-recursion cycles) are surfaced
  mechanically, so plans are valid by construction more often and the
  executor rejects less.
- The map's keys round-trip directly into the plan, reducing selection
  mistakes (no regex-guessing to match a decl set the LLM can already name).
- `analyze` before and after a split diffs cleanly (go-api lineage), giving a
  cheap verification that exactly the intended decls moved.

Harder / costs:

- A second published output contract beside `--tool-schema`. Every annotation
  is tokens on every line forever; the format must be kept lean and versioned
  in the schema docs. Ship core (`@span`, `refs:`) and advisory (`uses:`)
  only; resist additions until an agent demonstrably needs them.
- `refs:` is syntactic and so has a documented false-edge class (shadowing
  locals, field selectors that look like method refs). It informs clustering,
  not correctness, which bounds the damage — but the caveat must be documented
  like the stranded-refs one.
- Source-position order and the file prefix mean package-mode output grows
  with the package; very large packages produce large maps (still far smaller
  than the source). A future `-analyze` scope filter may be needed.
- Two output modes to keep coherent (single-file vs package); the file prefix
  generalizes them but the header summary differs.

### Revisit triggers

- If agents routinely need a decl's body (not just its signature) to cluster
  well, add an opt-in `-analyze -bodies` rather than bloating the default —
  the default's value is that it omits bodies.
- If the syntactic `refs:` false-edge rate misleads clustering in practice,
  reconsider a *scoped* (single-file/package, still no full type-check)
  identifier resolution pass to tighten edges — but only if it stays local and
  dependency-free.
- If `analyze` and the ADR-0002 `keys:` selector are both shipped and the
  regex/receiver selectors go unused, simplify the plan format toward
  key-lists as the primary selector.
- If package-mode maps grow unwieldy on large packages, add a scope/filter
  (by file, by receiver, by exported-ness) to `-analyze`.
- If a future Go tool emits an unexported-inclusive, dependency-annotated,
  headless structural map in a stable format, re-evaluate whether `analyze`
  should wrap it instead of computing its own (consistent with ADR-0001's
  "drop the copied code when the stdlib gains the API" stance).
