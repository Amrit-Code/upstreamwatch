Status: DONE
Work order: .claude/plans/m2/WO-11-findings-service-backup.md
Files changed:
- internal/findings/service.go (created)
- internal/findings/service_test.go (created)
- internal/ops/backup.go (created)
- internal/ops/backup_test.go (created)
- .claude/plans/m2/WO-11-findings-service-backup.report.md (created)

Commands run:
- `go test -race -count=1 ./internal/findings/ ./internal/ops/` -> exit 1 (first run; my own expected-order bug in TestBackup_ExistingStillPrunes, fixed in the test)
  --- FAIL: TestBackup_ExistingStillPrunes (0.00s)
      backup_test.go:152: directory = [upw-2025010.sqlite upw-20250101.sqlite upw-20260305.sqlite], want [upw-20250101.sqlite upw-2025010.sqlite upw-20260305.sqlite]
  FAIL
  FAIL	github.com/Amrit-Code/upstreamwatch/internal/ops	0.547s
- `go test -race -count=1 -coverprofile=... ./internal/findings/ ./internal/ops/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/findings	1.200s	coverage: 100.0% of statements
  ok  	github.com/Amrit-Code/upstreamwatch/internal/ops	1.361s	coverage: 93.0% of statements
- `make check` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	9.119s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	1.544s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/ingest	1.626s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/issues/github	1.220s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	1.192s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/ops	1.292s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	1.607s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	1.481s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/report	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	1.467s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	6.048s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/testutil/gitrepo	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/timeline	3.703s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/upstream/depsdev	1.209s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln	1.167s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/epss	1.260s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/kev	1.220s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/osv	1.244s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/web	1.649s
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
- `go test -race ./internal/findings/ ./internal/ops/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/findings	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/ops	(cached)
- `git status --porcelain` -> exit 0 (before this report was written)
  ?? internal/findings/service.go
  ?? internal/findings/service_test.go
  ?? internal/ops/backup.go
  ?? internal/ops/backup_test.go

Decisions:
- Snooze checks the length before loading the finding, so an invalid length makes no store call at all; the error is "snooze finding <id>: length <d> is not 1, 7 or 30 days" wrapping model.ErrInvalidTransition.
- Errors are wrapped as "<ack|snooze|dismiss|reopen> finding <id>: <cause>"; the store's and Transition's errors are passed through unchanged inside that wrap.
- findings.Service takes no logger (the produced signature has none) and logs nothing.
- BackupOptions.Keep of zero or less means the default 7.
- The file name uses `Now().Local().Format("20060102")`, the literal reading of "its local date"; with the default time.Now this is the same as formatting without conversion. Tests build clock values in time.Local so names do not depend on the machine's zone.
- Backup.Run stats always hold exists, written and deleted (zeros included), following the ingest job's pattern of fixed stat keys.
- A failing BackupTo returns its error at once and retention does not run (with no new file, retention would have nothing extra to remove anyway).
- Retention lists with os.ReadDir and keeps only entries whose type is regular, so symlinks and directories named like a backup are never deleted; deletions continue after a failure and the failures are joined with errors.Join, returned as "backup: retention: ...". `deleted` counts only successful removals.
- Run logs one Info line, "backup finished", with path, exists, written and deleted; errors are returned, not logged.
- Tests per function: Ack 5 (TestAck, TestAck_NotFound, TestAck_Resolved, TestAck_BadNote, TestAck_SaveFails), Snooze 7 (TestSnooze 6 rows, TestSnooze_Resolved), Dismiss 2, Reopen 2, NewService nil clock 1; Backup Name 1, Run 7 (EightDays, SameDayTwice, ExistingStillPrunes, DefaultKeepAndDirMode, BackupFails, DeleteFails, RealStore). backup_test.go also asserts `var _ ops.Backuper = (*store.DB)(nil)`.

Outside scope:
- docs/architecture.md, "You acknowledge a finding in the browser", shows `FindingStore.SaveState, in one transaction` and `Ack(findingID, note)`; the code follows the work order (findings.Store with GetFinding and SaveFindingState, no InTx; Ack takes note and by). docs-sync may want to align the sequence diagram and the "Ack, snooze, dismiss and auto-resolve" package row (Reopen now exists; auto-resolve is not in findings.Service).
- docs rows now out of date for docs-sync: `internal/findings` and `internal/ops` exist with the services above; the backup job's stats keys (exists, written, deleted).

Concerns: none
Question or blocker: none
