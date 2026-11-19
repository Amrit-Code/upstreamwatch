# WO-07: vuln.Service, the vulns job

Milestone: M2
Agent: go-builder   Model: opus
Depends on: WO-01, WO-03, WO-04, WO-05, WO-06 (merged to main)
Report file: `.claude/plans/m2/WO-07-vuln-service.report.md`
Ticket: UPW-47

## Objective

`vuln.Service` implements `pipeline.Job` as `vulns`: it queries OSV for new package versions, applies the `modified_id.csv` delta, fetches and stores changed records, refreshes KEV and EPSS once a day, rebuilds `advisory_matches` for the repos in scope with the WO-06 rules, and raises one advisory finding per (repo, package, advisory) ranked with `model.RankAdvisory`. Serves M2 checklist items 2, 3, 5, 6, 7 and 8.

## Owned files

- `internal/vuln/service.go`, `internal/vuln/service_test.go`, `internal/vuln/fakes_test.go` (new)
- `internal/vuln/integration_test.go` (new; real store in a temp dir, fake sources)

Read-only: `internal/vuln/interfaces.go`, `internal/vuln/doc.go`, `internal/vuln/match.go` and `version.go` (WO-06; call `evaluate`, `exposureWindow`, `normalizeVersion`, the `exposureBasis` constants), everything in `internal/model/`, `internal/store/`, `internal/vuln/osv`, `kev`, `epss`, `go.mod`, `go.sum`. If a WO-06 function is wrong, report it; do not edit it.

## Interfaces

### Consumed

Everything in `internal/vuln/interfaces.go` (copy nothing; read the file): `AdvisorySource`, `ExploitSignals`, `AdvisoryStore`, `KVStore`, `FindingStore`. `internal/pipeline/interfaces.go` `Job`. `internal/model`: `RepoInterval`, `PackageVersion`, `AdvisoryRef`, `Advisory`, `AdvisoryMatch`, `Signal`, `OSVEcosystem`, `Finding`, `KindAdvisory`, `RankAdvisory`, `AdvisoryRankInput`, `Redact`, `ErrNotFound`.

### Produced

```go
// internal/vuln/service.go
type Options struct {
	ExposureBasis   string        // "published" (default when empty) or "pin"
	EcosystemsExtra []string      // OSV ecosystem names whose modified_id.csv is read even if no package uses them
	SignalsEvery    time.Duration // minimum gap between KEV/EPSS refreshes; default 20h
	Now             func() time.Time
}
func NewService(src AdvisorySource, kev, epss ExploitSignals, store AdvisoryStore, kv KVStore, findings FindingStore, opts Options, logger *slog.Logger) *Service
func (s *Service) Name() string // "vulns"
func (s *Service) Run(ctx context.Context, scope model.Scope) (model.Stats, error)
```

`kev` or `epss` may be nil (signals not refreshed).

## Requirements

1. Intervals: `ListAllIntervals`. With `scope.RepoSlug` set, matching, match replacement and findings cover only that repo, and querybatch covers only its pairs; the delta and signals still run (they are global). An unknown slug is an error wrapping `model.ErrNotFound`.
2. Query set: distinct `(Package.PURL, Version)` pairs from the in-scope intervals (open and closed) with a non-empty version and `model.OSVEcosystem` true; intervals with an empty version are counted in stat `unversioned` and skipped. Pairs already in `QueriedVersions` are not sent. New pairs go to `QueryBatch`; after it succeeds they are `MarkQueried` at now.
3. Delta, per OSV ecosystem used by any stored interval (all repos) plus `EcosystemsExtra`, sorted: the cursor is kv key `osv.modified.<ecosystem>` holding an RFC 3339 UTC time. Missing cursor: call `ModifiedSince(eco, time.Time{})` and store the newest ref's `Modified` as the cursor without fetching any of them (the querybatch covers what exists); an empty list stores nothing. Present cursor: `ModifiedSince(eco, cursor)`; those refs join the fetch set; after their records are processed without error, store the greatest `Modified` as the new cursor.
4. Fetch set: union of querybatch refs and delta refs by id, keeping the greatest `Modified`. `AdvisoryModified` gives what is stored; fetch an id when it is not stored or the ref's `Modified` is after the stored one. A fetched record from the delta (not from the querybatch) none of whose `Affected` packages is used by any stored interval is not saved (stat `delta_irrelevant`). `Fetch` returning `model.ErrNotFound` counts `fetch_not_found` and continues. Other fetch errors fail the job after the rest of the run (see 9).
5. Signals: for each non-nil feed, if kv key `signals.<kind>.refreshed_at` is missing or older than `SignalsEvery`, `Refresh` then `ApplySignals(signals, now)` and store now under that key. A refresh error is logged, counted (`kev_errors`/`epss_errors`), keeps the stored values, and is returned at the end (requirement 9); the rest of the run continues.
6. Matching: `ListAdvisories` once; skip withdrawn advisories. For every in-scope interval, for every advisory with an `Affected` entry whose package URL equals the interval's, `evaluate` each such entry; if one or more are affected, record one match for (interval, advisory) using the first affected entry in record order, with `exposureWindow(interval, advisory.Published, basis)`, its fixed version, confidence and reason. Then `ReplaceMatches(repoID, matches)` for every in-scope repo, including repos with no matches.
7. Findings, one per (repo, package URL, advisory id) that has at least one match: fingerprint `advisory|<repo slug>|<package URL>|<advisory id>`; `DepVersionID` the open matched interval with the latest `From` (else the closed one with the latest `To`); `RankAdvisory` input: advisory severity, KEV, EPSS (`HasEPSS`, `EPSSPercentile`), `Direct` = `DirectYes` if any matched open interval is direct (any matched interval when none is open), `CurrentlyExposed` = any match open, `Confidence` = the best confidence among the open matches (among all when none open); `RankReason` = the rank reason, then `; fixed in <v>` when the linked match has a fixed version. `UpsertFinding` with `Kind` advisory, repo, package, dep version and advisory IDs, `LastChangedAt` now.
8. Resolve: every unresolved advisory finding (`ListUnresolvedFindings(model.KindAdvisory)`) of an in-scope repo whose fingerprint was not produced in step 7 is resolved at now (its advisory was withdrawn, or the intervals it matched are gone).
9. Errors: a failure of `ListAllIntervals`, `QueryBatch`, `ListAdvisories`, `ReplaceMatches` or a finding write stops the run and returns it. Fetch errors other than not-found and signal refresh errors are collected; the run continues through matching and findings and then returns them joined, with text passed through `model.Redact`. A context cancellation stops promptly.
10. Stats (all always present, zero when nothing happened): `intervals`, `unversioned`, `pairs_new`, `query_refs`, `delta_refs`, `fetched`, `fetch_not_found`, `delta_irrelevant`, `advisories_saved`, `kev_updated`, `epss_updated`, `kev_errors`, `epss_errors`, `matches`, `matches_open`, `findings_upserted`, `findings_resolved`.
11. One info log line per run with the main counts; no advisory text in logs.

## Tests to include

Unit tests with hand-written fakes:

- `TestRun_FirstRun`: two repos, intervals for a Go module, `pkg:golang/toolchain` 1.26 and a docker image → only Go pairs queried (docker skipped); cursor set for `Go` from the newest delta ref without fetching delta records; query refs fetched and saved; matches replaced per repo; one finding per (repo, package, advisory) with the right fingerprint, dep version, score and reason; a GO stdlib advisory affects the toolchain interval (checklist item 6).
- `TestRun_SecondRunDelta`: second run with the same intervals → no querybatch call; delta since the stored cursor returns one changed id (newer than stored) and one unchanged → only the changed one fetched; cursor advanced.
- `TestRun_NewPairOnly`: a new interval version appears → only that pair is queried.
- `TestRun_FindingStableAcrossRuns` (integration, real store): two runs with nothing changed → same finding ID, `LastChangedAt` unchanged (relies on WO-02's rule).
- `TestRun_ResolveVanished`: an advisory becomes withdrawn → its finding resolved, match removed.
- `TestRun_SignalsDaily`: signals refreshed on the first run, not on a second run 1 hour later, again after 21 hours; a failing EPSS feed → run returns an error, matches and findings still written, `epss_errors` 1.
- `TestRun_ExposureBasisPin`: with `ExposureBasis: "pin"` the match's `ExposedFrom` equals the interval start.
- `TestRun_ScopeOneRepo`: scope limits matches and findings to that repo; unknown slug → `ErrNotFound`.

## Acceptance

| Command | Expected |
|---|---|
| `make check` | exit 0 |
| `go test -race -run 'TestRun' ./internal/vuln/` | `ok` |
| `git status --porcelain` | only owned files and the report file |

## Out of scope

- Registering the job, config wiring, `upw findings` (WO-13). Digest (WO-10). Auto-resolve when the pin leaves the range while history remains (M5).

## Skills and docs

Load with the Skill tool: `upw-architecture` is preloaded; load nothing else unless needed.
Read: PLAN.md 5.4 and 6 (Ranking); `docs/decisions.md` "Lead decisions for M2"; `docs/architecture.md` job table.

## Report

Write the full report to the report file named at the top, in the form given in your agent definition, then reply with only the short form. If a question's answer would change behaviour and nothing here or in the docs answers it, stop and report NEEDS_CONTEXT rather than guessing.

## Rulings

