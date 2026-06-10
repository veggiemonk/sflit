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

## CHANGELOG.md tied to releases

- **Context:** no user-facing log of behavior changes. `experiment.md` tracks dev-only notes. The `-regex` semantics shift (commit `cf6841e`) was a breaking behavioral change with no changelog entry.
- **Why deferred:** waiting on release tooling above — changelog is most useful paired with tags.
- **Acceptance:** `CHANGELOG.md` follows Keep-a-Changelog, populated retroactively from git log; release workflow appends entries on tag.
