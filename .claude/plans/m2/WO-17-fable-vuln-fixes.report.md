Status: DONE
Work order: .claude/plans/m2/WO-17-fable-vuln-fixes.md
Files changed:
- internal/model/pypi.go (created)
- internal/model/pypi_test.go (created)
- internal/extract/library.go (modified)
- internal/extract/library_test.go (modified)
- internal/extract/uvlock_test.go (modified)
- internal/timeline/settings.go (modified: extractorVersion 1 -> 2 only)
- internal/timeline/settings_test.go (modified)
- internal/vuln/osv/record.go (modified)
- internal/vuln/osv/record_internal_test.go (modified)
- internal/vuln/osv/query.go (modified)
- internal/vuln/osv/query_internal_test.go (modified)
- internal/vuln/service.go (modified)
- internal/vuln/service_test.go (modified)
- internal/vuln/pypi_e2e_test.go (created)

Commands run:
- `make check` -> exit 0
  ```
  ok  	github.com/Amrit-Code/upstreamwatch/internal/ingest	1.637s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/issues/github	1.212s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	1.068s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/discord	1.267s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/email	1.293s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/ntfy	1.212s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/slack	1.209s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/stdout	1.155s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/ops	1.351s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	1.483s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	1.394s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/report	1.802s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	2.579s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	7.390s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/testutil/gitrepo	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/timeline	3.685s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/upstream/depsdev	1.218s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln	1.485s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/epss	1.164s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/kev	1.120s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/osv	1.202s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/web	1.431s
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
- `go test -race ./internal/model/ ./internal/extract/... ./internal/timeline/ ./internal/vuln/...` -> exit 0
  ```
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract/unity	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/timeline	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/epss	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/kev	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/osv	(cached)
  ```
  (cached from the `make check` run with -race just before; the same packages had passed with `-count=1` earlier.)
- `git status --porcelain` -> exit 0 (before this report file was written)
  ```
   M internal/extract/library.go
   M internal/extract/library_test.go
   M internal/extract/uvlock_test.go
   M internal/timeline/settings.go
   M internal/timeline/settings_test.go
   M internal/vuln/osv/query.go
   M internal/vuln/osv/query_internal_test.go
   M internal/vuln/osv/record.go
   M internal/vuln/osv/record_internal_test.go
   M internal/vuln/service.go
   M internal/vuln/service_test.go
  ?? internal/model/pypi.go
  ?? internal/model/pypi_test.go
  ?? internal/vuln/pypi_e2e_test.go
  ```
- `go vet -tags referee ./cmd/upw/ ./internal/...` -> exit 0; `go vet -tags e2e ./cmd/upw/ ./internal/...` -> exit 0 (the tagged referee files still compile).
- Revert checks, each with the fix disabled by a temporary edit and restored afterwards (`go test -count=1 -run ...`):
  - Requirement 1 off (extractor `packageURL` forced back to `versionless(purl)`): `TestPyPIDottedNameMatches`, `TestParse_PyPINamesNormalised`, `TestParse_UVLockDottedName`, `TestPackageURL` -> all FAIL, exit 1.
  - Requirement 2 off (affectsAny filter applied to stored ids again): `TestRun_DeltaCorrectionDropsPackage` -> FAIL (`delta_irrelevant = 1, want 0`, `advisories_saved = 0, want 1`, `resolved = [], want [...]`).
  - Requirement 3 off (cursor = newest CSV line): `TestRun_FirstCursorNotAfterRunStart` -> FAIL (`kv[osv.modified.Go] = "2026-04-02T00:15:00Z", want "2026-04-02T00:10:00Z"`, second-run `delta_refs = 0, want 1`).
  - Requirement 4 off (repo filter on unscoped runs again): `TestRun_ResolvesFindingsOfRepoWithoutIntervals` -> FAIL (`resolved = [], want [admin advLib, admin advLibB]`).
  All fixes restored, then `make check` above was run on the final code.

Decisions:
- Extractor: the PyPI package URL is rebuilt as `pkg:pypi/` + `model.PyPIName(dep.Name)` only when the library's URL has type `pypi` and `dep.Name` consists solely of ASCII letters, digits, `-`, `_`, `.`; any other name (for example a PEP 508 direct reference the library leaves unsplit, "foo @ https://...") keeps the library's percent-encoded version-less URL, so no raw `@`, `?` or `/` lands in a stored package URL. For every valid project name this is exactly `pkg:pypi/` + `PyPIName(name)`.
- `Package.Name` stays the name as written; `dedupe` (keyed by package URL) now also collapses dotted/underscore respellings in one file (first one wins), as the existing Requests/requests case already did.
- OSV query: `toQuery` applies `model.PyPIName` to the decoded PyPI name, so an interval stored before the timeline rebuild (`pkg:pypi/ruamel.yaml`) is also queried canonically.
- Removed `normalizePyPI` and its internal test `TestNormalizePyPI`; its cases are covered by `TestPyPIName` (model) plus new `toPackage`/`toQuery` rows for `ruamel.yaml` and `Zope_Interface`.
- First-run cursor: the run start is the existing `now` taken at the top of `Run`, before `queryNew`; passed to `readDeltas` as `runStart`. When the CSV has no lines at all, no cursor is stored, as before (the work order's "earlier of" needs a newest line; behaviour for an empty file unchanged).
- Resolution scope: `writeFindings` now takes the `model.Scope`; unscoped runs consider every unresolved advisory finding, scoped runs keep the RepoID filter from the in-scope intervals. A scoped run on a slug with no intervals still returns the existing ErrNotFound before reaching findings (unchanged).
- `TestPyPIDottedNameMatches` uses the real OSV adapter (`osv.New`) behind an `httptest` OSV server (querybatch, `/v1/vulns/<id>`, bucket `modified_id.csv`) rather than the in-memory `fakeSource`, so the record's `"name": "ruamel.yaml"` goes through the adapter's own mapping and the query name is asserted (`PyPI/ruamel-yaml@0.17.0`). It uses the existing `fakeStore`.
- Extra sanity tests beyond the list: `TestPackageURL` (5 rows), a pyproject case in `TestParse_PyPINamesNormalised`, and extra rows in `TestPyPIName`.

Outside scope:
- `internal/timeline/settings.go` doc comment on `settingsFingerprint` still shows the example `v1;unity=...`; the work order limited that file to the constant, so it was left. It is only an example string.
- `make e2e` was not run (needs network and the referee binaries). The parity referee (`cmd/upw/parity_e2e_test.go`, `packageName`) already compares PyPI names under full PEP 503 on both sides, so requirement 1 brings upw's identities in line with what the referee assumes. If any accepted known-difference entry was caused by a dotted or repeated-separator respelling on the upw side, it would now no longer occur and that expectation would be stale; I found none by inspection (the only dotted name in it, `Zope.Interface`, is in the referee normaliser's own unit table). The referee was not edited.
- docs-sync rows: `docs/decisions.md` M1 PyPI decision now matches the code; `docs/architecture.md`/`docs/data-access.md` descriptions of the vulns job may want: stored advisories always saved on delta, first cursor capped at run start, unscoped runs resolve every unmatched advisory finding. `model.PyPIName` is a new exported model function for `docs/domain-model.md`.

Concerns: none
Question or blocker: none

## Fix round 1

Status: DONE
Work order: .claude/plans/m2/WO-17-fable-vuln-fixes.md (rulings WO17-R2, WO17-R1), on top of ef18dcc
Files changed:
- internal/vuln/service.go (modified)
- internal/vuln/service_test.go (modified)
- internal/vuln/service_extra_test.go (modified)
- internal/vuln/integration_test.go (modified)
- internal/vuln/pypi_e2e_test.go (modified)

Commands run:
- `make check` -> exit 0
  ```
  ok  	github.com/Amrit-Code/upstreamwatch/internal/upstream/depsdev	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln	1.479s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/epss	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/kev	(cached)
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
  ```
- `go test -race -count=1 ./internal/model/ ./internal/extract/... ./internal/timeline/ ./internal/vuln/...` -> exit 0
  ```
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	1.191s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract	1.303s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract/unity	1.194s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/timeline	3.109s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln	1.343s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/epss	1.211s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/kev	1.432s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/osv	1.534s
  ```
- `git status --porcelain` -> exit 0
  ```
   M internal/vuln/integration_test.go
   M internal/vuln/pypi_e2e_test.go
   M internal/vuln/service.go
   M internal/vuln/service_extra_test.go
   M internal/vuln/service_test.go
  ```
  (plus this report file once this section is appended)
- Revert checks (temporary edits, restored before the runs above):
  - WO17-R2: restored the round-0 first-cursor logic (download the CSV, cursor = earlier of newest line and run start, nothing stored on an empty CSV or a failed download): `TestRun_FirstCursorAtRunStartWhenDeltaWouldFail` FAIL (`first Run() error = modified_id.csv: status 503, want nil`), `TestRun_FirstCursorAtRunStartWithEmptyDelta` FAIL (`kv[osv.modified.Go] = "" after the first run, want "2026-04-02T00:10:00Z"`).
  - WO17-R1: restored the early `ErrNotFound` return: `TestRun_ScopeOneRepo/repo_without_intervals` FAIL (`Run(example-admin) error = repo "example-admin": not found, want nil`).

Changes:
- WO17-R2: `readDeltas` with no stored cursor now stores the run start (`now`, taken before `queryNew`) and `continue`s without calling `ModifiedSince` for that ecosystem on that run. The round-0 "earlier of newest line and run start" code is gone.
- WO17-R1: a scoped run whose slug has no stored interval calls the new `resolveRepoWithoutIntervals`: it lists unresolved advisory findings, keeps those whose fingerprint starts with `advisory|<slug>|` (new `findingKey.fingerprintPrefix`, also used by `fingerprint`), resolves them at `now` (counted in `findings_resolved`) and returns nil; with none it returns `repo "<slug>": ... model.ErrNotFound` as before. The Run doc comment says so.
- Tests: new two-run regressions `TestRun_FirstCursorAtRunStartWithEmptyDelta` and `TestRun_FirstCursorAtRunStartWhenDeltaWouldFail` (shared helper `checkPublishedBetweenRunsFetched`: an advisory published between runs is fetched by run 2's delta from the run-1 start, raises its finding, no re-query); new subtest `TestRun_ScopeOneRepo/repo without intervals` (resolves both admin findings, leaves the server finding and an `example-admin-old` finding unresolved, then a second scoped run returns ErrNotFound). `TestRun_FirstCursorNotSetFromEmptyDelta` (old semantics) was replaced by the empty-CSV regression.
- Existing tests updated for the new first-run cursor (run start, no first-run download): `TestRun_FirstRun`, `TestRun_FetchErrorsCollected` (PyPI now has a run-1 cursor that a failed download leaves unchanged), `TestRun_EcosystemsSortedWithExtras` (checks the three cursors after run 1, then the sorted downloads on run 2), `TestRun_FindingStableAcrossRuns` (cursor = first run's start). `TestRun_SecondRunDelta` and `TestRun_FetchSetKeepsNewestModified` set the Go cursor to at(0) in the fake kv before run 2, keeping their original scenarios (a delta line at the stored time, and a delta line older than the run start). `TestPyPIDottedNameMatches` lost its now-unused bucket route; `BucketURL` still points at the fake server, so a stray bucket request fails the test instead of reaching the network.

Decisions:
- WO17-R1: the slug-without-intervals path returns straight after resolving, without querying, reading deltas, refreshing signals or replacing matches, mirroring the early return it replaces and the ruling's "resolves ... and returns nil". The repo's matches need no replacement: they cascade with its deleted `dep_versions` rows (`advisory_matches.dep_version_id ... ON DELETE CASCADE`).
- `findings_resolved` counts these resolutions; `intervals` stays 0.

Outside scope:
- docs-sync rows in addition to round 0: the vulns job's first delta cursor is the run start with no CSV download on that run; a scoped `upw run vulns --repo <slug>` for a repo with no intervals resolves its advisory findings and only errors when it has none.

Concerns: none
Question or blocker: none
