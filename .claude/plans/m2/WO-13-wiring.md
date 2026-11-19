# WO-13: Wiring: vulns, digest and backup jobs, notifiers from config, doctor, `upw findings`

Milestone: M2
Agent: go-mech   Model: opus
Depends on: WO-01 to WO-11 (merged); runs beside WO-12, which owns `cmd/upw/serve.go`
Report file: `.claude/plans/m2/WO-13-wiring.report.md`
Ticket: UPW-47, UPW-53, UPW-56 (wiring halves)

## Objective

`upw serve` and `upw run` gain the `vulns`, `digest` and `backup` jobs, built from config with every enabled notifier and the heartbeat; `upw doctor` checks KEV, EPSS and each notifier; a new `upw findings [-json]` command prints the ranked findings. Serves M2 checklist items 2, 3, 6, 8, 10, 11, 12, 13 and 19.

## Owned files

- `cmd/upw/jobs.go`, `cmd/upw/jobs_test.go`
- `cmd/upw/doctor.go`, `cmd/upw/doctor_test.go`
- `cmd/upw/main.go`, `cmd/upw/main_test.go` (usage text and the new command's dispatch)
- `cmd/upw/findings.go`, `cmd/upw/findings_test.go` (new)
- `cmd/upw/notifiers.go`, `cmd/upw/notifiers_test.go` (new)
- `cmd/upw/fakes_test.go` (only if a test helper is needed)
- `internal/sched/lastfire.go`, `internal/sched/lastfire_test.go` (new)
- `deploy/upw.example.yaml` (notify section comments only)
- `upw.dev.yaml` if it exists, else create it: a dev config with public fixture targets only, `data_dir: ./data-dev`, and a `stdout` notify channel (it must contain nothing private)

Read-only: `cmd/upw/serve.go` (WO-12 edits it), `cmd/upw/app.go` (`scheduleEntries` already lists the `vulns`, `digest` and `backup` chains), every `internal/` package other than `internal/sched/lastfire*.go`, `go.mod`, `go.sum`.

## Interfaces

### Consumed

Read the merged code for exact signatures: `vuln.NewService` and `vuln.Options` (`internal/vuln/service.go`); `osv.New`, `kev.New`, `epss.New`; `report.NewService`, `report.Channel`, `report.Options`; `email.New`/`email.Options`, `stdout.New`, `discord.New`, `slack.New`, `ntfy.New`; `ops.NewBackup`, `ops.BackupOptions`; `httpx.New` (pass the `*store.DB` as its `CacheStore` for the jobs); `query.NewService` and `query.Service.Findings`; `config.Config` (`Vulns`, `Notify`, `Retention`, `HTTP.BaseURL`); `sched.parseFiveField` (unexported, in the same package as your new file).

### Produced

```go
// internal/sched/lastfire.go
// LastFire returns the most recent time at or before now at which the
// five-field cron expression fires, searching back at most 35 days; ok is
// false when it does not fire in that window.
func LastFire(expr string, now time.Time) (t time.Time, ok bool, err error)
```

## Requirements

1. `registeredJobs` returns, in order: ingest, timeline, vulns, digest, backup, all wrapped in the existing job locks. The HTTP adapters for the jobs share one `httpx.New(db, httpx.Options{}, logger)` client. vulns: `osv.New(hc, osv.Options{}, logger)`, `kev.New`, `epss.New` with default options, `vuln.Options{ExposureBasis: cfg.Vulns.ExposureBasis, EcosystemsExtra: cfg.Vulns.EcosystemsExtra}`. backup: `ops.BackupOptions{Dir: <data_dir>/backups, Keep: cfg.Retention.BackupsKeep}`. digest: `report.Options{BaseURL: strings.TrimRight(cfg.HTTP.BaseURL, "/"), HeartbeatLastFire: <from sched.LastFire(cfg.Notify.Heartbeat, now), logging and returning ok=false on error>}` and the channels of requirement 2.
2. `notifiers.go` builds one `report.Channel` per `cfg.Notify.Channels` entry, in order. Names are the type (`email`, `discord_webhook`, `slack_webhook`, `ntfy`, `stdout`); a second channel of the same type is `<type>#2`, a third `<type>#3`. email → `email.New(email.Options{Host, Port, Username, Password, From, To}, logger)` (an error from `New` fails start-up with the channel's name, never the password); stdout → `stdout.New(os.Stdout)` (inject the writer for tests); discord, slack, ntfy → `New(hc, ch.URL, logger)`. A channel whose secret is empty (variable unset) is skipped with a warning log naming the channel, not an error.
3. `upw doctor` adds, after deps.dev: `kev` (`www.cisa.gov reachable`) and `epss` (`epss.empiricalsecurity.com reachable`) checks, and one check per notify channel named `notify <channel name>` using the adapter's `Check` (stdout has none: report it as ok "writes to standard output" without a check; slack's `Check` makes no request: success text "configured (not probed)"). The existing secret lines already show each channel's secret only as its last four characters; keep that and add a test that the email password never appears in full in doctor output.
4. `upw findings [-json] [-repo <slug>] [-suppressed] [-config ...] [-log-level ...]`: opens the store (no Runner), calls `query.Service.Findings` with the filter, and prints either a table (columns: `SCORE`, `KIND`, `REPO`, `PACKAGE`, `VERSION`, `ID`, `SEVERITY`, `CONFIDENCE`, `STATE`; ID is the advisory id, or the job name for a job_failure) with every field escaped like `upw timeline` output, or with `-json` a JSON array of objects with keys `id`, `kind`, `score`, `rank_reason`, `repo`, `package`, `version`, `advisory_id`, `severity`, `confidence`, `exposed_now` (bool), `state`, `first_seen`, `last_changed`, `url` (base URL + `/f/<id>`, omitted when no base URL). Highest score first (the query order). An unknown `-repo` slug is an error.
5. `LastFire`: uses the same parser as the scheduler (seconds fields and descriptors rejected); finds the last fire at or before `now` by stepping `Schedule.Next` from `now - 35 days`; `now` itself counts when the expression fires exactly then.
6. Usage text lists `findings`. `deploy/upw.example.yaml` documents that several channels may be listed and that the email password must be an environment reference.
7. `upw run all -once` now runs ingest, timeline, vulns, digest and backup in that order (it already runs every registered job); `upw run vulns`, `upw run digest`, `upw run backup` work.

## Tests to include

- `TestRegisteredJobs_Order` → names ingest, timeline, vulns, digest, backup.
- `TestChannels_Names` (two stdout and one email → `stdout`, `stdout#2`, `email`); `TestChannels_SkipUnsetSecret`; `TestChannels_EmailInvalid` (error names the channel, not the password).
- `TestDoctor_NotifyChecks` with fake checkers; `TestDoctor_PasswordRedacted`.
- `TestFindings_Table`, `TestFindings_JSON` against a real temp store seeded with an advisory finding (use the store methods); `TestFindings_UnknownRepo`.
- `TestLastFire` (table): weekly Monday 08:00 at Monday 08:00 → that instant; Monday 07:59 → previous Monday; Wednesday → last Monday; a yearly expression that last fired 200 days ago → ok false.
- `TestRunDigest_StdoutAndEmail` (cmd/upw level, may use `run` with a temp config): config with a `stdout` channel and an `email` channel pointed at a fake SMTP server on loopback (port from the test), one seeded open finding → `upw run digest` writes the digest to the injected stdout and the fake SMTP server receives the same text (checklist item 11). If the plumbing for an injected stdout writer makes this test awkward, keep the writer injectable through an unexported package variable.

## Acceptance

| Command | Expected |
|---|---|
| `make check` | exit 0 |
| `go test -race ./cmd/upw/ ./internal/sched/` | `ok` |
| `go run ./cmd/upw run all -once -config upw.dev.yaml` (network) | exit 0; stats lines for each job; the digest printed to stdout |
| `go run ./cmd/upw findings -json -config upw.dev.yaml` | a JSON array, highest score first |
| `git status --porcelain` | only owned files, `data-dev/` (gitignored; check `.gitignore` covers it and report if not) and the report file |

## Out of scope

- `serve.go` and web pages (WO-12). The parity referee (WO-15). Full test suites (WO-14).

## Skills and docs

Preloaded: go-code, upw-architecture. Load with the Skill tool: `outbound-client` if you touch adapter construction details.
Read: `docs/architecture.md` (CLI, Schedules); `docs/decisions.md` "Lead decisions for M2".

## Report

Write the full report to the report file named at the top, in the form given in your agent definition, then reply with only the short form. If a question's answer would change behaviour and nothing here or in the docs answers it, stop and report NEEDS_CONTEXT rather than guessing.

## Rulings

- 2026-11-19 (lead), on the BLOCKED report: (1) WO-12 is merged; the lead merged `main` into your branch (227fdca), so `query.Service.Findings` exists. Write `upw findings` now. (2) `cmd/upw/app.go` is added to your owned files for one change: `registeredJobs` returns `([]pipeline.Job, error)` and `start` returns that error (naming the channel, never the password) before building the Runner; update its callers. (3) `cmd/upw/runjob_test.go` is added to your owned files: update the expected job lists and `run all -once` output for the five jobs. Keep unit tests offline with an unexported package variable in `jobs.go` holding the OSV, KEV and EPSS endpoint options (zero value = production defaults); tests point it at local `httptest` servers that answer with empty results. No cmd/upw unit test may reach the internet.
