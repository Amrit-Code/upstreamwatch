# WO-17: Fixes from the Fable review of M2 (PyPI names, delta corrections, cursor gap, resolution scope)

Milestone: M2
Agent: go-builder   Model: opus
Depends on: WO-16 (merged; it edits `internal/vuln/osv/record.go` first)
Report file: `.claude/plans/m2/WO-17-fable-vuln-fixes.report.md`
Ticket: UPW-61 (M2 acceptance)

## Objective

Fix four defects the end-of-milestone review found across the timeline and the vulns job. Two are major: dotted or repeated-separator PyPI names never match an advisory, and a corrected advisory delivered through the delta is discarded so its stale copy keeps a finding open forever.

## Owned files

- `internal/model/pypi.go`, `internal/model/pypi_test.go` (new)
- `internal/extract/` (production and test files that build PyPI package URLs: `library.go`, `extract.go`, `uvlock.go` and their tests)
- `internal/timeline/settings.go` (the `extractorVersion` constant only) and `internal/timeline/settings_test.go`
- `internal/vuln/osv/record.go`, `internal/vuln/osv/record_internal_test.go`, `internal/vuln/osv/query.go`, `internal/vuln/osv/query_internal_test.go`
- `internal/vuln/service.go`, `internal/vuln/service_test.go`, `internal/vuln/service_extra_test.go`, `internal/vuln/integration_test.go`, `internal/vuln/fakes_test.go`
- `internal/vuln/pypi_e2e_test.go` (new; an ordinary test, no build tag)

Read-only: everything else, including every other file in `internal/model/`, `internal/store/`, `go.mod`, `go.sum`.

## Requirements

1. (major) One canonical PyPI name, PEP 503: `model.PyPIName(name string) string` lower-cases and replaces each run of `-`, `_` and `.` with a single `-`. The extractor builds every PyPI package URL as `pkg:pypi/` + `model.PyPIName(name)` (requirements files, pyproject, poetry.lock, uv.lock and any other PyPI source), keeping `Package.Name` as written. The OSV adapter uses `model.PyPIName` for both the query name and the record mapping (replace its private `normalizePyPI`). Bump `extractorVersion` in `internal/timeline/settings.go` so every stored timeline rebuilds with the new package URLs on its next run. This makes the code match the M1 decision in `docs/decisions.md` ("runs of -, _ and . as -").
2. (major) In `fetchAll`, a fetched record is saved whenever its id is already stored, whatever its affected packages; the `affectsAny` filter (stat `delta_irrelevant`) applies only to delta records whose id is not stored. A stored advisory corrected upstream to drop the package therefore loses its match on the same run, and its finding resolves.
3. (minor) First-run delta cursor: when an ecosystem has no cursor, store the earlier of the newest `modified_id.csv` time and the run's start time (taken before `queryNew`), so a record created between the querybatch and the CSV download is picked up by the next delta.
4. (minor) Resolution scope: resolve unresolved advisory findings not produced by this run for every repo in scope, where an unscoped run covers every unresolved advisory finding (not only repos that still have intervals) and a scoped run covers findings whose `RepoID` is that repo's. A repo whose intervals all vanished therefore has its advisory findings resolved.

## Tests to include

- `TestPyPIName` (table: `ruamel.yaml`, `Zope_Interface`, `Foo--Bar`, `a._-b`, already canonical).
- Extractor tests: a requirements file and a uv.lock with dotted names produce `pkg:pypi/ruamel-yaml`.
- `TestPyPIDottedNameMatches` (`internal/vuln/pypi_e2e_test.go`): parse a requirements file with `ruamel.yaml==0.17.0` through `internal/extract`, build the interval, run `vuln.Service` with a fake source whose record names `ruamel.yaml` in OSV form, and get a match and a finding. It must fail without requirement 1.
- `TestRun_DeltaCorrectionDropsPackage`: stored advisory affects the package, delta brings a newer record without it → saved, match removed, finding resolved.
- `TestRun_FirstCursorNotAfterRunStart` and `TestRun_ResolvesFindingsOfRepoWithoutIntervals`.
- Each new test must fail with its fix reverted; say in the report that you checked.

## Acceptance

| Command | Expected |
|---|---|
| `make check` | exit 0 |
| `go test -race ./internal/model/ ./internal/extract/... ./internal/timeline/ ./internal/vuln/...` | `ok` |
| `git status --porcelain` | only owned files and the report file |

## Out of scope

- Clearing KEV flags that disappear from the feed (deferred), marking only shown digest items reported (the cap ruling stands), the M1 parity referee's expectations (if `make e2e` would change because of requirement 1, say so in the report; do not edit the referee).

## Skills and docs

Preloaded: go-code, upw-architecture. Read: `docs/decisions.md` (M1 PyPI decision, M2 decisions), PLAN.md 5.4.

## Report

Write the full report to the report file named at the top, in the form given in your agent definition, then reply with only the short form.

## Rulings

