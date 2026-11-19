Status: BLOCKED
Work order: .claude/plans/m2/WO-13-wiring.md
Files changed:
- internal/sched/lastfire.go (created)
- internal/sched/lastfire_test.go (created)
- cmd/upw/notifiers.go (created)
- cmd/upw/notifiers_test.go (created)
- cmd/upw/doctor.go (modified)
- cmd/upw/doctor_test.go (modified)
- cmd/upw/jobs.go (modified: vulns and backup registered; digest not yet, see blocker 2)
- cmd/upw/jobs_test.go (modified)
- deploy/upw.example.yaml (modified: notify comments only)

Not created: cmd/upw/findings.go, cmd/upw/findings_test.go (blocker 1). main.go and main_test.go are unchanged: the usage line and dispatch for `findings` wait for the command itself.

Done and tested:
- `sched.LastFire` (requirement 5), exactly as specified under Produced.
- `notifiers.go` (requirement 2): `channelNames`, `secretUnset`, `newChannelNotifier`, `buildChannels(chs, hc, w, logger) ([]report.Channel, error)`; the stdout writer comes from the unexported package variable `notifyStdout` (default `os.Stdout`).
- `upw doctor` (requirement 3): `kev` and `epss` checks after deps.dev, one `notify <channel name>` check per channel; stdout is reported ok "writes to standard output" without a check, slack "configured (not probed)".
- `deploy/upw.example.yaml` (requirement 6, yaml half).
- vulns and backup jobs registered with the options in requirement 1, sharing one `httpx.New(db, httpx.Options{}, logger)` client.

Commands run:
- `make check` -> exit 2
  ```
  ==> gofmt
  ==> vet
  ==> build
  ==> test
  go test -race ./...
  --- FAIL: TestRun_UnknownJob (0.36s)
      runjob_test.go:25: run(["run" "bogus" "-config" ".../TestRun_UnknownJob4130752232/001/upw.yaml"]) stderr = "...upw: unknown job \"bogus\"; known jobs: ingest, timeline, vulns, backup\n", want it to end with "upw: unknown job \"bogus\"; known jobs: ingest, timeline\n"
  --- FAIL: TestRun_AllOnceNoTargets (4.92s)
      runjob_test.go:112: run(["run" "all" "-once" "-config" ".../upw.yaml"]) stdout = "ingest: succeeded cloned=0 failed=0 fetched=0 targets=0\ntimeline: succeeded commits_walked=0 deps_dropped=0 events_written=0 failed=0 files_excluded=0 files_read=0 files_skipped=0 intervals=0 parse_errors=0 resets=0 skipped=0 targets=0\nvulns: succeeded advisories_saved=0 delta_irrelevant=0 delta_refs=0 epss_errors=0 epss_updated=0 fetch_not_found=0 fetched=0 findings_resolved=0 findings_upserted=0 intervals=0 kev_errors=0 kev_updated=0 matches=0 matches_open=0 pairs_new=0 query_refs=0 unversioned=0\nbackup: succeeded deleted=0 exists=0 written=1\n", want "ingest: succeeded ...\ntimeline: succeeded ...\n"
  FAIL
  FAIL	github.com/Amrit-Code/upstreamwatch/cmd/upw	5.705s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	1.302s
  ... (every other package ok, internal/sched included)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/web	1.411s
  FAIL
  ```
  Both failures are in `cmd/upw/runjob_test.go`, which this work order does not own (blocker 3).
- `go run github.com/fe3dback/go-arch-lint@v1.19.2 check --output-color=false` -> exit 0 (`OK - No warnings found`). Run by hand because make stops at the test step.
- `sh scripts/check-imports.sh` -> exit 0 (no output)
- `go test -race -count=1 ./internal/sched/` -> exit 0 (`ok  github.com/Amrit-Code/upstreamwatch/internal/sched 1.562s`)
- `go test -race -count=1 -run 'Doctor|Notify|Channel' ./cmd/upw/` -> exit 0 (`ok  github.com/Amrit-Code/upstreamwatch/cmd/upw 1.337s`)
- `gofmt -l` (via make) -> clean after `gofmt -w cmd/upw/notifiers_test.go`
- Acceptance commands `go run ./cmd/upw run all -once -config upw.dev.yaml` and `go run ./cmd/upw findings -json -config upw.dev.yaml`: not run. The first would run without the digest job and the second command does not exist yet.
- `git status --porcelain` -> only owned files (listed above). `data-dev/` is covered by `.gitignore`.

Decisions:
- Notify checks come after the GitHub checks, last in the report: git, osv, deps.dev, kev, epss, github..., notify.... The WO says "after deps.dev: kev, epss, and one check per notify channel", which could also be read as notify before github.
- `#N` counts every configured channel of the type, including one skipped for an unset secret, so a channel's recorded name (digests.channel, which failed-send retry matches on) does not change with the environment.
- An error from `discord.New`, `slack.New` or `ntfy.New` is handled like `email.New`: `notify channel <name>: <adapter error>`. The adapter errors name at most the host.
- Doctor success text, for the channels the WO does not give one: email "login accepted", discord "webhook reachable", ntfy "server healthy".
- Doctor: a channel whose secret is unset gets no check line, because its secret line already warns. A channel that cannot be built gets a `fail` line with the build error.
- `LastFire` evaluates the expression in `now`'s location, which is what `cron.Schedule.Next` does with the scheduler's parser.
- `upw.dev.yaml` already exists (tracked) with `data_dir: ./data-dev`, public targets only and a stdout channel, so it is unchanged.

Outside scope:
- `vuln.Service` refreshes KEV and EPSS on its first run even when no advisory is stored. It downloads the whole EPSS CSV and applies nothing (`kev_updated=0 epss_updated=0` above). That is wasted bandwidth on a fresh install; whether to skip it is the lead's call.
- Docs rows to update once this lands: `docs/architecture.md` CLI (doctor probes kev, epss and each notify channel; `upw findings`), Schedules (vulns, digest and backup chains now populated; the heartbeat is now used by the digest job, so drop "the scheduler does not run it yet"), and the sentence "After M1 the Runner has two jobs". `docs/external-sources.md` doctor probes for KEV, EPSS and the notifiers.

Question or blocker:

1. `query.Service.Findings` does not exist at this base (3a40173). WO-12 produces it (`func (s *Service) Findings(ctx context.Context, f model.FindingFilter) ([]model.FindingDetail, error)` in `internal/query`), and WO-12 runs in parallel. `internal/query` is read-only here, and the rules forbid working around it by calling the store from cmd/upw. So `upw findings` (requirement 4, plus the usage line and dispatch in main.go and its three tests) was not written. Unblock: merge WO-12, then resume me on top of it.

2. Requirement 2 says an `email.New` error "fails start-up with the channel's name". `registeredJobs` cannot return an error without a change to `cmd/upw/app.go`, which is read-only here. Current call in `start`: `pipeline.NewRunner(registeredJobs(a.cfg, a.db, logger), a.db, a.db, logger)` with `func registeredJobs(cfg config.Config, db *store.DB, logger *slog.Logger) []pipeline.Job`. Needed: `func registeredJobs(cfg config.Config, db *store.DB, logger *slog.Logger) ([]pipeline.Job, error)`, with `start` doing `jobs, err := registeredJobs(...); if err != nil { return nil, errors.Join(err, a.close()) }` before `NewRunner`. Callers that change: `start` in app.go, and the two `registeredJobs` calls in jobs_test.go (owned). `buildChannels` already returns the joined, password-free error. So the digest job (and `TestRunDigest_StdoutAndEmail`) waits on this. Unblock: add app.go to the owned files for that edit, or rule another behaviour for an invalid channel.

3. `cmd/upw/runjob_test.go` is not owned, but requirement 7 necessarily breaks two of its tests: `TestRun_UnknownJob` asserts `known jobs: ingest, timeline`, and `TestRun_AllOnceNoTargets` asserts exactly the two M1 stats lines. Worse, with vulns registered, `TestRun_AllOnceNoTargets` now downloads the real KEV catalog and EPSS CSV, so a unit test touches the internet (the run took 4.9 s, and `kev_errors=0 epss_errors=0` show the downloads succeeded). Keeping it offline needs a seam, for example an unexported package variable holding the osv/kev/epss Options that the test points at an unroutable or fake URL, or a test that runs only the jobs that stay offline. That is a design choice the WO does not make. Unblock: add runjob_test.go to the owned files and rule on the seam (or move the fix to WO-14).
