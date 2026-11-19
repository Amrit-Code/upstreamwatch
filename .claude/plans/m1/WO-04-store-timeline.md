# WO-04: Store methods for repos and the timeline tables

Milestone: M1
Agent: go-builder   Model: opus
Depends on: none (lead groundwork commit for M1, which adds migration `0002_timeline.sql`)
Report file: `.claude/plans/m1/WO-04-store-timeline.report.md`
Ticket: UPW-34 (store half)

## Objective

`store.DB` implements `ingest.RepoStore`, `timeline.RepoStore` and `timeline.TimelineStore`, plus two read methods the query service will use in WO-08, so the ingest and timeline jobs can persist targets, commits, manifests, packages, events and intervals. Serves M1 checklist items 6, 10 and 13.

## Owned files

- `internal/store/repos.go`, `internal/store/repos_test.go` (new)
- `internal/store/timeline.go`, `internal/store/timeline_test.go` (new)
- `internal/store/interfaces_test.go` (add assertion lines only)

Lead-owned, read-only for you: every other file in `internal/store/` (including `db.go`, `migrate.go` and both migrations), `internal/model/`, `internal/ingest/interfaces.go`, `internal/timeline/interfaces.go`. If a column or index you need is missing, report BLOCKED; do not add a migration.

## Interfaces

### Consumed

```go
// internal/ingest/interfaces.go
type RepoStore interface {
	UpsertRepo(ctx context.Context, slug, url string) (model.TargetRepo, error)
	RecordFetch(ctx context.Context, r model.FetchResult) error
}

// internal/timeline/interfaces.go
type RepoStore interface {
	GetRepo(ctx context.Context, slug string) (model.TargetRepo, error)
}
type TimelineStore interface {
	ListEvents(ctx context.Context, repoID int64) ([]model.DepEvent, error)
	ApplyTimeline(ctx context.Context, u model.TimelineUpdate) error
}
```

Read the doc comments in those files and in `internal/model/timeline.go`; they are part of the contract. Tables: `repos`, `commits`, `manifests`, `packages`, `dep_events`, `dep_versions` in `0001_initial_schema.sql` as changed by `0002_timeline.sql` (`dep_versions.requirement`, unique index `dep_versions_identity`, index `commits_repo_position`).

### Produced

```go
func (db *DB) UpsertRepo(ctx context.Context, slug, url string) (model.TargetRepo, error)
func (db *DB) RecordFetch(ctx context.Context, r model.FetchResult) error
func (db *DB) GetRepo(ctx context.Context, slug string) (model.TargetRepo, error)
func (db *DB) ListEvents(ctx context.Context, repoID int64) ([]model.DepEvent, error)
func (db *DB) ApplyTimeline(ctx context.Context, u model.TimelineUpdate) error

// For the query service (WO-08):
// ListRepos returns every stored repo ordered by slug.
func (db *DB) ListRepos(ctx context.Context) ([]model.TargetRepo, error)
// ListIntervals returns the repo's intervals with their row IDs, ordered by
// package URL, then manifest path, then opening time, then ID.
func (db *DB) ListIntervals(ctx context.Context, repoID int64) ([]model.DepInterval, error)
```

## Requirements

1. Follow the sqlite-store skill and the patterns in `internal/store/runs.go` and `findings.go`: column constants and scan helpers per table, bound parameters, the package's time helpers (ISO-8601 UTC), `created_ts`/`updated_ts` set on insert and `updated_ts` on update, no `sql.Null*` in returned types, every method usable inside `InTx`.
2. `UpsertRepo`: insert on first sight of the slug; afterwards update `url` only. Returns the full stored repo (ID, slug, URL, default branch, clone path, last indexed SHA, last fetch time and error).
3. `RecordFetch`: sets `last_fetch_ts` and `last_fetch_error` (NULL when `Error` is empty); when `Error` is empty also sets `default_branch` and `clone_path`; when not empty leaves them as they were. Unknown `RepoID` → error wrapping `model.ErrNotFound`.
4. `GetRepo`: unknown slug → error wrapping `model.ErrNotFound`.
5. `Directness` maps to the nullable `direct` column: `DirectUnknown` ↔ NULL, `DirectYes` ↔ 1, `DirectNo` ↔ 0, in both events and intervals. Empty strings for version, requirement, scope, prev version and SHAs are stored as NULL and read back as empty.
6. `ListEvents`: joins commits, manifests and packages; fills `CommitSHA`, `CommittedAt` (from `commits.committed_ts`), `ManifestPath`, `Package` (purl, ecosystem, name) and the event columns. Order: commit position, manifest path, package purl.
7. `ApplyTimeline` runs in one `InTx`, so on any error nothing of the update is stored (checklist item 13 relies on this):
   a. `Reset`: delete the repo's `dep_versions`, then `dep_events`, then `commits`, in that order (foreign keys are on; `advisory_matches` and `issue_candidates` cascade from `dep_versions`, `findings.dep_version_id` is set NULL). Manifests and packages stay.
   b. Insert `Commits` in the given order with `position` continuing after the repo's highest stored position (0 for the first commit of a repo), and their SHA, committed time (UTC), author name and email and subject.
   c. Upsert `manifests` for every entry of `Manifests` by `(repo_id, path)`, setting ecosystem and kind. An event or interval whose manifest path is not in `Manifests` must already have a row; otherwise return an error naming the path.
   d. Upsert `packages` by `purl` for every package in `Events` and `Intervals`, setting ecosystem and name.
   e. Insert `Events`, resolving commit, manifest and package IDs. An event's commit must be among this update's commits or already stored for the repo.
   f. Replace the repo's intervals with `Intervals`, keyed by the identity `(manifest, package, opening commit)`: rows whose identity is in the new set keep their ID and get version, requirement, direct, from time, closing commit and closing time updated; stored rows whose identity is absent are deleted; new identities are inserted. Closing commit SHAs resolve within the repo like opening ones; an open interval stores NULL `to_commit_id` and `to_ts`.
   g. Set `repos.last_indexed_sha` to `IndexedSHA` and update `updated_ts`.
   An update with no commits, events or interval changes and the same `IndexedSHA` succeeds and changes nothing else.
8. `ListIntervals` returns `DepInterval` with `ID`, `ManifestPath`, `Package`, `Version`, `Requirement`, `Direct`, `FromSHA`, `From`, `ToSHA`, `To`.
9. `ListRepos` returns the same fields as `GetRepo` for every repo, ordered by slug.

## Tests to include

Against a real temp-file database (see `.claude/skills/go-test/templates/store-tests.md`).

- `TestDB_UpsertRepo_InsertThenUpdate`, `TestDB_RecordFetch_SuccessAndFailure` (failure keeps the earlier branch and clone path), `TestDB_RecordFetch_UnknownRepo`, `TestDB_GetRepo_NotFound`, `TestDB_ListRepos_Order`.
- `TestDB_ApplyTimeline_RoundTrip`: two commits, two manifests, events including all three changes and all three `Directness` values → `ListEvents` returns them in order with equal fields; positions are 0 and 1.
- `TestDB_ApplyTimeline_Incremental`: a second update appends commits at positions 2 and 3, closes an interval opened by the first update and opens a new one; the unchanged interval keeps its ID, the closed one keeps its ID with `ToSHA` set.
- `TestDB_ApplyTimeline_DeletesVanishedIntervals`.
- `TestDB_ApplyTimeline_Reset`: rows of the old history are gone (including an `advisory_matches` row seeded on an old interval, which cascades), a `findings` row pointing at an old interval survives with `dep_version_id` NULL, manifests and packages survive.
- `TestDB_ApplyTimeline_AtomicOnError`: an event naming an unknown manifest path makes the whole call fail and leaves the database exactly as before (commits, events, intervals and `last_indexed_sha` unchanged).
- `TestDB_ListIntervals_Order`.
- In `interfaces_test.go`: `var _ ingest.RepoStore = (*store.DB)(nil)`, `var _ timeline.RepoStore = (*store.DB)(nil)`, `var _ timeline.TimelineStore = (*store.DB)(nil)`.

## Acceptance

| Command | Expected |
|---|---|
| `make check` | exit 0 |
| `go test -race -run 'TestDB_(UpsertRepo\|RecordFetch\|GetRepo\|ListRepos\|ApplyTimeline\|ListIntervals)' -v ./internal/store/` | `ok`, the tests above PASS |
| `git status --porcelain` | only owned files and the report file |

## Out of scope

- The ingest and timeline services (WO-05, WO-06) and the query service (WO-08).
- Any migration or schema change (lead).

## Skills and docs

Preloaded by your agent definition; do not reload. Load with the Skill tool: `sqlite-store`, `go-test`.
Read: `docs/data-access.md`, `docs/database.md` (the six tables), `docs/decisions.md` ("Lead decisions for M1"), `internal/store/db.go`, `internal/store/findings.go`.

## Report

Follow the report contract in your agent definition: full report to the report file named at the top, then the short reply. Do not commit, stash or otherwise write to git. If a question's answer would change behaviour and nothing here or in the docs answers it, report NEEDS_CONTEXT rather than guessing.

## Rulings

None.
