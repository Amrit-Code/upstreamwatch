Status: DONE_WITH_CONCERNS
Work order: .claude/plans/m1/WO-18-cli-config-store-followups.md
Files changed:
- cmd/upw/joblock.go (created)
- cmd/upw/joblock_test.go (created)
- cmd/upw/fakes_test.go (created)
- cmd/upw/jobs.go (modified)
- cmd/upw/jobs_test.go (modified)
- cmd/upw/main.go (modified)
- cmd/upw/runjob.go (modified)
- cmd/upw/runjob_test.go (modified)
- cmd/upw/timeline.go (modified)
- cmd/upw/doctor.go (modified)
- cmd/upw/doctor_test.go (modified)
- internal/config/config.go (modified)
- internal/config/file.go (modified)
- internal/config/load.go (modified)
- internal/config/validate.go (modified)
- internal/config/load_git_test.go (modified)
- internal/config/load_test.go (modified)
- internal/store/timeline.go (modified)
- internal/store/timeline_test.go (modified)
- deploy/upw.example.yaml (modified)

Commands run:
- `git log --oneline -1` -> exit 0
  eb623f2 model: Scope.Rebuild; plans: WO-17 to WO-19 from the Fable review of M1
- `make check` -> exit 0
  ==> gofmt
  ==> vet
  go vet ./...
  ==> build
  go build ./...
  ==> test
  go test -race ./...
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	2.043s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	1.274s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract	1.408s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract/unity	1.170s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	10.647s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	1.577s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/ingest	1.713s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/issues/github	1.217s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	1.194s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	1.516s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	1.749s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	1.748s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	5.909s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/testutil/gitrepo	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/timeline	3.330s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/upstream/depsdev	1.215s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/osv	1.324s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/web	1.390s
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
- `go test -race -count=1 ./cmd/upw/ ./internal/config/ ./internal/store/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	1.862s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	1.144s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	3.386s
- `go test -race -count=3 -run 'TestJobLock|TestRun_' ./cmd/upw/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	1.529s
- `GOOS=linux go vet ./cmd/upw/` -> exit 0 (syscall.Flock builds for the deployed platform)
- `go vet -tags e2e ./cmd/upw/` -> exit 0 (WO-19's parity_e2e_test.go still compiles beside the new test helpers)
- `git status --porcelain` -> exit 0
   M cmd/upw/doctor.go
   M cmd/upw/doctor_test.go
   M cmd/upw/jobs.go
   M cmd/upw/jobs_test.go
   M cmd/upw/main.go
   M cmd/upw/runjob.go
   M cmd/upw/runjob_test.go
   M cmd/upw/timeline.go
   M deploy/upw.example.yaml
   M internal/config/config.go
   M internal/config/file.go
   M internal/config/load.go
   M internal/config/load_git_test.go
   M internal/config/load_test.go
   M internal/config/validate.go
   M internal/store/timeline.go
   M internal/store/timeline_test.go
  ?? cmd/upw/fakes_test.go
  ?? cmd/upw/joblock.go
  ?? cmd/upw/joblock_test.go
  (plus this report file once written)
- Mutation check (run, then reverted): `tryLock` made to return nil without locking; `go test -race -count=1 -run TestJobLock ./cmd/upw/` -> FAIL in TestJobLock_NoOverlap (both subtests: "job 2 entered while job 1 held the lock", "most jobs running at once = 3, want 1") and TestJobLock_CancelledWait (both rows). After the revert the tests pass (runs above). The store test was checked by reading the old SQL: it set `name = excluded.name` and moved `updated_ts`, which the new test rejects.

Decisions:
- Rebuild flag: the check is `job != "timeline" && -rebuild` -> usage error `-rebuild is only used with "run timeline"`, placed after the existing -once/-repo checks, so `run all -once -rebuild` and `run <other> -rebuild` both exit 2.
- cmdRun is split into `parseRun` (flags -> `runRequest{job, scope}`) and `runRequested` (runner + printing), so TestRun_RebuildFlag asserts the scope through a fake job on a real `pipeline.Runner`; main's exit-code switch moved into `exitCode(err, stderr)` so the single-job error print is tested directly. `upw run -h` now returns flag.ErrHelp instead of nil (same exit code 0, same output).
- Terminal-safe rule: `terminalSafe` (timeline table) keeps its rule unchanged; a new `terminalSafeText` applies the work order's rule (control characters other than `\n` and `\t`, or invalid UTF-8, -> strconv.Quote). Both are one-liners over one shared `quoteUnsafe(s, keep)`, so the rule is written once. Only the message field is quoted: `upw: "<err>"`, `<job>: failed: "<err>"`; the stats line is passed whole (it holds only job and stat names).
- Doctor's failed-check detail also goes through `terminalSafeText` (requirement 4 says "every place cmd/upw prints an error"; a check error can carry text a remote service sent). Test: TestDoctor_FailureIsTerminalSafe. The test helper `doctor()` now calls `exitCode` instead of copying main's switch.
- Job lock: `withJobLocks(dir, jobs)` wraps each job in an unexported `*lockedJob` inside `registeredJobs`, dir `<data_dir>/locks`. `MkdirAll(dir, 0700)`, `OpenFile(<job>.lock, O_RDWR|O_CREATE, 0600)`; an existing directory's mode is not changed. `LOCK_EX|LOCK_NB` via `SyscallConn().Control`, retried every 200 ms (`jobLockRetry`) on EWOULDBLOCK/EINTR until ctx ends; the wait error is `wait for job lock <job>: <ctx.Err()>` (wraps it). The lock is released by closing the file; a close error is joined into the job's error. The wait is not logged (not requested).
- Lock tests use `testing/synctest` with two (and three) independent `pipeline.Runner`s on one lock directory, so the 200 ms retries run in fake time and no test sleeps in real time.
- `git.max_blob_mb`: field `config.Git.MaxBlobMB` (int, default 8, validation `git.max_blob_mb: must be at least 1, got N`), passed as `MaxBlobBytes: int64(MaxBlobMB) << 20`. No upper bound, like `max_clone_mb`. The example-config test now also asserts the four git keys appear in `deploy/upw.example.yaml`.
- Package upsert: `ON CONFLICT (purl) DO UPDATE SET ecosystem = excluded.ecosystem, updated_ts = excluded.updated_ts WHERE (ecosystem IS NULL OR ecosystem = '') AND excluded.ecosystem IS NOT NULL`. A set ecosystem is never replaced; a NULL name is not filled in later (the work order names only the ecosystem). Tests: TestDB_ApplyTimeline_PackageKeepsFirstSpelling (Jinja2 then jinja2: name stays, updated_ts unchanged) and TestDB_ApplyTimeline_PackageGainsOnlyAMissingEcosystem.

Outside scope:
- Docs to update in the docs sync: `docs/external-sources.md` (git section) still says the blob limit is 1 MiB with no config key; it is now `git.max_blob_mb`, default 8 MiB, which `cmd/upw` passes as `gitsrc.Options.MaxBlobBytes` (gitsrc's own zero-value default is still 1 MiB, WO-17's area). `docs/architecture.md` CLI table: `upw run timeline [-repo slug] -rebuild`; job errors, stats lines and doctor failure details are printed through the terminal-safe rule; each job holds `<data_dir>/locks/<job>.lock` while it runs. Config docs: new key `git.max_blob_mb`. `docs/data-access.md`/`docs/database.md`: no change needed (decisions.md already says `packages.name` keeps the first spelling).
- The work order's parenthetical describes the `upw timeline` rule as exempting `\n` and `\t`; the existing helper (and `docs/architecture.md`, CLI table) quote any control character in a table field. The timeline rule was left as documented; see Decisions and Concerns.

Concerns:
- Two quoting rules over one helper. Read literally, "the same quoting rule as upw timeline" and "(control other than \n and \t)" cannot both hold: the timeline helper quotes `\n` and `\t`. Exempting them in the table would let repository data break the table's columns. Applying the strict rule to errors would print every `errors.Join` config error on one quoted line. I kept the strict rule for the table and used the work order's exempting rule for messages, both built on one function. If the lead wanted a single rule everywhere, it is a one-line change in `cmd/upw/timeline.go`. Note that with `\n` allowed, a remote-controlled error (scrubbed git stderr) can still start a new output line, for example a fake `timeline: succeeded` line under `run all`. It cannot send escape sequences.
- Lock placement: the lock is taken inside `Job.Run`, as requirement 3 says, so the Runner calls `StartRun` before the wait. A `upw run ingest` waiting behind `upw serve`'s ingest shows as a `running` row in `runs` for the whole wait, and a cancelled wait is recorded as a failed run with a job_failure finding (`wait for job lock ingest: context canceled`). That fits "the Runner records every run", but the operator will see it.

Question or blocker: none

## Gate (lead, 2026-11-19)

make check rerun: exit 0; cmd/upw, config and store tests ok. Concerns accepted: two quoting rules over one helper (strict in the timeline table, newlines and tabs kept in errors), doctor detail quoted, lock waits visible as running runs. Sonnet reviewer: ACCEPT; minors accepted (a remote-controlled newline can still start an output line on the operator's own terminal; escape sequences are blocked; lock wait recorded inside the run). Accepted.
