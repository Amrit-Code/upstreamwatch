# WO-01: Store methods for advisories, matches, feed cursors and finding detail

Milestone: M2
Agent: go-builder   Model: opus
Depends on: none (interface files committed)
Report file: `.claude/plans/m2/WO-01-store-advisories.report.md`
Ticket: UPW-47 (store half of the vulnerability job)

## Objective

`*store.DB` implements `vuln.AdvisoryStore`, `vuln.KVStore` and `GetFindingDetail` (consumed by `report.Store` and the web pages later), over the tables `advisories`, `advisory_affected`, `advisory_matches`, `osv_queries`, `kv`, and reads of `findings`, `repos`, `packages`, `dep_versions`. Serves M2 checklist items 2 to 7.

## Owned files

- `internal/store/advisories.go` (new)
- `internal/store/kv.go` (new)
- `internal/store/finding_detail.go` (new)
- `internal/store/advisories_test.go`, `internal/store/kv_test.go`, `internal/store/finding_detail_test.go` (new)

Read-only for you (another work order, WO-02, edits `findings.go`, `db.go`, `runs.go`, `repos.go`, `httpcache.go` in parallel; you may call their unexported helpers such as `scanFinding`, `formatTime`, `parseTime`, `nullID`, `nullText`, `scanner`, and the interval scanning in `timeline.go`, but not change them):

- every other file in `internal/store/`, including `migrations/` (`0004_vulns.sql` is lead-owned)
- `internal/model/*.go`, `internal/vuln/interfaces.go`, `internal/report/interfaces.go`
- `go.mod`, `go.sum`

If you need a helper that does not exist, write it in one of your own files with a name that cannot collide (prefix it `adv` or `kv`).

## Interfaces

### Consumed

`internal/model/advisory.go`, `internal/model/finding_detail.go`, `internal/model/finding.go`, `internal/model/timeline.go`: read them in full; every type below comes from there.

`internal/store/migrations/0004_vulns.sql` adds `osv_queries (purl, version, queried_ts, created_ts, updated_ts, PRIMARY KEY (purl, version))`. The other tables are in `0001_initial_schema.sql` and `0002_timeline.sql`.

### Produced (methods on `*store.DB`)

```go
// internal/vuln/interfaces.go: AdvisoryStore
ListAllIntervals(ctx context.Context) ([]model.RepoInterval, error)
QueriedVersions(ctx context.Context) ([]model.PackageVersion, error)
MarkQueried(ctx context.Context, pvs []model.PackageVersion, at time.Time) error
AdvisoryModified(ctx context.Context, ids []string) (map[string]time.Time, error)
SaveAdvisory(ctx context.Context, a model.Advisory) error
ListAdvisories(ctx context.Context) ([]model.Advisory, error)
ApplySignals(ctx context.Context, signals []model.Signal, at time.Time) (int, error)
ReplaceMatches(ctx context.Context, repoID int64, matches []model.AdvisoryMatch) error

// internal/vuln/interfaces.go: KVStore
GetKV(ctx context.Context, key string) (string, error)
SetKV(ctx context.Context, key, value string) error

// internal/report/interfaces.go: Store (this method only)
GetFindingDetail(ctx context.Context, id int64) (model.FindingDetail, error)
```

The doc comments in `internal/vuln/interfaces.go` and `internal/report/interfaces.go` are the contract; the requirements below add the storage details.

## Requirements

1. Every write method (`MarkQueried`, `SaveAdvisory`, `ApplySignals`, `ReplaceMatches`, `SetKV`) runs inside `db.InTx`, so it takes the in-process write queue. No write runs on `db.q` outside a transaction.
2. Storage formats in `advisories`: `aliases` is a JSON array of strings (`[]` when none); `severity_label` the `model.SeverityLabel` string, NULL when empty; `cvss_score` NULL when `CVSSVector` is empty; `published_ts`, `modified_ts`, `withdrawn_ts`, `kev_added_ts`, `epss_ts` ISO-8601 UTC text via `formatTime`, NULL when zero; `epss` and `epss_percentile` NULL when `HasEPSS` is false; `raw` the bytes of `a.Raw` as text, NULL when nil.
3. `advisory_affected` per `AffectedPackage`: `package_id` from `packages` by `Package.PURL` (insert the package with its `Ecosystem` and `Name` if missing, the same way `ApplyTimeline` upserts packages: an existing row keeps its name); `ranges` a JSON array in OSV shape `[{"type":"SEMVER","repo":"...","events":[{"introduced":"0"},{"fixed":"1.2.3"}]}]` (omit `repo` when empty, each event object has exactly one key); `versions` a JSON array (`[]` when none); `ecosystem_specific` the raw JSON or NULL; `source_db` the `SourceDB` field. `ListAdvisories` reads these back into equal values (round trip).
4. `SaveAdvisory` on an existing id updates every column except `kev`, `kev_added_ts`, `epss`, `epss_percentile`, `epss_ts` and `created_ts`, and replaces the advisory's `advisory_affected` rows. One transaction.
5. `ListAdvisories` orders by id; `Affected` entries are in insertion order (row ID); `Package` is filled from `packages` (PURL, ecosystem, name). `Raw` is nil. Withdrawn advisories are included.
6. `AdvisoryModified` with an empty slice returns an empty map without querying. Large inputs (over 500 ids) are chunked so no statement exceeds SQLite's variable limit.
7. `QueriedVersions` returns rows ordered by purl, then version. `MarkQueried` upserts: an existing pair gets the new `queried_ts` and `updated_ts`.
8. `ApplySignals`: a signal matches an advisory when `signal.CVE` equals the advisory id or any element of its `aliases`. KEV sets `kev = 1` and `kev_added_ts` (NULL when `KEVAddedAt` is zero); EPSS sets `epss`, `epss_percentile` and `epss_ts = at`. Signals are processed efficiently for a feed of 300,000 EPSS rows against a few thousand advisories (build the CVE-to-advisory map once in Go, then update only matching advisories); the return value counts distinct advisories updated.
9. `ReplaceMatches`: every match must have `RepoID == repoID` and a non-empty `AdvisoryID` and non-zero `DepVersionID`, else an error and nothing written. Rows keyed by `(dep_version_id, advisory_id)` keep their ID when updated; all columns are written from the match (`exposed_to_ts` NULL when zero, `fixed_version`, `confidence_reason` NULL when empty). The repo's rows not in `matches` are deleted. An empty `matches` deletes all the repo's rows.
10. `ListAllIntervals` returns every `dep_versions` row of every repo with `RepoID`, `RepoSlug`, `PackageID` and an `Interval` filled exactly as `ListIntervals` fills it (ID, manifest path, package, version, requirement, direct, from and to SHA and time). Order: repo slug, package URL, manifest path, `from_ts`, ID.
11. `GetKV` returns `model.ErrNotFound` (wrapped) for a missing key; `SetKV` upserts and sets `updated_ts`.
12. `GetFindingDetail(id)`: the finding with its state (as `scanFinding` reads it); `RepoSlug` from `repos` when `repo_id` is set; `Package` from `packages` when `package_id` is set; `Interval` when `dep_version_id` is set; for kind `advisory`, `Advisory` (as `ListAdvisories` builds it, Raw nil) and `Exposures`: every `advisory_matches` row with the finding's `advisory_id` and `repo_id` whose interval's `package_id` equals the finding's `package_id`, each with its interval, ordered by `exposed_from_ts`, then `dep_version_id`. An unknown id returns an error wrapping `model.ErrNotFound`. A `job_failure` finding returns only `Finding`.
13. Store methods return model types only; no `sql.Null*` leaks.

## Tests to include

Against a real temp-file database (see the existing store tests and the go-test skill):

- `TestSaveAdvisory_RoundTrip`: an advisory with aliases, severity, CVSS, three time fields, two affected packages (one new package, one existing), SEMVER and GIT ranges, versions and ecosystem_specific → `ListAdvisories` returns an equal value (Raw nil), and the new package exists in `packages`.
- `TestSaveAdvisory_UpdateKeepsSignals`: save, apply a KEV and an EPSS signal, save again with a changed summary and one fewer affected entry → summary changed, signals kept, affected rows replaced.
- `TestApplySignals_ByAlias`: advisory `GHSA-x` with alias `CVE-2026-1`, advisory `CVE-2026-2` → signals for `CVE-2026-1` (KEV), `CVE-2026-2` (EPSS) and `CVE-2026-9` (unknown) update two advisories, return 2.
- `TestReplaceMatches_KeepsIDsAndDeletes`: insert two matches, replace with one updated plus one new → updated row keeps its ID, the dropped one is gone, other repos' matches untouched; a match with the wrong RepoID → error, nothing changed.
- `TestQueriedVersions_Upsert`, `TestKV_GetSet` (missing key → `errors.Is(err, model.ErrNotFound)`).
- `TestListAllIntervals`: two repos with intervals (built with `ApplyTimeline`) → all rows, correct IDs and order.
- `TestGetFindingDetail_Advisory`: a repo, an interval, an advisory, two matches on two intervals of the same package plus one on another package, an advisory finding → detail has slug, package, interval, advisory with affected, and the two exposures in order; `TestGetFindingDetail_NotFound`; `TestGetFindingDetail_JobFailure`.

## Acceptance

| Command | Expected |
|---|---|
| `make check` | exit 0 |
| `go test -race -run 'TestSaveAdvisory\|TestApplySignals\|TestReplaceMatches\|TestQueriedVersions\|TestKV\|TestListAllIntervals\|TestGetFindingDetail' ./internal/store/` | `ok` |
| `git status --porcelain` | only owned files and the report file |

## Out of scope

- Finding writes, `ListFindings`, digests, backup and the write-queue changes for existing methods (WO-02).
- Matching, exposure, confidence (WO-06), the OSV adapter (WO-03), the vulns job (later work order).
- Docs (the lead runs docs-sync).

## Skills and docs

Load with the Skill tool: `sqlite-store`.
Read: `docs/data-access.md` (Rules), `docs/database.md` (Advisories, Findings), `internal/store/timeline.go` and `internal/store/findings.go` for the house style.

## Report

Write the full report to the report file named at the top, in the form given in your agent definition (Status, Work order, Files changed, Commands run with verbatim output, Decisions, Outside scope, Concerns, Question or blocker), then reply with only the short form (Status, Report, make check). If a question's answer would change behaviour and nothing here or in the docs answers it, stop and report NEEDS_CONTEXT rather than guessing.

## Rulings

