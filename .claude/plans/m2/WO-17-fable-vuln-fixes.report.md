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
