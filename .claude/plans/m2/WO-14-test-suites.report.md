Status: DONE_WITH_CONCERNS
Work order: .claude/plans/m2/WO-14-test-suites.md
Files changed:
- internal/store/interfaces_test.go (modified)
- internal/store/m2_suite_test.go (created)
- internal/model/advisory_test.go (created)
- internal/model/digest_test.go (created)
- internal/vuln/match_boundary_test.go (created)
- internal/vuln/service_extra_test.go (created)
- internal/vuln/osv/credentials_test.go (created)
- internal/report/e2e_test.go (created)
- internal/report/testdata/TestDigest_EndToEndStdout.golden (created)
- internal/notify/discord/hostile_test.go (created)
- internal/notify/slack/hostile_test.go (created)
- internal/notify/email/dotstuff_test.go (created)
- internal/ops/retention_test.go (created)
- internal/findings/integration_test.go (created)
- .claude/plans/m2/WO-14-test-suites.report.md (created)

Commands run:
- `make check` (first run; its exit code was not captured because zsh has no PIPESTATUS, so I ran it again below) -> all steps passed, output:
  ```
  ==> gofmt
  ==> vet
  go vet ./...
  ==> build
  go build ./...
  ==> test
  go test -race ./...
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	2.024s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/config	1.233s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract	1.383s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract/unity	1.187s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/findings	1.364s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	9.440s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	1.551s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/ingest	1.649s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/issues/github	1.213s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	1.182s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/discord	1.240s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/email	1.296s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/ntfy	1.196s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/slack	1.204s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/stdout	1.156s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/ops	1.348s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	1.492s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	1.405s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/report	1.445s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	1.428s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	7.493s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/testutil/gitrepo	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/timeline	3.775s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/upstream/depsdev	1.211s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln	1.529s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/epss	1.255s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/kev	1.218s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/osv	1.232s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/web	1.420s
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
  ```
- `make check` (second run, output to a log file, `echo exit=$?`) -> exit 0
  (test results were cached from the first run; the tail matches the first run: every package `ok`, arch-lint `OK - No warnings found`, import-check silent)
- `go test -race -count=1 ./internal/vuln/... ./internal/report/ ./internal/notify/... ./internal/findings/ ./internal/ops/ ./internal/model/ ./internal/store/` -> exit 0
  ```
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln	1.306s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/epss	1.188s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/kev	1.165s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/osv	1.238s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/report	1.353s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/discord	1.217s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/email	1.239s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/ntfy	1.150s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/slack	1.148s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/stdout	1.067s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/findings	1.341s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/ops	1.294s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	1.129s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	4.742s
  ```
- `go test -race -count=3 -run '<every new test name>' <same packages>` (flakiness check of the new tests) -> exit 0, every package `ok` (epss, kev, ntfy and stdout report `[no tests to run]`: no new tests there)
- `go test -count=1 -cover <same packages>` -> exit 0
  ```
  internal/vuln            93.1% (was 92.0%)
  internal/vuln/epss       94.8%
  internal/vuln/kev        97.4%
  internal/vuln/osv        98.3%
  internal/report          93.3%
  internal/notify/discord  97.8%
  internal/notify/email    89.0%
  internal/notify/ntfy     98.3%
  internal/notify/slack    93.5%
  internal/notify/stdout   100.0%
  internal/findings        100.0%
  internal/ops             93.0%
  internal/model           96.7% (was 82.4%)
  internal/store           86.6%
  ```
- `go test -count=1 -run 'TestDigest_' ./internal/report/ -update` -> exit 0. This created the golden file. I read it line by line against WO-05 requirement 5 and WO-10 requirement 3 (header counts, score order 147/100/50, the headline format, the summary sanitised to one line, the first sentence of details, `Fixed in`, `KEV`, `EPSS 0.43 (p87)`, the exposure line with `(pinned since …)` only when the pin started earlier, dates and run times in the digest's zone (+02:00), the counts line with the findings URL).
- `git status --porcelain` -> only the owned test files above and this report (shown before this report existed):
  ```
   M internal/store/interfaces_test.go
  ?? internal/findings/integration_test.go
  ?? internal/model/advisory_test.go
  ?? internal/model/digest_test.go
  ?? internal/notify/discord/hostile_test.go
  ?? internal/notify/email/dotstuff_test.go
  ?? internal/notify/slack/hostile_test.go
  ?? internal/ops/retention_test.go
  ?? internal/report/e2e_test.go
  ?? internal/report/testdata/TestDigest_EndToEndStdout.golden
  ?? internal/store/m2_suite_test.go
  ?? internal/vuln/match_boundary_test.go
  ?? internal/vuln/osv/credentials_test.go
  ?? internal/vuln/service_extra_test.go
  ```

Decisions:
- Requirement 1, the gaps found: requirements of WO-01 to WO-13 that had no test that would fail if they broke. Each now has one.
  - internal/vuln (WO-06): no systematic boundary check of range evaluation (only hand-picked pins) -> TestEvaluate_RangeBoundaries.
  - internal/vuln (WO-07): req 6, the first affected entry in record order picks the match -> TestRun_FirstAffectedEntryInRecordOrder. Req 7, which interval the finding links (two closed: latest To; two open: latest From), Direct only from open matches (any match when none is open), confidence the best among open matches (all when none is open) -> TestRun_FindingLinkDirectAndConfidence. Req 3, a missing cursor with an empty delta list stores nothing -> TestRun_FirstCursorNotSetFromEmptyDelta. Req 3, ecosystems sorted with EcosystemsExtra and no repeats -> TestRun_EcosystemsSortedWithExtras. Req 4, the union keeps the greatest Modified across query and delta refs -> TestRun_FetchSetKeepsNewestModified. Req 4, "used by any stored interval" holds across repos when scoped -> TestRun_DeltaRecordForAnotherRepoSavedUnderScope. Req 5, "older than SignalsEvery" at the exact gap -> TestRun_SignalsEveryBoundary. Req 9, ListAdvisories, MarkQueried and ResolveFinding failures stop the run -> TestRun_StopsOnOtherStoreFailures. Req 10, the stats are present on an error return -> TestRun_StatsPresentOnError. Req 11, one info line per run and no advisory text in logs -> TestRun_LogsNoAdvisoryText.
  - internal/vuln/osv (WO-03): req 8, "no token is sent" was tested for Fetch only -> TestClient_RequestsCarryNoCredentials (querybatch and modified_id.csv).
  - internal/report (WO-10): no end-to-end test from the real store through a real notifier, and no test that an acked finding changed after its report stays out -> TestDigest_EndToEndStdout, TestDigest_AckedThenChangedLeftOut.
  - internal/notify/discord and slack (WO-09): hostile text was covered by one golden row each -> a table per notifier, plus the truncation edge for Discord.
  - internal/notify/email (WO-08): the existing "line starting with a dot" fixture is indented five spaces by PlainText, so dot-stuffing was never exercised -> TestSend_BodyLineStartingWithDotSurvives (a line starting with "." and a lone "." line, found by padding, with the precondition asserted). Display-name and bare-CR header injection -> TestNew_RefusesHeaderInjection.
  - internal/ops (WO-11): retention with Keep 1 over eight days, checked day by day -> TestBackup_RetentionOverEightDays.
  - internal/findings (WO-11): no test against the real store (the snooze actually hides the finding until expiry, the state round trip, a resolved finding unchanged in the DB) -> TestService_RealStore, TestService_RealStoreErrors.
  - internal/model (lead-owned helpers): CVEs, SourceDB, PURLType, OSVEcosystem, IsOpen, IsWithdrawn and IsEmpty had no direct tests.
  - internal/store (WO-01/WO-02): the M2 consumer interfaces were not asserted (PROGRESS.md deferred item); UpsertFinding after MarkReported and SaveFindingState keeping the digest time, first-seen time and operator state (docs/data-access.md) was untested across steps; ListFindings ties on equal score; ReplaceMatches with an empty list clearing only that repo.
  - internal/vuln/kev, internal/vuln/epss, internal/notify/ntfy, internal/notify/stdout: no gap found against WO-04/08/09; no tests added.
- Case counts per function under test:
  - TestEvaluate_RangeBoundaries (evaluate): 380 cases. 12 Go SEMVER shapes, each at every boundary version (just below, the pre-release just below, at, just above each event; plain and `v`-prefixed). 5 PyPI ECOSYSTEM shapes, each over a hand-ordered run of 12 PEP 440 versions. The expectation comes from the test's own comparator and from the wording of WO-06 req 4, 7 and 8 (via the decisions.md wording "no fixed version above the pin" for medium), not from vers.
  - vuln.Service.Run (new tests): StopsOnOtherStoreFailures 3, StatsPresentOnError 2, FindingLinkDirectAndConfidence 6, FirstAffectedEntryInRecordOrder 4, FirstCursorNotSetFromEmptyDelta 1, EcosystemsSortedWithExtras 1, FetchSetKeepsNewestModified 2, DeltaRecordForAnotherRepoSavedUnderScope 1, SignalsEveryBoundary 4 steps, LogsNoAdvisoryText 1.
  - osv.Client QueryBatch/ModifiedSince credentials: 1.
  - report.Service.Run end to end: TestDigest_EndToEndStdout 1 (golden, stored payload equals what stdout got, second run skipped), TestDigest_AckedThenChangedLeftOut 1 (three runs: reported, acked and changed so left out, reopened so CHANGED).
  - discord Send: hostile table 12, cut sweep 26 (title and description, 13 cut positions each).
  - slack Send: hostile table 12.
  - email Send dot-stuffing: 2. email New header injection: 5.
  - ops.Backup.Run retention: 2 settings (Keep 7, Keep 1) x 8 days, stats and directory checked each day.
  - findings.Service on the real store: RealStore 1 (Ack, Snooze with three time steps, Dismiss, Reopen), RealStoreErrors 5 (not found plus 4 actions on a resolved finding).
  - model: Advisory.CVEs 7, SourceDB 9, PURLType 10, OSVEcosystem 17, AdvisoryMatch.IsOpen 4, Advisory.IsWithdrawn 2, Digest.IsEmpty 9.
  - store: interface assertions 15 (6 new M2, plus query.TimelineStore, which was missing); TestM2_UpsertAfterReportKeepsDigestTimeAndState 4 steps; TestM2_ListFindings_EqualScoresByID 1; TestM2_ReplaceMatches_EmptyClearsOnlyThatRepo 1.
- I wrote the digest end-to-end test in package report (internal), so it can use the package's existing `-update` flag and golden helper. The external report_test package cannot define a second `update` flag in the same test binary. It writes the vuln findings straight to the real store (SaveAdvisory, ApplySignals, ReplaceMatches, UpsertFinding scored with model.RankAdvisory), as WO-14 req 5 says, rather than running vuln.Service with a fake source.
- I used a fixed +02:00 zone for the end-to-end digest instead of time.LoadLocation, so the golden file does not depend on the machine's tz database.
- "Mention or link" in WO-14 req 6 is read as markup the platform turns into a ping or a masked link. See AMBIGUOUS under Concerns for bare URLs and Discord channel references, which the suite leaves out.
- Email "Subject containing CR/LF is refused": Digest.Subject() is built only from the date and counts, so a CR/LF subject cannot reach Send. The guard is covered by the existing internal TestSubjectEncoding (encodeSubject). I added address and host injection cases at New.

Outside scope:
- None of the production code under test looked wrong.
- internal/web/findings.go normalises CRLF in notes before Transition (checked because model.Transition rejects CR), so browser textarea notes are fine.

Concerns:
- AMBIGUOUS (not in the suite): WO-14 req 6 says "no upstream text can produce a mention or a link". WO-09 specifies the escaping that the code implements, and that escaping does not stop two things:
  - (a) A bare URL in upstream text (`https://evil.example`) is auto-linked by both Discord and Slack, so it shows as a clickable link with its real address.
  - (b) A Discord channel reference `<#123456789012345678>` is not escaped (`<` and `#` are not in WO-09's list), so Discord renders it as a channel link. allowed_mentions only suppresses pings.
  Reading 1: "link" and "mention" mean deceptive markup (masked links, pings). The code meets it, and the suite tests exactly that. Reading 2: no clickable reference of any kind. Then both notifiers fall short, and a fix would need a spec change (for example, breaking `://` and escaping `<` in Discord). The lead should decide which reading holds.
- No skipped tests. No BUG or FLAKY findings: every new test passed under `-race -count=3`.
- Status is DONE_WITH_CONCERNS only because of the AMBIGUOUS item above.
- internal/store took 7.5 s under `make check` (the whole existing suite under -race). That is under the 10 s budget, but not by much as the store suite grows.

Question or blocker: none

## Fix round 1

Status: DONE
Work order: .claude/plans/m2/WO-14-test-suites.md
Files changed:
- internal/vuln/service_extra_test.go (modified)
- internal/notify/discord/hostile_test.go (modified)
- internal/notify/slack/hostile_test.go (modified)
- .claude/plans/m2/WO-14-test-suites.report.md (modified)

Commands run:
- `go test -count=1 -run TestRun_FetchSetKeepsNewestModified ./internal/vuln/` with a temporary "last wins" mutation in internal/vuln/service.go fetchAll (`if !ok || true {`) -> exit 1
  ```
  --- FAIL: TestRun_FetchSetKeepsNewestModified (0.00s)
      --- FAIL: TestRun_FetchSetKeepsNewestModified/query_ref_newer_than_the_delta (0.00s)
          service_extra_test.go:395: Fetch calls = [], want [GO-2026-0001] (one fetch, the newest time is after the stored one)
  FAIL
  FAIL	github.com/Amrit-Code/upstreamwatch/internal/vuln	0.244s
  FAIL
  ```
- the same with a temporary "oldest wins" mutation (`if !ok || r.Modified.Before(it.modified) {`) -> exit 1
  ```
  --- FAIL: TestRun_FetchSetKeepsNewestModified (0.00s)
      --- FAIL: TestRun_FetchSetKeepsNewestModified/query_ref_newer_than_the_delta (0.00s)
          service_extra_test.go:395: Fetch calls = [], want [GO-2026-0001] (one fetch, the newest time is after the stored one)
      --- FAIL: TestRun_FetchSetKeepsNewestModified/delta_newer_than_the_query_ref (0.00s)
          service_extra_test.go:395: Fetch calls = [], want [GO-2026-0001] (one fetch, the newest time is after the stored one)
  FAIL
  FAIL	github.com/Amrit-Code/upstreamwatch/internal/vuln	0.241s
  FAIL
  ```
  I restored the mutation by hand. `git diff --stat` then showed only internal/vuln/service_extra_test.go changed, so service.go matches 4415649.
- `go test -race -count=3 -run 'FetchSetKeepsNewest|Hostile' ./internal/vuln/ ./internal/notify/discord/ ./internal/notify/slack/` -> exit 0
  ```
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln	1.169s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/discord	1.324s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/slack	1.274s
  ```
- `make check` -> exit 0 (last lines)
  ```
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/discord	1.138s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/slack	1.125s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln	1.238s
  (every other package ok, cached)
  ==> arch-lint
  OK - No warnings found
  ==> import-check
  sh scripts/check-imports.sh
  ```
- `git status --porcelain` -> only the three test files above (plus this report)

Decisions:
- Finding 1: I did not use day(8) as the case 2 delta time, as suggested. The fake ModifiedSince, like OSV's modified_id.csv, returns only lines after the delta cursor, and after the first run the cursor is at(0), already later than day(9). A day(8) delta line was never returned, so the "last wins" mutation still passed; I checked this with the mutation before the change. Instead, the test now sets advLib's stored time to at(10) before the first run, after the cursor, and asserts that precondition (stored at(10), cursor at(0)). Case 1 is query at(10) (equal to stored) and delta at(48). Case 2 is query at(48) and delta at(5) (after the cursor, before the stored time). Only keeping the newer of the two refs causes the fetch. Both mutations fail, as shown above.
- Finding 2: I renamed the Discord test to TestDiscordSend_HostileTextCannotPingOrMaskLinks, with a doc comment saying bare URLs and channel references are out of scope per the lead's ruling. I gave the Slack test, which had the same old name and falls under the same ruling, the matching name TestSlackSend_HostileTextCannotPingOrMaskLinks and a matching comment.
- Case counts unchanged: TestRun_FetchSetKeepsNewestModified 2, Discord hostile 12 (+26 cut sweep), Slack hostile 12.

Outside scope: none
Concerns: none. The lead ruled on the AMBIGUOUS item: reading 1 holds.
Question or blocker: none
