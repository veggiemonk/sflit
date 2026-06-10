# ADR-0001: Optimistic concurrency with a commit-window flock for parallel sflit invocations

Date: 2026-06-10

## Status

Accepted (2026-06-10). Amended 2026-06-10: windows support is best-effort
(see Amendment 1).

## Context

sflit exists for agents: an orchestrator splitting a 5000-line file fans out
subagents, each running `sflit -source big.go -sink <topic>.go -regex <R> -move`.
Today each invocation is an unguarded read–modify–write: parse source → select →
render → temp-file + rename (`internal/splitter/write.go`). The temp+rename makes
each write **crash-safe** but nothing makes concurrent runs **isolation-safe** —
last writer wins.

The concrete failure: agents A and B both parse the original `big.go`. A moves
`^Filter` into `filter.go` and commits a new source. B moves `^Render` into
`render.go` — but B's source output was rendered from the *original* parse, so
B's commit resurrects every `Filter` declaration in `big.go` while they also
live in `filter.go`. Duplicate top-level names; the package stops compiling.
Worse, it fails *silently*: both invocations exit 0.

Forces at play:

- The primary consumer is an LLM tool-use loop. It must be safe to fan out N
  concurrent invocations without the orchestrator implementing its own mutex.
  Asking every agent harness to serialize calls is not a real option.
- The CLI contract (one shot, exit 0/1/2, JSON result) is published via
  `--tool-schema` and should not change shape.
- sflit selection is **semantic, not positional** (`-regex`/`-receiver`
  re-applied to a newer version of the file is still well-defined). This is the
  property that makes lock-free retry viable where a line-based patch tool
  would need full serialization.
- Self-contained binary, minimal dependencies ("a little copying is better
  than a little dependency").
- Must work on darwin and linux. Windows is best-effort (amended
  2026-06-10): the lock protocol must stay correct there, but it is verified
  by cross-compilation only, and unix-only refinements are acceptable when
  windows degrades gracefully instead of breaking.

## Decision

Adopt **optimistic concurrency with a short advisory lock held only around the
commit window**, plus bounded retry. Three pieces:

1. **Hash at parse, verify at commit.** `parseGoFile` reads the file bytes
   once, hashes them (SHA-256; file sizes make hash choice irrelevant), and
   parses from those bytes. The pre-image hash is recorded for the source
   *and* the sink (an existing sink can be appended to by another run). At
   commit, re-read and compare; on mismatch, abort the commit with nothing
   written.
2. **Per-file advisory lock around verify+rename only.** The verify-then-rename
   pair must be atomic against other sflit processes. Take an exclusive
   `flock` on a sidecar lockfile (`.<name>.sflit.lock`) — *not* on the target
   file itself, because temp+rename swaps inodes and a waiter would hold a
   lock on a dead inode. When both source and sink are locked, acquire in
   sorted-path order to rule out deadlock. The kernel releases `flock` on
   process death, so crashes leave no stale locks to clean up. The critical
   section is a re-read/hash plus two renames — microseconds — so contention
   exists in name only.
3. **Retry on conflict.** On hash mismatch, re-run the whole pipeline against
   the fresh content, bounded (default 5 attempts). Because selection is
   semantic, this converges: disjoint selections commute and both succeed;
   overlapping selections resolve to "no matches" or a sink collision on the
   later run — both already-defined exit-1 paths. The CLI surface is unchanged
   except an optional `Retries` knob on `Config`.

Implementation seams: `parse.go` (parse-from-bytes + hash), `write.go` (the
commit seam — one `commit` type owning the lock–verify–rename atom for both
`writePair` and `writeSingle`), and a retry loop wrapping the body of `Run` in
`splitter.go`. Platform lock code lives in `lock_unix.go` / `lock_windows.go`
(`syscall.Flock` / `LockFileEx`), copied (~40 lines, with attribution) from
[gofrs/flock](https://github.com/gofrs/flock) rather than imported.

Test strategy: a test-only hook on `Config` between plan and commit mutates the
source mid-flight and asserts attempt 1 detects the conflict and attempt 2
succeeds; an `errgroup` test launches N real concurrent `Run`s with disjoint
regexes and asserts the package still builds with every declaration existing
exactly once; a testscript case covers sequential back-to-back moves.

### Options considered

| Option | sflit-vs-sflit safe | Detects non-sflit writers | Stale-lock risk | Liveness under fan-out | New CLI surface |
|---|---|---|---|---|---|
| A. Status quo (document last-writer-wins) | no | no | — | n/a | none |
| B. flock around entire `Run` | yes | **no** | none | serialized (fine, runs are ms) | none |
| C. O_EXCL lockfile (git `index.lock` style) | yes | no | **yes** (crash leaves lockfile) | blocked until manual cleanup | none |
| D. Optimistic hash + commit-window flock + retry — **chosen** | yes | yes | none | retries, converges | optional `-retries` |
| E. Batch/plan mode (one invocation, N splits) | by construction | n/a | none | n/a | **new** plan format |

**Option A — do nothing, document the hazard.** Zero cost. Rejected because
the agent fan-out scenario is the tool's reason to exist, and the failure mode
is a silent exit-0 that breaks the build — the worst possible behavior for an
unattended agent loop.

**Option B — advisory lock around the whole run.** Simplest correct-ish
answer; a full run is milliseconds so serialization costs nothing. Rejected as
*insufficient rather than wrong*: it only serializes sflit against sflit. The
realistic environment has the same agent editing files with its own Edit tool
between sflit's parse and commit; only a content check catches that. Since the
hash verify is needed anyway, B is subsumed by D (D's lock *is* B's lock,
narrowed to the commit window).

**Option C — git-style `O_CREAT|O_EXCL` lockfile.** Proven pattern
([git api-lockfile](https://git-scm.com/docs/api-lockfile)). Rejected because
its crash story requires atexit/signal-handler cleanup and still leaks locks
on SIGKILL — git users know the stale `index.lock` dance well, and agent
sandboxes kill processes unceremoniously. `flock`'s release-on-death makes the
whole class of stale-lock recovery code unnecessary.

**Option D — chosen.** The hash check catches *any* writer (sflit or not);
the commit-window flock closes the check-then-rename race between sflit
processes (~30 lines); semantic re-selection makes retry well-defined. Pure
optimistic (no lock at all) was considered and rejected: two processes can
both pass the verify and still clobber — the window is tiny but real, and the
fix is cheap.

**Option E — batch/plan mode** (`sflit -plan plan.json`, parse once, validate
all selections disjoint, write everything in one process). Contention-free by
construction and the most token-efficient shape for an orchestrator that
pre-plans the entire split — the `experiment.md` app.go split (one agent, 11
sinks, ~15 sequential invocations, each re-parsing and re-rendering) is
exactly this shape collapsed to one call. Not a substitute for D: the two
address different orchestration shapes. Batch serves *one brain with a
complete plan* and validates disjointness **statically** — within one
invocation, a declaration matched by two selections errors before anything is
written. D serves *many brains with no global plan*, where no process sees all
selections, so disjointness is resolved **dynamically** (retry, "no matches",
sink collision). Deferred as a complementary feature (tracked in TODO.md).

How the two integrate — and why D builds first:

- **Batch reuses D's commit atom, generalized from 2 files to N+1.** D's
  commit is lock-in-sorted-path-order → verify all pre-image hashes → rename
  all (sinks first, source last, preserving `writePair`'s "failure leaves
  duplicates, never data loss" ordering). A batch commit is the same atom
  with more files; the multi-file revisit trigger below is batch mode
  arriving.
- **Batch still needs the hash-verify and retry.** A batch invocation is not
  immune to contention: the agent's own editor or a sibling single-shot sflit
  can write mid-batch. Without D, batch is safe only under an external
  "nothing else writes" guarantee no agent harness provides. With D, a batch
  is just a larger optimistic transaction — on conflict, the whole plan
  re-runs against fresh content, well-defined because selection is semantic.
- **They compose without knowing which scenario they're in.** Two agents
  issuing batches against the same file are serialized by the commit lock and
  converge via retry; batches against different files don't interact.

Built E-first instead, E would need its own multi-file atomic commit — which
*is* D's commit atom — while every single-shot invocation stayed unsafe in the
meantime. If orchestrators turn out to always pre-plan, D's retry machinery
becomes dead weight; that is carried as a revisit trigger, not assumed.

**Ecosystem note.** [gofrs/flock](https://pkg.go.dev/github.com/gofrs/flock)
is the standard Go cross-platform lock library (LockFileEx on windows via
`x/sys`); no meaningful alternative library exists, and the stdlib offers no
public API ([cross-platform file locking in Go](https://www.chronohq.com/blog/cross-platform-file-locking-with-go)).
Optimistic verify-then-commit is the same shape as HTTP `If-Match`/ETag and
database optimistic locking ([overview](https://vivekbansal.substack.com/p/how-to-implement-optimistic-locking)) —
sflit's twist is that semantic selection gives a natural, conflict-free retry.

## Consequences

Easier:

- Orchestrators can fan out N concurrent `-move` invocations on the same
  source with no external coordination; conflicts resolve by retry instead of
  silently corrupting the package.
- Edits made by non-sflit writers mid-run are *detected* (clean error or
  retry) instead of clobbered — this guards against the agent's own editor,
  not just sibling sflit processes.
- Crash behavior is unchanged or better: temp+rename stays; flock evaporates
  with the process; no lockfile janitor code.

Harder / costs:

- `parseGoFile` must parse from bytes so the hash and the AST come from the
  same read — a small refactor touching every parse call.
- Two platform-specific lock files to maintain (unix/windows), plus ~40 copied
  lines to attribute and keep an eye on. The windows file is best-effort
  (amended 2026-06-10): gated by `GOOS=windows` cross-compilation, no windows
  runtime CI.
- Concurrency tests are timing-sensitive by nature; the mid-commit mutation
  hook adds a test-only seam to `Config`.
- Retry re-runs the full pipeline (parse, select, render). At sflit file sizes
  this is milliseconds and irrelevant; it would only matter for pathological
  contention (hundreds of writers on one file).
- Sidecar lockfiles (`.<name>.sflit.lock`) are unlinked on release while the
  lock is still held; acquirers re-check after locking that fd and path
  still name the same file (`os.SameFile`) and retry on mismatch — naive
  unlink would let a third process lock a fresh inode while a waiter holds
  the dead one, two winners. The recheck adds an open/lock/stat retry loop
  to acquire. On windows the sidecar is never unlinked and litter remains
  (Amendment 1).
- `flock` is advisory: a writer that ignores both the lock and plain rename
  semantics can still race within the microsecond commit window. Accepted —
  every editor and tool in practice writes via rename or direct write, and the
  hash check bounds the damage to a detectable conflict.

### Revisit triggers

- If batch/plan mode (Option E) lands and telemetry/usage shows orchestrators
  always pre-plan, the retry machinery may be dead weight worth simplifying to
  Option B.
- If sflit grows multi-file operations (one invocation writing >2 files), the
  sorted-path two-lock scheme needs generalizing — re-examine whether a single
  per-directory lock is simpler than N ordered locks.
- If conflict retries are observed exhausting the bound in real agent runs
  (exit-1 "source changed" after 5 attempts), reconsider serializing the whole
  run per directory (Option B semantics) instead of raising the retry count.
- If sflit ever runs against files on NFS, `flock` semantics degrade —
  revisit with `fcntl`-style locks or require local filesystems explicitly.
- When/if the Go standard library gains a public cross-platform file-locking
  API, drop the copied platform code.
- If windows users materialize (bug reports, or windows runtime CI is added),
  re-promote windows to first-class: implement sidecar unlink with
  `FILE_DISPOSITION_POSIX_SEMANTICS` and keep the acquire-side identity
  recheck (`os.SameFile` already compares volume serial + file index on
  windows, so the shared acquire loop needs no change).

## Amendment 1 (2026-06-10): windows support is best-effort

Windows is demoted from a first-class target ("must work on darwin, linux,
and windows") to best-effort: the concurrency protocol must remain *correct*
on windows — the `LockFileEx` path in `lock_windows.go` stays, and concurrent
sflit runs still exclude each other there — but it is verified by
`GOOS=windows` cross-compilation only (no windows runtime CI), and unix-only
refinements are acceptable when windows degrades gracefully instead of
breaking.

The forcing decision is sidecar lock cleanup (previously deferred in
TODO.md). The litter is the one user-visible cost of the chosen design, and
sound cleanup is platform-asymmetric:

- **unix:** the release path unlinks the sidecar *while still holding the
  lock*; the acquire path, after locking, verifies the locked fd and the
  path still name the same file (`os.SameFile`, i.e. dev+inode) and retries
  on mismatch. Naive unlink without the recheck is unsound: a third process
  can lock a freshly created sidecar while a waiter holds the unlinked one —
  two lock holders. A few lines, fully testable here.
- **windows:** deleting an open file requires
  `FILE_DISPOSITION_POSIX_SEMANTICS` (Win10+, NTFS only) — more copied
  `x/sys` plumbing for a code path this repo cannot run-test. Rejected under
  best-effort support. The sidecar is simply never unlinked on windows;
  because nothing unlinks it, the fresh-inode race cannot occur and the
  acquire-side recheck passes trivially — the shared protocol stays uniform.

Consequence: lock files are cleaned up on darwin/linux; on windows they
remain (inert, safe to gitignore), and user-facing docs say so instead of
"left behind by design" on all platforms.
