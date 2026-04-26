# TODO

Deferred-work ledger. Items ordered by priority — top is next. When work
cannot be completed (out of scope, blocked, needs a decision, deferred
improvement), append an entry here. Never silently drop incomplete work.

Entry format:

```
## <title>
- **Context:** what / where
- **Why deferred:** reason
- **Acceptance:** how we'll know it's done
```

---

## CI pipeline for forgejo remote

- **Context:** `origin` points at forgejo (`ssh://forgejo/eijlnu/sflit.git`).
  GitHub Actions workflow added under `.github/workflows/ci.yml`; forgejo
  has no equivalent config yet.
- **Why deferred:** forgejo Actions syntax differs slightly; needs a
  separate pass. Tests already run on the GitHub mirror.
- **Acceptance:** `.forgejo/workflows/ci.yml` runs `go build/vet/test` on
  push to main + PRs on the forgejo instance.

## Release tooling (goreleaser + tagged binaries)

- **Context:** `internal/version` already exposes a version string; no
  release pipeline wires tags → binaries.
- **Why deferred:** not blocking any user today; version string is
  populated from commit hash via `-ldflags`.
- **Acceptance:** `.goreleaser.yaml` builds darwin/linux amd64+arm64
  artifacts on tag push; release workflow uploads them to GitHub
  Releases.

## CHANGELOG.md tied to releases

- **Context:** no user-facing log of behavior changes. `experiment.md`
  tracks dev-only notes. The `-regex` semantics shift (commit `cf6841e`)
  was a breaking behavioral change with no changelog entry.
- **Why deferred:** waiting on release tooling above — changelog is most
  useful paired with tags.
- **Acceptance:** `CHANGELOG.md` follows Keep-a-Changelog, populated
  retroactively from git log; release workflow appends entries on tag.
