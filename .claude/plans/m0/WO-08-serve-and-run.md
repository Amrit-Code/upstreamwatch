# WO-08: `upw serve` and `upw run` wiring in cmd/upw

Milestone: M0
Agent: go-mech   Model: opus
Depends on: WO-03, WO-04, WO-05, WO-06, WO-07 (all merged)
Report file: `.claude/plans/m0/WO-08-serve-and-run.report.md`
Tickets: UPW-17 (the `upw serve` half), UPW-15 (`upw run`)

## Objective

`cmd/upw` becomes the wiring: it loads the config, opens and migrates the store, builds the Runner, the read service, the scheduler and the web server, and exposes them as `upw serve` and `upw run`. After this work order the binary starts, serves `/health` and the findings page, logs its schedules and shuts down in order. Serves M0 checklist items 7 to 14 and 16.

## Owned files

- `cmd/upw/` (all files)

Lead-owned, read-only for you (stop and report if one is wrong): everything else, including every package under `internal/`, `go.mod`, `go.sum`, `.go-arch-lint.yml`, `Makefile`, `upw.dev.yaml`, `deploy/`.

## Interfaces

### Consumed

Read the exported API of these packages from the source; the doc comments are the contract:

```go
// internal/config
func Load(path string, getenv func(string) string) (Config, error)

// internal/store
func Open(ctx context.Context, path string, logger *slog.Logger) (*DB, error)
func (db *DB) Migrate(ctx context.Context) error
func (db *DB) Close() error
var ErrSchemaTooNew

// internal/pipeline
func NewRunner(jobs []Job, runs RunStore, findings FindingStore, logger *slog.Logger) (*Runner, error)
func (r *Runner) Run(ctx context.Context, job string, scope model.Scope) (model.Run, error)
func (r *Runner) Jobs() []string
var ErrUnknownJob

// internal/query
func NewService(runs RunStore, findings FindingStore, logger *slog.Logger) *Service

// internal/sched
type Entry struct{ Name, Cron string; Jobs []string }
func New(runner Runner, entries []Entry, logger *slog.Logger) (*Scheduler, error)
func (s *Scheduler) Start(ctx context.Context)
func (s *Scheduler) Stop(ctx context.Context) error

// internal/web
type Options struct{ Listen, BaseURL string }
func NewServer(health HealthReader, findings FindingsReader, opts Options, logger *slog.Logger) (*Server, error)
func (s *Server) Serve(ctx context.Context) error
```

### Produced

The CLI surface:

```
upw serve [-config path] [-log-level level]
upw run <job> [-repo slug] [-config path] [-log-level level]
upw run all -once [-config path] [-log-level level]
upw help | -h | --help
```

and, for later work orders, one function in `cmd/upw` that returns the registered jobs in pipeline order (empty in M0), so each milestone adds its jobs in one place.

## Requirements

1. Structure follows the go-code skill's "The CLI" section and `templates/cmd-upw.md`: `main` stays thin; `run(ctx, args, stdout, stderr, getenv) int` dispatches on `args[0]`; each subcommand has its own `flag.NewFlagSet(name, flag.ContinueOnError)` writing to stderr; exit codes 0 success, 1 failure, 2 usage error; `-h` on a subcommand prints that subcommand's usage to stdout and exits 0. The top-level usage now lists `serve` and `run`, and nothing that does not exist.
2. Every subcommand registers `-config` (default `upw.yaml`) and `-log-level` (`debug`, `info`, `warn`, `error`; default `info`; anything else is a usage error). Logs go to stderr through one `slog.TextHandler`; stdout carries command output only.
3. Shared start-up, used by both subcommands, in this order: `config.Load(path, getenv)`; `store.Open(ctx, filepath.Join(cfg.DataDir, "upw.sqlite"), logger)`; `Migrate`; build the Runner from the registered jobs with the store as `RunStore` and `FindingStore`. Any failure prints `upw: <error>` to stderr and returns 1; a config error prints every joined problem, one per line. When `Migrate` fails with `store.ErrSchemaTooNew` the message is the store's text unchanged (it says the database is newer than the binary). The store is closed on every exit path after it was opened.
4. `upw serve`, after the shared start-up:
   - builds `query.NewService(db, db, logger)` and `web.NewServer(q, q, web.Options{Listen: cfg.HTTP.Listen, BaseURL: cfg.HTTP.BaseURL}, logger)`;
   - builds one `sched.Entry` per config schedule key, in this order and with these job chains (from `docs/architecture.md`, "Schedules"): `ingest` → `ingest, timeline, usage, upstream`; `vulns` → `vulns`; `issues` → `issues, prefilter`; `llm` → `triage`; `digest` → `digest`; `backup` → `backup`. A job name that is not registered with the Runner is dropped from its chain, so an entry may have no jobs; the entry is still passed to the scheduler, which logs it;
   - starts the scheduler, then calls `web.Server.Serve(ctx)`, which blocks;
   - on return shuts down in order: the HTTP server has already stopped; `Scheduler.Stop` with a 30-second timeout; the store closed last;
   - returns 0 when the context was cancelled (SIGINT or SIGTERM) and everything stopped cleanly, 1 if `Serve` or `Stop` returned an error.
   Nothing listens before the config is valid and the database is migrated: with a config whose first target `url` is `file:///tmp/x`, `serve` returns 1 without opening the database or a socket, and stderr names `targets[0].url` and the scheme.
5. `upw run <job>`: takes the positional job name off the front before parsing flags. After the shared start-up it calls `Runner.Run(ctx, job, model.Scope{RepoSlug: repo})`.
   - unknown job (`errors.Is(err, pipeline.ErrUnknownJob)`): stderr `upw: unknown job "<name>"; known jobs: <comma-separated names>` (or `known jobs: none registered yet`), return 1;
   - job failure or store error: `upw: <error>` on stderr, return 1;
   - success: one line on stdout, `<job>: succeeded` followed by each stat as ` <name>=<value>` in name order, return 0;
   - missing job name: usage on stderr, return 2.
6. `upw run all -once` runs every registered job in pipeline order, continuing after a failure, printing the same per-job line on stdout (`<job>: failed: <error>` for a failure), and returns 1 if any job failed. With no registered jobs it prints `upw: no jobs registered yet` to stderr and returns 0. `upw run all` without `-once` is a usage error (2) whose message says continuous running is `upw serve`.
7. `cmd/upw` holds no logic beyond parsing, wiring and formatting. It does not import `database/sql` or `os/exec`.

## Tests to include

All through `run(...)` with temp directories and a `getenv` func; configs are written to `t.TempDir()` with `data_dir` inside it and `http.listen: 127.0.0.1:0`.

- `TestServe_HealthAndShutdown`: start `serve` in a goroutine; read the listening address from the `http server listening` log line on the captured stderr (use a concurrency-safe buffer); `GET /health` → 200 and `"ok":true`; `GET /` → 200 containing `No open findings.`; cancel the context → `run` returns 0 within 5 s. Afterwards the database file exists with one `schema_migrations` row (open it with `store.Open` in the test) and stderr has one `schedule registered` line for each of the six schedule keys with its cron expression.
- `TestServe_SecondStartIsNoop`: run `serve` twice on the same data directory; both return 0; `schema_migrations` unchanged.
- `TestServe_BadTargetScheme`: requirement 4's last paragraph; assert no database file was created.
- `TestServe_SchemaTooNew`: insert a `schema_migrations` row with version 9999 through `database/sql` is not allowed here, so prepare it with the `sqlite3` binary if present (skip otherwise) or by any route that keeps `cmd/upw` test code inside the import rules; `serve` returns 1 and stderr says the database is newer.
- `TestRun_UnknownJob`, `TestRun_MissingJob`, `TestRun_AllWithoutOnce`, `TestRun_AllOnceNoJobs`.
- `TestRun_FlagsAfterJobName`: `run somejob -repo x -config <path>` parses both flags (it then fails with unknown job, exit 1, not a usage error).
- `TestLogLevel_Invalid`: exit 2.
- Keep the WO-01 tests (`TestRunNoArgs`, `TestRunHelp`, `TestRunUnknownCommand`) passing, updated for the new usage text.

## Acceptance

Run from the repository root.

| Command | Expected |
|---|---|
| `make check` | exit 0 |
| `go test -race -count=2 ./cmd/upw/` | `ok` |
| `make build && rm -rf data-dev && (./bin/upw serve -config upw.dev.yaml & pid=$!; sleep 2; curl -s http://127.0.0.1:8080/health; echo; curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8080/; lsof -nP -iTCP:8080 -sTCP:LISTEN \| tail -1; kill -TERM $pid; wait $pid; echo "exit=$?")` | `{"ok":true,"last_runs":[]}`, `200`, a line showing `127.0.0.1:8080`, `exit=0`; stderr shows six `schedule registered` lines |
| `sqlite3 data-dev/upw.sqlite 'select count(*) from schema_migrations; pragma journal_mode'` | `1` and `wal` |
| `./bin/upw run bogus -config upw.dev.yaml; echo $?` | the unknown-job message, then `1` |
| `./bin/upw run all -once -config upw.dev.yaml; echo $?` | `upw: no jobs registered yet`, then `0` |
| `rm -rf data-dev bin; git status --porcelain` | only owned files and the report file |

## Out of scope

- `upw doctor` (WO-11), `upw timeline`, `findings`, `export`, `llm`, `eval` (later milestones).
- Any job. The registered-jobs function returns an empty slice.
- Changing any package under `internal/`. If one of them lacks something this work order needs, report BLOCKED with the signature you need.

## Skills and docs

Preloaded by your agent definition; do not reload. Load with the Skill tool: none.
Read: `.claude/skills/go-code/templates/cmd-upw.md`; `docs/architecture.md` ("Entry points", "Schedules"); `.claude/skills/milestone-accept/references/M0-SKELETON.md` (items 7 to 16, which this work order must make pass).

## Report

Follow the report contract in your agent definition: full report to the report file named at the top, then the short reply. Do not commit, stash or otherwise write to git. If a question's answer would change behaviour and nothing here or in the docs answers it, report NEEDS_CONTEXT rather than guessing.

## Rulings

- 2026-11-19: Schedules whose jobs are not built yet are still registered and logged, with an empty job list, so the start-up log always shows every configured schedule (checklist item 16) and nothing raises an unknown-job failure on a tick.
