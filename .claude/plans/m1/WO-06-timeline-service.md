# WO-06: Timeline job: history walk, event diffing and interval folding

Milestone: M1
Agent: go-builder   Model: opus
Depends on: none (lead groundwork commit for M1)
Report file: `.claude/plans/m1/WO-06-timeline-service.report.md`
Ticket: UPW-33, UPW-34

## Objective

`timeline.Service` is the `timeline` job: for each target it walks the first-parent history of the indexed branch from the last indexed commit, parses every changed manifest, diffs it against the previous state into `added`/`changed`/`removed` events, folds all events into intervals, and hands the result to the store in one update. A second run with no new commits does no work. Serves M1 checklist items 6, 7, 8, 9, 10 and 13.

## Owned files

- `internal/timeline/` (all files except `interfaces.go`)

Lead-owned, read-only for you: `internal/timeline/interfaces.go`, `internal/model/`, `internal/pipeline/`. Wiring into `cmd/upw` is WO-08.

## Interfaces

### Consumed

```go
// internal/timeline/interfaces.go
type GitSource interface {
	Head(ctx context.Context, repo model.TargetRepo) (string, error)
	IsAncestor(ctx context.Context, repo model.TargetRepo, ancestor, commit string) (bool, error)
	ManifestCommits(ctx context.Context, repo model.TargetRepo, from, to string, patterns []string) ([]model.Commit, error)
	ReadBlobs(ctx context.Context, repo model.TargetRepo, refs []model.BlobRef) ([]model.Blob, error)
}
type Extractor interface {
	Patterns() []string
	Parse(path string, content []byte) (model.ParsedManifest, error)
}
type RepoStore interface {
	GetRepo(ctx context.Context, slug string) (model.TargetRepo, error)
}
type TimelineStore interface {
	ListEvents(ctx context.Context, repoID int64) ([]model.DepEvent, error)
	ApplyTimeline(ctx context.Context, u model.TimelineUpdate) error
}
```

The doc comments in that file and in `internal/model/timeline.go` (`TargetRepo`, `Commit`, `BlobRef`, `Blob`, `ParsedManifest`, `Dependency`, `DepEvent`, `DepInterval`, `TimelineUpdate`, `ManifestInfo`, `ErrUnsupportedFile`) are part of the contract.

### Produced

```go
// internal/timeline
type TargetSettings struct {
	Slug string
	// Unity adds the Unity extractor for this target.
	Unity bool
	// Ecosystems, when not empty, keeps only dependencies whose
	// Package.Ecosystem is listed.
	Ecosystems []string
}

type Options struct {
	Targets []TargetSettings // config order
}

func NewService(git GitSource, repos RepoStore, store TimelineStore, extractors []Extractor, unity Extractor, opts Options, logger *slog.Logger) *Service
func (s *Service) Name() string // "timeline"
func (s *Service) Run(ctx context.Context, scope model.Scope) (model.Stats, error)
```

## Requirements

Decisions in pure functions (no I/O, in their own file):

1. **Diff.** For one manifest path at one commit, compare the previous state (package URL → last event's values) with the newly parsed dependencies, keyed by `Package.PURL`. Absent before, present now → `added`. Present before, absent now → `removed`, carrying the previous `Version`, `Requirement`, `Direct`, `Scope` and `PrevVersion` = previous `Version`. Present in both with a different `Version` or `Requirement` → `changed` with the new values and `PrevVersion` = previous `Version`. A difference in `Direct` or `Scope` alone produces no event and leaves the previous state as it was. Events of one commit are ordered by manifest path, then package URL. Every event carries the commit's SHA and committed time.
2. **Fold.** Build the complete interval set from an ordered event list (stored events followed by new ones). Per `(manifest path, package URL)`: `added` opens an interval at that event's commit (`FromSHA`, `From`); `changed` closes the open one at that commit (`ToSHA`, `To`) and opens a new one there; `removed` closes the open one. Each interval takes `Version`, `Requirement` and `Direct` from the event that opened it. An event that would close a non-existent interval, or open one while one is already open, is treated as close-then-open, never as an error. Output order: package URL, manifest path, `From`.
3. **State from events.** The previous state of every manifest is folded from the stored events: per `(manifest path, package URL)`, the latest event unless it is `removed`.

Per target, in config order (`Run`):

4. Scope as in the ingest job: all targets, or the one whose slug is `scope.RepoSlug`; an unknown slug is an error before any work.
5. `GetRepo(slug)`. A repo that is not stored, or has an empty `ClonePath` or `DefaultBranch` (never fetched successfully), is skipped with a warn log and counted in `skipped`; that is not an error.
6. `head := Head(repo)`. If `head == repo.LastIndexedSHA`, do nothing more for this target: no `ManifestCommits`, no `ReadBlobs`, no `ApplyTimeline` (checklist item 10).
7. If `LastIndexedSHA` is set and `IsAncestor(LastIndexedSHA, head)` is false, the history was rewritten: rebuild from the root (`from` empty, previous state empty, stored events ignored, `Reset: true`), log at warn and count it in `resets`. Otherwise `from` = `LastIndexedSHA` and the previous state comes from `ListEvents`.
8. Extractors for the target: the Unity extractor first when `Unity` is true, then `extractors` in the order given. Patterns are the union of their `Patterns()` in that order without duplicates.
9. `ManifestCommits(repo, from, head, patterns)`. Read the blobs of every `(commit, path)` pair with `ReadBlobs`, in chunks of at most 500 refs.
10. For each commit in order and each path in it: `Missing` → the manifest was deleted: `removed` for every package in its state. `TooLarge` → skip this file at this commit, warn, count `files_skipped`. Otherwise offer the content to each of the target's extractors in order; the first that does not return `model.ErrUnsupportedFile` handles it. No extractor handles it → skip silently. A parse error → warn, count `parse_errors`, no events, previous state kept. Then apply the ecosystem allowlist, drop duplicate package URLs within the file keeping the first, diff, and update the state. Record the path's `ManifestInfo` (ecosystem and kind) from the parse.
11. Build intervals by folding all events (stored, unless reset, plus new) and call `ApplyTimeline` once for the target with `RepoID`, `Reset`, the walked `Commits` (all of them, including those that produced no events), `Manifests` (every path parsed in this run), the new `Events`, the full `Intervals`, and `IndexedSHA: head`. `ApplyTimeline` is called even when the walk found no manifest commits, so the indexed commit moves to `head`.
12. A target whose git or store call fails is logged and counted in `failed`; the others continue. Return nil when none failed, else `timeline: <n> of <m> targets failed: <slug>: <error>; ...` in config order. Because the store applies an update in one transaction, a failed target stays at its last indexed commit (checklist item 13). When the context is done, stop before the next target and return the context's error.
13. Stats keys, always present, summed over targets: `targets`, `skipped`, `failed`, `resets`, `commits_walked` (commits returned by `ManifestCommits`), `files_read` (blobs parsed), `events_written` (new events passed to `ApplyTimeline`), `intervals` (intervals passed to `ApplyTimeline`), `parse_errors`, `files_skipped`. On the zero-work path of requirement 6, `commits_walked` and `events_written` stay 0.

## Tests to include

Pure-function tables:

- `TestDiff`: added; removed (carries previous values); changed version; changed requirement only (version empty both sides, as for an unpinned requirement); direct-only change → no event; ordering within a commit.
- `TestFold`: add, change, change, remove → three intervals with correct `From`/`To`; two manifests pinning the same package → two independent intervals; remove then re-add → two intervals; a `changed` without a prior `added` → still one open interval.
- `TestStateFromEvents`.

Service tests with hand-written fakes for all four interfaces:

- `TestRun_FirstRun`: three commits touching `go.mod` and `Dockerfile` → one `ApplyTimeline` with all commits, events and intervals, `IndexedSHA` = head, and stats as expected.
- `TestRun_NoNewCommits`: `LastIndexedSHA == head` → no `ManifestCommits`, `ReadBlobs` or `ApplyTimeline` call; `commits_walked` and `events_written` 0.
- `TestRun_Incremental`: stored events seed the state; the new commit bumps one package → one `changed` event and the folded intervals close the old interval and open the new one.
- `TestRun_DeletedManifest`: `Missing` blob → `removed` for every package of that path.
- `TestRun_RewrittenHistory`: `IsAncestor` false → `from` empty, `Reset: true`, stored events not used.
- `TestRun_UnityAndAllowlist`: Unity on → the Unity extractor's patterns come first and it handles its files; `Ecosystems: ["golang"]` drops docker dependencies.
- `TestRun_ParseErrorKeepsState`, `TestRun_TooLargeSkipped`.
- `TestRun_OneTargetFails`: `ApplyTimeline` fails for the first target → the second still runs; the error text matches requirement 12.
- `TestRun_NeverFetched` → skipped, no error.

## Acceptance

| Command | Expected |
|---|---|
| `make check` | exit 0 |
| `go test -race -v ./internal/timeline/` | `ok`, the tests above PASS |
| `go list -f '{{join .Imports " "}}' ./internal/timeline/` | imports `internal/model` and the standard library only |
| `git status --porcelain` | only owned files and the report file |

## Out of scope

- git invocation (WO-01), parsing formats (WO-02, WO-03), SQL (WO-04), wiring and config (WO-08).
- The git-pkgs parity test (WO-10).

## Skills and docs

Preloaded by your agent definition; do not reload. Load with the Skill tool: `go-test`.
Read: PLAN.md sections 5.2 and 5.3, `docs/decisions.md` ("Lead decisions for M1"), `docs/architecture.md` ("What each job does").

## Report

Follow the report contract in your agent definition: full report to the report file named at the top, then the short reply. Do not commit, stash or otherwise write to git. If a question's answer would change behaviour and nothing here or in the docs answers it, report NEEDS_CONTEXT rather than guessing.

## Rulings

None.
