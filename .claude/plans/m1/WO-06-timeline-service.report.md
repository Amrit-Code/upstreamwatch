Status: DONE
Work order: .claude/plans/m1/WO-06-timeline-service.md
Files changed:
- internal/timeline/doc.go (created)
- internal/timeline/events.go (created): pure decisions: stateFromEvents, diffCommit/diffManifest, fold, filterDeps, unionPatterns
- internal/timeline/service.go (created): TargetSettings, Options, Service, NewService, Name, Run
- internal/timeline/events_test.go (created)
- internal/timeline/service_test.go (created)

Commands run (in the worktree, after the lead fast-forwarded it to 0b4f89a):
- `make check` -> exit 0
  ==> gofmt
  ==> vet
  go vet ./...
  ==> build
  go build ./...
  ==> test
  go test -race ./...
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	1.741s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	1.250s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	1.216s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	1.590s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/ingest	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/issues/github	1.245s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	1.198s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	1.629s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	1.544s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	1.424s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	2.806s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/timeline	1.189s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/upstream/depsdev	1.217s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/osv	1.217s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/web	1.322s
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
- `go test -race -count=1 -v ./internal/timeline/` -> exit 0 (filtered to result lines)
  --- PASS: TestRun_UnityAndAllowlist (0.00s)
  --- PASS: TestRun_ParseErrorKeepsState (0.00s)
  --- PASS: TestRun_RewrittenHistory (0.00s)
  --- PASS: TestRun_NoNewCommits (0.00s)
  --- PASS: TestService_Name (0.00s)
  --- PASS: TestRun_Incremental (0.00s)
  --- PASS: TestUnionPatterns (0.00s)
  --- PASS: TestRun_ScopeUnknownRepo (0.00s)
  --- PASS: TestRun_DeletedManifest (0.00s)
  --- PASS: TestRun_ScopeOneRepo (0.00s)
  --- PASS: TestRun_ContextCancelled (0.00s)
  --- PASS: TestRun_OneTargetFails (0.00s)
  --- PASS: TestRun_NoManifestCommitsStillMovesIndexedSHA (0.00s)
  --- PASS: TestRun_NeverFetched (0.00s)
  --- PASS: TestRun_UnhandledFileSkipped (0.00s)
  --- PASS: TestRun_FirstRun (0.00s)
  --- PASS: TestRun_TooLargeSkipped (0.00s)
  --- PASS: TestRun_GitAndStoreErrorsFailTarget (0.00s)
  --- PASS: TestFilterDeps (0.00s)
  --- PASS: TestStateFromEvents (0.00s)
  --- PASS: TestDiff (0.00s)
  --- PASS: TestFold (0.00s)
  --- PASS: TestRun_ReadsBlobsInChunks (0.00s)
  PASS
  ok  	github.com/Amrit-Code/upstreamwatch/internal/timeline	1.102s
- `go test -race -count=1 -cover ./internal/timeline/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/timeline	1.231s	coverage: 98.6% of statements
- `go list -f '{{join .Imports " "}}' ./internal/timeline/` -> exit 0
  cmp context errors fmt github.com/Amrit-Code/upstreamwatch/internal/model log/slog slices strings
- `git status --porcelain` -> exit 0
  ?? internal/timeline/doc.go
  ?? internal/timeline/events.go
  ?? internal/timeline/events_test.go
  ?? internal/timeline/service.go
  ?? internal/timeline/service_test.go
  (the report file was written after this run)

Decisions:
- Unknown scope slug error text: `timeline: unknown target "<slug>"` (WO-05 leaves the ingest wording to its builder too; the two may differ).
- Per-target error wrapping, which makes up the `<error>` part of requirement 12: `read repo: `, `read head: `, `check ancestry: `, `list events: `, `list manifest commits: `, `read blobs: `, `apply timeline: `.
- GetRepo errors other than model.ErrNotFound count the target as failed; ErrNotFound counts it as skipped.
- A failed target adds only `targets` and `failed` to the stats. Its walk counters (commits_walked, files_read, events_written, intervals, parse_errors, files_skipped, resets) are discarded, because nothing of that target was stored. Successful and skipped targets add all their counters.
- `files_read` counts blobs an extractor parsed successfully. A parse error counts in `parse_errors` only. A blob no extractor handles counts nowhere.
- `m` in "n of m targets failed" is the number of targets attempted (the `targets` stat). Without a cancellation that is every target in scope.
- When LastIndexedSHA is empty (first run), ListEvents is still called, as requirement 7's "otherwise" branch says. It is skipped only on a reset.
- Blobs are read in chunks of 500 and processed as they arrive, so at most one chunk of file contents is held in memory. A ReadBlobs result with a different count than requested is a target failure (`read blobs: got X blobs for Y refs`).
- A `removed` event with no open interval opens nothing (fold); an `added` while one is open closes it at that commit and opens a new one. Intervals with equal opening times keep commit order (stable sort).
- A manifest path whose last package was removed is dropped from the in-memory state; the Manifests map in the update still holds every path parsed in the run, including ones whose dependencies the ecosystem allowlist removed entirely.
- NewService does not check that `unity` is non-nil. Its doc comment says it must be non-nil when any target sets Unity (WO-08 wiring).
- Log messages (all Warn, attributes snake_case, the slug under `repo`): `timeline target skipped` (reason `not stored` | `never fetched`), `timeline history rewritten, rebuilding from root`, `timeline manifest too large, skipped`, `timeline manifest parse failed`, `timeline target failed`. No per-target info summary; the Runner records the stats.

Outside scope:
- The worktree started at 4dff154 without the groundwork commit 0b4f89a; the lead fast-forwarded it mid-task. No other issue found.
- Doc rows for docs-sync: none of the structure changed beyond what `docs/architecture.md` already lists for the timeline job (`timeline.Service`, GitSource and Extractor, the tables it writes). `docs/data-access.md` should list `timeline` as a user of RepoStore (GetRepo) and TimelineStore (ListEvents, ApplyTimeline) if it does not yet.

Concerns: none
Question or blocker: none

## Fix round 1

Status: DONE
Work order: .claude/plans/m1/WO-06-timeline-service.md
Files changed:
- internal/timeline/service.go (modified)
- internal/timeline/service_test.go (modified)

Findings addressed:
- WO06-001: Run now checks `ctx.Err()` after every target returns, success or failure. When it is set, Run returns the context's error unwrapped, so `errors.Is(err, context.Canceled)` holds even when the cancellation surfaced as the only or final target's failure. A failed target is still counted in `failed` and logged before the context error is returned. A finished final target followed by a cancellation now also returns the context error.
- WO06-002: indexTarget returns its accumulated counters on every path, and Run adds them before it handles a failure. `events_written` and `intervals` are set when the update is built, just before ApplyTimeline is called, so a failed ApplyTimeline still reports them. `resets` is counted when the rewrite is detected.

Tests added or changed:
- TestRun_ContextCancelledDuringOnlyTarget: Head cancels the context and returns a wrapped `context.Canceled` → `errors.Is(err, context.Canceled)`, stats targets 1, failed 1.
- TestRun_ContextCancelledDuringFinalTargetApply: the second target's ApplyTimeline cancels and returns a wrapped context error → `context.Canceled`; both targets' work is counted.
- TestRun_ContextCancelledAfterFinalTargetSucceeds: cancellation during a successful final target → `context.Canceled`.
- TestRun_OneTargetFails: alpha now walks three commits (one parsed, one parse error, one too large) before its ApplyTimeline fails; the stats keep alpha's commits_walked, files_read, parse_errors, files_skipped, events_written and intervals.
- TestRun_FailureInLaterBlobChunkKeepsCounters: 501 refs, the second ReadBlobs call fails → commits_walked 501, files_read 500, failed 1, no ApplyTimeline.
- TestRun_GitAndStoreErrorsFailTarget: per-row expected stats (commits_walked 1 for the blob-read failures).

Commands run:
- `make check` -> exit 0
  go vet ./...
  ==> build
  go build ./...
  ==> test
  go test -race ./...
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	(cached)
  ?   	github.com/Amrit-Code/upstreamwatch/internal/ingest	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/issues/github	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/timeline	1.232s
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
- `go test -race -count=1 -cover ./internal/timeline/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/timeline	1.239s	coverage: 98.1% of statements
- `go test -race -count=1 -v ./internal/timeline/` -> exit 0 (filtered to result lines)
  --- PASS: TestRun_ContextCancelledAfterFinalTargetSucceeds (0.00s)
  --- PASS: TestService_Name (0.00s)
  --- PASS: TestRun_UnityAndAllowlist (0.00s)
  --- PASS: TestRun_ContextCancelled (0.00s)
  --- PASS: TestRun_ContextCancelledDuringFinalTargetApply (0.00s)
  --- PASS: TestRun_RewrittenHistory (0.00s)
  --- PASS: TestRun_ParseErrorKeepsState (0.00s)
  --- PASS: TestRun_ContextCancelledDuringOnlyTarget (0.00s)
  --- PASS: TestUnionPatterns (0.00s)
  --- PASS: TestRun_ScopeUnknownRepo (0.00s)
  --- PASS: TestRun_Incremental (0.00s)
  --- PASS: TestRun_NoManifestCommitsStillMovesIndexedSHA (0.00s)
  --- PASS: TestRun_UnhandledFileSkipped (0.00s)
  --- PASS: TestRun_ScopeOneRepo (0.00s)
  --- PASS: TestRun_OneTargetFails (0.00s)
  --- PASS: TestRun_TooLargeSkipped (0.00s)
  --- PASS: TestRun_DeletedManifest (0.00s)
  --- PASS: TestRun_NoNewCommits (0.00s)
  --- PASS: TestFilterDeps (0.00s)
  --- PASS: TestRun_FirstRun (0.00s)
  --- PASS: TestRun_NeverFetched (0.00s)
  --- PASS: TestStateFromEvents (0.00s)
  --- PASS: TestRun_GitAndStoreErrorsFailTarget (0.00s)
  --- PASS: TestFold (0.00s)
  --- PASS: TestDiff (0.00s)
  --- PASS: TestRun_ReadsBlobsInChunks (0.01s)
  --- PASS: TestRun_FailureInLaterBlobChunkKeepsCounters (0.01s)
  PASS
  ok  	github.com/Amrit-Code/upstreamwatch/internal/timeline	1.103s
- `go list -f '{{join .Imports " "}}' ./internal/timeline/` -> exit 0
  cmp context errors fmt github.com/Amrit-Code/upstreamwatch/internal/model log/slog slices strings
- `git status --porcelain` -> exit 0
   A internal/timeline/doc.go
   A internal/timeline/events.go
   A internal/timeline/events_test.go
   A internal/timeline/service.go
   A internal/timeline/service_test.go
  ?? .claude/plans/m1/WO-06-timeline-service.report.md
  (the files show as staged; I ran no git write, so the staging came from outside this agent)

Decisions:
- The context error is returned as it is, not wrapped, matching the check before each target.
- When a target fails because of the cancellation, it still counts in `failed` and is logged before Run returns the context error.
- The round 0 decision that a failed target adds only `targets` and `failed` is withdrawn: the stats now count work done, as requirement 13 reads.

Outside scope: none
Concerns: none
Question or blocker: none

## Gate (lead, 2026-11-19)

make check rerun: exit 0; timeline tests ok; imports model and the standard library only. Lead read the diff/fold/state functions against requirements 1 to 3. Astra round 0: REVISE (WO06-001 cancellation identity lost on the final target; WO06-002 counters of a failed target dropped), both accepted. Fix round 1 rechecked: make check exit 0; Sonnet reviewer (Codex budget kept for WO-01): both findings addressed, one minor (context error returned after a successful final target) dismissed as matching requirement 12. Accepted.
