# WO-20: Rebuild a repo's timeline when its indexing settings change

Milestone: M1
Agent: go-builder   Model: opus
Depends on: WO-17 and WO-18 (merged); lead commit with migration `0003_timeline_settings.sql` and the model fields
Report file: `.claude/plans/m1/WO-20-timeline-settings-rebuild.report.md`
Ticket: UPW-41 (follow-up from the WO-17 review)

## Objective

WO-17 applies the current exclusion and ecosystem filters to stored events. The WO-17 review found the flaw: stored events of a filtered path stay in `dep_events`, so loosening a filter later brings back intervals for versions as they were at exclusion time (a dependency removed upstream meanwhile reappears as open). Replace that mechanism: the timeline stores a fingerprint of each repo's indexing settings and rebuilds the repo from the root whenever the fingerprint differs from the current one, in either direction.

## Owned files

- `internal/timeline/` (all files except `interfaces.go`)
- `internal/store/repos.go`, `internal/store/timeline.go` and their tests
- `cmd/upw/jobs.go`, `cmd/upw/jobs_test.go` (only if the extractor version needs passing; see requirement 1)

Lead-owned, read-only: `internal/model/timeline.go` (`TargetRepo.TimelineSettings`, `TimelineUpdate.Settings` added), `internal/timeline/interfaces.go`, `internal/store/migrations/0003_timeline_settings.sql` (adds `repos.timeline_settings TEXT`).

## Requirements

1. **Fingerprint.** A pure function in `internal/timeline` builds a stable string from a target's settings: the extractor version (a package-level constant `extractorVersion = 1` in `internal/timeline`, documented as "bump when any extractor's output for the same bytes changes"), `Unity`, the sorted and de-duplicated `Ecosystems`, and the sorted and de-duplicated, trailing-slash-normalised `ExcludePaths`. Format it as a short canonical text (for example `v1;unity=false;eco=golang,pypi;exclude=examples`) or its SHA-256 hex; either way equal settings give equal strings and any change gives a different one.
2. **Store.** `GetRepo` and `ListRepos` return `TimelineSettings` from `repos.timeline_settings` (NULL reads as empty). `ApplyTimeline` sets `repos.timeline_settings` to `u.Settings` in the same transaction.
3. **Rebuild on change.** In `Run`, a target whose stored `TimelineSettings` is non-empty and differs from the current fingerprint is rebuilt from the root (`Reset: true`, stored events ignored, counted in `resets`, warn log naming the slug and that its settings changed). An empty stored value (first run, or a repo indexed before this change) is not a change: index normally and store the fingerprint. Every `ApplyTimeline` call passes `Settings` with the current fingerprint.
4. **Remove the retroactive filtering** added by WO-17 (`dropFilteredEvents` on stored events and the forced `ApplyTimeline` when filtering dropped events): with requirement 3 it is redundant and was the source of the flaw. New commits are still filtered as before (`dropExcluded`, `filterDeps`). The zero-work shortcut (head unchanged, settings unchanged, no rebuild) again calls nothing after `Head` and the settings comparison; `ListEvents` is not called on that path.
5. Rebuild from `scope.Rebuild` (WO-17) is unchanged.

## Tests to include

- `TestSettingsFingerprint`: order and duplicates of ecosystems and exclusions do not matter; `examples` and `examples/` are equal; changing any field changes it.
- `TestRun_SettingsChangedRebuilds` (fakes): stored fingerprint differs → `Reset: true`; equal → normal incremental; empty stored → no reset, fingerprint written.
- `TestRun_NoWorkPathSkipsListEvents`: head and settings unchanged → no `ListEvents`, no `ApplyTimeline`.
- Integration (`internal/timeline/integration_test.go`): index a fixture with `examples/go.mod`; add `ExcludePaths: ["examples"]` → its intervals are gone; meanwhile the fixture deletes a dependency from `examples/go.mod` upstream; remove the exclusion again → the timeline equals a fresh full index (no resurrected interval for the deleted dependency). Replace WO-17's retroactive-exclusion tests accordingly.
- Store: `TimelineSettings` round-trips through `ApplyTimeline` and `GetRepo`.

## Acceptance

| Command | Expected |
|---|---|
| `make check` | exit 0 |
| `go test -race -count=1 ./internal/timeline/ ./internal/store/` | `ok` |
| `git status --porcelain` | only owned files and the report file |

## Skills and docs

Preloaded by your agent definition; do not reload. Load with the Skill tool: `sqlite-store`, `go-test`.
Read: `docs/decisions.md` ("Lead decisions for M1", the Fable-review entry as amended), `internal/timeline/service.go`, `exclude.go`, `internal/store/timeline.go`, `repos.go`, the WO-17 report.

## Report

Follow the report contract in your agent definition: full report to the report file named at the top, then the short reply. Do not commit, stash or otherwise write to git. If a question's answer would change behaviour and nothing here or in the docs answers it, report NEEDS_CONTEXT rather than guessing.

## Rulings

None.
