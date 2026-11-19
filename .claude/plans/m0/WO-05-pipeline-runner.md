# WO-05: pipeline.Runner with run recording and job_failure findings

Milestone: M0
Agent: go-builder   Model: opus
Depends on: WO-01, WO-02 (merged)
Report file: `.claude/plans/m0/WO-05-pipeline-runner.report.md`
Ticket: UPW-15

## Objective

`pipeline.Runner` is the one way a job starts, for the scheduler, `upw run` and tests alike. It records each run in the `runs` table through `RunStore`, and a failed job becomes a `job_failure` finding through `FindingStore`, so no failure is silent. Serves M0 checklist item 15.

## Owned files

- `internal/pipeline/` (all files except `interfaces.go`; keep the package comment in `doc.go`)

Lead-owned, read-only for you (stop and report if one is wrong):

- `internal/pipeline/interfaces.go`
- `internal/model/` (all files)
- `go.mod`, `go.sum`, `.go-arch-lint.yml`, `Makefile`, `scripts/`

## Interfaces

### Consumed

```go
// internal/pipeline/interfaces.go
type Job interface {
	Name() string
	Run(ctx context.Context, scope model.Scope) (model.Stats, error)
}
type RunStore interface {
	StartRun(ctx context.Context, job string, startedAt time.Time) (model.Run, error)
	FinishRun(ctx context.Context, run model.Run) error
}
type FindingStore interface {
	UpsertFinding(ctx context.Context, f model.Finding) (model.Finding, error)
	ResolveFinding(ctx context.Context, fingerprint string, at time.Time) error
}
```

Plus `model.Run`, `model.RunStatus` and its constants, `model.Finding`, `model.KindJobFailure`, `model.Scope`, `model.Stats` from the committed `internal/model` files. Read those files and `interfaces.go` for the doc comments; they are part of the contract.

### Produced

```go
// internal/pipeline
var ErrUnknownJob = errors.New("unknown job")
var ErrDuplicateJob = errors.New("duplicate job name")

func NewRunner(jobs []Job, runs RunStore, findings FindingStore, logger *slog.Logger) (*Runner, error)
func (r *Runner) Run(ctx context.Context, job string, scope model.Scope) (model.Run, error)
func (r *Runner) Jobs() []string // registered names, in registration order

// JobFailureFingerprint returns the findings fingerprint for a failed job: "job_failure|<job>".
func JobFailureFingerprint(job string) string
```

## Requirements

1. `NewRunner` returns an error wrapping `ErrDuplicateJob` (naming the job) if two jobs share a name, or an error if a job name is empty. It does no I/O.
2. `Run` with a name that is not registered returns an error wrapping `ErrUnknownJob` that names the job, and writes nothing.
3. `Run` on a registered job, in order: `StartRun(job, now)`; call `Job.Run(ctx, scope)`; `FinishRun` with `FinishedAt = now`, the returned `Stats`, and status `model.RunSucceeded`, or `model.RunFailed` with `Error` set to the job error's text.
4. On success, after `FinishRun`, call `ResolveFinding(JobFailureFingerprint(job), now)`, so a recovered job clears its failure finding.
5. On failure, after `FinishRun`, call `UpsertFinding` with `Kind: model.KindJobFailure`, `Fingerprint: JobFailureFingerprint(job)`, `Score: 50` (plan section 6), `RankReason: "job <job> failed: <error text>"` with the error text cut to 500 bytes on a UTF-8 boundary, and `LastChangedAt: now`. All ID fields stay zero.
6. A job that panics is treated as a failed job: the panic is recovered inside `Run`, the run is finished as failed with an error text that starts `panic: `, the finding is raised, and `Run` returns an error. The Runner never lets a job's panic escape.
7. `Run` returns the finished `model.Run` and the job's error wrapped as `run job <name>: %w` (nil on success). A job failure is returned, not logged twice: the Runner logs one `Info` line on success (`job finished`, with `job`, `run_id`, `duration_ms` and each stat) and one `Error` line on failure (`job failed`, with `job`, `run_id`, `error`).
8. Store errors: if `StartRun` fails, the job does not run and the error is returned. If `FinishRun`, `UpsertFinding` or `ResolveFinding` fails, the Runner still attempts the remaining store calls, and returns all errors joined with `errors.Join` (the job error first, if any).
9. Recording must survive cancellation: when `ctx` is cancelled while the job runs, the job receives the cancelled context, and the `FinishRun` and finding calls use `context.WithoutCancel(ctx)` with a 10-second timeout, so a SIGTERM still leaves a finished `runs` row. A run ended by cancellation is recorded as `model.RunFailed` with the context error's text, and raises the finding like any other failure.
10. Only one job runs at a time per Runner: concurrent `Run` calls are serialised in arrival order by a mutex held for the whole run. A caller whose `ctx` is cancelled while it waits returns `ctx.Err()` wrapped, without starting a run.
11. The clock is a `now func() time.Time` field set to `time.Now` by `NewRunner`; tests replace it from inside the package. All times passed to the store are UTC.
12. `internal/pipeline` imports only the standard library and `internal/model`.

## Tests to include

Hand-written fakes for `Job`, `RunStore` and `FindingStore` in the test file; the fakes record calls in order.

- `TestRunner_Success`: stats and `RunSucceeded` reach `FinishRun`; `ResolveFinding` called with `job_failure|<job>`; no `UpsertFinding`.
- `TestRunner_Failure`: `RunFailed`, error text stored, `UpsertFinding` called with the exact finding of requirement 5; returned error satisfies `errors.Is` for the job's error.
- `TestRunner_Panic`: requirement 6.
- `TestRunner_UnknownJob`: `errors.Is(err, ErrUnknownJob)`, zero store calls.
- `TestNewRunner_DuplicateAndEmptyName`.
- `TestRunner_StartRunFails`: job not called.
- `TestRunner_FinishRunFailsStillRaisesFinding`: requirement 8.
- `TestRunner_CancelledContextStillRecords`: the job blocks until `ctx.Done()`; assert the fakes were called with a context that is not cancelled, and the run is `RunFailed`.
- `TestRunner_SerialisesRuns`: two goroutines call `Run`; a job that records overlap sees none (run with `-race`).
- `TestRunner_LongErrorTruncated`: a 2000-byte error with multi-byte runes → `RankReason` error part is at most 500 bytes and valid UTF-8.
- `TestJobFailureFingerprint`.

## Acceptance

| Command | Expected |
|---|---|
| `make check` | exit 0 |
| `go test -race -count=3 ./internal/pipeline/` | `ok` |
| `go list -f '{{join .Imports "\n"}}' ./internal/pipeline \| grep upstreamwatch` | only `.../internal/model` |
| `git status --porcelain` | only owned files and the report file |

## Out of scope

- `upw run` in `cmd/upw` and its exit codes (WO-07).
- The scheduler and job chains (WO-06).
- Any real job (M1 onward). `run all --once` ordering (WO-07).
- Wrapping the store calls in a transaction: the run row and the finding are two independent writes in M0.

## Skills and docs

Preloaded by your agent definition; do not reload. Load with the Skill tool: none.
Read: `docs/architecture.md` (Layers; "What each job does"); `docs/interfaces.md` (`Job`); `mvp-plan/PLAN.md` section 6 "Ranking" (job_failure line).

## Report

Follow the report contract in your agent definition: full report to the report file named at the top, then the short reply. Do not commit, stash or otherwise write to git. If a question's answer would change behaviour and nothing here or in the docs answers it, report NEEDS_CONTEXT rather than guessing.

## Rulings

- 2026-11-19: One `job_failure` finding per job, keyed `job_failure|<job>`. A repeated failure updates it; the next successful run resolves it. The `findings` table has no run column, so the failing run is identified through `rank_reason` and the `runs` table.
- 2026-11-19 (fix round 1, Astra finding WO05-001): Requirement 9 holds even when the job returns nil. If `ctx` is cancelled or past its deadline when `Job.Run` returns, and the job returned a nil error, the Runner treats the run as failed with `ctx.Err()` as the job error: status `model.RunFailed`, the context error's text, `UpsertFinding` raised, `ResolveFinding` not called, and the returned error satisfies `errors.Is` for the context error. A job that returns its own non-nil error keeps that error.
