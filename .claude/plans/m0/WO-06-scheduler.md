# WO-06: In-process cron scheduler

Milestone: M0
Agent: go-builder   Model: opus
Depends on: WO-01, WO-02 (merged)
Report file: `.claude/plans/m0/WO-06-scheduler.report.md`
Ticket: UPW-16

## Objective

`sched.Scheduler` turns cron expressions from the config into calls on the Runner: each schedule entry names an ordered chain of jobs, and on every tick the chain runs one job after another. It contains no job logic and records nothing itself; the Runner does that. Serves M0 checklist items 15 and 16.

## Owned files

- `internal/sched/` (all files except `interfaces.go`; keep the package comment in `doc.go`)

Lead-owned, read-only for you (stop and report if one is wrong):

- `internal/sched/interfaces.go`
- `internal/model/`, `internal/pipeline/`
- `go.mod`, `go.sum` (`github.com/robfig/cron/v3 v3.0.1` is already required), `.go-arch-lint.yml`, `Makefile`, `scripts/`

## Interfaces

### Consumed

```go
// internal/sched/interfaces.go
type Runner interface {
	Run(ctx context.Context, job string, scope model.Scope) (model.Run, error)
}
```

`github.com/robfig/cron/v3` for cron parsing and ticking. Do not import `internal/pipeline`; the interface above is all the scheduler needs.

### Produced

```go
// internal/sched

// Entry is one schedule: a cron expression and the jobs it runs, in order.
type Entry struct {
	Name string   // the config schedules key, for example "ingest"
	Cron string   // five-field cron expression, evaluated in the process's local time zone
	Jobs []string // job names handed to the Runner, in order
}

func New(runner Runner, entries []Entry, logger *slog.Logger) (*Scheduler, error)
func (s *Scheduler) Start(ctx context.Context)
func (s *Scheduler) Stop(ctx context.Context) error
```

## Requirements

1. `New` validates every entry and returns all problems joined with `errors.Join`: empty `Name`, duplicate `Name`, an expression the standard five-field parser rejects (the error names the entry and the expression), an empty job name inside `Jobs`. An entry with no jobs is valid: it is registered and logged but runs nothing. `New` does no I/O and starts nothing.
2. Expressions are standard five-field cron (minute, hour, day of month, month, day of week) in the local time zone (`time.Local`, so the container's `TZ` applies). Six-field (seconds) expressions and `@every`/`@daily` descriptors are rejected by `New`.
3. `Start` logs one `Info` line per entry, message `schedule registered`, with attributes `schedule` (the name), `cron` (the expression) and `jobs` (the job names joined with a comma, empty string for none), then starts ticking. It returns immediately. Calling `Start` twice is a no-op on the second call.
4. On a tick, the entry's jobs run in order through `runner.Run(ctx, job, model.Scope{})`, where `ctx` is the one given to `Start`. A job that returns an error does not stop the chain: the next job still runs. The scheduler logs one `Warn` line per returned error, message `scheduled run returned error`, with `schedule`, `job` and `error`; it does not otherwise interpret the error.
5. Before each job in a chain the scheduler checks `ctx.Err()`; once the context is cancelled the rest of the chain is skipped.
6. If an entry's previous chain is still running when its next tick fires, that tick is skipped and one `Warn` line is logged (`schedule tick skipped, previous run still in progress`, with `schedule`). Different entries may tick concurrently; the Runner serialises the jobs.
7. `Stop` stops new ticks, then waits until every chain already running has returned, or until `ctx` is done, in which case it returns `ctx.Err()` wrapped. `Stop` before `Start`, and a second `Stop`, return nil. After `Stop` no further `runner.Run` call is started.
8. A panic inside a chain (for example from a faulty Runner) is recovered, logged at `Error` (`schedule chain panicked`, with `schedule` and `panic`), and does not stop the scheduler.
9. `internal/sched` imports only the standard library, `github.com/robfig/cron/v3` and `internal/model`.

## Tests to include

A hand-written fake `Runner` that records calls and can block or fail per job. No test may wait for a real minute boundary: drive ticks through an unexported seam (for example a function that runs one entry's chain, and a constructor option that swaps the parser or the clock for in-package tests). The whole package's tests finish in under 10 seconds.

- `TestNew_Validation`: each problem in requirement 1, several at once reported together; `* * * * * *` and `@every 1s` rejected; `5 */6 * * *` and `0 8 * * 1` accepted.
- `TestStart_LogsOneLinePerEntry`: capture the logger; exactly one `schedule registered` line per entry with the three attributes.
- `TestChain_RunsJobsInOrder`.
- `TestChain_ContinuesAfterError`: second job still runs; one `Warn` line.
- `TestChain_StopsOnCancelledContext`.
- `TestTick_SkippedWhilePreviousRunning`.
- `TestStop_WaitsForRunningChain` and `TestStop_ReturnsOnContextDone`.
- `TestStop_BeforeStartAndTwice`.
- `TestChain_PanicRecovered`.
- One end-to-end test with real ticking through the seam (sub-second schedule) proving a registered entry actually fires.

## Acceptance

| Command | Expected |
|---|---|
| `make check` | exit 0 |
| `go test -race -count=3 ./internal/sched/` | `ok`, each run under 10 s |
| `go list -f '{{join .Imports "\n"}}' ./internal/sched \| grep -E 'upstreamwatch\|robfig'` | only `.../internal/model` and `github.com/robfig/cron/v3` |
| `git status --porcelain` | only owned files and the report file |

## Out of scope

- Which jobs each config schedule key runs, and building `Entry` values from config (WO-07, in `cmd/upw`).
- Recording runs or raising findings (the Runner does both).
- `upw serve` shutdown ordering (WO-07).

## Skills and docs

Preloaded by your agent definition; do not reload. Load with the Skill tool: none.
Read: `docs/architecture.md` ("Schedules", "Layers"); `mvp-plan/PLAN.md` section 3 "Scheduler"; go-code skill sections "Context and goroutines" and "Logging".

## Report

Follow the report contract in your agent definition: full report to the report file named at the top, then the short reply. Do not commit, stash or otherwise write to git. If a question's answer would change behaviour and nothing here or in the docs answers it, report NEEDS_CONTEXT rather than guessing.

## Rulings

- 2026-11-19: A failed job does not stop its chain. Each failure already raises its own `job_failure` finding through the Runner, and later jobs working from the last good data is the documented failure behaviour (`docs/external-sources.md`).
- 2026-11-19 (fix round 1): `Stop` returns the wrapped `ctx.Err()` only if a chain is actually still running when `ctx` is done. If no chain is running, `Stop` returns nil whatever the state of `ctx`. Ticks that fire after `Stop` began start no chain.
- 2026-11-19: Rejecting a `TZ=`/`CRON_TZ=` prefix and ending a chain on a panic are accepted as built.
