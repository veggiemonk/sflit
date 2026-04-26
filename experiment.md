# sflit run log — app.go split

Format: `invocation | exit | notes`

## app.go → 11 files (source split)

Historical run note: entries mentioning regex-only method skips describe pre-`cf6841e`
behavior. Current `-regex` matches funcs, methods by method name, vars, consts,
and types.

- `-sink session_switch.go -receiver App -regex '^switchSession$' -move` — OK.
- `-sink new.go -regex '^(New|defaultThemes|defaultUISource|defaultCellRegistry|defaultBridge|resolveModels)$' -move` — OK for free funcs in the historical run; current `-regex` also matches methods by method name.
- `-sink init.go -regex '^(Init|bannerCmd|BannerCmdForTest|emit|emitBlank)$' -move` — historical run matched free funcs only; current behavior also matches the `Init` method, so the follow-up `-receiver App -regex '^Init$'` is no longer needed.
- `-sink keys.go -receiver App -regex '^(globalKey|toggleThinking|toggleToolOutput|cycleModel)$' -move` — OK.
- `-sink update.go -receiver App -regex '^(Update|forwardToSubModels)$' -move` — OK.
- `-sink view.go -receiver App -regex '^View$' -move` — OK.
- `-sink stream.go -receiver App -regex '^(flushPendingText|handleStreamEvent|flushCompletedLines|markLastPairAbortedOnCancel|buildUsageFooter)$' -move` — OK.
- `-sink submit.go -receiver App -regex '^(onSubmit|dispatchSlash|AwaitSlashDone)$' -move` — OK.
- `-sink submit.go -receiver sendWriter -move` — OK (type + all methods).
- `-sink modal_choice.go -receiver modalChoice -move` — OK (type only; no methods on modalChoice).
- `-sink modal_choice.go -receiver App -regex '^handleModalChoice' -move` — OK.
- `-sink stream.go -receiver pendingToolCall -move` — OK (type with no methods).
- `-sink new.go -receiver Config -move` — OK (type with no methods).
- `-sink test_shims.go -regex 'ForTest$' -move` — historical run matched free funcs only; current behavior also matches methods by method name.
- `-sink test_shims.go -receiver App -regex 'ForTest$' -move` — historical follow-up for methods; no longer needed after `cf6841e` unless you want to restrict matches to `App` methods.

## Quirks found

1. ~~**`-regex` silently skips methods.**~~ **FIXED in cf6841e.** New
   semantic: `-regex R` matches any top-level decl by name — funcs,
   methods (any receiver), vars, consts, types. No more silent skips.
2. ~~**Free funcs + methods require two invocations.**~~ **FIXED in
   cf6841e.** Single `-regex` call now picks both.
3. ~~**`-regex` does not match top-level vars.**~~ **FIXED in cf6841e.**
   Grouped var/const blocks split spec-by-spec; multi-name specs
   (`var a, b = 1, 2`) split name-by-name.
4. **Sink package-mismatch guard is correct.** Attempting to append
   `package app_test` decls into a file that is `package app` fails
   fast with `sink package "app" does not match source package
   "app_test"`. Good behavior; keep it.
5. ~~**Orphan freestanding comments.**~~ **FIXED.** Trailing comment
   groups that sat after the last moved decl were stranded in the
   source. `extractMatches` now detects the all-moved-tail case and
   carries trailing comments with the last matched decl. Regression
   tests: `TestExtract_TrailingComment*` and
   `testdata/script/trailing_comment.txt`.

## Summary — source split

app.go 1208 → 141 lines (kept: Focus type, App struct, small accessors). 10 new files:
`session_switch.go`, `new.go`, `init.go`, `keys.go`, `update.go`, `view.go`, `stream.go`, `submit.go`, `modal_choice.go`, `test_shims.go`.

## Summary — test split

app_test.go 1460 → deleted (all decls moved; stub had 3 orphan comments, removed manually). 11 new or extended files:
`testhelpers_test.go`, `turn_end_test.go`, `session_switch_test.go`, `lifecycle_test.go`, `init_banner_test.go`, `stream_test.go` (+3 tests), `submit_test.go`, `new_external_test.go`, `keys_test.go`, `toggle_test.go`, `queue_test.go`, `window_test.go`, `slash_test.go` (+1 test).

All go build / go vet / package tests green. Preexisting lint in `cmd/docreport` unchanged, unrelated.

