Status: DONE_WITH_CONCERNS
Work order: .claude/plans/m0/WO-06-scheduler.md
Files changed:
- internal/sched/scheduler.go (created)
- internal/sched/scheduler_test.go (created)
- .claude/plans/m0/WO-06-scheduler.report.md (created, this report)

Commands run:
- `gofmt -l internal/sched` -> exit 0 (no output)
- `go test -race -count=1 -v ./internal/sched/` -> exit 0
  --- PASS: TestChain_RunsJobsInOrder (0.00s)
  --- PASS: TestTick_SkippedWhilePreviousRunning (0.00s)
  --- PASS: TestChain_NoJobsRunsNothing (0.00s)
  --- PASS: TestStart_LogsOneLinePerEntry (0.00s)
  --- PASS: TestChain_ContinuesAfterError (0.00s)
  --- PASS: TestStop_ReturnsOnContextDone (0.00s)
  --- PASS: TestStop_BeforeStartAndTwice (0.00s)
  --- PASS: TestStop_WaitsForRunningChain (0.00s)
  --- PASS: TestChain_StopsOnCancelledContext (0.00s)
  --- PASS: TestChain_PanicRecovered (0.00s)
  --- PASS: TestNew_Validation (0.00s)
  --- PASS: TestScheduler_FiresOnRealTicks (0.02s)
  PASS
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	1.139s
  (no skipped tests)
- `go test -race -count=1 -coverprofile=<scratchpad>/cover.out ./internal/sched/` then `go tool cover -func=...` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	1.244s	coverage: 100.0% of statements
  total:									(statements)	100.0%
- `make check` -> exit 0
  ==> gofmt
  ==> vet
  go vet ./...
  ==> build
  go build ./...
  ==> test
  go test -race ./...
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	1.186s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/config	[no test files]
  ?   	github.com/Amrit-Code/upstreamwatch/internal/model	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	1.187s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/query	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	1.139s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/store	[no test files]
  ?   	github.com/Amrit-Code/upstreamwatch/internal/web	[no test files]
  ==> arch-lint
  go run github.com/fe3dback/go-arch-lint@v1.19.2 check --output-color=false
  module: github.com/Amrit-Code/upstreamwatch
  linters:
     On | Base: component imports # always on
     On | Advanced: vendor imports # switch 'allow.depOnAnyVendor = false' (or delete) to on
     On | Advanced: method calls and dependency injections # switch 'allow.deepScan = true' (or delete) to on

  OK - No warnings found
  ==> import-check
  sh scripts/check-imports.sh
- `go test -race -count=3 ./internal/sched/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	1.181s
- `go list -f '{{join .Imports "\n"}}' ./internal/sched | grep -E 'upstreamwatch|robfig'` -> exit 0
  github.com/Amrit-Code/upstreamwatch/internal/model
  github.com/robfig/cron/v3
  (full import list: context, errors, fmt, internal/model, github.com/robfig/cron/v3, log/slog, slices, strings, sync, sync/atomic, time)
- `git status --porcelain` -> exit 0 (run before this report was written)
  ?? internal/sched/scheduler.go
  ?? internal/sched/scheduler_test.go

Decisions:
- Ticking uses robfig `cron.Cron` (created in New, jobs added and started in Start) with `cron.WithLocation(time.Local)` and `cron.WithLogger(cron.DiscardLogger)`, so the library never writes to stdout. Stop waits on the context `cron.Stop()` returns, because cron adds to its job wait group in its run loop before Stop takes effect, which our own WaitGroup could not do safely.
- Parser: `cron.NewParser(Minute|Hour|Dom|Month|Dow)` with no `Descriptor`, so `@every` and `@daily` are rejected, and six fields fail on the field count.
- A `TZ=` / `CRON_TZ=` prefix is rejected by New. Requirement 2 says expressions are five fields evaluated in `time.Local`, and the prefix would override that. Also, robfig v3.0.1 panics on a prefix with no space after it, for example `TZ=UTC`. See Concerns.
- Start ctx reaches chains through the closure registered with cron. It is not stored in a struct field.
- Panic recovery wraps the whole chain, as the message `schedule chain panicked` and its attributes `schedule` and `panic` (no `job`) suggest. A panic therefore ends that chain, and later jobs in the same tick do not run. The entry's running flag is released, and the next tick runs normally. The `panic` attribute is `fmt.Sprint(p)`.
- The `error` attribute is `err.Error()`, matching the style in pipeline.Runner.
- "After Stop no further runner.Run call is started": Stop sets an atomic `stopping` flag before stopping cron, and each chain checks it next to `ctx.Err()` before every job. Stop does not cancel the running job's context; it waits for the job.
- Start after Stop is a no-op, and a second Stop running while the first still waits returns nil immediately. Both are documented on the methods.
- An entry with no jobs is still scheduled with cron. Its ticks run an empty chain.
- Validation messages: `schedule <name>: empty name|duplicate name|parse cron expression "<expr>": <cause>|job <i>: empty job name`. An entry with an empty name is labelled `at index <i>`. New returns the `errors.Join` result unwrapped, with no sentinels exported.
- Test seam: unexported `newScheduler(runner, entries, logger, parse parseFunc)`. The end-to-end test passes a parser that reads a Go duration ("20ms") as a constant-interval schedule. The Stop and panic tests run inside `testing/synctest` with the real five-field parser, where the fake clock reaches the next minute at once.
- Tests per function: New 13 table rows; Start 1; tick/runChain 8 (order, no jobs, error continues, cancelled before and during, skip, panic direct, panic keeps ticking); Stop 4 (waits, ctx done, before start, twice); end-to-end real ticking 1.

Outside scope:
- `go.mod` still marks `github.com/robfig/cron/v3 v3.0.1` as `// indirect`, but it is now a direct import of internal/sched. Build, vet and tests are unaffected. `go mod tidy` would drop the comment and move the line into a direct `require` block. go.mod is lead-owned, so I did not change it.
- Doc rows for docs-sync: `docs/architecture.md` "Schedules" gives intervals in prose ("every 6 h"), while the scheduler takes five-field cron in local time with no descriptors or TZ prefix. WO-07 should document the accepted syntax when it builds Entries from config.

Concerns:
- The TZ-prefix rejection is my reading of requirement 2. Requirement 1 says only "an expression the standard five-field parser rejects", and the robfig parser on its own accepts `CRON_TZ=Zone 0 8 * * *`. If the lead wants per-schedule zones, delete the prefix check in `parseFiveField` and the two `time zone prefix` rows in TestNew_Validation. The panic on a bare `TZ=UTC` would then need another guard.
- When a chain panics, the rest of that chain is skipped. The Runner already recovers job panics, so this only happens when the Runner itself is faulty, but it differs from the error path, where the chain continues.

## Fix round 1

Status: DONE
Work order: .claude/plans/m0/WO-06-scheduler.md (finding: Stop with an already-cancelled ctx returned the wrapped error with no chain running; ruling 2026-11-19 added to requirement 7)
Files changed:
- internal/sched/scheduler.go (modified)
- internal/sched/scheduler_test.go (modified)
- .claude/plans/m0/WO-06-scheduler.report.md (modified, this section)

Commands run:
- `go test -race -count=1 -v ./internal/sched/` -> exit 0
  --- PASS: TestTick_SkippedWhilePreviousRunning (0.00s)
  --- PASS: TestStart_LogsOneLinePerEntry (0.00s)
  --- PASS: TestStop_CancelledContextWhenIdle (0.00s)
  --- PASS: TestTick_AfterStopStartsNoChain (0.00s)
  --- PASS: TestChain_RunsJobsInOrder (0.00s)
  --- PASS: TestChain_NoJobsRunsNothing (0.00s)
  --- PASS: TestStop_ReturnsOnContextDone (0.00s)
  --- PASS: TestChain_ContinuesAfterError (0.00s)
  --- PASS: TestStop_BeforeStartAndTwice (0.00s)
  --- PASS: TestStop_WaitsForRunningChain (0.00s)
  --- PASS: TestChain_StopsOnCancelledContext (0.00s)
  --- PASS: TestChain_PanicRecovered (0.00s)
  --- PASS: TestNew_Validation (0.00s)
  --- PASS: TestScheduler_FiresOnRealTicks (0.02s)
  PASS
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	1.312s
  (no skipped tests)
- `go test -race -count=200 -run 'TestStop' ./internal/sched/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	1.167s
- `go test -race -count=3 ./internal/sched/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	1.190s
- `go test -race -count=1 -coverprofile=<scratchpad>/cover.out ./internal/sched/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	1.237s	coverage: 98.8% of statements
- `make check` -> exit 0
  ==> gofmt
  ==> vet
  go vet ./...
  ==> build
  go build ./...
  ==> test
  go test -race ./...
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	(cached)
  ?   	github.com/Amrit-Code/upstreamwatch/internal/config	[no test files]
  ?   	github.com/Amrit-Code/upstreamwatch/internal/model	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	(cached)
  ?   	github.com/Amrit-Code/upstreamwatch/internal/query	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	1.142s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/store	[no test files]
  ?   	github.com/Amrit-Code/upstreamwatch/internal/web	[no test files]
  ==> arch-lint
  go run github.com/fe3dback/go-arch-lint@v1.19.2 check --output-color=false
  module: github.com/Amrit-Code/upstreamwatch
  linters:
     On | Base: component imports # always on
     On | Advanced: vendor imports # switch 'allow.depOnAnyVendor = false' (or delete) to on
     On | Advanced: method calls and dependency injections # switch 'allow.deepScan = true' (or delete) to on

  OK - No warnings found
  ==> import-check
  sh scripts/check-imports.sh
- `go list -f '{{join .Imports "\n"}}' ./internal/sched | grep -E 'upstreamwatch|robfig'` -> exit 0
  github.com/Amrit-Code/upstreamwatch/internal/model
  github.com/robfig/cron/v3
- `git status --porcelain` -> exit 0
   M internal/sched/scheduler.go
   M internal/sched/scheduler_test.go

Decisions:
- The Scheduler now counts running chains itself. Under `s.mu`, `tick` checks `stopped` (and returns without starting a chain if it is set), checks and sets the entry's `running` flag (now a plain bool guarded by `s.mu` instead of an atomic), and increments `active`. A deferred `leave` resets the flag, decrements `active`, and closes `s.idle` when the count reaches zero.
- `Stop` sets `stopped` under the lock. If `active > 0` it creates `idle`, then calls `cron.Stop()` to end the tick loop. It does not wait on the context cron returns. With no chain active it returns nil, whatever the state of ctx. Otherwise it waits for `idle` or `ctx.Done()`. When ctx is done it checks `idle` once more without blocking, so a chain that has already returned is not reported as still running, which could otherwise happen because select picks at random between two ready cases.
- The atomic `stopping` flag is gone: `runChain` reads `stopped` through `isStopped()` before each job, so `sync/atomic` is no longer imported.
- Added `TestTick_AfterStopStartsNoChain` for "ticks that fire after Stop began must not start a chain", alongside the required `TestStop_CancelledContextWhenIdle`.

Outside scope: none new.
Concerns: none new. The two concerns from the first round still apply.
