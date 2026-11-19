Status: DONE
Work order: .claude/plans/m0/WO-05-pipeline-runner.md
Files changed:
- internal/pipeline/runner.go (created)
- internal/pipeline/runner_test.go (created)
- .claude/plans/m0/WO-05-pipeline-runner.report.md (created)

Commands run:
- `make check` -> exit 0
  ```
  ==> gofmt
  ==> vet
  go vet ./...
  ==> build
  go build ./...
  ==> test
  go test -race ./...
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	1.164s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/config	[no test files]
  ?   	github.com/Amrit-Code/upstreamwatch/internal/model	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	1.099s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/query	[no test files]
  ?   	github.com/Amrit-Code/upstreamwatch/internal/sched	[no test files]
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
  ```
- `go test -race -count=3 ./internal/pipeline/` -> exit 0
  ```
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	1.096s
  ```
- `go test -race -cover ./internal/pipeline/` -> exit 0
  ```
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	1.195s	coverage: 95.9% of statements
  ```
- `go list -f '{{join .Imports "\n"}}' ./internal/pipeline | grep upstreamwatch` -> exit 0
  ```
  github.com/Amrit-Code/upstreamwatch/internal/model
  ```
- `git status --porcelain` -> exit 0 (run before the report file was written; the report file is the only addition since)
  ```
  ?? internal/pipeline/runner.go
  ?? internal/pipeline/runner_test.go
  ```

Decisions:
- Run lock is a one-slot channel (`chan struct{}`, capacity 1) rather than `sync.Mutex`: blocked channel senders are queued FIFO (arrival order) and `select` lets a waiter give up on `ctx.Done()`. `ctx.Err()` is checked before and after acquiring, so an already-cancelled caller never starts a run even when the lock is free.
- The unknown-job check happens before taking the lock, so it neither waits nor writes.
- Wait-cancellation error text: `run job <name>: wait for running job: <ctx err>`; no log line (the caller logs).
- Store error wrapping: `run job <name>: start run: ...`, `finish run <id> of job <name>: ...`, `raise job_failure finding for job <name>: ...`, `resolve job_failure finding for job <name>: ...`; joined after the job error with `errors.Join`.
- One recording context (`context.WithoutCancel(ctx)` + 10 s timeout) is shared by FinishRun and the finding call, and is used on every run, not only after cancellation.
- Cancellation: the job's own returned error is what is stored and returned (a job that returns `ctx.Err()` stores `context canceled`). A job that returns nil after its context was cancelled is recorded as succeeded, since it reported no failure.
- Panic text is `panic: <value formatted with %v>`; no stack trace is stored or logged. Stats on a panicked run are nil.
- Stats returned alongside a job error are passed to FinishRun as-is (partial counters kept); nil stats are passed as nil.
- `Run.Error` stores the full job error text; only the `RankReason` copy is cut to 500 bytes (rune boundary, via `utf8.RuneStart`).
- The `job finished` line puts each stat as a top-level attribute keyed by the stat name, in sorted key order, after `job`, `run_id`, `duration_ms`. `duration_ms` is measured with the injected clock.
- `NewRunner` also rejects a nil `Job` with an error (calling `Name()` on it would otherwise panic outside `main`).
- `Jobs()` returns a copy so callers cannot alter the registration order.
- Extra sanity tests beyond the listed ones: `TestRunner_Jobs`, `TestRunner_CancelledWhileWaiting` (req. 10 wait path), `TestTruncateUTF8`.

Outside scope:
- Docs rows that may need a sync once accepted: `docs/architecture.md` pipeline row is still accurate; `docs/interfaces.md` lists `Job` only. The Runner's exported surface (`NewRunner`, `Run`, `Jobs`, `JobFailureFingerprint`, `ErrUnknownJob`, `ErrDuplicateJob`) and the `job_failure|<job>` fingerprint format are not recorded in `docs/` yet; `mvp-plan/PLAN.md` describes the findings fingerprint as `kind|repo|package|advisory_or_issue`, which the job_failure form (per the Ruling) does not follow.
