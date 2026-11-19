Status: DONE_WITH_CONCERNS
Work order: .claude/plans/m1/WO-17-timeline-first-parent-and-rebuild.md
Files changed:
- internal/timeline/interfaces.go (modified: `IsAncestor` replaced by `OnFirstParentChain`, text as given in requirement 1)
- internal/timeline/service.go (modified)
- internal/timeline/events.go (modified)
- internal/timeline/exclude.go (modified)
- internal/timeline/service_test.go (modified)
- internal/timeline/events_test.go (modified)
- internal/timeline/integration_test.go (modified)
- internal/gitsrc/history.go (modified)
- internal/gitsrc/cli.go (modified)
- internal/gitsrc/blobs_test.go (modified)
- internal/gitsrc/history_test.go (modified)
- internal/gitsrc/remote_internal_test.go (modified)
- internal/gitsrc/history_internal_test.go (created)
- .claude/plans/m1/WO-17-timeline-first-parent-and-rebuild.report.md (created)

What was done, by requirement:
1. `gitsrc.CLI.OnFirstParentChain` runs `git rev-list --first-parent --end-of-options <head>` through the existing `run` path. Its stdout goes to a line scanner (`chainScanner`) that compares each line with `commit`. On a match, or when the next line would pass `MaxLogBytes`, the scanner cancels the context, which kills the git process group through the existing `cmd.Cancel`. It then refuses further writes. An unknown commit (or a non-full object name) gives false with no error. An unknown head is an error. The timeline resets (`Reset: true`, counted in `resets`, same Warn line as before) when `LastIndexedSHA` is set, differs from head and is not on the chain. `IsAncestor` stays in gitsrc, unused by the timeline.
2. `scope.Rebuild` makes `indexTarget` reset every target in scope: no chain check, no `ListEvents`, no zero-work shortcut, a walk from the root, `Reset: true`.
3. When not resetting, stored events pass through `dropFilteredEvents`, which applies `excludedPath` with the target's `ExcludePaths` and the non-empty `Ecosystems` allowlist. This happens before both `stateFromEvents` and `fold`. If head == `LastIndexedSHA`, ApplyTimeline still runs when at least one stored event was dropped. In that case no git walk happens: `ManifestCommits` is skipped when head == from.
4. `fold` still folds in the given order. A closing commit dated before the opening one gives `To = From`. The output sort is unchanged.
5. `defaultMaxBlobBytes` = 8 MiB (and the `Options` comment says so). `blobChunkSize` = 8.
6. `--sparse` is removed from the `ManifestCommits` log arguments, and the comment above them is updated. `TestManifestCommits_FirstParent`, `TestManifestCommits_FromAndDeletes`, `TestManifestCommits_LogCap` and `TestTimeline_FirstParentSemantics` all pass without it.
7. `scrub` now runs the existing secret scrub and `model.Redact`. It then replaces every `unicode.IsControl` rune except `\n` and `\t`, and every invalid UTF-8 byte, with `?` (a genuine U+FFFD is kept).

Tests added or updated:
- `TestTimeline_FastForwardAfterOursMerge` (integration). Before the service change it failed: resets 0, open interval `v1.2.0` at M, M still in `commits`. After the change it passes: resets 1, open interval `v1.0.0` from c1, and events, intervals and the `commits` table equal a fresh database's. The bump uses `v1.2.0` rather than the finding's `v2.0.0`, because `example.com/a v2.0.0` is not a valid Go module version without a `/v2` path and modfile rejects it, which produces no event.
- `TestTimeline_RetroactiveExclusion` (integration, beyond the list): after `exclude_paths` is tightened and no new commit lands, the store drops the excluded manifest's interval, keeps the other interval's ID, and keeps the events.
- `TestOnFirstParentChain`: head itself, first parent, first-parent ancestor, second-parent-only ancestor (false), unknown commit (false, no error), all-zero, abbreviated and empty names (false). Errors for an unknown head, a head that looks like an option, an empty head and a missing clone. Also `TestOnFirstParentChain_PrunedAfterForcePush` and `TestOnFirstParentChain_LogCap`.
- `TestChainScanner` (internal): matches split across writes, a prefix that is not a match, the limit boundary, and writes refused after a stop.
- `TestRun_Rebuild`: all targets, and one repo in scope.
- `TestRun_RetroactiveExclusion`: the path case, the ecosystem case, and filtered stored state diffed against new commits.
- `TestRun_ChainCheckErrorFailsTarget`.
- `TestFold_ClampsBackwardDates`, `TestDropFilteredEvents`.
- `TestReadBlobs_DefaultCap`: 9 MiB is `TooLarge`; 8 MiB and 2 MiB are read.
- Chunk tests: `TestRun_ReadsBlobsInChunks` now expects [8 8 1]; `TestRun_FailureInLaterBlobChunkKeepsCounters` uses 9 commits and expects 8 files read.
- `TestRunError_ReplacesControlCharacters`: `\x1b[2J` and U+009B come back as `?`; also CR, BEL, invalid UTF-8, kept `\n`/`\t`, and the secret scrubbed first.
- `TestRun_NoNewCommits` now expects one `ListEvents` call, because stored events are checked against the current rules on every run. It still expects no ApplyTimeline and no git calls beyond Head.

Commands run:
- `make check` -> exit 0
  ```
  go build ./...
  ==> test
  go test -race ./...
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract/unity	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/ingest	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/issues/github	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	(cached)
  ?   	github.com/Amrit-Code/upstreamwatch/internal/testutil/gitrepo	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/timeline	2.969s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/upstream/depsdev	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/osv	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/web	(cached)
  ==> arch-lint
  go run github.com/fe3dback/go-arch-lint@v1.19.2 check --output-color=false
  module: github.com/Amrit-Code/upstreamwatch
  linters:
     On | Base: component imports # always on
     On | Advanced: vendor imports # switch 'allow.depOnAnyVendor = false' (or delete) to on
    Off | Advanced: method calls and dependency injections # switch 'allow.deepScan = true' (or delete) to on

  OK - No warnings found
  ==> import-check
  sh scripts/check-imports.sh
  ```
  (The run before this one, uncached, also exited 0. gofmt printed nothing.)
- `go test -race -count=1 ./internal/gitsrc/ ./internal/timeline/` -> exit 0
  ```
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	8.431s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/timeline	3.045s
  ```
- `go vet -tags referee ./...` -> exit 0 (the referee tests still compile against the changed interface)
- `go test -race -count=1 -coverprofile=... ./internal/gitsrc/ ./internal/timeline/` -> exit 0: gitsrc 86.1%, timeline 98.9%
- `go test -race -count=1 -run TestTimeline_FastForwardAfterOursMerge ./internal/timeline/` before the service change -> exit 1 (resets 0, stale `v1.2.0` open interval, M left in `commits`), proving the test catches finding 1
- `git status --porcelain` -> only owned files and this report:
  ```
   M internal/gitsrc/blobs_test.go
   M internal/gitsrc/cli.go
   M internal/gitsrc/history.go
   M internal/gitsrc/history_test.go
   M internal/gitsrc/remote_internal_test.go
   M internal/timeline/events.go
   M internal/timeline/events_test.go
   M internal/timeline/exclude.go
   M internal/timeline/integration_test.go
   M internal/timeline/interfaces.go
   M internal/timeline/service.go
   M internal/timeline/service_test.go
  ?? internal/gitsrc/history_internal_test.go
  ```
  (plus this report file once written)

Decisions:
- A rebuild counts 1 in `resets` per target, like a chain reset, because both are `Reset: true` updates. A rebuild adds no log line of its own. The chain-reset Warn keeps its message "timeline history rewritten, rebuilding from root", so existing greps still match.
- The wrapped error for a failed chain check reads "check first-parent chain: ..." (it was "check ancestry: ..."), to name the method that now runs.
- `OnFirstParentChain` returns false, without running git, for a `commit` that is not a full lowercase object name, because rev-list prints only full names and nothing else can match. It validates `head` with `checkRevision`.
- The `MaxLogBytes` bound is applied per line: a line counts only if it ends within the limit, so a match past the limit is reported as the limit error, not as true.
- When head == `LastIndexedSHA` and filtering dropped events, `ManifestCommits` is not called; `Commits` in the update is an empty slice (from `dropExcluded`).
- `dropFilteredEvents` lives in `exclude.go`. It returns the input slice untouched when nothing is dropped, and a new slice otherwise, so a store-owned slice is never modified.
- The new interval-clamp and filter logic stays in the pure files (`events.go`, `exclude.go`). `stateFromEvents` and `fold` are unchanged apart from the clamp.

Outside scope:
- `docs/interfaces.md` (class diagram line 13, signature at line 71, prose at line 137, table at line 148) still lists `IsAncestor` as part of `timeline.GitSource` and describes its semantics. These lines need `OnFirstParentChain`, and gitsrc's method list should add it. docs-sync should pick this up.
- docs rows to sync: the blob cap default (8 MiB) and batch size (8) are already in `docs/decisions.md`. Check any doc that quotes the 1 MiB cap or the `--sparse` flag in the ManifestCommits description.

Concerns:
- Retroactive filtering re-applies on every run while the stricter rule stands. `dep_events` rows for excluded paths or ecosystems are kept, as the work order says, so each later run drops them again (`filtered > 0`) and calls ApplyTimeline with the same intervals and no new commits. This is idempotent (interval IDs are kept, verified by `TestTimeline_RetroactiveExclusion`), but it costs one write transaction per affected repo per run, indefinitely, until a rebuild clears the stored events. The run's stats show `commits_walked: 0, events_written: 0` with an apply behind them. Avoiding it would need the timeline to compare the folded intervals with the stored ones. `ListIntervals` is not in `timeline.TimelineStore`, and that file is lead-owned apart from the one change this order allowed, so I left it. Options for the lead: add `ListIntervals` to the interface, have ApplyTimeline skip unchanged interval sets, or accept the cost.
- Every zero-work run now calls `ListEvents` for every target, because finding 3 requires checking stored events against the current rules even when the head has not moved. For large histories this is a full read of `dep_events` per repo per scheduled run, where before there was none.

Question or blocker: none

## Gate (lead, 2026-11-19)

make check rerun: exit 0; gitsrc and timeline tests ok. Lead proved TestTimeline_FastForwardAfterOursMerge fails on the pre-WO-17 code (stale interval, off-chain commit M kept) and passes with it. Sonnet reviewer: ACCEPT. Its main minor (loosening a filter brings back stored events, because dep_events of filtered paths stay) is a work-order design gap: WO-20 replaces retroactive filtering with a rebuild whenever a target's indexing settings change. Other minors accepted (per-run ApplyTimeline while a filter stands, removed by WO-20; small test gaps). Accepted.
