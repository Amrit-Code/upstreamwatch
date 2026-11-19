# WO-05: Ingest job

Milestone: M1
Agent: go-builder   Model: opus
Depends on: none (lead groundwork commit for M1)
Report file: `.claude/plans/m1/WO-05-ingest-service.report.md`
Ticket: UPW-29

## Objective

`ingest.Service` is the `ingest` job: for every configured target it registers the repo, syncs its bare clone through `GitSource`, and records the attempt, so one failing target is visible without stopping the others. Serves M1 checklist items 2, 10 and 13.

## Owned files

- `internal/ingest/` (all files except `interfaces.go`)

Lead-owned, read-only for you: `internal/ingest/interfaces.go`, `internal/model/`, `internal/pipeline/`. The service is wired into `cmd/upw` by WO-08, not here.

## Interfaces

### Consumed

```go
// internal/ingest/interfaces.go
type GitSource interface {
	Sync(ctx context.Context, repo model.TargetRepo) (model.SyncResult, error)
}
type RepoStore interface {
	UpsertRepo(ctx context.Context, slug, url string) (model.TargetRepo, error)
	RecordFetch(ctx context.Context, r model.FetchResult) error
}
```

Read the doc comments in that file and in `internal/model/timeline.go` (`TargetRepo`, `SyncResult`, `FetchResult`). The job contract is `pipeline.Job` in `internal/pipeline/interfaces.go`: `Name() string` and `Run(ctx context.Context, scope model.Scope) (model.Stats, error)`.

### Produced

```go
// internal/ingest
type Options struct {
	// Targets are the configured targets in config order, with Slug, URL,
	// Branch, Token and AllowPrivateHost set.
	Targets []model.TargetRepo
	// Now returns the current time; nil means time.Now.
	Now func() time.Time
}

func NewService(git GitSource, repos RepoStore, opts Options, logger *slog.Logger) *Service
func (s *Service) Name() string // "ingest"
func (s *Service) Run(ctx context.Context, scope model.Scope) (model.Stats, error)
```

## Requirements

1. `Name()` returns `ingest`.
2. Scope: all targets when `scope.All()`, otherwise only the target whose slug is `scope.RepoSlug`; an unknown slug is an error naming it, returned before any work.
3. Per target, in config order: `UpsertRepo(slug, url)`; then `Sync` with the stored repo's `ID` plus the configured `Slug`, `URL`, `Branch`, `Token` and `AllowPrivateHost`; then `RecordFetch` with `At` from `Now()` in UTC and either the result's `Branch` and `ClonePath` (success) or the error text (failure).
4. A target whose `Sync` fails is logged at warn with its slug and error and does not stop the remaining targets. A `RecordFetch` or `UpsertRepo` error is also counted as that target failing.
5. When the context is done, stop before the next target and return the context's error.
6. Stats keys, always present: `targets` (targets attempted), `fetched` (synced successfully), `cloned` (of those, newly cloned), `failed`.
7. Return value: nil when no target failed; otherwise one error whose text is `ingest: <n> of <m> targets failed: <slug>: <error>; <slug>: <error>` in config order. The runner turns it into a failed run and a `job_failure` finding (checklist item 13).
8. The token is never logged: log attributes carry the slug, never the `TargetRepo` value or its `Token`. (`model.Secret` prints as its last four characters, but do not rely on that.)

## Tests to include

With hand-written fakes for `GitSource` and `RepoStore` (no mocking library):

- `TestRun_AllTargetsSucceed`: two targets → two upserts, two syncs carrying the stored ID and configured fields (including `Token` and `AllowPrivateHost`), two successful `RecordFetch` calls; stats `targets 2, fetched 2, cloned 1, failed 0` when one sync reports `Cloned`.
- `TestRun_OneTargetFails`: the first target's sync fails → its `RecordFetch` carries the error and no branch; the second still syncs; the returned error text is exactly as in requirement 7; stats `failed 1`.
- `TestRun_ScopeOneRepo` and `TestRun_ScopeUnknownRepo`.
- `TestRun_ContextCancelled`: cancel after the first target → the second is not attempted and the context error is returned.
- `TestRun_TokenNotLogged`: capture the logger output during a failing sync with a token `github_pat_` plus 30 characters → the output does not contain the token.

## Acceptance

| Command | Expected |
|---|---|
| `make check` | exit 0 |
| `go test -race -v ./internal/ingest/` | `ok`, the tests above PASS |
| `git status --porcelain` | only owned files and the report file |

## Out of scope

- URL policy, git invocation and credentials (gitsrc, WO-01).
- Config keys and wiring in `cmd/upw` (WO-08).
- Redacting error text (the runner does it, WO-07).

## Skills and docs

Preloaded by your agent definition; do not reload. Load with the Skill tool: `go-test`.
Read: PLAN.md section 5.1, `docs/architecture.md` ("What each job does"), `internal/pipeline/runner.go` for how a job's error is recorded.

## Report

Follow the report contract in your agent definition: full report to the report file named at the top, then the short reply. Do not commit, stash or otherwise write to git. If a question's answer would change behaviour and nothing here or in the docs answers it, report NEEDS_CONTEXT rather than guessing.

## Rulings

None.
