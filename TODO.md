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

## Orphan standalone comments left behind on receiver moves
- **Context:** `internal/splitter/extract.go` + `render.go`. Observed while
  splitting `app.go` / `app_test.go` (see `experiment.md` quirk 5). When a
  receiver-bundled move (`-receiver T`) extracts methods, freestanding
  comment groups that sit between decls (e.g. `// tea.sequenceMsg is
  unexported…`) are sometimes left behind in the source even though their
  anchor decl moved.
- **Why deferred:** cosmetic only; `goimports` doesn't touch them and
  build/tests stay green. Needs a comment-association pass in the
  extractor.
- **Acceptance:** add a testdata case with an orphan comment above a
  moved method; comment travels with the decl; source has no dangling
  comment.

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

## Expand README with `--tool-schema` + agent integration guide
- **Context:** `--tool-schema` emits a JSON tool definition with
  examples (see `internal/splitter/schema.go`). README only mentions
  the flag name.
- **Why deferred:** initial pass landed Installation + flag surface.
  Agent integration doc needs a worked example (Claude / OpenAI tool
  use payload) which merits its own section.
- **Acceptance:** README section showing how to feed `--tool-schema`
  output to an LLM tool-use loop; link to `internal/splitter/schema.go`.
