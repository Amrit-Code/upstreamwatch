# WO-12: Store methods for the http_cache table

Milestone: M0
Agent: go-mech   Model: opus
Depends on: WO-04 (merged)
Report file: `.claude/plans/m0/WO-12-store-http-cache.report.md`
Ticket: UPW-44 (store half)

## Objective

`store.DB` implements `httpx.CacheStore`, so the shared HTTP client can keep conditional-request validators in the `http_cache` table.

## Owned files

- `internal/store/httpcache.go`
- `internal/store/httpcache_test.go`
- `internal/store/interfaces_test.go` (add one assertion line only)

Lead-owned, read-only for you: everything else, including the other files in `internal/store/`, the migration, `internal/httpx/interfaces.go` and `internal/model/`.

## Interfaces

### Consumed

```go
// internal/httpx/interfaces.go
type CacheStore interface {
	GetHTTPValidator(ctx context.Context, url string) (model.HTTPValidator, error)
	SaveHTTPValidator(ctx context.Context, v model.HTTPValidator) error
}

// internal/model/httpcache.go
type HTTPValidator struct {
	URL, ETag, LastModified string
	FetchedAt               time.Time
	Status                  int
}
```

The `http_cache` table as created by `internal/store/migrations/0001_initial_schema.sql`. Do not change the migration; if a column this work order needs is missing or has the wrong type, report BLOCKED.

### Produced

```go
func (db *DB) GetHTTPValidator(ctx context.Context, url string) (model.HTTPValidator, error)
func (db *DB) SaveHTTPValidator(ctx context.Context, v model.HTTPValidator) error
```

## Requirements

1. Both methods follow the sqlite-store skill's "Store methods" and "Timestamps" sections and the patterns already in `internal/store/runs.go`: one column constant and one scan helper for the table, bound parameters, the package's time helpers.
2. `GetHTTPValidator` returns the row for the exact `url`; no row is an error wrapping `model.ErrNotFound` that names the URL's host but not its path or query.
3. `SaveHTTPValidator` is one upsert keyed on `url`: it replaces `etag`, `last_modified`, `fetched_ts` and `status`. An empty `v.URL` is an error. Empty `ETag` or `LastModified` are stored as the table's representation of "none" and read back as empty strings.
4. Both work inside `InTx`.

## Tests to include

- `TestDB_HTTPValidator_RoundTrip`, `TestDB_HTTPValidator_NotFound` (`errors.Is(err, model.ErrNotFound)`; the error text does not contain the path), `TestDB_HTTPValidator_Replace`, `TestDB_HTTPValidator_EmptyURL`, `TestDB_HTTPValidator_ExactURLMatch` (two URLs differing only in query string are separate rows).
- In `interfaces_test.go`: `var _ httpx.CacheStore = (*store.DB)(nil)`.

## Acceptance

| Command | Expected |
|---|---|
| `make check` | exit 0 |
| `go test -race -run 'HTTPValidator' -v ./internal/store/ \| grep -c '^--- PASS'` | 5 |
| `git status --porcelain` | only owned files and the report file |

## Out of scope

- The `kv` table (M2, with the feed cursors).
- Anything in `internal/httpx`.

## Skills and docs

Preloaded by your agent definition; do not reload. Load with the Skill tool: `sqlite-store`.
Read: `internal/store/runs.go`, `internal/store/db.go`, `.claude/skills/go-test/templates/store-tests.md`.

## Report

Follow the report contract in your agent definition: full report to the report file named at the top, then the short reply. Do not commit, stash or otherwise write to git. If a question's answer would change behaviour and nothing here or in the docs answers it, report NEEDS_CONTEXT rather than guessing.

## Rulings

None.
