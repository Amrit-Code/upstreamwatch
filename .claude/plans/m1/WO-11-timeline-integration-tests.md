# WO-11: Integration tests for ingest and timeline on fixture repositories

Milestone: M1
Agent: test-writer   Model: opus
Depends on: WO-01 to WO-07 (merged)
Report file: `.claude/plans/m1/WO-11-timeline-integration-tests.report.md`
Ticket: UPW-37

## Objective

The ingest and timeline jobs are proven end to end in-process: the real `gitsrc.CLI` syncing fixture repositories built with `internal/testutil/gitrepo`, the real extractors, the real `store.DB` on a temp SQLite file, and the services run through `pipeline.Runner`. These tests are the evidence for M1 checklist items 9 (first-parent semantics), 10 (a second run does zero work) and 13 (a failed fetch leaves the timeline at the last good commit).

## Owned files

- `internal/timeline/integration_test.go` (new)
- `internal/ingest/integration_test.go` (new)
- `internal/testutil/gitrepo/` (you may add helpers; keep existing exported names and behaviour)

Read-only for you: every production file. A test that exposes a production bug stays in the suite failing; report it (DONE_WITH_CONCERNS) with the failing test name and the smallest reproduction. Do not change production code.

## Interfaces

### Consumed

Read the merged code for exact signatures: `gitsrc.New`, `gitsrc.Options` (use `AllowFileURLs: true`, temp `ReposDir` and `HomeDir`), `extract.New`, `unity.New`, `ingest.NewService`, `ingest.Options`, `timeline.NewService`, `timeline.Options`, `timeline.TargetSettings`, `pipeline.NewRunner`, `store.Open`, `(*store.DB).Migrate`, and the store read methods `GetRepo`, `ListEvents`, `ListIntervals`, `LatestRuns`. Model types are in `internal/model/timeline.go`. Use package-external tests (`package timeline_test`, `package ingest_test`).

## Requirements

1. One helper builds the whole stack for a test: temp data directory, store opened and migrated, one `gitsrc.CLI`, both services and a `pipeline.Runner` with both jobs. Targets point at `file://` URLs of `gitrepo` fixtures.
2. Every run goes through `Runner.Run(ctx, "ingest" | "timeline", scope)` so the `runs` table and `job_failure` findings are exercised as in production.
3. Fixture dates are fixed (committer dates set by the builder) so intervals can be asserted exactly.

## Tests to include

- `TestTimeline_FirstParentSemantics` (checklist item 9): main commits `go.mod` requiring `example.com/a v1.0.0` (date D1); a branch bumps `a` to v1.1.0 then reverts to v1.0.0, and is merged with `--no-ff` at D3 → no event for the branch's bump or revert; a second branch bumps `a` to v1.2.0 at D4 and is merged at D5 → one `changed` event dated D5 at the merge commit's SHA; the intervals for `a` are `[D1, D5)` at v1.0.0 and `[D5, open)` at v1.2.0.
- `TestTimeline_SecondRunDoesZeroWork` (checklist item 10): ingest and timeline twice with no new upstream commit → the second timeline run's stored stats have `commits_walked` 0 and `events_written` 0, and the events and interval IDs are unchanged.
- `TestTimeline_IncrementalKeepsIntervalIDs`: after a new upstream commit bumps one package, the next run adds one `changed` event; the unchanged packages' interval IDs are the same as before and the bumped package's old interval keeps its ID with `ToSHA` set.
- `TestTimeline_FetchFailureKeepsLastGoodCommit` (checklist item 13): after a successful ingest and timeline, point the target at a `file://` path that does not exist (or make the fixture unreadable) → the ingest run is recorded as failed in `runs`, an open `job_failure` finding exists for `ingest`, the repo's `last_fetch_error` is set, and a following timeline run leaves `last_indexed_sha`, events and intervals as they were.
- `TestTimeline_ManifestDeletedAndReadded`: deleting `requirements.txt` emits `removed` for each package and closes their intervals; re-adding it later opens new intervals.
- `TestTimeline_Ecosystems`: one fixture with `go.mod` (with `go` and `toolchain` lines), a `Dockerfile` with `FROM golang:1.26-alpine AS build` and `FROM alpine:3.22`, a `docker-compose.yml`, a `.github/workflows/ci.yml`, a `pyproject.toml` and a `poetry.lock`; with derivation on → intervals exist for each ecosystem, `pkg:golang/toolchain` has one interval from `go.mod` and one from the `Dockerfile` (version `1.26`), and no `pkg:docker/build` package exists.
- `TestTimeline_Unity`: a fixture Unity project (`ProjectSettings/ProjectVersion.txt` changed twice, `Packages/manifest.json`, `Packages/packages-lock.json`) with `Unity: true` → two `pkg:generic/unity-editor` intervals with the right versions and dates; with `Unity: false` → none.
- `TestTimeline_RewrittenHistory`: force-move the fixture branch to an unrelated history and re-ingest → the timeline run rebuilds (its stats show `resets` 1), the old commits' events are gone and the new history's events are present.
- `TestIngest_DefaultBranchAndBareClone` (checklist items 2 and 5 in-process): a fixture whose HEAD is `trunk` with no configured branch → the stored repo's default branch is `trunk`, the clone directory is bare and its `git config --list` holds no URL or credential.

## Acceptance

| Command | Expected |
|---|---|
| `make check` | exit 0 |
| `go test -race -count=1 -run 'TestTimeline_\|TestIngest_' -v ./internal/timeline/ ./internal/ingest/` | `ok` for both packages, every test above PASS (or DONE_WITH_CONCERNS with the failing tests reported) |
| `git status --porcelain` | only owned files and the report file |

## Out of scope

- The git-pkgs parity test (WO-10) and anything needing the network.
- CLI and config wiring (WO-08).

## Skills and docs

Preloaded by your agent definition; do not reload. Read: `internal/testutil/gitrepo/gitrepo.go`, `internal/pipeline/integration_test.go` (the existing integration style), `docs/decisions.md` ("Lead decisions for M1"), PLAN.md sections 5.1 to 5.3.

## Report

Follow the report contract in your agent definition: full report to the report file named at the top, then the short reply. Do not commit, stash or otherwise write to git. If a question's answer would change behaviour and nothing here or in the docs answers it, report NEEDS_CONTEXT rather than guessing.

## Rulings

None.
