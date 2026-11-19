Status: DONE
Work order: .claude/plans/m1/WO-20-timeline-settings-rebuild.md
Files changed:
- internal/timeline/settings.go (created): `extractorVersion = 1`, `settingsFingerprint`, `canonicalList`
- internal/timeline/settings_test.go (created): `TestSettingsFingerprint`
- internal/timeline/service.go (modified): settings comparison and rebuild, `Settings` on every `ApplyTimeline`, zero-work shortcut restored, retroactive filtering removed, doc comments updated
- internal/timeline/exclude.go (modified): `dropFilteredEvents` deleted
- internal/timeline/events_test.go (modified): `TestDropFilteredEvents` deleted
- internal/timeline/service_test.go (modified): `TestRun_NoNewCommits` replaced by `TestRun_NoWorkPathSkipsListEvents`; `TestRun_RetroactiveExclusion` replaced by `TestRun_SettingsChangedRebuilds`; expected updates in `TestRun_FirstRun`, `TestRun_ExcludedPaths`, `TestRun_RewrittenHistory`, `TestRun_Rebuild` now carry `Settings`
- internal/timeline/integration_test.go (modified): `TestTimeline_RetroactiveExclusion` replaced by `TestTimeline_SettingsChangeRebuilds`
- internal/store/repos.go (modified): `repoColumns` and `scanRepo` read `timeline_settings` (NULL reads as empty)
- internal/store/timeline.go (modified): `ApplyTimeline` writes `timeline_settings = u.Settings` (NULL when empty) in the same transaction as `last_indexed_sha`
- internal/store/timeline_test.go (modified): `TestDB_ApplyTimeline_StoresSettings`, `TestDB_ApplyTimeline_SettingsRolledBackOnError`
- cmd/upw/jobs.go, cmd/upw/jobs_test.go: not changed (the extractor version is a constant inside `internal/timeline`, so nothing needs passing)

Commands run:
- `git log --oneline -1` -> exit 0
  4b1a0ae store: migration 0003 (repos.timeline_settings); model fields; plans: WO-20
- `make check` -> exit 0
  ==> gofmt
  ==> vet
  go vet ./...
  ==> build
  go build ./...
  ==> test
  go test -race ./...
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	2.119s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	1.273s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract	1.354s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract/unity	1.194s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	10.118s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	1.589s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/ingest	1.942s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/issues/github	1.242s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	1.196s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	1.734s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	1.723s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	1.688s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	5.438s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/testutil/gitrepo	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/timeline	4.053s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/upstream/depsdev	1.216s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/osv	1.221s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/web	1.359s
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
- `go test -race -count=1 ./internal/timeline/ ./internal/store/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/timeline	3.411s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	4.840s
- `git status --porcelain` -> exit 0 (before writing this report)
   M internal/store/repos.go
   M internal/store/timeline.go
   M internal/store/timeline_test.go
   M internal/timeline/events_test.go
   M internal/timeline/exclude.go
   M internal/timeline/integration_test.go
   M internal/timeline/service.go
   M internal/timeline/service_test.go
  ?? internal/timeline/settings.go
  ?? internal/timeline/settings_test.go
- `go test -race -count=1 -coverprofile=... ./internal/timeline/` then `go tool cover -func` -> exit 0
  indexTarget 97.8%, settingsFingerprint 100.0%, canonicalList 100.0%, package total 98.9%

Decisions:
- Fingerprint format: canonical text, not a SHA-256, e.g. `v1;unity=false;eco="golang","pypi";exclude="docs/site","examples"`. Each list element is Go-quoted (`strconv.Quote`), so different settings never give the same text: `ecosystems: []` and `[""]` stay distinct (an unset `${VAR}` expands to `""` and ecosystems are not validated), as do an element containing `,` or `;` and two separate elements.
- Normalisation follows the filters' semantics and nothing more. Exclusions lose trailing slashes (`strings.TrimRight(p, "/")`, the same as `excludedPath`). Ecosystems are compared exactly, with no case folding, because `filterDeps` uses exact match. Slug is not part of the fingerprint.
- The fingerprint is computed in `indexTarget` once per target. The settings check runs before the first-parent chain check, so a settings rebuild skips `OnFirstParentChain`. `resets` is counted once even when `scope.Rebuild` and a settings change coincide.
- Empty stored fingerprint with the head unchanged: per requirement 3 the target "indexes normally", so it reads the stored events, refolds them and calls `ApplyTimeline` with no commits, which writes the fingerprint. The zero-work shortcut applies only when the stored fingerprint equals the current one.
- Warn log: `"timeline settings changed, rebuilding from root"` with attribute `repo`, in the same style as the existing `"timeline history rewritten, rebuilding from root"`. The old and new fingerprints are not logged.
- Store: `ApplyTimeline` updates `repos` when either `last_indexed_sha` or `timeline_settings` differs from the stored value, in the same `UPDATE` as before. An empty `u.Settings` is stored as NULL. The wrapped error text became "set last indexed commit and settings".

Outside scope:
- `internal/timeline/interfaces.go` (lead-owned): the `ApplyTimeline` doc comment does not yet say it also sets the repo's timeline settings fingerprint to `u.Settings`.
- Doc rows for docs-sync: `docs/database.md` `repos.timeline_settings` (migration 0003); the timeline job description in `docs/architecture.md` (settings fingerprint and rebuild on change; stored events are no longer filtered retroactively); `docs/data-access.md` if it describes what `ApplyTimeline` writes to `repos`. The `docs/decisions.md` Fable-review entry already describes the new behaviour.
- A timeline indexed by the WO-17 build after a filter was tightened still has the filtered events in `dep_events`, and its stored fingerprint is empty. Its first run under this build counts as "not a change" (requirement 3), so those events come back into its intervals until the next settings change or `-rebuild`. Only a dev database could be in this state, since M1 is not deployed. A one-off `upw run timeline -rebuild` clears it.

Concerns: none

## Gate (lead, 2026-11-19)

make check rerun: exit 0; timeline and store tests ok; dropFilteredEvents removed with no references. Sonnet reviewer: ACCEPT (no stale intervals in either direction, fingerprint and indexed commit in one transaction on every path, zero-work path skips everything). Minors: inputs outside the fingerprint (blob cap, always-skipped directories, the extractor set) need `-rebuild` or an extractorVersion bump when changed: recorded in docs/decisions.md; the interface doc comment was updated by the lead (74edd16). Empty-fingerprint databases from the WO-17 build are cleared by the VM's `-rebuild` on redeploy. Accepted.
