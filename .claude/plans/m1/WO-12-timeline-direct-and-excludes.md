# WO-12: Timeline: direct-flag changes and excluded paths

Milestone: M1
Agent: go-builder   Model: opus
Depends on: WO-08 (merged)
Report file: `.claude/plans/m1/WO-12-timeline-direct-and-excludes.report.md`
Ticket: UPW-34

## Objective

Two corrections found while running `upw timeline` on the dogfood repository after WO-08. First, a dependency whose direct flag changes (a Go module moving from `// indirect` to a direct require) gets a `changed` event and a new interval, so the timeline's direct column is current. Second, manifests under test-data, vendored and installed-package directories, and under per-target excluded prefixes, are not indexed. Serves M1 checklist items 6, 8 and 15, and keeps M2 from matching advisories against test fixtures.

## Owned files

- `internal/timeline/` (all files except `interfaces.go`), including the existing unit and integration tests
- `internal/config/` (the new key only)
- `cmd/upw/jobs.go`, `cmd/upw/jobs_test.go` (passing the new setting to the timeline)
- `deploy/upw.example.yaml` (document the new key)

Lead-owned, read-only for you: `internal/timeline/interfaces.go`, `internal/model/`, `docs/`. Not owned: `internal/web/` and `cmd/upw/serve.go` (WO-09 runs in parallel).

## Interfaces

Unchanged consumed interfaces (`internal/timeline/interfaces.go`). Produced changes:

```go
// internal/timeline
type TargetSettings struct {
	Slug       string
	Unity      bool
	Ecosystems []string
	// ExcludePaths are directory prefixes (relative to the repository root,
	// slash-separated) whose manifests are not indexed for this target.
	ExcludePaths []string
}

// internal/config
type Target struct {
	// ... existing fields, plus:
	ExcludePaths []string // targets[].exclude_paths
}
```

## Requirements

1. Diff: a dependency present before and after whose `Version`, `Requirement` or `Direct` differs emits `changed` (with `PrevVersion` the previous version). A difference in `Scope` alone still emits no event and keeps the previous state. The fold is unchanged, so the new interval carries the new direct flag. This replaces the earlier rule; see `docs/decisions.md` ("Lead decisions for M1").
2. Excluded paths: a path is skipped when any of its directory segments (not the file name) is `testdata`, `vendor` or `node_modules`, or when it equals or lies under one of the target's `ExcludePaths` prefixes (a prefix `examples` or `examples/` excludes `examples/go.mod` and `examples/a/go.mod`, not `examples2/go.mod`). Skipped paths are dropped from the walked commits before blobs are read, so they produce no blob reads, no events and no manifest rows. A commit whose only changed paths are all skipped is still walked (counted in `commits_walked`) and stored, as today for commits with no events. Add a stats key `files_excluded` counting skipped path occurrences.
3. Config: `targets[].exclude_paths` is a list of strings; each must be a relative slash-separated path without `..` segments and without a leading `/`; validation errors name the field and index in the existing style. Default empty.
4. `cmd/upw/jobs.go` copies each target's `ExcludePaths` into its `TargetSettings`.
5. Already-indexed repos are not rewritten by this change: the new rules apply to commits walked from now on. (No deployed instance has indexed targets yet; the dev data directory is disposable.)

## Tests to include

- `TestDiff`: replace the "direct-only change → no event" case with "direct-only change → `changed`", keep a "scope-only change → no event" case.
- A fold or service case: `go.mod` marks `a` indirect at commit 1 and direct at commit 2 → two intervals for `a`, the second with `DirectYes`.
- `TestRun_ExcludedPaths`: commits touching `go.mod`, `internal/x/testdata/uv.lock`, `vendor/github.com/y/go.mod`, `web/node_modules/z/package-lock.json`, `examples/demo/go.mod` (with `ExcludePaths: ["examples"]`) and `examples2/go.mod` → only `go.mod` and `examples2/go.mod` are read and produce events; `files_excluded` is 4.
- Integration (extend `internal/timeline/integration_test.go`): a fixture whose `go.mod` moves a module from indirect to direct shows the second interval as direct; a fixture with a `testdata/requirements.txt` produces no intervals for it.
- Config: `exclude_paths` parsed; `..`, a leading `/` and an empty string are rejected.

## Acceptance

| Command | Expected |
|---|---|
| `make check` | exit 0 |
| `go test -race -count=1 ./internal/timeline/ ./internal/config/ ./cmd/upw/` | `ok` |
| `rm -rf data-dev && go build -o bin/upw ./cmd/upw && bin/upw run ingest -config upw.dev.yaml -repo upstreamwatch && bin/upw run timeline -config upw.dev.yaml -repo upstreamwatch && bin/upw timeline upstreamwatch -config upw.dev.yaml` | no `internal/extract/testdata/` manifests listed; the go.mod modules that `go.mod` lists without `// indirect` show `DIRECT yes` |
| `git status --porcelain` | only owned files and the report file (`data-dev/` and `bin/` are gitignored; remove them anyway) |

## Out of scope

- Rewriting existing timelines (requirement 5).
- The git-pkgs parity exceptions that follow from these rules (WO-10 and the lead).

## Skills and docs

Preloaded by your agent definition; do not reload. Load with the Skill tool: `go-test`.
Read: `docs/decisions.md` ("Lead decisions for M1"), `internal/timeline/events.go` and `service.go`, `cmd/upw/jobs.go`, `internal/config/validate.go`.

## Report

Follow the report contract in your agent definition: full report to the report file named at the top, then the short reply. Do not commit, stash or otherwise write to git. If a question's answer would change behaviour and nothing here or in the docs answers it, report NEEDS_CONTEXT rather than guessing.

## Rulings

None.
