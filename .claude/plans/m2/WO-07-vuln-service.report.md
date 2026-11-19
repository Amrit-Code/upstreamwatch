Status: DONE_WITH_CONCERNS
Work order: .claude/plans/m2/WO-07-vuln-service.md
Files changed:
- internal/vuln/service.go (created)
- internal/vuln/service_test.go (created)
- internal/vuln/fakes_test.go (created)
- internal/vuln/integration_test.go (created)

Commands run:
- `gofmt -l internal/vuln` -> exit 0 (no output)
- `go vet ./internal/vuln/` -> exit 0
- `go test -race -count=1 ./internal/vuln/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln	1.413s
- `go test -race -count=1 -coverprofile=... ./internal/vuln/` + `go tool cover -func` -> exit 0
  service.go per function: NewService 87.5%, Run 87.5%, queryNew 91.4%, readDeltas 80.8%,
  fetchAll 91.5%, advanceCursors 87.5%, refreshSignals 82.1%, signalsDue 77.8%,
  buildMatches 96.2%, replaceMatches 91.7%, writeFindings 83.3%, buildFindings 100%,
  linkedMatch 87.5%, the small helpers 80-100%.
- `make check` -> exit 0
  ==> test
  go test -race ./...
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	2.103s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	1.334s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract	1.424s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract/unity	1.198s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/findings	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	9.207s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	1.619s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/ingest	1.676s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/issues/github	1.214s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	1.188s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/ops	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	1.520s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	1.445s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/report	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	1.396s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	7.392s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/testutil/gitrepo	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/timeline	3.696s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/upstream/depsdev	1.210s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln	1.265s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/epss	1.263s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/kev	1.220s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/osv	1.302s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/web	1.519s
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
- `go test -race -count=1 -run 'TestRun' ./internal/vuln/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln	1.322s
- `git status --porcelain` -> exit 0
  ?? internal/vuln/fakes_test.go
  ?? internal/vuln/integration_test.go
  ?? internal/vuln/service.go
  ?? internal/vuln/service_test.go
  (run before the report was written; afterwards `git status --porcelain .claude/plans/m2/` also lists
  `?? .claude/plans/m2/WO-07-vuln-service.report.md`)

Tests written (all in package vuln_test): TestService_Name; TestRun_FirstRun; TestRun_SecondRunDelta;
TestRun_NewPairOnly; TestRun_FindingStableAcrossRuns (integration, real store); TestRun_ResolveVanished;
TestRun_SignalsDaily (2 subtests: daily cadence, failing EPSS feed); TestRun_ExposureBasisPin (3 rows:
pin, published, empty); TestRun_ScopeOneRepo (2 subtests: one repo, unknown slug);
TestRun_FetchErrorsCollected (fetch error collected and redacted, not-found counted, delta_irrelevant,
cursor held back, failed PyPI delta download collected); TestRun_StopsOnStoreAndQueryFailures (4 rows);
TestRun_Cancelled.

Decisions:
- Unknown slug: the interfaces give no repo lookup, so a slug is unknown when no stored interval has it. A stored repo with no intervals therefore also gives the ErrNotFound error.
- Store failures that requirement 9 does not name (QueriedVersions, MarkQueried, AdvisoryModified, SaveAdvisory, GetKV, SetKV, ApplySignals, ListUnresolvedFindings, ResolveFinding) stop the run, the same as the store calls it does name.
- A failed `ModifiedSince` download is handled like a fetch or refresh error: logged at Warn, that ecosystem's cursor left alone, the run continues through matching and findings, and the error is returned joined at the end.
- A kv cursor or refresh time that does not parse as RFC 3339 stops the run (only this job writes those keys).
- Cursor and refresh times are written with `time.RFC3339Nano` in UTC. This is valid RFC 3339 and keeps any fraction of a second the source gave; whole-second times print as `2026-04-01T00:00:00Z`.
- The fetch decision compares the ref's `Modified`, truncated to whole seconds, with the stored time, because the store keeps whole seconds and a fraction would otherwise refetch an unchanged record.
- "Older than SignalsEvery" is read strictly: a feed is refreshed when `now - refreshed_at > SignalsEvery`.
- `delta_refs` counts only refs from reads against a stored cursor, the ones that join the fetch set. The first-run listing that only sets the cursor is not counted.
- The delta ecosystems are taken from every stored interval with an OSV ecosystem, unversioned ones included ("used by any stored interval"), plus `EcosystemsExtra`. Empty strings are dropped, and the list is deduplicated and sorted.
- Intervals with no version are also skipped in matching (docs/decisions.md: "neither queried nor matched").
- `ExposureBasis` values other than "pin" mean published. Validating the value belongs to config (WO-13).
- Step order: query batch, then delta reads, fetch and save, cursor advance, signals, ListAdvisories, matching, ReplaceMatches, findings, resolve. Signals are applied after this run's saves and before ListAdvisories, so findings rank with that day's KEV and EPSS values.
- Ties in the linked interval go to the interval that comes first in ListAllIntervals order.
- Collected errors are joined and returned as one error. Its text has been through `model.Redact`, and it still unwraps to the originals, so `errors.Is` works.
- Logging: one Info line `vulns run finished` with the main counts, logged on every return path. Warn lines `vulns advisory fetch failed`, `vulns delta download failed` and `vulns signal refresh failed` carry only the id, ecosystem or feed plus the redacted error text, never advisory text.
- Tests are in the external package `vuln_test`, so they do not clash with names in WO-06's internal test files (`adv`, `pinned`, `entry` and others).

Outside scope:
- Doc rows now out of date, for docs-sync: in docs/architecture.md, the job table row for `vulns` lists the tables it writes as advisories, advisory_affected, advisory_matches and findings; it also writes `osv_queries` (MarkQueried) and `kv` (keys `osv.modified.<ecosystem>` and `signals.<kev|epss>.refreshed_at`). docs/data-access.md should list `vuln.Service` as a user of `AdvisoryStore`, `KVStore` and the vuln `FindingStore`, if it does not already.
- `pipeline.Runner` already redacts job error text (internal/pipeline/redact.go). The service's own redaction follows requirement 9 and repeats that pattern in an unexported `redactedError` type in package vuln.

Concerns:
- Requirement 2 marks new pairs as queried as soon as QueryBatch succeeds. If fetching one of the refs that batch returned then fails with a transient error (not not-found), that advisory is never retried: the pair is not queried again, and the delta lists the id only if OSV modifies the record after the cursor. The run returns an error that time, but the advisory can stay missing for good. Two possible fixes: call MarkQueried only when every query-ref fetch succeeded, or keep the failed ids in kv and retry them on the next run. I followed the work order as written.
- The unknown-slug rule above means `upw run vulns --repo <slug>` for a repo that exists but has no intervals yet returns not found, not an empty run.

## Fix round 1

Status: DONE
Work order: .claude/plans/m2/WO-07-vuln-service.md (rulings 1 and 2 from the lead)
Files changed:
- internal/vuln/service.go (modified)
- internal/vuln/service_test.go (modified)
- .claude/plans/m2/WO-07-vuln-service.report.md (modified: this section)

Commands run:
- `go test -race -count=1 ./internal/vuln/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln	1.393s
- Check that the new test fails without the fix: I changed the guard to mark pairs regardless of failed fetches and ran `go test -race -count=1 -run TestRun_QueryRefFetchRetried ./internal/vuln/` -> exit 1
  service_test.go:608: stats["findings_upserted"] = 3, want 5
  service_test.go:608: stats["pairs_new"] = 0, want 3
  service_test.go:608: stats["fetched"] = 0, want 1
  FAIL	github.com/Amrit-Code/upstreamwatch/internal/vuln	0.293s
  The file was then restored from a copy; the guard reads `if len(newPairs) > 0 && !slices.ContainsFunc(queryRefs, ...)`.
- `make check` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln	1.257s
  (every other package ok or [no test files], as in the first round)
  ==> arch-lint
  OK - No warnings found
  ==> import-check
  sh scripts/check-imports.sh
- `go test -race -count=1 -run 'TestRun' ./internal/vuln/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln	1.257s
- `git status --porcelain` -> exit 0
   M internal/vuln/service.go
   M internal/vuln/service_test.go
  (plus this report once it was appended)

Changes:
- Ruling 1: `queryNew` no longer calls MarkQueried. It returns the new pairs together with the query refs. After the fetch phase, `Run` marks those pairs queried at now, but only when no query-batch ref is among the ids whose fetch failed. A not-found result is counted and does not count as a failure. If any query-ref fetch failed, none of this run's new pairs are marked, so the next run queries them again and retries the fetch. A MarkQueried failure still stops the run. The `Run` doc comment says this.
- Ruling 2: no change. An in-scope slug with no stored intervals is still an error wrapping model.ErrNotFound.
- New test `TestRun_QueryRefFetchRetried`:
  - "transient fetch error": run 1 fails with a fetch error and leaves every pair unmarked. Run 2 sends the same 3 pairs to QueryBatch again, fetches only the missing advisory, saves it, and marks the 3 pairs at run 2's time.
  - "not found counts as fetched": a query ref whose record is missing still lets the pairs be marked.

Decisions: none new.
Outside scope: none new.
Concerns: none. First-round concern 1 is fixed by ruling 1, and concern 2 is settled by ruling 2.
