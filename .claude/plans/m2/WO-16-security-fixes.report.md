Status: DONE (after Fix round 1, below; the original round was BLOCKED on requirement 4)
Work order: .claude/plans/m2/WO-16-security-fixes.md
Files changed:
- internal/report/render.go (modified)
- internal/report/render_test.go (modified)
- internal/report/testdata/TestRender_DefangsUpstreamURLs_Golden.golden (created)
- internal/notify/discord/notifier.go (modified)
- internal/notify/discord/notifier_test.go (modified)
- internal/notify/discord/hostile_test.go (modified)
- internal/notify/discord/testdata/TestDiscordSend_Golden.golden (modified)
- internal/notify/slack/notifier.go (modified)
- internal/notify/slack/notifier_test.go (modified)
- internal/notify/slack/hostile_test.go (modified)
- internal/notify/slack/testdata/TestSlackSend_Golden.golden (modified)
- internal/vuln/osv/record.go (modified)
- internal/vuln/osv/record_test.go (modified)
- internal/vuln/osv/record_internal_test.go (modified)
- cmd/upw/joblock.go, cmd/upw/joblock_test.go: not changed (requirement 4 is blocked, see below)

Requirements 1, 2 and 3 are done. Requirement 4 is not started because it needs an edit outside the owned files.

Commands run:
- `make check` -> exit 0
  (last lines)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/discord	1.139s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/email	1.189s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/ntfy	1.104s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/slack	1.105s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/stdout	1.057s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/ops	1.349s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	1.475s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	1.408s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/report	1.403s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	1.584s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	7.278s
  ?   	github.com/Amrit-Code/upstreamwatch/internal/testutil/gitrepo	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/timeline	3.685s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/upstream/depsdev	1.227s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln	1.486s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/epss	1.242s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/kev	1.230s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/osv	1.358s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/web	1.608s
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
- `go test -race -count=1 ./internal/report/ ./internal/notify/... ./internal/vuln/osv/ ./cmd/upw/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/report	1.396s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/discord	1.178s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/email	1.228s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/ntfy	1.144s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/slack	1.165s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/stdout	1.096s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/osv	1.216s
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	2.253s
- `git status --porcelain` -> exit 0 (owned files only, before this report was written)
   M internal/notify/discord/hostile_test.go
   M internal/notify/discord/notifier.go
   M internal/notify/discord/notifier_test.go
   M internal/notify/discord/testdata/TestDiscordSend_Golden.golden
   M internal/notify/slack/hostile_test.go
   M internal/notify/slack/notifier.go
   M internal/notify/slack/notifier_test.go
   M internal/notify/slack/testdata/TestSlackSend_Golden.golden
   M internal/report/render.go
   M internal/report/render_test.go
   M internal/vuln/osv/record.go
   M internal/vuln/osv/record_internal_test.go
   M internal/vuln/osv/record_test.go
  ?? internal/report/testdata/TestRender_DefangsUpstreamURLs_Golden.golden
- `gofmt -l .` -> no output
- Golden updates, each diff read before accepting:
  `go test ./internal/report -run TestRender_DefangsUpstreamURLs_Golden -update -count=1` -> ok (new file)
  `go test ./internal/notify/discord -run TestDiscordSend_Golden -update -count=1` -> ok (one description line gains `\\<@​123456789012345678\\> \\<t:1700000000\\>`)
  `go test ./internal/notify/slack -run TestSlackSend_Golden -update -count=1` -> ok (first section shows `end​* bold*` and `​*note​* ​_here​_ ​~gone​~ ​`code​``)
- Revert checks. Each fix was undone on its own in a scratch copy, the package tests were run, and the file was restored. `cmp` against the backups confirmed all four restored files match:
  defangURLs returning its input -> FAIL TestRender_DefangsUpstreamURLs_Golden, TestDefangURLs
  Discord without '<' in escapeRune -> FAIL TestDiscordSend_Golden, TestDiscordSend_HostileTextCannotPingOrMaskLinks
  Discord heartbeat without allowed_mentions -> FAIL TestDiscordSend_Heartbeat
  Slack escaper without the four formatting replacements -> FAIL TestSlackSend_Golden, TestSlackSend_FormattingCharsArePrefixed
  OSV Fetch without MaxBodyBytes -> FAIL TestFetch_BodyOverCapIsError
  OSV Fetch without the id check -> FAIL TestFetch_RecordIDMismatch
  OSV without the Details cap -> FAIL TestCapDetails, TestParseRecord_LongDetailsCutRawKept, TestFetch_LongDetailsCut

Decisions:
- Defang (req 1): for each "://" the text before it is checked. If it ends in "https" (any case) that part becomes "hxxps"; if it ends in "http" (any case) it becomes "hxxp". The "://" itself always becomes "[://]". So "xhttps://" gives "xhxxps[://]", and a "://" with no scheme in front gives "[://]". This is substring replacement, as the work order words it, not a strict RFC 3986 scheme parse.
- Defang is applied to the summary/details line before the 200-rune cap, so the cap still holds after the text grows. It is also applied to exp.Match.FixedVersion ("Fixed in ..."), because that value comes from the advisory record's range events. The headline (severity, advisory id, package URL, slug), the exposure line (manifest path, dates), RankReason and item.URL are left alone.
- Slack (req 2): a zero-width space (U+200B) goes in front of each `*`, `_`, `~` and backtick. This is done in the same escaper as &, < and >. That escaper covers the tag, the headline and the lines together, because a DigestItem does not mark which parts are upstream text. Internal text such as `my_lib` in a package URL also gets the prefix. It renders the same, but copy-paste picks up the invisible character. The Slack golden helper now writes U+200B as the JSON escape `​` so the golden file shows it. The bytes compared are unchanged.
- Discord (req 2): '<' was added to markdownSpecial in hostile_test.go, and two hostile rows were added (`<#...>` channel and `<t:...:R>` timestamp). The test now checks that '<' is escaped.
- OSV id mismatch (req 3): exact string comparison. The error is "osv: fetch <id>: <host>: record id differs from the id requested". The id the server sent is left out of the error because it is upstream text.
- OSV Details cap (req 3): "longer than 64 KiB" is read as more than 65,536 bytes. The text is cut back to the last rune start at or before byte 65,536, then "…" is appended, so the result can be up to 65,539 bytes. The cap is in parseRecord, which only Fetch uses.

Outside scope:
- The slack and discord hostile_test.go doc comments said bare URLs were left to the security review. They now say the digest job defangs them. That is accurate only for advisory text rendered by internal/report. Issue findings render RankReason, which this work order calls internal text and leaves alone.

Concerns:
- Slack rendering of the ZWSP escape has not been checked against real Slack. No Slack workspace is available here, and Slack documents no escape for mrkdwn formatting characters. A ZWSP in front of a marker stops it opening formatting. It is less clear that it stops a '*' from closing the headline's bold wrapper early: in "a​* b" the '*' has a non-space character before it and a space after it. If Slack still reads that as a closer, the bold ends early. That is cosmetic, because no link or mention can form; '<', '>' and '&' are escaped separately. Putting a ZWSP on both sides, or using look-alike characters, would be options if a live check shows a problem.

Question or blocker:
- Requirement 4 (log one Info line before waiting for the pipeline lock) needs a logger inside cmd/upw/joblock.go, and none reaches it. lockedJob is built by `withPipelineLock(dir string, jobs []pipeline.Job) []pipeline.Job`. The only production caller is cmd/upw/jobs.go:61, inside registeredJobs, which has `logger *slog.Logger` in scope. jobs.go is not an owned file. The go-code rules forbid slog.Default(), and a nil-logger fallback would leave a code path that production never uses.
- Change needed. In cmd/upw/jobs.go:61, which is not owned:
    `return withPipelineLock(filepath.Join(cfg.DataDir, "locks"), []pipeline.Job{ ... }, logger), nil`
  In joblock.go, which is owned:
    `func withPipelineLock(dir string, jobs []pipeline.Job, logger *slog.Logger) []pipeline.Job`
  lockedJob gets a logger field. acquirePipelineLock gets the job name and the logger, and on the first EWOULDBLOCK only, before the first wait, it logs
    `logger.Info("waiting for pipeline lock", "job", name, "lock", path)`.
  joblock_test.go callers are updated, plus a test that captures the log line while another holder has the lock, and checks that an uncontended acquire logs nothing.
- To unblock: add cmd/upw/jobs.go to the owned files for this one-argument change (cmd/upw/jobs_test.go needs no change), or make that edit yourself. Then resume me and I will finish requirement 4.

## Fix round 1

Status: DONE
Work order: .claude/plans/m2/WO-16-security-fixes.md (cmd/upw/jobs.go added to the owned files by the lead, for the change proposed above)
Files changed:
- cmd/upw/joblock.go (modified)
- cmd/upw/joblock_test.go (modified)
- cmd/upw/jobs.go (modified: one argument, `logger`, passed to withPipelineLock)
Commands run:
- `go test -race -count=1 ./cmd/upw/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	2.439s
- Revert checks, each done on a scratch copy and then restored (`cmp` against the backup: restored-ok):
  logger.Info line removed -> FAIL TestJobLock_LogsOnceWhenWaiting
  log-once guard defeated (`logged = false`), so a line is logged on every retry -> FAIL TestJobLock_LogsOnceWhenWaiting
- `make check` -> exit 0
  (last lines)
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
- `go test -race -count=1 ./internal/report/ ./internal/notify/... ./internal/vuln/osv/ ./cmd/upw/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/report	1.426s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/discord	1.272s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/email	1.318s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/ntfy	1.230s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/slack	1.242s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/stdout	1.189s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/osv	1.311s
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	2.320s
- `git status --porcelain` -> owned files and the report only
   M cmd/upw/joblock.go
   M cmd/upw/joblock_test.go
   M cmd/upw/jobs.go
   M internal/notify/discord/hostile_test.go
   M internal/notify/discord/notifier.go
   M internal/notify/discord/notifier_test.go
   M internal/notify/discord/testdata/TestDiscordSend_Golden.golden
   M internal/notify/slack/hostile_test.go
   M internal/notify/slack/notifier.go
   M internal/notify/slack/notifier_test.go
   M internal/notify/slack/testdata/TestSlackSend_Golden.golden
   M internal/report/render.go
   M internal/report/render_test.go
   M internal/vuln/osv/record.go
   M internal/vuln/osv/record_internal_test.go
   M internal/vuln/osv/record_test.go
  ?? .claude/plans/m2/WO-16-security-fixes.report.md
  ?? internal/report/testdata/TestRender_DefangsUpstreamURLs_Golden.golden
- `gofmt -l .` -> no output
Decisions:
- New signatures: `withPipelineLock(dir string, jobs []pipeline.Job, logger *slog.Logger) []pipeline.Job`, and `acquirePipelineLock(ctx context.Context, dir, job string, retry time.Duration, logger *slog.Logger) (func() error, error)`. lockedJob has a `logger` field and passes its job's Name().
- The log line is `logger.Info("waiting for pipeline lock", "job", <name>, "lock", <dir>/pipeline.lock)`. It is logged once per acquire, on the first EWOULDBLOCK and before the first wait. An EINTR retry does not log, because it is not a wait on another holder.
- Tests: TestJobLock_LogsOnceWhenWaiting (synctest; another holder keeps the lock for five retry periods, and exactly one line names the job and the lock path) and TestJobLock_NoLogWhenFree (a free lock logs nothing). Existing joblock tests use a package-level discard logger, `quiet`.
Outside scope: none new. Docs rows for docs-sync: none needed beyond the new log line, which no doc lists.
Concerns: none. The Slack concern was accepted by the lead.

## Fix round 2

Status: DONE
Work order: .claude/plans/m2/WO-16-security-fixes.md (review finding WO16-R1, on top of 1fdaa39)
Files changed:
- internal/report/render.go (modified: the "Now:" line of a CHANGED advisory is now `"Now: " + defangURLs(f.RankReason)`)
- internal/report/render_test.go (modified: new TestRender_ChangedAdvisoryDefangsEveryLine)
Commands run:
- `go test -race -count=1 ./internal/report/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/report	1.471s
- Revert check: the "Now:" line put back to `"Now: "+f.RankReason` on a scratch copy -> FAIL TestRender_ChangedAdvisoryDefangsEveryLine. The file was then restored (`cmp`: restored-ok).
- `make check` -> exit 0
  (last lines)
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
- `go test -race -count=1 ./internal/report/ ./internal/notify/... ./internal/vuln/osv/ ./cmd/upw/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/report	1.352s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/discord	1.239s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/email	1.371s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/ntfy	1.204s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/slack	1.400s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/notify/stdout	1.350s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/osv	1.479s
  ok  	github.com/Amrit-Code/upstreamwatch/cmd/upw	2.296s
- `git status --porcelain` -> M internal/report/render.go, M internal/report/render_test.go (plus this report)
- `gofmt -l .` -> no output
Decisions:
- The regression test renders a CHANGED Maven advisory (changed=true) with fixed version "2.0-https://phish.example/login". The fixed version appears both in the match and in a rank reason that quotes it.
- The test checks every emitted line, headline included. After every "[://]" is removed, no "://" may be left. The three lines are also compared exactly.
- I wrote the rank reason text in the test by hand to stand in for what internal/vuln builds. It does not call the vuln service.
- No golden file changed: the existing golden tests use changed=true only on an item with no URL in its rank reason.
Outside scope: none
Concerns: none
