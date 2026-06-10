# Splitting a real file

A worked example: moving a 1208-line `app.go` — the heart of a Bubble Tea
TUI — into 11 files, then mirroring its 1460-line `app_test.go` the same
way. Every command below ran against real code; the result built, vetted,
and tested green.

Use this as a template for designing your own partition: the hard part is
not running sflit, it is deciding which declarations belong together and
writing the selectors that pick them.

## 1. Design the partition first

Read the file and group declarations by what they own, not by kind. In
`app.go` the natural seams were lifecycle stages and features:
construction, init, key handling, update, view, streaming, submission,
modals, session switching, test shims. Name each sink after what it owns —
after the split, the filenames are the map of the package.

What stays behind is the core: here, the `App` struct itself, the `Focus`
type, and small accessors — 141 lines.

## 2. Move feature clusters of methods

Most of the work is `-receiver` plus an anchored alternation: methods of
one type that form a feature.

```sh
sflit -source app.go -receiver App -regex '^(globalKey|toggleThinking|toggleToolOutput|cycleModel)$' -sink keys.go -move
sflit -source app.go -receiver App -regex '^(Update|forwardToSubModels)$'                           -sink update.go -move
sflit -source app.go -receiver App -regex '^View$'                                                  -sink view.go -move
sflit -source app.go -receiver App -regex '^(onSubmit|dispatchSlash|AwaitSlashDone)$'               -sink submit.go -move
sflit -source app.go -receiver App -regex '^(flushPendingText|handleStreamEvent|flushCompletedLines|markLastPairAbortedOnCancel|buildUsageFooter)$' -sink stream.go -move
```

Selector habits that pay off:

- **Anchor everything** (`^...$`). Unanchored patterns match substrings —
  `Update` would also pick `forceUpdateLayout`.
- **Alternation over multiple runs.** One explicit `^(a|b|c)$` list per
  sink documents the partition in your shell history.
- **A regex prefix groups a family.** `-regex '^handleModalChoice'` moved
  every modal-choice handler in one go.

## 3. Move a type together with its methods

`-receiver` alone selects the type declaration and all its methods:

```sh
sflit -source app.go -receiver sendWriter -sink submit.go -move
```

Types without methods work the same way — `-receiver` still selects the
type:

```sh
sflit -source app.go -receiver pendingToolCall -sink stream.go -move
sflit -source app.go -receiver Config          -sink new.go -move
```

## 4. Free functions, vars, and consts

`-regex` matches any top-level declaration by name — funcs, methods (by
method name, any receiver), vars, consts, types. Constructors and their
default-value helpers moved as one cluster:

```sh
sflit -source app.go -regex '^(New|defaultThemes|defaultUISource|defaultCellRegistry|defaultBridge|resolveModels)$' -sink new.go -move
```

Grouped `var`/`const`/`type` blocks are narrowed: the matching specs
travel, the siblings stay behind. (Exception: a const block using iota or
implicit expressions is a blocked split — move the whole block or refactor
it first.)

## 5. Exploit naming conventions

A suffix convention turns into a one-line sweep. Everything named `*ForTest`
went to a shims file:

```sh
sflit -source app.go -regex 'ForTest$' -sink test_shims.go -move
```

## 6. Mirror the tests

The same approach applied to `app_test.go` keeps test-file parity:
`stream.go` gets `stream_test.go`, `keys.go` gets `keys_test.go`, and so
on. The 1460-line test file dissolved into 11 feature-named test files and
was deleted once every declaration had moved.

One guard you will meet here: a sink's package must match the source's. An
attempt to move `package app_test` declarations into a `package app` file
fails fast with a package mismatch — pick a sink with the right package
clause.

## 7. When a move turns out wrong

A reversal is the same move with source and sink swapped:

```sh
sflit -source keys.go -regex '^cycleModel$' -sink app.go -move
```

sflit keeps no history; the reversal is just another move, with the same
guarantees.

## Result

| File          | Before     | After                          |
| ------------- | ---------- | ------------------------------ |
| `app.go`      | 1208 lines | 141 lines (core type + accessors) + 10 feature files |
| `app_test.go` | 1460 lines | deleted; 11 feature-named test files |

Each move is semantically accurate — re-parsed and reprinted through
`gofmt`, imports updated, comments traveling with their declarations — so
the whole split landed as pure-move commits with the build green after
every step.
