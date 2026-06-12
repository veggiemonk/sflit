# Ubiquitous Language

Canonical vocabulary for `sflit`, a *declaration mover*: it moves Go
declarations between files, and **split** and **merge** are its two directions.
Use these terms in docs, help text, ADRs, commit messages, and code
identifiers.

## Operation

| Term            | Definition                                                                                  | Aliases to avoid                  |
| --------------- | ------------------------------------------------------------------------------------------- | --------------------------------- |
| **Declaration** | A top-level Go construct (func, method, type, var, const) — the unit sflit selects and moves | decl (ok in code), symbol, member |
| **Source**      | The Go file declarations are taken from                                                      | origin, input file                |
| **Sink**        | The destination Go file; created if absent, re-rendered if present                           | target, destination, output file  |
| **Selector**    | The criteria (`-regex`, `-receiver`, or both) that choose which declarations are selected    | filter, matcher, pattern          |
| **Copy**        | Writing selected declarations to the sink while leaving the source intact (the default)      | duplicate, extract                |
| **Move**        | A copy plus deletion of the selected declarations from the source (`-move`)                  | transfer, relocate, cut           |
| **Split**       | The file-level outcome (one file → many) of partitioning a file via a series of moves        | breakup, explode, shard           |
| **Merge**       | The opposite direction of a split: moving declarations from several files into one sink      | consolidation, join               |
| **Partition**   | The chosen mapping of a source's declarations onto target files — the design of a split      | layout, grouping, plan            |
| **Narrowing**   | Partial selection from a grouped var/const/type block: matching specs travel, siblings stay  | partial split, block split        |
| **Reversal**    | A move with source and sink swapped, restoring the prior layout — sflit keeps no history     | undo, rollback, revert            |

## Safety

| Term                  | Definition                                                                                              | Aliases to avoid           |
| --------------------- | -------------------------------------------------------------------------------------------------------- | -------------------------- |
| **Blocked split**     | A copy or move rejected before any write because it could silently change semantics or produce invalid Go | unsupported case, error    |
| **Collision**         | A selected package-namespace name already existing in the sink — a blocked split                          | conflict, duplicate name   |
| **Package mismatch**  | The sink's package clause differing from the source's — a blocked split                                   | wrong package              |
| **Same-directory copy** | Copying (without `-move`) into the source's own directory, which would duplicate names — a blocked split | in-place copy              |
| **Stranded reference** | A cross-directory operation tearing references apart: a moved declaration referencing a name that stays, or a staying declaration referencing a name that leaves — a blocked split (file-local check) | broken ref, dangling reference |
| **Alias collision**   | The sink importing a different path under an alias the source also uses — a blocked split                 | import clash               |
| **Directive block**   | Rejecting cross-directory operations on declarations carrying `//go:embed` or `//go:linkname` — a blocked split; same-directory moves carry the required blank import | pragma rejection           |
| **Semantic accuracy** | The guarantee that output preserves meaning (AST re-parsed, reprinted through gofmt), not bytes            | byte-for-byte, lossless    |
| **Orphan comment**    | A trailing comment group after the last moved declaration that must travel with it, not be stranded       | dangling comment, leftover |

## Concurrency (ADR-0001)

| Term                 | Definition                                                                                                  | Aliases to avoid              |
| -------------------- | ----------------------------------------------------------------------------------------------------------- | ----------------------------- |
| **Conflict**         | Another writer changing source or sink between parse and commit, detected by pre-image hash mismatch         | race, collision (names only)  |
| **Commit window**    | The short critical section under the sidecar locks: re-read, verify pre-images, rename temps into place      | critical section, lock window |
| **Pre-image hash**   | The SHA-256 of a file's bytes captured at parse, verified inside the commit window                            | snapshot hash, checksum       |
| **Sidecar lock**     | The per-file `.<name>.sflit.lock` advisory file lock held only for the commit window                          | lockfile, mutex               |
| **Retry**            | One full re-run of the pipeline after a conflict, bounded by `-retries`; selection is semantic, so re-runs converge | re-attempt, backoff loop |
| **Attempt**          | One pipeline execution; attempts = retries + 1 (the first run is not a retry)                                 | try, round                    |

## Workflow

| Term                     | Definition                                                                                       | Aliases to avoid          |
| ------------------------ | ------------------------------------------------------------------------------------------------ | ------------------------- |
| **Pure-move commit**     | A commit containing only sflit moves, verifiably free of behavioral change                        | mechanical commit, refactor commit |
| **Test-file parity**     | Keeping `_test.go` file layout mirrored with the source layout after a split                      | test sync                 |
| **Parallel-edit contention** | A large file acting as a serialization point where concurrent editors (agents or humans) collide | merge-conflict problem    |
| **File-size policy**     | A team rule that files over N lines must be split; sflit is the remediation tool                  | line limit, lint rule     |
| **Tool schema**          | The JSON tool definition emitted by `--tool-schema` for LLM tool-use loops                        | agent manifest, API spec  |
| **Plan mode** (proposed) | A batch input listing multiple `{selector, sink}` pairs applied atomically, all-or-nothing        | batch mode, script mode   |
| **Discovery mode** (proposed) | A read-only analysis that proposes a partition for an oversized file                          | analyze mode, suggest mode |

## Relationships

- A **Split** is one or more **Moves** from a single **Source**; each **Move** writes to exactly one **Sink**.
- A **Move** is a **Copy** plus deletion from the **Source**; **Copy** is the default.
- A **Selector** chooses **Declarations**; at least one of regex or receiver is required.
- A **Blocked split** rejects before writing — **Collision**, **Package mismatch**, **Same-directory copy**, **Stranded reference**, **Alias collision**, and the **Directive block** are kinds of blocked splits.
- A **Conflict** is not a **Collision**: a conflict is two writers racing on a file (resolved by **Retry**); a collision is two declarations sharing a name (a **Blocked split**).
- A **Pure-move commit** contains only **Moves**/**Copies** and relies on **Semantic accuracy** for its review guarantee.
- A **Partition** is what **Discovery mode** would propose and **Plan mode** would apply.
- A **Reversal** is an ordinary **Move**, not a separate operation.
- **Narrowing** happens within a **Move** or **Copy** when a **Selector** matches only part of a grouped block.

## Usage rules

- **Split** and **merge** are workflow nouns naming outcomes; **copy** and **move** are the only operation verbs.
- Instructions always say move or copy, never "split X into Y."
- A partially selected grouped block is **narrowed**, never split — **split** is file-level only.
- A move back is a **reversal**, never an undo — sflit keeps no history or state to undo from.

## Example dialogue

> **Dev:** "I ran a **split** on `app.go` but the **sink** already had a `Filter` func — it failed."
>
> **Domain expert:** "That's a **collision**, one of the **blocked splits**. sflit bails before writing anything; rename one side or pick a different **sink**."
>
> **Dev:** "And if I want the source to keep the declarations too?"
>
> **Domain expert:** "That's a **copy** — the default. But note a **same-directory copy** is also blocked, because the package would gain duplicate names. **Copy** targets another directory; same-directory splits need **move**."
>
> **Dev:** "My regex matched two consts inside a grouped block — what happens to the rest?"
>
> **Domain expert:** "The block is **narrowed**: the matching specs travel to the **sink**, the siblings stay in the **source**. Unless the block uses iota — partial selection there is a **blocked split**."
>
> **Dev:** "Can reviewers trust the result wasn't changed?"
>
> **Domain expert:** "Yes — **semantic accuracy** means the AST is re-parsed and reprinted, never edited textually. Land it as a **pure-move commit** and the reviewer only checks the **partition**, not the code. And if the **partition** was wrong, a **reversal** is just the same **move** with **source** and **sink** swapped."
