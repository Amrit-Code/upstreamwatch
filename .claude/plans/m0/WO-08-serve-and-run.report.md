Status: BLOCKED
Work order: .claude/plans/m0/WO-08-serve-and-run.md
Files changed:
- cmd/upw/main.go (modified)
- cmd/upw/app.go (created)
- cmd/upw/serve.go (created)
- cmd/upw/runjob.go (created)
- cmd/upw/main_test.go (modified)
- cmd/upw/serve_test.go (created)
- cmd/upw/runjob_test.go (created)

Commands run:
- `go build ./... && go vet ./cmd/upw/` -> exit 0 (no output)
- `go test -race -count=1 ./cmd/upw/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	1.619s
- `go test -race -count=1 -v -run 'TestServe_SchemaTooNew|TestServe_BadTarget' ./cmd/upw/` -> exit 0 (TestServe_SchemaTooNew ran, not skipped)
  --- PASS: TestServe_BadTargetScheme (0.00s)
  --- PASS: TestServe_SchemaTooNew (0.08s)
- `go test -race -count=1 -cover ./cmd/upw/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	1.558s	coverage: 83.3% of statements
- `make check` -> exit 2 (make: *** [check] Error 1). gofmt, vet, build and `go test -race ./...` pass; arch-lint fails:
  ==> gofmt
  ==> vet
  go vet ./...
  ==> build
  go build ./...
  ==> test
  go test -race ./...
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	(cached)
  ?   	github.com/Amrit-Code/upstreamwatch/internal/httpx	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/web	(cached)
  ==> arch-lint
  go run github.com/fe3dback/go-arch-lint@v1.19.2 check --output-color=false
  module: github.com/Amrit-Code/upstreamwatch
  linters:
     On | Base: component imports # always on
     On | Advanced: vendor imports # switch 'allow.depOnAnyVendor = false' (or delete) to on
     On | Advanced: method calls and dependency injections # switch 'allow.deepScan = true' (or delete) to on

  Dependency store -\-> pipeline not allowed
    ├─ store store.DB in /internal/store/db.go:33
    └─ pipeline NewRunner in /internal/pipeline/runner.go:53
       .../cmd/upw/app.go:44
       >   44 |   runner, err := pipeline.NewRunner(registeredJobs(), db, db, logger)
  (reported twice, once per argument)

  Dependency store -\-> query not allowed
    ├─ store store.DB in /internal/store/db.go:33
    └─ query NewService in /internal/query/service.go:21
       .../cmd/upw/serve.go:37
       >   37 |   q := query.NewService(a.db, a.db, a.logger)
  (reported twice, once per argument)

  --
  total notices: 4
  exit status 1
  make: *** [check] Error 1
- `sh scripts/check-imports.sh` (the step after arch-lint, run by hand) -> exit 0 (no output)
- `gofmt -l .` -> exit 0 (no output)
- `go test -race -count=2 ./cmd/upw/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	1.630s
- `lsof -nP -iTCP:8080 -sTCP:LISTEN` -> exit 0. Port 8080 was held for the whole session by a Python process (PID 34786, up about 46 hours), not an upw run. I waited on it with a background loop, then gave up and did not kill it. So the literal 8080 acceptance command was not run. I ran the same sequence on port 8099 instead, with a scratchpad copy of upw.dev.yaml (only `data_dir` and `http.listen` changed):
- `make build && rm -rf <scratch>/data-alt && (./bin/upw serve -config <scratch>/upw.alt.yaml & pid=$!; sleep 2; curl -s http://127.0.0.1:8099/health; echo; curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8099/; lsof -nP -iTCP:8099 -sTCP:LISTEN | tail -1; kill -TERM $pid; wait $pid; echo "exit=$?")` -> exit 0
  time=2026-11-19T12:35:15.912Z level=INFO msg="migration applied" version=1 name=0001_initial_schema.sql
  time=2026-11-19T12:35:15.913Z level=INFO msg="schedule registered" schedule=ingest cron="5 */6 * * *" jobs=""
  time=2026-11-19T12:35:15.913Z level=INFO msg="schedule registered" schedule=vulns cron="10 */6 * * *" jobs=""
  time=2026-11-19T12:35:15.913Z level=INFO msg="schedule registered" schedule=issues cron="15 * * * *" jobs=""
  time=2026-11-19T12:35:15.913Z level=INFO msg="schedule registered" schedule=llm cron="20 * * * *" jobs=""
  time=2026-11-19T12:35:15.913Z level=INFO msg="schedule registered" schedule=digest cron="0 8 * * *" jobs=""
  time=2026-11-19T12:35:15.913Z level=INFO msg="schedule registered" schedule=backup cron="30 3 * * *" jobs=""
  time=2026-11-19T12:35:15.913Z level=INFO msg="http server listening" addr=127.0.0.1:8099
  {"ok":true,"last_runs":[]}

  200
  upw     7104 amrit    9u  IPv4 0x5fdc97256a626fc0      0t0  TCP 127.0.0.1:8099 (LISTEN)
  exit=0
  (The blank line after the JSON is the response's trailing newline followed by the command's own `echo`.)
- `sqlite3 <scratch>/data-alt/upw.sqlite 'select count(*) from schema_migrations; pragma journal_mode'` -> exit 0
  1
  wal
- `make build && rm -rf data-dev && ./bin/upw run bogus -config upw.dev.yaml; echo $?` -> 
  go build -o bin/upw ./cmd/upw
  time=2026-11-19T12:34:34.711Z level=INFO msg="migration applied" version=1 name=0001_initial_schema.sql
  upw: unknown job "bogus"; known jobs: none registered yet
  1
- `./bin/upw run all -once -config upw.dev.yaml; echo $?` ->
  upw: no jobs registered yet
  0
- `./bin/upw run all -config upw.dev.yaml; echo $?` -> the message `upw: "run all" needs -once; continuous running is "upw serve"`, then the run usage, then `2`
- `rm -rf data-dev bin && git status --porcelain` -> exit 0
   M cmd/upw/main.go
   M cmd/upw/main_test.go
  ?? cmd/upw/app.go
  ?? cmd/upw/runjob.go
  ?? cmd/upw/runjob_test.go
  ?? cmd/upw/serve.go
  ?? cmd/upw/serve_test.go
  (the run happened before this report file existed; the report file is now the only other change)

Decisions:
- `-once` with a single job, and `-repo` with `run all`, are usage errors (exit 2). This follows the CLI surface in the work order, which lists `-repo` only for `<job>` and `-once` only for `all`.
- The unknown-job list is joined with ", ".
- Config and store errors are printed unwrapped as `upw: <error>`. They already name the field or step, and requirement 3 asks for the ErrSchemaTooNew text unchanged. A joined config error prints one problem per line, and only the first line has the `upw: ` prefix (that is errors.Join's own format).
- `upw run all -once` writes nothing extra to stderr when a job fails. It prints the per-job `failed:` lines on stdout and exits 1, using the internal sentinel `errReported`.
- `-log-level` uses `flag.Func`, so a bad value prints the flag package's `invalid value ... for flag -log-level: want debug, info, warn or error` and then the usage, and exits 2.
- `registeredJobs()` takes no parameters for now, because unused parameters are not allowed. Later work orders add the store and adapters as arguments when they register jobs.
- A store Close error on any exit path is joined into the command's error, so it exits 1.
- The tests check the schema_migrations row count without database/sql. The first serve must log exactly one `migration applied` line. Then reopening with `store.Open` and running `Migrate` must apply nothing and must not hit ErrSchemaTooNew.
- TestServe_SchemaTooNew inserts the version-9999 row with the `sqlite3` binary through `os/exec` in the test file, and skips when sqlite3 is missing. The work order names this route. Test files are outside the import check, but the go-code rule says only `internal/gitsrc` and `internal/testutil` use `os/exec`, so the reviewer may want to confirm this test exception.

Outside scope:
- `.go-arch-lint.yml` (lead-owned) leaves go-arch-lint's deep scan on by default. The deep scan treats passing a concrete `*store.DB` into a constructor that takes a consumer-declared interface as a dependency from `store` to the consumer. That makes the wiring this architecture requires (`pipeline.NewRunner(..., db, db, ...)`, `query.NewService(db, db, ...)`) fail the lint. Every later work order that wires the store into a service will hit the same failure. See the blocker below.

Concerns: none beyond the blocker.

Question or blocker:
- `make check` fails at the arch-lint step because of `.go-arch-lint.yml`, which I may not edit. Current config: the `allow:` block has `depOnAnyVendor: false` and `ignoreNotFoundComponents: true`, with no `deepScan` key. The lint's banner says "method calls and dependency injections ... switch 'allow.deepScan = true' (or delete) to on", so a missing key means deep scan is on. Needed: `deepScan: false` under `allow:`. The other option is to give `pipeline` and `query` (and every future consumer of a store interface) `mayDependOn: [store]`, but that would let them import the store, which the layering forbids. Why the current config cannot work: the architecture requires `cmd/upw` to pass `*store.DB` as interface values into service constructors, and the deep scan rejects exactly that pass. Implementers and callers affected: none in Go code. Only the lint config changes. I did not work around it: no interface-typed intermediate variable, no wrapper type in `cmd/upw`. I could not confirm the fix by running the lint on a scratch copy, because the sandbox refused to run go-arch-lint outside the worktree. What would unblock it: the lead adds `deepScan: false` (or an equivalent ruling) to `.go-arch-lint.yml` and reruns `make check`. Every other step of `make check` already passes, including `scripts/check-imports.sh` run by hand.
- The literal 8080 acceptance command was not run because port 8080 was taken by an unrelated long-running Python process (see Commands run). The same sequence on port 8099 gave every expected result.
