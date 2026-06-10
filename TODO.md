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

## CHANGELOG.md tied to releases

- **Context:** no user-facing log of behavior changes. `experiment.md` tracks dev-only notes. The `-regex` semantics shift (commit `cf6841e`) was a breaking behavioral change with no changelog entry.
- **Why deferred:** waiting on release tooling above — changelog is most useful paired with tags.
- **Acceptance:** `CHANGELOG.md` follows Keep-a-Changelog, populated retroactively from git log; release workflow appends entries on tag.
