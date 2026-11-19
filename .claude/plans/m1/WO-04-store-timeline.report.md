Status: DONE
Work order: .claude/plans/m1/WO-04-store-timeline.md
Files changed:
- internal/store/repos.go (created)
- internal/store/repos_test.go (created)
- internal/store/timeline.go (created)
- internal/store/timeline_test.go (created)
- internal/store/interfaces_test.go (modified: two imports and three assertion lines; gofmt realigned the existing five lines)
- .claude/plans/m1/WO-04-store-timeline.report.md (created)

Commands run:
- `make check` -> exit 0
  ==> gofmt
  ==> vet
  go vet ./...
  ==> build
  go build ./...
  ==> test
  go test -race ./...
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	1.756s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	1.295s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	1.211s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	1.630s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/ingest	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/issues/github	1.246s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	1.188s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	1.672s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	1.563s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	1.610s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	3.628s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/timeline	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/upstream/depsdev	1.221s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/osv	1.210s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/web	1.307s
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
- `go test -race -count=1 -run 'TestDB_(UpsertRepo|RecordFetch|GetRepo|ListRepos|ApplyTimeline|ListIntervals)' -v ./internal/store/` -> exit 0 (last lines)
  --- PASS: TestDB_RecordFetch_UnknownRepo (0.21s)
  --- PASS: TestDB_GetRepo_NotFound (0.21s)
  --- PASS: TestDB_RecordFetch_SuccessAndFailure (0.22s)
  --- PASS: TestDB_ListRepos_Order (0.22s)
  --- PASS: TestDB_ApplyTimeline_UnchangedUpdateWritesNothing (0.22s)
  --- PASS: TestDB_ListIntervals_Order (0.23s)
  --- PASS: TestDB_ApplyTimeline_RoundTrip (0.24s)
  --- PASS: TestDB_ApplyTimeline_Incremental (0.25s)
  --- PASS: TestDB_ApplyTimeline_AtomicOnError (0.25s)
  --- PASS: TestDB_ApplyTimeline_Reset (0.26s)
  --- PASS: TestDB_UpsertRepo_InsertThenUpdate (0.16s)
  --- PASS: TestDB_ApplyTimeline_Errors (0.00s)
      --- PASS: TestDB_ApplyTimeline_Errors/interval_manifest_not_stored (0.15s)
      --- PASS: TestDB_ApplyTimeline_Errors/interval_closing_commit_not_stored (0.15s)
      --- PASS: TestDB_ApplyTimeline_Errors/duplicate_commit (0.15s)
      --- PASS: TestDB_ApplyTimeline_Errors/unknown_repo (0.14s)
      --- PASS: TestDB_ApplyTimeline_Errors/event_commit_not_stored (0.14s)
  --- PASS: TestDB_ApplyTimeline_DeletesVanishedIntervals (0.17s)
  PASS
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	1.580s
- `go test -race -count=1 -coverprofile=<scratchpad>/cover.out ./internal/store/` then `go tool cover -func` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	3.243s	coverage: 83.9% of statements
  (repos.go functions 76.9-91.7%; timeline.go functions 75.0-100%; the uncovered lines are driver-error returns)
- `git status --porcelain` -> exit 0 (run before this report was written)
   M internal/store/interfaces_test.go
  ?? internal/store/repos.go
  ?? internal/store/repos_test.go
  ?? internal/store/timeline.go
  ?? internal/store/timeline_test.go

Decisions:
- ApplyTimeline with an unknown RepoID returns an error wrapping model.ErrNotFound. It checks the repo first because an update with no commits would otherwise pass silently. This matches RecordFetch.
- An event's or interval's commit SHA (opening or closing) that is neither in this update nor stored for the repo is an error naming the SHA. An empty FromSHA is handled the same way, so every stored interval has a from_commit_id. That keeps the identity index meaningful, since NULLs never collide in a SQLite unique index.
- A closed interval (ToSHA set) with a zero To is an error. Otherwise it would be stored with to_ts NULL, and the dep_state view would treat it as open.
- Two intervals in one update with the same identity (manifest, package, opening commit) is an error, raised before anything is written.
- An invalid Directness value (outside the three constants) is an error on write. An invalid stored `direct` value is an error on read.
- Rows whose values are unchanged are not rewritten: manifests and packages use `ON CONFLICT DO UPDATE ... WHERE <a value differs>`, intervals are compared before updating, and `repos.last_indexed_sha`/`updated_ts` are written only when the SHA changes. This is how "an update with no changes and the same IndexedSHA changes nothing else" is met. TestDB_ApplyTimeline_UnchangedUpdateWritesNothing checks that no updated_ts moves.
- Manifests are upserted in sorted path order, and packages in order of first appearance (events, then intervals), so row IDs do not depend on map iteration.
- Vanished intervals are deleted before new ones are inserted or updated.
- Empty commit author name, email and subject are stored as NULL, like the other text columns. A zero CommittedAt is stored as NULL.
- UpsertRepo always sets updated_ts on conflict (a single `INSERT ... ON CONFLICT DO UPDATE ... RETURNING` statement). An empty URL is stored as NULL and read back as "".
- RecordFetch on success stores an empty Branch or ClonePath as NULL.
- The `reset` helper builds `DELETE FROM <table>` from a fixed three-name list inside the function. No input reaches the SQL text.
- interfaces_test.go: gofmt realigned the five existing assertion lines when the three new ones were added. This is whitespace only.

Outside scope:
- `dep_versions.id` is `INTEGER PRIMARY KEY` without AUTOINCREMENT, so SQLite can reuse the ID of a deleted interval when it was the highest. TestDB_ApplyTimeline_Reset saw the new interval get the deleted one's ID. Inside the database nothing dangles: advisory_matches and issue_candidates cascade, and findings.dep_version_id is set NULL first. But anything that keeps an interval ID outside the database, such as a URL or a digest payload, could later resolve to a different interval. This is a schema question for the lead; changing it would need a new migration.
- Docs rows for docs-sync: docs/data-access.md lists the users of `RepoStore` and `TimelineStore`; now that store.DB implements them, ingest, timeline and the coming query methods (ListRepos, ListIntervals) may need their rows confirmed. No schema, route, CLI or config changes.

## Gate (lead, 2026-11-19)

make check rerun in the worktree: exit 0; the acceptance test command passes (13 tests). Diff inside owned files (interfaces_test.go: assertion lines plus gofmt realignment). Astra (gpt-6-astra, high): APPROVED, no findings. Outside-scope note on rowid reuse in dep_versions: accepted without a migration. Interval IDs are only referenced inside the database (cascade and SET NULL run before a reused ID could appear), and no page or digest exposes an interval ID; revisit if one ever does. Accepted.
