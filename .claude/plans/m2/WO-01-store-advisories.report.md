Status: DONE
Work order: .claude/plans/m2/WO-01-store-advisories.md
Files changed:
- internal/store/advisories.go (created)
- internal/store/kv.go (created)
- internal/store/finding_detail.go (created)
- internal/store/advisories_test.go (created)
- internal/store/kv_test.go (created)
- internal/store/finding_detail_test.go (created)
- .claude/plans/m2/WO-01-store-advisories.report.md (created)

Commands run:
- `make check > check.log 2>&1; echo $?` -> exit 0
  (last 30 lines)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract/unity	(cached)
  ?   	github.com/Amrit-Code/upstreamwatch/internal/findings	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/ingest	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/issues/github	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	(cached)
  ?   	github.com/Amrit-Code/upstreamwatch/internal/ops	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	(cached)
  ?   	github.com/Amrit-Code/upstreamwatch/internal/report	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	(cached)
  ?   	github.com/Amrit-Code/upstreamwatch/internal/testutil/gitrepo	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/timeline	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/upstream/depsdev	(cached)
  ?   	github.com/Amrit-Code/upstreamwatch/internal/vuln	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/osv	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/web	(cached)
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
  (The uncached run just before it, after the last code change, also exited 0; the store package took 10.6s.)
- `go test -race -run 'TestSaveAdvisory|TestApplySignals|TestReplaceMatches|TestQueriedVersions|TestKV|TestListAllIntervals|TestGetFindingDetail' ./internal/store/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	1.904s
- `go test -race -count=1 -v -run '...same plus TestListAdvisories|TestAdvisory' ./internal/store/ | grep '^--- '` -> exit 0
  --- PASS: TestAdvisoryStore_ClosedReturnsErrors
  --- PASS: TestGetFindingDetail_NotFound
  --- PASS: TestQueriedVersions_Upsert
  --- PASS: TestKV_GetSet
  --- PASS: TestGetFindingDetail_InsideTransaction
  --- PASS: TestKV_SetJoinsCallerTransaction
  --- PASS: TestSaveAdvisory_NullColumns
  --- PASS: TestGetFindingDetail_JobFailure
  --- PASS: TestGetFindingDetail_NoInterval
  --- PASS: TestGetFindingDetail_Advisory
  --- PASS: TestApplySignals_SharedAliasAndZeroKEVDate
  --- PASS: TestApplySignals_EmptyAndUnknownKind
  --- PASS: TestSaveAdvisory_InsertTakesSignals
  --- PASS: TestListAdvisories_EmptyAndOrder
  --- PASS: TestApplySignals_ByAlias
  --- PASS: TestAdvisoryModified
  --- PASS: TestReplaceMatches_KeepsIDsAndDeletes
  --- PASS: TestReplaceMatches_Invalid
  --- PASS: TestListAllIntervals
  --- PASS: TestSaveAdvisory_RoundTrip
  --- PASS: TestSaveAdvisory_UpdateKeepsSignals
  --- PASS: TestSaveAdvisory_Errors
- `go test -race -count=1 -coverprofile=cover.out ./internal/store/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	4.312s	coverage: 85.1% of statements
  (new files: every exported method 84-100%; uncovered lines are the scan/JSON-decode error branches)
- `git status --porcelain` -> exit 0
  ?? internal/store/advisories.go
  ?? internal/store/advisories_test.go
  ?? internal/store/finding_detail.go
  ?? internal/store/finding_detail_test.go
  ?? internal/store/kv.go
  ?? internal/store/kv_test.go
  (plus this report file, written afterwards)

Decisions:
- Helper names all carry the `adv` prefix as the work order asks (advColumns/advScan, advAffectedColumns/advScanAffected, advMatchColumns/advMatchRow, advList, advRead, advPrefixScanner, advByCVE, advPlaceholders, advEncodeRanges and so on); test helpers too (advSeedRepo, advSample, advSave, advListAll, advMatches, advPackageID, advUpsertFinding).
- ListAdvisories and GetFindingDetail run their several reads in one read-only transaction (`BeginTx` with `ReadOnly: true`, as the sqlite-store skill says), or in the caller's transaction when called inside InTx, so an advisory is never read without its affected rows.
- ListAllIntervals and the exposure query reuse `scanInterval` unchanged by prefixing extra scan destinations (advPrefixScanner), so the interval is filled exactly as ListIntervals fills it.
- Empty JSON arrays (aliases, versions, ranges, events) read back as nil slices, so a value saved with nil slices round-trips with reflect.DeepEqual.
- Ranges are stored as `[]` when an entry has none (only versions was specified); a range event that sets zero or more than one field is an error and nothing is saved (keeps "exactly one key" true).
- summary, details, cvss_vector, source_db and confidence are stored NULL when empty (house style nullText); they read back as "".
- SaveAdvisory on a new id writes the signal columns from the advisory (kev, kev_added_ts, epss* NULL when HasEPSS false), per requirement 2; an update leaves them alone, per requirement 4. advisory_affected rows are deleted and reinserted on every save (nothing references their IDs).
- SaveAdvisory rejects an empty advisory id and an affected entry with an empty package URL.
- ApplySignals: signals are applied in input order, so when several signals of one kind reach the same advisory the last wins; a signal whose Kind is neither kev nor epss is an error and the transaction rolls back; an empty slice returns 0 without opening a transaction. Updates run in advisory-id order, one UPDATE per kind per advisory, and set updated_ts.
- ReplaceMatches also rejects two matches with the same (DepVersionID, AdvisoryID), before any write. Stored rows not in the set are deleted first, then each match is upserted; every column is rewritten even when unchanged.
- MarkQueried with an empty slice returns nil without opening a transaction (no write happens).
- AdvisoryModified includes a stored advisory whose modified_ts is NULL, with the zero time.
- GetFindingDetail returns an error (not ErrNotFound) if a finding of kind advisory names an advisory that is not stored; the foreign key makes this unreachable in practice.
- Compile-time checks `var _ vuln.AdvisoryStore = (*DB)(nil)` and `var _ vuln.KVStore = (*DB)(nil)` are in my own test files (advisories_test.go, kv_test.go), since interfaces_test.go is not mine. report.Store is not asserted because ListFindings and the digest methods belong to WO-02.

Outside scope:
- `internal/store/interfaces_test.go` could gain `vuln.AdvisoryStore`, `vuln.KVStore` and, once WO-02 lands, `report.Store` alongside the existing assertions.
- GetFindingDetail reads the finding with `findingColumns`/`scanFinding` and the join documented above them (`FROM findings f JOIN finding_state s ON s.finding_id = f.id`). If WO-02 changes that column list or join in findings.go, finding_detail.go must follow.
- Doc rows for docs-sync: docs/data-access.md table-to-interface map and "What is built" (advisories, advisory_affected, advisory_matches, osv_queries, kv now written by `*store.DB` through vuln.AdvisoryStore / vuln.KVStore; GetFindingDetail for report.Store); docs/database.md Advisories section could record the stored formats (aliases JSON array, ranges in OSV shape, NULL rules).

Concerns: none
Question or blocker: none

## Fix round 1

Status: DONE
Work order: .claude/plans/m2/WO-01-store-advisories.md (on top of 9dacae1)
Files changed:
- internal/store/advisories.go (modified)
- internal/store/advisories_test.go (modified)
- .claude/plans/m2/WO-01-store-advisories.report.md (modified: this section)

Findings fixed:
1. ReplaceMatches now checks, inside the transaction and before any write, that every match's DepVersionID is a dep_versions row of repoID. It does this with one query, `SELECT id FROM dep_versions WHERE repo_id = ?`, read into a set by the new helper `advRepoIntervalIDs`. A match naming another repo's interval, or an interval that is not stored, is an error, and the transaction rolls back with nothing written. The doc comment states the rule.
2. On insert, SaveAdvisory writes epss_ts only when HasEPSS is true and NULL otherwise, together with epss and epss_percentile. The ON CONFLICT update still leaves every signal column alone. The doc comment states it.

Tests added:
- TestReplaceMatches_Invalid gains two cases, `interval of another repo` and `interval not stored`. Each sends a valid match plus the bad one, and checks that both repos' matches are unchanged. The other repo has a stored match for the same advisory on its own interval, which the old code moved over to this repo.
- TestSaveAdvisory_InsertWithoutEPSSStoresNullEPSSTime: an insert with EPSSAt set and HasEPSS false stores epss, epss_percentile and epss_ts as NULL and reads back with a zero EPSSAt.

Commands run:
- With both fixes disabled in a temporary copy, `go test -race -count=1 -run 'TestReplaceMatches_Invalid|TestSaveAdvisory_InsertWithoutEPSS' ./internal/store/` -> FAIL (shows the new tests catch both bugs). The fixed file was then copied back.
  --- FAIL: TestSaveAdvisory_InsertWithoutEPSSStoresNullEPSSTime (0.09s)
      advisories_test.go:223: advisory rows with NULL epss, epss_percentile and epss_ts = 0, want 1
      advisories_test.go:227: ListAdvisories() = [{ID:CVE-2026-8 ... EPSSAt:2026-03-03 12:00:00 +0000 UTC Raw:[]}], want [{ID:CVE-2026-8 ... EPSSAt:0001-01-01 00:00:00 +0000 UTC Raw:[]}]
  --- FAIL: TestReplaceMatches_Invalid (0.12s)
      advisories_test.go:520: interval of another repo: ReplaceMatches() error = nil, want an error
      advisories_test.go:523: interval of another repo: matches = [{ID:1 RepoID:1 DepVersionID:1 ...} {ID:2 RepoID:1 DepVersionID:2 ...}], want unchanged [{ID:1 RepoID:1 DepVersionID:1 ...}]
      advisories_test.go:526: interval of another repo: other repo's matches = [], want unchanged [{ID:2 RepoID:2 DepVersionID:2 ... Confidence:low ...}]
      (the "interval not stored" lines that follow only repeat the state left behind by the case above)
  FAIL
- `go test -race -count=1 -v -run 'TestReplaceMatches|TestSaveAdvisory' ./internal/store/` (fixed code) -> exit 0
  --- PASS: TestSaveAdvisory_InsertTakesSignals
  --- PASS: TestSaveAdvisory_NullColumns
  --- PASS: TestSaveAdvisory_InsertWithoutEPSSStoresNullEPSSTime
  --- PASS: TestSaveAdvisory_UpdateKeepsSignals
  --- PASS: TestSaveAdvisory_RoundTrip
  --- PASS: TestReplaceMatches_Invalid
  --- PASS: TestReplaceMatches_KeepsIDsAndDeletes
  --- PASS: TestSaveAdvisory_Errors
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	1.613s
- `make check > check1.log 2>&1; echo $?` -> exit 0
  (tail)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/web	(cached)
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
  (store package line: ok  	github.com/Amrit-Code/upstreamwatch/internal/store	7.621s)
- `go test -race -run 'TestSaveAdvisory|TestApplySignals|TestReplaceMatches|TestQueriedVersions|TestKV|TestListAllIntervals|TestGetFindingDetail' ./internal/store/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	4.129s
- `git status --porcelain` -> exit 0
   M internal/store/advisories.go
   M internal/store/advisories_test.go
  (plus this report file)

Decisions:
- The ownership check reads the repo's interval IDs once and checks them in Go, rather than binding the match IDs in an IN list. That keeps it to one query with no chunking at 500, however many matches there are.
- An interval ID that is not stored at all is refused with the same error as another repo's interval. Before, the foreign key caught it in the middle of the writes.

Outside scope: none new
Concerns: none
Question or blocker: none
