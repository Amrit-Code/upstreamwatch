Status: DONE_WITH_CONCERNS
Work order: .claude/plans/m0/WO-13-m0-integration-tests.md
Files changed:
- internal/pipeline/integration_test.go (created)
- internal/pipeline/runner_extra_test.go (created)
- internal/sched/integration_test.go (created)
- internal/query/integration_test.go (created)
- internal/store/store_extra_test.go (created)
- internal/config/load_extra_test.go (created)
Commands run:
- `make check` -> exit 2
  ==> gofmt
  ==> vet
  go vet ./...
  ==> build
  go build ./...
  ==> test
  go test -race ./...
  --- FAIL: TestIntegration_StopWithCancelledContextWhenIdle (0.00s)
      --- FAIL: TestIntegration_StopWithCancelledContextWhenIdle/after_start,_no_chain_running (0.17s)
          integration_test.go:203: Stop(cancelled ctx) after Start with no chain running error = stop scheduler: wait for running schedules: context canceled, want nil
  FAIL
  FAIL	github.com/Amrit-Code/upstreamwatch/internal/sched	0.441s
  FAIL
  make: *** [check] Error 1
  (The other 8 packages with tests printed `ok`. gofmt, vet and build passed. That one kept bug test is the only failure.)
- `go run github.com/fe3dback/go-arch-lint@v1.19.2 check --output-color=false` -> exit 0 (run on its own, because make check stops at the failing test step before reaching it)
  OK - No warnings found
- `sh scripts/check-imports.sh` -> exit 0 (run on its own for the same reason; no output)
- `go test -race -count=3 ./internal/pipeline/ ./internal/sched/ ./internal/query/ ./internal/config/ ./internal/store/` -> exit 1
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	1.847s
  --- FAIL: TestIntegration_StopWithCancelledContextWhenIdle (0.00s)
      --- FAIL: TestIntegration_StopWithCancelledContextWhenIdle/after_start,_no_chain_running (0.14s)
          integration_test.go:203: Stop(cancelled ctx) after Start with no chain running error = stop scheduler: wait for running schedules: context canceled, want nil
  (the same failure is printed for each of the 3 runs)
  FAIL
  FAIL	github.com/Amrit-Code/upstreamwatch/internal/sched	0.638s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	1.632s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	1.198s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	5.252s
  FAIL
  Four of the five packages give `ok`. sched fails only on the kept bug test, the same way in all 3 runs.
- `go test -race -run 'TestIntegration' -v ./internal/pipeline/ | grep -c '^--- PASS'` -> 7
- `git status --porcelain` -> only owned files (the report file is added by this write)
  ?? internal/config/load_extra_test.go
  ?? internal/pipeline/integration_test.go
  ?? internal/pipeline/runner_extra_test.go
  ?? internal/query/integration_test.go
  ?? internal/sched/integration_test.go
  ?? internal/store/store_extra_test.go
- `go test -race -count=1 -cover ./internal/pipeline/ ./internal/query/ ./internal/config/ ./internal/store/ ./internal/sched/` -> exit 1 (sched: the kept bug test)
  pipeline 95.9%, query 100.0%, config 100.0%, store 85.6%, sched 100.0% of statements
- Bug reproduction outside synctest: a temporary probe test (Start, then Stop with a context cancelled beforehand, repeated 200 times) printed `fails 200/200`. The probe file was deleted afterwards, and `ls internal/sched` confirms it is gone.

Decisions:
- Clock: the integration files are external test packages and cannot set Runner.now, so the pipeline and query integration tests run inside `synctest.Test`. The Runner's time.Now is then the bubble's fake clock, which gives whole-second times that compare exactly with the store's second precision, and lets `TestIntegration_RepeatedFailureUpdatesOneFinding` advance LastChangedAt deterministically. Real SQLite (modernc, opened inside the bubble and closed at cleanup) works in the bubble.
- Requirement 6 says "no time.Sleep longer than 50 ms". `internal/pipeline/integration_test.go` has a helper `advance(d)` that calls time.Sleep with up to 1 h, but only inside a synctest bubble, where it moves the fake clock without waiting. The whole file runs in about 0.15 s. If the lead reads requirement 6 literally, this needs a ruling. No other file sleeps.
- sched: Start/Stop run inside a synctest bubble, so no wall-clock minute boundary can fire a default schedule between Start and Stop. Entries are the six config defaults, each running a job of the same name registered on a real Runner, so a stray tick would show up as a runs row and a job call.
- sched: "Stop with a cancelled context when nothing is running" is tested both before Start (trivially nil) and after Start with no chain running (the case the work order cares about). The second case exposes the BUG below.
- The six default schedules are read with `config.Load` on an empty file, not copied, so the test follows the config defaults.
- Store gap file is named `store_extra_test.go`, config `load_extra_test.go`, pipeline `runner_extra_test.go`.
- Runner.Run against the real store (pipeline/integration_test.go): 7 tests (SuccessfulRunIsRecorded, FailedRunRaisesJobFailure, RepeatedFailureUpdatesOneFinding, SuccessResolvesFailure, PanickingJob, CancelledRunIsStillRecorded, TwoJobsLatestRuns).
- Runner gaps (runner_extra_test.go): LogsOneLinePerOutcome 3 cases (success, failure, panic); StoreTimesAreUTC 2; SerialisesInArrivalOrder 1 (three queued callers); RecordingUsesDetachedTenSecondContext 3 (success, failure, cancelled); PassesScopeAndContextToJob 2; AlreadyCancelledContextStartsNothing 1.
- sched.New / Start / Stop over a real Runner: NewAcceptsDefaultSchedules 1 test (7 New calls: all six together and each alone); StartStopRecordsNothing 1; StopWithCancelledContextWhenIdle 2 cases.
- query.Service over a real store: ServiceReadsWhatRunnerWrote 1 (LastRuns and OpenFindings against exact values and against the store's own reads); ServiceOrdersFindingsAndDropsResolved 1; ServiceOnEmptyStore 1; ServiceOnClosedStore 1.
- Store gaps (store_extra_test.go): QueryIndexes 2 indexes; DocUniqueConstraints 13 constraints; ForeignKeyChildColumnsIndexed every foreign key in the schema; TimestampColumns every table; RowTimestampsFromStoreClock 2 (runs, findings plus finding_state); StartRun_ReturnsStoredTime 1; ConcurrentWriters 1 (8 writers x 20 transactions); ReadDuringWriteTransaction 1.
- config.Load gaps (load_extra_test.go): ValidationExtra 28 cases; AcceptsEveryAllowedValue 12; EveryErrorLineStartsWithPath 1 (20 paths, every line checked against a path regexp); ExpandsValuesOnly 4; SecretMustBeExactlyOneReference 5; InvalidTargetURLNotEchoed 1; ParseErrorsReturnZeroConfig 4.

Gap review (requirement 5), each gap and the test that now covers it:
- pipeline (WO-05):
  - Req 7, the log lines (one Info `job finished` with job, run_id, duration_ms and each stat; one Error `job failed` with job, run_id, error; never logged twice) had no test -> TestRunner_LogsOneLinePerOutcome.
  - Req 11, all times passed to the store are UTC, was only exercised with a UTC clock -> TestRunner_StoreTimesAreUTC (clock in UTC+5:30).
  - Req 10, arrival order: the existing test checked only that runs do not overlap -> TestRunner_SerialisesInArrivalOrder.
  - Req 9, the 10-second timeout on the detached recording context and the parent's values kept, had no test -> TestRunner_RecordingUsesDetachedTenSecondContext.
  - Req 3, the scope (and context) reaching Job.Run unchanged: the existing success test passed a scope but never checked it -> TestRunner_PassesScopeAndContextToJob.
  - Req 10, a caller whose context is already cancelled before Run (rather than while waiting) -> TestRunner_AlreadyCancelledContextStartsNothing.
- store (WO-04 and the store-tests template):
  - Req 4, the indexes `runs(job, id)` and `findings(resolved_ts)` -> TestSchema_QueryIndexes.
  - Req 4, every UNIQUE constraint the doc states (only findings.fingerprint was exercised, indirectly) -> TestSchema_DocUniqueConstraints.
  - Req 4, an index on each foreign-key child column -> TestSchema_ForeignKeyChildColumnsIndexed.
  - Req 4, created_ts/updated_ts TEXT NOT NULL on every table except schema_migrations, kv, http_cache and settings (which have updated_ts only); a few columns were checked before -> TestSchema_TimestampColumns.
  - Req 1, created_ts/updated_ts from the store clock on runs (findings had it on insert only), plus updated_ts moving on re-upsert and resolve while finding_state stays untouched -> TestDB_RowTimestampsFromStoreClock.
  - StartRun's returned StartedAt (UTC, truncated, equal to what is read back) -> TestDB_StartRun_ReturnsStoredTime.
  - Template: concurrent writers all succeed -> TestDB_ConcurrentWriters.
  - Template: a reader is not blocked by an open write transaction and does not see its rows -> TestDB_ReadDuringWriteTransaction.
- config (WO-03):
  - Req 4 had no case for: the schedules keys vulns, issues, llm and digest; an empty schedule; negative backfill_max_issues, max_snippets, max_issue_chars, max_calls_per_run, both prices, issues_closed_keep_days and threshold; slack_webhook and ntfy without url; email without username or from; email port above 65535 or negative; an empty `to` entry; index paths on a second channel or target; listen without a port or with a port out of range; base_url with no host; slug with uppercase or underscore -> TestLoad_ValidationExtra (each case also checks that the zero Config is returned).
  - Allowed values were never shown to be accepted (pin, openai_compat, snippets, none, auth upstream, every channel type, http base_url, zero caps, IPv6 listen, ranged cron) -> TestLoad_AcceptsEveryAllowedValue.
  - Req 4, "each error starting with the YAML path": existing tests used Contains -> TestLoad_EveryErrorLineStartsWithPath.
  - Req 1, expansion in decoded values only: covered before for data_dir only. Now also lists, nested target, override and channel fields, validation of an expanded URL, a required value from an unset variable, and keys not expanded -> TestLoad_ExpandsValuesOnly.
  - Req 3, "exactly one ${NAME} reference" (two references, `$NAME`, leading prefix, trailing space) -> TestLoad_SecretMustBeExactlyOneReference.
  - checkTargetURL's promise never to repeat the URL, for an unparseable URL carrying user info -> TestLoad_InvalidTargetURLNotEchoed.
  - Req 1, zero Config on a parse or decode error -> TestLoad_ParseErrorsReturnZeroConfig.

Outside scope:
- `make check` stops at the first failing step, so while the kept sched test fails, the arch-lint and import-check steps do not run. I ran both on their own (exit 0 above).
- The doc comment on `query.FindingStore.ListOpenFindings` (internal/query/interfaces.go) says "highest score first" but not the lowest-ID tie-break that `store.ListOpenFindings` documents and that the query integration test relies on. This is a doc gap only.

Concerns:
- BUG: internal/sched/scheduler.go:159-163 (Scheduler.Stop). Test: TestIntegration_StopWithCancelledContextWhenIdle/after_start,_no_chain_running in internal/sched/integration_test.go. Input: sched.New over a real pipeline.Runner and store with the six default schedules, Start(ctx), then Stop with a context cancelled before the call, with no chain running. Got: `stop scheduler: wait for running schedules: context canceled`. Want: nil (WO-13 requirement 3; WO-06 requirement 7 says Stop returns ctx.Err() only when it has to wait for a running chain). Diagnosis: `cron.Stop()` returns a context that a goroutine cancels after `jobWaiter.Wait()`, so when Stop's select runs, only `ctx.Done()` is ready and Stop returns the context error even though nothing is running. It fails every time: 3 of 3 in the suite, 20 of 20 under synctest, and 200 of 200 in a probe outside synctest. Production impact today is low, because `cmd/upw/serve.go:52` passes a fresh 10 s timeout context, but any caller whose shutdown context has already expired gets a spurious error. The test is kept unweakened, with a comment naming the bug.
- No test is skipped.

Question or blocker: none
