# WO-13: M0 integration tests across store, Runner, scheduler and read service

Milestone: M0
Agent: test-writer   Model: opus
Depends on: WO-03 to WO-08, WO-10, WO-12 (all merged)
Report file: `.claude/plans/m0/WO-13-m0-integration-tests.report.md`
Ticket: UPW-19

## Objective

Each M0 package has unit tests written with fakes by its builder. Nothing yet proves the real pieces work together. This work order adds tests that run the real `store.DB` under the real `pipeline.Runner`, `sched.Scheduler` and `query.Service`, and fills the gaps you find in the existing suites of `internal/config`, `internal/store` and `internal/pipeline`. Serves M0 checklist item 15.

## Owned files

- `internal/pipeline/integration_test.go` (new; package `pipeline_test`)
- `internal/sched/integration_test.go` (new; package `sched_test`)
- `internal/query/integration_test.go` (new; package `query_test`)
- New `*_test.go` files in `internal/config/`, `internal/store/` and `internal/pipeline/` whose names end in `_extra_test.go`
- `testdata/` directories those tests need

Read-only for you: every non-test file, every existing test file, and everything under `cmd/upw/` and `internal/httpx/` (another work order is editing `cmd/upw`). If a test exposes a production bug, keep the failing test, do not fix the code, and report DONE_WITH_CONCERNS with the reproduction.

## Interfaces

### Consumed

The exported APIs of `internal/store`, `internal/pipeline`, `internal/sched`, `internal/query`, `internal/config` and `internal/model` as merged. Read the source; the doc comments are the contract. Test files may import across packages (the layering lint excludes `_test.go`).

### Produced

Tests only.

## Requirements

1. Integration tests open a real database with `store.Open` on a file under `t.TempDir()` and call `Migrate`, exactly as start-up does. No fakes for the store in these files. Jobs are small hand-written `pipeline.Job` implementations in the test file.
2. `internal/pipeline/integration_test.go`:
   - `TestIntegration_SuccessfulRunIsRecorded`: a job returning stats → `db.LatestRuns` shows one run for that job with status `succeeded`, the stats, a non-zero `FinishedAt` and an empty error; `db.ListOpenFindings` is empty.
   - `TestIntegration_FailedRunRaisesJobFailure`: a failing job → latest run `failed` with the error text; exactly one open finding of kind `job_failure` with fingerprint `job_failure|<job>`, score 50, state `open`, and a rank reason containing the error.
   - `TestIntegration_RepeatedFailureUpdatesOneFinding`: two failures with different errors → still one finding; `FirstSeenAt` unchanged, `LastChangedAt` advanced, rank reason from the second error.
   - `TestIntegration_SuccessResolvesFailure`: failure then success → `ListOpenFindings` empty; a later failure makes the same finding open again (same ID).
   - `TestIntegration_PanickingJob`: recorded as failed, finding raised, the Runner still usable afterwards.
   - `TestIntegration_CancelledRunIsStillRecorded`: cancel the context while the job blocks → the run row is `failed` with a finished time.
   - `TestIntegration_TwoJobsLatestRuns`: several runs of two jobs → `LatestRuns` returns one row per job, the most recent, ordered by job name.
3. `internal/sched/integration_test.go`: build a `sched.Scheduler` over a real Runner and real store. Because `sched.New` accepts only five-field expressions, do not wait for a tick; assert what is observable without one: `New` accepts the six default schedule expressions from `internal/config`'s defaults; `Start` then `Stop` on a real Runner returns nil and leaves no rows in `runs`; `Stop` with a context that is already cancelled, when nothing is running, returns nil.
4. `internal/query/integration_test.go`: `query.NewService(db, db, logger)` over a real store returns the same runs and findings the store holds after the Runner has produced one success and one failure, in the documented order.
5. Gap review. Read the existing tests of `internal/config`, `internal/store` and `internal/pipeline` against each package's exported API and doc comments, and against the requirements in `.claude/plans/m0/WO-03-config-loader.md`, `WO-04-sqlite-store.md` and `WO-05-pipeline-runner.md`. For every requirement or documented behaviour with no test that would fail if it broke, add one in an `_extra_test.go` file. List in the report each gap found and the test that now covers it; if you find none in a package, say so. Do not duplicate cases that exist.
6. Every test is deterministic and fast: no `time.Sleep` longer than 50 ms, no network, no dependence on port 8080 or on the machine's time zone. `go test -race -count=3` passes for every touched package.

## Acceptance

| Command | Expected |
|---|---|
| `make check` | exit 0 (or, if a kept test exposes a production bug, failing only on that test, named in the report) |
| `go test -race -count=3 ./internal/pipeline/ ./internal/sched/ ./internal/query/ ./internal/config/ ./internal/store/` | `ok` for all five |
| `go test -race -run 'TestIntegration' -v ./internal/pipeline/ \| grep -c '^--- PASS'` | 7 |
| `git status --porcelain` | only owned files and the report file |

## Out of scope

- Tests for `cmd/upw`, `internal/web`, `internal/httpx` and the doctor adapters.
- Any production code change.

## Skills and docs

Preloaded by your agent definition; do not reload. Load with the Skill tool: none.
Read: `.claude/skills/go-test/templates/store-tests.md`; `.claude/skills/milestone-accept/references/M0-SKELETON.md` (item 15); the three work orders named in requirement 5.

## Report

Follow the report contract in your agent definition: full report to the report file named at the top, then the short reply. Do not commit, stash or otherwise write to git.

## Rulings

None.
