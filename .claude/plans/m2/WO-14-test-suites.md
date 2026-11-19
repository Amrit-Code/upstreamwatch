# WO-14: Test suites for vulnerability matching, the digest and the notifiers

Milestone: M2
Agent: test-writer   Model: opus
Depends on: WO-01 to WO-13 (merged)
Report file: `.claude/plans/m2/WO-14-test-suites.report.md`
Ticket: UPW-57

## Objective

Fill the gaps the builders' sanity tests leave in the M2 packages, so the M2 checklist items 4, 8, 9, 12 to 17 and 19 rest on tests that would fail if the behaviour broke. Report production bugs the tests expose instead of fixing them.

## Owned files

Test files only (`*_test.go`, `testdata/`) in: `internal/vuln/`, `internal/vuln/osv/`, `internal/vuln/kev/`, `internal/vuln/epss/`, `internal/report/`, `internal/findings/`, `internal/ops/`, `internal/notify/*/`, `internal/model/` (new test files only: `advisory_test.go`, `digest_test.go`), `internal/store/` (new test files only: `m2_suite_test.go`, and `interfaces_test.go`).

No production file. A kept test that fails because of a production bug makes your status DONE_WITH_CONCERNS with the failing test, the bug and the smallest reproduction; do not delete the test and do not fix the code.

## Requirements

1. Read the merged code and the work orders WO-01 to WO-13 first. List in the report, per package, which requirement of which work order had no test that would fail if it broke, and add one for each.
2. `internal/store/interfaces_test.go`: compile-time assertions that `*store.DB` satisfies `vuln.AdvisoryStore`, `vuln.KVStore`, `vuln.FindingStore`, `report.Store`, `findings.Store`, `ops.Backuper`, and the `query` and `pipeline` interfaces it already covered.
3. `internal/model`: tests for `Advisory.CVEs`, `SourceDB`, `PURLType`, `OSVEcosystem`, `AdvisoryMatch.IsOpen`, `Digest.IsEmpty`.
4. `internal/vuln` matching: a property-style table that, for each range shape (introduced/fixed, introduced/last_affected, multiple segments, limit), checks every boundary version (just below, at, just above each event) against an independent expectation written in the test.
5. Digest: an end-to-end test from a real store (vuln findings written through `UpsertFinding`) through `report.Service` to the stdout notifier, asserting the exact text against a golden file; and a test that a finding acked after one digest and then changed (score up) does not appear (M2 leaves ack re-send to M5).
6. Notifiers: for Discord and Slack, a test that no upstream text can produce a mention or a link (table of hostile headlines: `@everyone`, `<@123>`, `<!channel>`, `[click](https://evil)`, `<https://evil|x>`); for email, a test that a Subject or address containing CR/LF is refused and a body line starting with `.` survives SMTP dot-stuffing.
7. Backup: retention over 8 days with `Keep: 7` and with `Keep: 1`.
8. Every test runs under `go test -race` in under 10 seconds per package, uses no network, and uses the fakes the packages already have where they fit.

## Acceptance

| Command | Expected |
|---|---|
| `make check` | exit 0 (or DONE_WITH_CONCERNS with the failing kept test named) |
| `go test -race -count=1 ./internal/vuln/... ./internal/report/ ./internal/notify/... ./internal/findings/ ./internal/ops/ ./internal/model/ ./internal/store/` | `ok` for each package |
| `git status --porcelain` | only owned test files and the report file |

## Out of scope

- Production code, the parity referee (WO-15), web golden pages (WO-12 has them).

## Skills and docs

Preloaded: go-test. Read: the M2 work orders and `docs/decisions.md` "Lead decisions for M2".

## Report

Write the full report to the report file named at the top, in the form given in your agent definition, then reply with only the short form.

## Rulings

