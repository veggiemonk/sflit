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
