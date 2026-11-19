Status: DONE_WITH_CONCERNS
Work order: .claude/plans/m1/WO-08-wiring-config-timeline-cli.md
Files changed:
- internal/config/config.go (modified): `Target.AllowPrivateHosts`, new `Git` type, `Config.Git`
- internal/config/file.go (modified): `targets[].allow_private_hosts`, `git:` section with defaults 2048 / 30m / 10m
- internal/config/load.go (modified): converts the new keys; durations are `${NAME}`-expanded and then parsed, with errors naming the field
- internal/config/validate.go (modified): `git.max_clone_mb` at least 1; `positiveDuration` helper
- internal/config/load_test.go (modified): defaults include `Git`; `TestLoad_DevConfig` expects the four dev targets
- internal/config/load_git_test.go (created)
- internal/query/service.go (modified): `NewService(runs, findings, timeline, logger)`
- internal/query/timeline.go (created): `TimelineFilter`, `Timeline`, `Repos`
- internal/query/timeline_test.go (created)
- internal/query/service_test.go (modified): new constructor argument only
- internal/query/integration_test.go (modified): new constructor argument only
- cmd/upw/app.go (modified): start-up split into `openApp` (config + store) and `start` (adds the Runner); `registeredJobs` moved out
- cmd/upw/jobs.go (created): `registeredJobs`, `gitOptions`, `ingestTargets`, `timelineTargets`
- cmd/upw/jobs_test.go (created)
- cmd/upw/timeline.go (created): `upw timeline`
- cmd/upw/timeline_test.go (created)
- cmd/upw/main.go (modified): `timeline` dispatch and help line
- cmd/upw/serve.go (modified): passes the store as the query service's TimelineStore
- cmd/upw/main_test.go (modified): help lists `timeline`; `timeline -h` case
- cmd/upw/runjob_test.go (modified): unknown-job message now lists `ingest, timeline`; `TestRun_AllOnceNoJobs` replaced by `TestRun_AllOnceNoTargets` (both jobs succeed with zero targets)
- deploy/upw.example.yaml (modified): `allow_private_hosts` on the first target, new `git:` section
- upw.dev.yaml (modified): glow, cleo, naughtyattributes (unity), upstreamwatch, all `auth: none`

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
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	1.820s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	1.125s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract	1.327s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract/unity	1.183s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	2.714s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	1.536s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/ingest	1.174s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/issues/github	1.204s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	1.173s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	1.457s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	1.378s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	1.406s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	3.491s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/testutil/gitrepo	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/timeline	1.218s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/upstream/depsdev	1.238s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/osv	1.237s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/web	1.317s
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
  ```
  (The first `make check` run stopped at gofmt on internal/config/load.go; fixed with `gofmt -w` and the run above is the rerun.)
- `go test -race -count=1 -cover ./cmd/upw/ ./internal/query/ ./internal/config/` -> exit 0
  ```
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	1.804s	coverage: 92.1% of statements
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	1.421s	coverage: 100.0% of statements
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	1.282s	coverage: 98.3% of statements
  ```
- `go build -o bin/upw ./cmd/upw && bin/upw run ingest -config upw.dev.yaml -repo upstreamwatch && bin/upw run timeline -config upw.dev.yaml -repo upstreamwatch && bin/upw timeline upstreamwatch -config upw.dev.yaml` -> exit 0 (stderr log lines and stdout interleaved as shown)
  ```
  time=2026-11-19T18:54:51.498Z level=INFO msg="migration applied" version=1 name=0001_initial_schema.sql
  time=2026-11-19T18:54:51.499Z level=INFO msg="migration applied" version=2 name=0002_timeline.sql
  time=2026-11-19T18:54:52.861Z level=INFO msg="git sync finished" repo=upstreamwatch branch=main head=57a07ec89e5c3d6753e418911c9fd3622909939e cloned=true
  time=2026-11-19T18:54:52.861Z level=INFO msg="target synced" job=ingest slug=upstreamwatch branch=main head=57a07ec89e5c3d6753e418911c9fd3622909939e cloned=true
  time=2026-11-19T18:54:52.862Z level=INFO msg="job finished" job=ingest run_id=1 duration_ms=1362 cloned=1 failed=0 fetched=1 targets=1
  ingest: succeeded cloned=1 failed=0 fetched=1 targets=1
  time=2026-11-19T18:54:52.902Z level=INFO msg="job finished" job=timeline run_id=2 duration_ms=31 commits_walked=8 events_written=33 failed=0 files_read=11 files_skipped=0 intervals=33 parse_errors=0 resets=0 skipped=0 targets=1
  timeline: succeeded commits_walked=8 events_written=33 failed=0 files_read=11 files_skipped=0 intervals=33 parse_errors=0 resets=0 skipped=0 targets=1
  PACKAGE                                          VERSION                                   FROM        TO          DIRECT  MANIFEST
  pkg:docker/alpine                                3.22                                      2026-11-19  now         yes     deploy/Dockerfile
  pkg:docker/golang                                1.27-alpine                               2026-11-19  now         yes     deploy/Dockerfile
  pkg:githubactions/actions/checkout               3d3c42e5aac5ba805825da76410c181273ba90b1  2026-11-19  now         yes     .github/workflows/ci.yml
  pkg:githubactions/actions/setup-go               d8cbb8045d7f43706b9177cceb36c1f91a6644d5  2026-11-19  now         yes     .github/workflows/ci.yml
  pkg:golang/github.com/BurntSushi/toml            v1.6.0                                    2026-11-19  now         no      go.mod
  pkg:golang/github.com/bazelbuild/buildtools      v0.0.0-20260716142318-04cf7de1434f        2026-11-19  now         no      go.mod
  pkg:golang/github.com/bmatcuk/doublestar/v4      v4.10.4                                   2026-11-19  now         no      go.mod
  pkg:golang/github.com/dustin/go-humanize         v1.1.0                                    2026-11-19  now         no      go.mod
  pkg:golang/github.com/git-pkgs/manifests         v0.14.0                                   2026-11-19  now         no      go.mod
  pkg:golang/github.com/git-pkgs/pom               v0.1.8                                    2026-11-19  now         no      go.mod
  pkg:golang/github.com/git-pkgs/purl              v0.1.24                                   2026-11-19  2026-11-19  no      go.mod
  pkg:golang/github.com/git-pkgs/purl              v0.2.1                                    2026-11-19  now         no      go.mod
  pkg:golang/github.com/git-pkgs/vers              v0.7.2                                    2026-11-19  now         no      go.mod
  pkg:golang/github.com/google/uuid                v1.6.0                                    2026-11-19  now         no      go.mod
  pkg:golang/github.com/mattn/go-isatty            v0.0.24                                   2026-11-19  now         no      go.mod
  pkg:golang/github.com/ncruces/go-strftime        v1.1.0                                    2026-11-19  now         no      go.mod
  pkg:golang/github.com/package-url/packageurl-go  v0.1.7                                    2026-11-19  now         no      go.mod
  pkg:golang/github.com/remyoudompheng/bigfft      v0.0.0-20230129092748-24d4a6f8daec        2026-11-19  now         no      go.mod
  pkg:golang/github.com/robfig/cron/v3             v3.0.1                                    2026-11-19  now         no      go.mod
  pkg:golang/go.yaml.in/yaml/v3                    v3.0.6                                    2026-11-19  now         no      go.mod
  pkg:golang/golang.org/x/mod                      v0.43.0                                   2026-11-19  now         no      go.mod
  pkg:golang/golang.org/x/sys                      v0.49.0                                   2026-11-19  now         no      go.mod
  pkg:golang/modernc.org/libc                      v1.80.1                                   2026-11-19  now         no      go.mod
  pkg:golang/modernc.org/mathutil                  v1.7.1                                    2026-11-19  now         no      go.mod
  pkg:golang/modernc.org/memory                    v1.12.1                                   2026-11-19  now         no      go.mod
  pkg:golang/modernc.org/sqlite                    v1.62.0                                   2026-11-19  now         no      go.mod
  pkg:golang/toolchain                             1.27                                      2026-11-19  now         yes     deploy/Dockerfile
  pkg:golang/toolchain                             1.27                                      2026-11-19  now         yes     go.mod
  pkg:pypi/certifi                                 2025.8.3                                  2026-11-19  now         ?       internal/extract/testdata/uv.lock
  pkg:pypi/click                                   8.1.7                                     2026-11-19  now         ?       internal/extract/testdata/poetry.lock
  pkg:pypi/example-git                             1.0.0                                     2026-11-19  now         ?       internal/extract/testdata/poetry.lock
  pkg:pypi/pytest                                  8.3.3                                     2026-11-19  now         ?       internal/extract/testdata/poetry.lock
  pkg:pypi/requests                                2.32.5                                    2026-11-19  now         ?       internal/extract/testdata/uv.lock
  ```
  Expected result met: both runs succeed; the table lists the go.mod modules, `pkg:golang/toolchain`, `pkg:docker/golang` and `pkg:docker/alpine` from `deploy/Dockerfile`, and the two Actions from `.github/workflows/ci.yml`. See Concerns for the DIRECT column and the testdata rows.
- `bin/upw run timeline -config upw.dev.yaml -repo upstreamwatch` (second time) -> exit 0
  ```
  time=2026-11-19T18:55:22.283Z level=INFO msg="job finished" job=timeline run_id=3 duration_ms=21 commits_walked=0 events_written=0 failed=0 files_read=0 files_skipped=0 intervals=0 parse_errors=0 resets=0 skipped=0 targets=1
  timeline: succeeded commits_walked=0 events_written=0 failed=0 files_read=0 files_skipped=0 intervals=0 parse_errors=0 resets=0 skipped=0 targets=1
  ```
  Expected result met: `commits_walked=0`, `events_written=0`.
- `bin/upw timeline upstreamwatch pkg:docker/golang -json -config upw.dev.yaml 2>/dev/null; bin/upw timeline nope -config upw.dev.yaml; bin/upw timeline upstreamwatch -on 2026-13-01 -config upw.dev.yaml >/dev/null 2>&1` (extra manual check) -> as shown
  ```
  [
    {
      "package": "pkg:docker/golang",
      "ecosystem": "docker",
      "name": "golang",
      "version": "1.27-alpine",
      "requirement": "sha256:70bd7ddea8b6cbe5d6943cde279d6b67feb10cc924fb9100d1d8757c1575b280",
      "direct": true,
      "manifest": "deploy/Dockerfile",
      "from": "2026-11-19T12:22:44Z",
      "to": null,
      "from_sha": "4a972304f9585703c40c05675f063ff2b5b744ec",
      "to_sha": null
    }
  ]
  upw: unknown repo "nope"
  unknown exit=1
  bad date exit=2
  ```
- `bin/upw run all -once -config upw.dev.yaml -log-level warn` (extra check: all four dev targets, including the Unity one) -> exit 0
  ```
  ingest: succeeded cloned=3 failed=0 fetched=4 targets=4
  timeline: succeeded commits_walked=499 events_written=1602 failed=0 files_read=628 files_skipped=0 intervals=1423 parse_errors=0 resets=0 skipped=0 targets=4
  ```
  `bin/upw timeline naughtyattributes` then listed `pkg:generic/unity-editor` intervals from `ProjectSettings/ProjectVersion.txt`, so the Unity extractor is wired. `data-dev/` and `bin/` were deleted afterwards (both gitignored).
- `git status --porcelain` -> exit 0
  ```
   M cmd/upw/app.go
   M cmd/upw/main.go
   M cmd/upw/main_test.go
   M cmd/upw/runjob_test.go
   M cmd/upw/serve.go
   M deploy/upw.example.yaml
   M internal/config/config.go
   M internal/config/file.go
   M internal/config/load.go
   M internal/config/load_test.go
   M internal/config/validate.go
   M internal/query/integration_test.go
   M internal/query/service.go
   M internal/query/service_test.go
   M upw.dev.yaml
  ?? cmd/upw/jobs.go
  ?? cmd/upw/jobs_test.go
  ?? cmd/upw/timeline.go
  ?? cmd/upw/timeline_test.go
  ?? internal/config/load_git_test.go
  ?? internal/query/timeline.go
  ?? internal/query/timeline_test.go
  ```
  Only owned files (this report was written after the command ran).

Decisions:
- Text output "header line": read as the column-title row `PACKAGE VERSION FROM TO DIRECT MANIFEST`, with no separate repo line above it.
- tabwriter settings: minwidth 0, padding 2, space padding, no flags. The last column is not padded, so lines have no trailing spaces.
- Positional arguments: the slug, then an optional package URL, must come before any flag, as with `upw run <job>`. A first argument that starts with `-` or is empty is the usage error "missing repo slug". A third positional argument is `unexpected argument`.
- `-on` is parsed with `time.Parse(time.DateOnly, ...)`, so it is strict (`2026-4-1`, `2026-02-30` and RFC 3339 are rejected). The flag error text is `want a date as YYYY-MM-DD`, which the flag package prints as `invalid value "<v>" for flag -on: ...`, followed by the usage. Exit 2.
- When both version and requirement are empty, VERSION prints `()`, as the `(<requirement>)` rule says literally.
- JSON is written with `SetEscapeHTML(false)` so requirements such as `>=2.31` are not escaped to `>=2.31`. Times use `time.RFC3339` in UTC.
- `query.Timeline` returns a nil slice when no interval passes the filter. The CLI prints `[]` for JSON either way.
- Duration keys are `${NAME}`-expanded like other string values, then parsed. An unparseable value and a non-positive value are each reported once, as `git.clone_timeout: "x" is not a duration such as 30m or 1h30m` and `git.clone_timeout: must be positive, got 0s`. The check runs where the value is converted (as literal secrets are), so a bad value is not also reported as non-positive.
- `MaxBlobBytes` is left at zero in `gitOptions` (gitsrc default 5 MiB), because the work order does not mention it.
- Start-up was split into `openApp` (config, store, migrate) for `upw timeline` and `start` (`openApp` plus the Runner) for `serve` and `run`. The wiring helpers live in the new `cmd/upw/jobs.go`.
- `TestRegisteredJobs_Order` passes a nil `*store.DB`, because the constructors only keep it and do no I/O.
- The example config documents `allow_private_hosts: false` on the first example target and puts the `git:` section before `schedules:`.

Outside scope:
- The `runAll` branch in `cmd/upw/runjob.go` that prints "upw: no jobs registered yet" can no longer be reached, because two jobs are always registered. I left it in place.
- Rows to update in docs (for docs-sync): in `docs/architecture.md` "CLI", `upw timeline` is now built, and the sentence "After M0 no job is registered with the Runner..." is out of date (ingest and timeline are registered). In "Schedules", the ingest chain now runs ingest and timeline. The new config keys `targets[].allow_private_hosts` and `git.max_clone_mb`, `git.clone_timeout` and `git.fetch_timeout` (defaults 2048, 30m, 10m) need adding wherever the config is documented. `query.Service` gains `Timeline` and `Repos` and takes a `TimelineStore` (docs/data-access.md store-interface users: query uses `GetRepo`, `ListRepos`, `ListIntervals`).

Concerns:
- DIRECT is `no` for go.mod modules that are direct today (git-pkgs/manifests, robfig/cron, yaml, x/mod, sqlite). Their intervals opened at a commit where go.mod marked them `// indirect`. Under the recorded M1 decision "a change of only the direct flag or scope emits no event", the interval keeps the directness it had when it opened. The CLI prints what the store holds; my store round-trip test confirms yes, no and ? come through unchanged. An operator may still find this misleading, so it may be worth a ruling (for example, refresh directness on the open interval without emitting an event).
- The dogfood timeline includes lockfiles under `internal/extract/testdata/` (`pkg:pypi/requests`, `click`, `pytest`, a fake `example-git`), because the extractor patterns (`**/uv.lock`, `**/poetry.lock`) match any path. Test fixtures in a target will show up as dependencies, and in M2 they would be matched against advisories. That belongs to extract and timeline (WO-02 and WO-06), not this work order.

## Gate (lead, 2026-11-19)

make check rerun: exit 0. Lead reran the live acceptance on the dogfood target: ingest and timeline succeed, `upw timeline upstreamwatch` lists the go.mod modules, both toolchain intervals, the Docker images and the Actions; the second timeline run reports commits_walked=0 events_written=0; an unknown repo exits 1 with the message. Sonnet reviewer: ACCEPT, one minor (unreachable "no jobs registered yet" branch in runAll), deferred. The two concerns (stale direct flag, test fixtures indexed) are ruled on in docs/decisions.md and fixed in WO-12. Accepted.
