Status: DONE (was BLOCKED; resolved in Fix round 1 below)
Work order: .claude/plans/m1/WO-12-timeline-direct-and-excludes.md
Files changed:
- internal/timeline/events.go (modified)
- internal/timeline/exclude.go (created)
- internal/timeline/service.go (modified)
- internal/timeline/events_test.go (modified)
- internal/timeline/service_test.go (modified)
- internal/timeline/integration_test.go (modified)
- internal/config/config.go (modified)
- internal/config/file.go (modified)
- internal/config/load.go (modified)
- internal/config/validate.go (modified)
- internal/config/load_git_test.go (modified)
- cmd/upw/jobs.go (modified)
- cmd/upw/jobs_test.go (modified)
- deploy/upw.example.yaml (modified)

Commands run:
- `git log --oneline -1` -> exit 0
  92ba0c3 web: TimelineReader interface for WO-09
- `go test -race -count=1 ./internal/timeline/ ./internal/config/ ./cmd/upw/` -> exit 1
  ok  	github.com/Amrit-Code/upstreamwatch/internal/timeline	2.570s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	1.229s
  --- FAIL: TestRun_AllOnceNoTargets (0.52s)
      runjob_test.go:108: run(["run" "all" "-once" "-config" "/var/folders/5f/3h0ch9qn69716__5htzy9vvw0000gn/T/TestRun_AllOnceNoTargets328632177/001/upw.yaml"]) stdout = "ingest: succeeded cloned=0 failed=0 fetched=0 targets=0\ntimeline: succeeded commits_walked=0 events_written=0 failed=0 files_excluded=0 files_read=0 files_skipped=0 intervals=0 parse_errors=0 resets=0 skipped=0 targets=0\n", want "ingest: succeeded cloned=0 failed=0 fetched=0 targets=0\ntimeline: succeeded commits_walked=0 events_written=0 failed=0 files_read=0 files_skipped=0 intervals=0 parse_errors=0 resets=0 skipped=0 targets=0\n"
  FAIL
  FAIL	github.com/Amrit-Code/upstreamwatch/cmd/upw	1.163s
  FAIL
- `go test -race -count=1 -v -run 'TestTimeline_IndirectBecomesDirect|TestTimeline_ExcludedPaths|TestRun_ExcludedPaths|TestRun_IndirectBecomesDirect|TestDiff$|TestDiffThenFold|TestDropExcluded' ./internal/timeline/` -> exit 0
  --- PASS: TestDropExcluded (0.00s)
  --- PASS: TestDiffThenFold_IndirectBecomesDirect (0.00s)
  --- PASS: TestRun_IndirectBecomesDirect (0.00s)
  --- PASS: TestRun_ExcludedPaths (0.00s)
  --- PASS: TestDiff (0.00s)
  --- PASS: TestTimeline_IndirectBecomesDirect (0.33s)
  --- PASS: TestTimeline_ExcludedPaths (0.35s)
  PASS
  ok  	github.com/Amrit-Code/upstreamwatch/internal/timeline	1.540s
- `make check` -> exit 2 (make reports Error 1 from the test step)
  ==> gofmt
  ==> vet
  go vet ./...
  ==> build
  go build ./...
  ==> test
  go test -race ./...
  --- FAIL: TestRun_AllOnceNoTargets (0.33s)
      runjob_test.go:108: run(["run" "all" "-once" "-config" "/var/folders/5f/3h0ch9qn69716__5htzy9vvw0000gn/T/TestRun_AllOnceNoTargets1420833679/001/upw.yaml"]) stdout = "ingest: succeeded cloned=0 failed=0 fetched=0 targets=0\ntimeline: succeeded commits_walked=0 events_written=0 failed=0 files_excluded=0 files_read=0 files_skipped=0 intervals=0 parse_errors=0 resets=0 skipped=0 targets=0\n", want "ingest: succeeded cloned=0 failed=0 fetched=0 targets=0\ntimeline: succeeded commits_walked=0 events_written=0 failed=0 files_read=0 files_skipped=0 intervals=0 parse_errors=0 resets=0 skipped=0 targets=0\n"
  FAIL
  FAIL	github.com/Amrit-Code/upstreamwatch/cmd/upw	0.892s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	1.124s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract	1.392s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract/unity	1.223s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	2.744s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	1.572s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/ingest	1.703s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/issues/github	1.216s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	1.172s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	1.460s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	1.521s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	1.569s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	3.855s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/testutil/gitrepo	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/timeline	2.644s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/upstream/depsdev	1.233s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/osv	1.224s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/web	1.389s
  FAIL
  make: *** [check] Error 1
- Because `make check` stops at the test step, I ran its last two steps by hand:
  `go run github.com/fe3dback/go-arch-lint@v1.19.2 check --output-color=false` -> exit 0
  OK - No warnings found
  `sh scripts/check-imports.sh` -> exit 0 (no output)
- `rm -rf data-dev && go build -o bin/upw ./cmd/upw && bin/upw run ingest -config upw.dev.yaml -repo upstreamwatch` -> exit 0
  ingest: succeeded cloned=1 failed=0 fetched=1 targets=1
- `bin/upw run timeline -config upw.dev.yaml -repo upstreamwatch` -> exit 0
  timeline: succeeded commits_walked=8 events_written=33 failed=0 files_excluded=2 files_read=9 files_skipped=0 intervals=33 parse_errors=0 resets=0 skipped=0 targets=1
- `bin/upw timeline upstreamwatch -config upw.dev.yaml` -> exit 0 (open go.mod rows, plus the manifests listed)
  pkg:golang/github.com/git-pkgs/manifests  v0.14.0  2026-11-19  now  yes  go.mod
  pkg:golang/github.com/robfig/cron/v3      v3.0.1   2026-11-19  now  yes  go.mod
  pkg:golang/go.yaml.in/yaml/v3             v3.0.6   2026-11-19  now  yes  go.mod
  pkg:golang/golang.org/x/mod               v0.43.0  2026-11-19  now  yes  go.mod
  pkg:golang/modernc.org/sqlite             v1.62.0  2026-11-19  now  yes  go.mod
  (every `// indirect` module shows `no`; the only manifests listed are deploy/Dockerfile, .github/workflows/ci.yml and go.mod; no internal/extract/testdata/ manifest appears)
  Result: as expected. The five direct requires in go.mod each have a closed `no` interval followed by an open `yes` interval, because an earlier commit listed them as indirect.
- `rm -rf data-dev bin && git status --porcelain` -> exit 0
   M cmd/upw/jobs.go
   M cmd/upw/jobs_test.go
   M deploy/upw.example.yaml
   M internal/config/config.go
   M internal/config/file.go
   M internal/config/load.go
   M internal/config/load_git_test.go
   M internal/config/validate.go
   M internal/timeline/events.go
   M internal/timeline/events_test.go
   M internal/timeline/integration_test.go
   M internal/timeline/service.go
   M internal/timeline/service_test.go
  ?? internal/timeline/exclude.go
  (the report file is written after this command was run)

Decisions:
- The exclusion rules are pure functions in a new file, internal/timeline/exclude.go: `excludedPath(path, prefixes)` and `dropExcluded(commits, prefixes)`. The second returns copies of the commits and leaves its input untouched.
- A commit whose paths are all excluded is passed to ApplyTimeline with `Paths` set to an empty slice. The store does not read `Commit.Paths`.
- A prefix is matched after trimming its trailing slashes, so `examples`, `examples/` and `examples//` all exclude the same paths. An empty prefix matches nothing.
- The always-skipped directory names (`testdata`, `vendor`, `node_modules`) are matched exactly and case-sensitively against directory segments only. A file named `vendor` and a directory named `Vendor` are not skipped.
- `exclude_paths` entries go through `${NAME}` expansion, like every other target string list (`ecosystems`).
- Validation messages, one per bad entry: `targets[i].exclude_paths[j]: must not be empty`, `...: "<p>" must be relative to the repository root, without a leading /` and `...: "<p>" must not contain a .. segment`. The rejected value is shown, which is safe because these paths are not secrets.
- The example config documents the key as `exclude_paths: []` on the `server` target, with a comment giving the format, so copying the example changes nothing.
- The diff case where the direct flag and the scope both change emits `changed` carrying the new scope. This follows from requirement 1, since the direct flag differs.

Outside scope:
- `cmd/upw/runjob_test.go:105-106` (not owned) asserts the exact timeline stats line. It has to gain `files_excluded=0`; see the blocker.
- docs-sync should pick up: the new config key `targets[].exclude_paths` (config section of docs/architecture.md, or wherever the config keys are listed), the timeline job's new stats key `files_excluded`, and the `TargetSettings.ExcludePaths` field. docs/decisions.md already records both rules.
- Validation accepts `.` segments and backslashes (`./examples`, `a\b`). An entry like `./examples` never matches, because git paths never begin with `./`. The work order rejects only `..`, a leading `/` and the empty string, so I added no further rules.

Question or blocker:
`make check` fails in a file I do not own: `cmd/upw/runjob_test.go`, test `TestRun_AllOnceNoTargets`. Requirement 2 adds the stats key `files_excluded`. Like every timeline stats key, it is reported even when zero, and the CLI prints the keys sorted. The test's expected stdout on line 106 is missing it. The fix is one line: change
  "timeline: succeeded commits_walked=0 events_written=0 failed=0 files_read=0 files_skipped=0 intervals=0 parse_errors=0 resets=0 skipped=0 targets=0\n"
to
  "timeline: succeeded commits_walked=0 events_written=0 failed=0 files_excluded=0 files_read=0 files_skipped=0 intervals=0 parse_errors=0 resets=0 skipped=0 targets=0\n"
I did not work around it by emitting the key only when it is non-zero. That would break the "present even when zero" contract the other stats keys follow. Everything else is finished: every other package passes, the lint and import checks pass, and the dev-instance acceptance run gives the expected result. To unblock, the lead can make that one-line edit or add `cmd/upw/runjob_test.go` to this work order's owned files.

## Fix round 1

Status: DONE
Work order: .claude/plans/m1/WO-12-timeline-direct-and-excludes.md
Files changed:
- cmd/upw/runjob_test.go (modified): following the 2026-11-19 Ruling, `files_excluded=0` is inserted in sorted position in TestRun_AllOnceNoTargets's expected timeline stats line. No other line changed.
Commands run:
- `make check` -> exit 0
  go build ./...
  ==> test
  go test -race ./...
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	1.882s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract/unity	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/ingest	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/issues/github	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	(cached)
  ?   	github.com/Amrit-Code/upstreamwatch/internal/testutil/gitrepo	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/timeline	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/upstream/depsdev	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/osv	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/web	(cached)
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
- `go test -race -count=1 ./internal/timeline/ ./internal/config/ ./cmd/upw/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/timeline	2.470s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	1.154s
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	2.197s
- `git status --porcelain` -> exit 0
  The same owned files as before, plus cmd/upw/runjob_test.go and this report. `.claude/plans/m1/WO-12-timeline-direct-and-excludes.md` also shows as modified; that is the lead's Rulings edit, and I did not touch it.
- The dev-instance acceptance run was not repeated. This round changed only a test expectation, so that run's result still stands.
Decisions: none. `files_excluded` is still emitted when zero, as the Ruling requires.
Outside scope: the docs-sync items listed in the first round are unchanged.

## Gate (lead, 2026-11-19)

BLOCKED round 0 resolved by ruling (cmd/upw/runjob_test.go added for the stats line). make check rerun: exit 0. Lead reran the dev acceptance on the dogfood target: files_excluded=2, no internal/extract/testdata manifests, the five direct go.mod requires show DIRECT yes. Sonnet reviewer: ACCEPT, one minor (exclude_paths entries like `./x` validate but never match) dismissed as harmless. Accepted.
