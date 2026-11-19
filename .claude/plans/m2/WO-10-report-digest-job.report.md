Status: DONE
Work order: .claude/plans/m2/WO-10-report-digest-job.md
Files changed:
- internal/report/service.go (created)
- internal/report/render.go (created)
- internal/report/fakes_test.go (created)
- internal/report/service_test.go (created)
- internal/report/render_test.go (created)
- internal/report/integration_test.go (created)
- internal/report/testdata/TestRender_Advisory_Golden.golden (created; diff reviewed by hand: CET dates shift across midnight as expected, ANSI ESC and CR/LF/tab gone, 200-rune summary cap ends in "…")

Commands run:
- `make check` -> exit 0
  (first run failed at gofmt on internal/report/fakes_test.go; fixed with gofmt -w; the last 30 lines of the final run:)
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
  ok  	github.com/Amrit-Code/upstreamwatch/internal/report	1.279s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	(cached)
  ?   	github.com/Amrit-Code/upstreamwatch/internal/testutil/gitrepo	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/timeline	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/upstream/depsdev	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/epss	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/kev	(cached)
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
- `go test -race ./internal/report/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/report	(cached)
- `go test -race -count=1 -coverprofile=cover.out ./internal/report/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/report	1.426s	coverage: 91.8% of statements
  (measured before TestRun_RecordErrorsOnRetryAndHeartbeat was added)
- `git status --porcelain` -> exit 0
  ?? internal/report/fakes_test.go
  ?? internal/report/integration_test.go
  ?? internal/report/render.go
  ?? internal/report/render_test.go
  ?? internal/report/service.go
  ?? internal/report/service_test.go
  ?? internal/report/testdata/
  (the report file is in .claude/, which this worktree's status does not list)

Tests: every test the work order lists, under the names it gives: TestRun_NewAndChanged, TestRun_NothingThenSkip (integration), TestRun_Heartbeat, TestRun_FailedSendRetried, TestRun_AckedLeftOut (integration), TestRender_Advisory_Golden, TestRun_MaxItems. Also: TestNewService_Defaults, TestRun_HeartbeatDecision (8 cases), TestRun_RetryLeavesRowsItCannotSend, TestRun_NoChannels, TestRun_StoreErrors (6 cases), TestRun_CancelledContext, TestSelectCandidates (6 cases), TestRun_ChannelsInOrder, TestRun_RecordErrorsOnRetryAndHeartbeat (2 cases), TestRenderItem_OtherKinds, TestRenderItem_NoBaseURLAndUnknownKind, TestSanitizeLine, TestCleanUpstream, TestFirstSentence, TestTruncateRunes. integration_test.go also asserts `var _ report.Store = (*store.DB)(nil)`.

Decisions:
- The advisory summary and its facts share one line ("<summary> Fixed in v. KEV: no. EPSS 0.12 (p71)."). I took this from the PLAN.md section 6 example, the sample digest in internal/model/digest_text_test.go, and the 200-rune summary cap sitting inside the 300-rune line cap.
- The KEV fact always appears ("KEV: yes." or "KEV: no."). "Fixed in" appears only when the chosen match has a fixed version, and the EPSS fact only when HasEPSS is set.
- Which match the exposure line (and "Fixed in") describes: the linked interval's match if it is open; otherwise the first open match; otherwise the linked match; otherwise the latest match. "no longer pinned" therefore appears only when no match is open. With no matches there is no exposure line.
- "(pinned since <date>)" is added when Interval.From and Match.ExposedFrom are not equal as times, which is the literal reading of the work order. Two different times on the same day would print the same date twice.
- Dates are YYYY-MM-DD in the location of Options.Now(), the same location as GeneratedAt.
- The headline leaves out the version, and the space before it, when the finding links no interval. If Advisory.ID is empty the headline uses Finding.AdvisoryID.
- The 200-rune summary cap and the 300-rune line cap both keep the first n-1 runes and add "…", so the result is at most n runes. Invalid UTF-8 becomes U+FFFD. "Control character" means unicode.IsControl. Upstream text is collapsed with strings.Fields, which also trims both ends.
- First sentence of details: everything up to and including the first ".", "!" or "?" that ends the text or is followed by a space.
- A finding of unknown kind is an error from renderItem, so Run fails and nothing is marked or sent.
- Empty lines are dropped. Lines is nil when there are none (JSON null).
- Stats: all nine keys are always present. `new` and `changed` count every candidate, both shown and counted in "more". `more` is MoreNew+MoreChanged. `sent` counts successful sends of this run's digest, heartbeat included. `retried` counts successful resends. `failed` counts every failed send, resends included, plus resends whose stored payload would not decode.
- A failed row for a channel that is no longer configured is logged at Warn once per channel name per run ("failed digest left for unconfigured channel", channel, rows).
- A stored payload that does not decode counts as a failed resend. The row is left failed and the error names it.
- SaveDigest and MarkDigestRetried run on context.WithoutCancel(ctx) with a 10 s timeout, as the Runner records its runs. A send cut short by shutdown, after the findings were marked reported, still leaves a failed row that the next run resends. Run checks ctx before MarkReported, between resends and between item renders.
- A FailedDigests error does not stop the run. The new digest is still built and sent, and the error is returned joined. Every other store error before sending returns at once, with nothing marked or sent.
- Error shape: "digest send failed: <channel>: <redacted error>; <channel> (retry of digest <id>): <redacted error>", joined with any store errors.
- No channels: Run returns before it reads anything, including FailedDigests.
- Log lines: Warn "digest has no channels configured", Warn "digest send failed" (channel, error), Warn "digest resend failed" (channel, digest_id, error), and the unconfigured-channel line above. Every error is passed through model.Redact first.

Outside scope:
- docs/data-access.md line 58 says the digest job uses the `FindingStore` group. internal/report/interfaces.go names its interface `Store`. This is a naming difference only; docs-sync can align it.
- Doc rows now out of date, for docs-sync: in docs/architecture.md the job table row for `digest` says "ranking; what changed since last digest". Ranking is model.RankAdvisory; the job decides what changed, the heartbeat and resends. The row also does not list the tables the job reads (runs, through LatestRuns).
- internal/model/digest.go's DigestItem.Headline example and the sample in internal/model/digest_text_test.go use "vulns failed" for a job failure. The work order says "job <name> failed", and that is what is rendered. The model sample is only test data, but it no longer matches what the job produces.

## Fix round 1

Status: DONE
Work order: .claude/plans/m2/WO-10-report-digest-job.md (review finding, major: in sendAll one 10 s record context was created before the send loop, so slow sends could use up the time and leave SaveDigest failing with context deadline exceeded)
Files changed:
- internal/report/service.go (modified): the record timeout is now an unexported field, `Service.recordTimeout`, which NewService sets to `defaultRecordTimeout` (10 s). A new helper, `recordContext(ctx)`, returns `context.WithTimeout(context.WithoutCancel(ctx), s.recordTimeout)`. sendAll now calls `recordSend(ctx, rec)` after each Send returns, and recordSend makes a new record context for that one SaveDigest. recordRetry uses the same helper; it already built its context after the send.
- internal/report/fakes_test.go (modified): fakeStore.SaveDigest and MarkDigestRetried return ctx.Err() once ctx is done, as the real store's write queue does. Added slowNotifier, which sleeps for its delay and then returns its error.
- internal/report/service_test.go (modified): added TestRun_SlowSendStillRecorded. It runs inside synctest.Test with recordTimeout set to 50 ms. Channel "email" sleeps 100 ms and then fails, and channel "webhook" succeeds. The test asserts that the email failed row and the webhook sent row are both saved, that the error names the email send, and that it does not wrap context.DeadlineExceeded.
Commands run:
- Confirmed the new test catches the bug: I temporarily restored the shared record context in sendAll and ran `go test -race -count=1 -run TestRun_SlowSendStillRecorded ./internal/report/` -> exit 1
  --- FAIL: TestRun_SlowSendStillRecorded (0.00s)
      service_test.go:557: Run() error = digest send failed: email: smtp: i/o timeout
          record digest for channel email: context deadline exceeded
          record digest for channel webhook: context deadline exceeded, a record ran out of time
      service_test.go:560: digests rows = [], want 2
  FAIL
  FAIL	github.com/Amrit-Code/upstreamwatch/internal/report	0.280s
  FAIL
  Then I put the fix back.
- `make check` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	7.101s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/ingest	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/issues/github	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	(cached)
  ?   	github.com/Amrit-Code/upstreamwatch/internal/ops	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/pipeline	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/query	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/report	1.241s
  ok  	github.com/Amrit-Code/upstreamwatch/internal/sched	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/store	(cached)
  ?   	github.com/Amrit-Code/upstreamwatch/internal/testutil/gitrepo	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/timeline	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/upstream/depsdev	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/epss	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/vuln/kev	(cached)
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
- `go test -race -count=3 ./internal/report/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/report	1.444s
- `git status --porcelain` -> exit 0
   M internal/report/fakes_test.go
   M internal/report/service.go
   M internal/report/service_test.go
Decisions: Each record still gets 10 s, counted from when its own send returns; that is the value from round 0. In recordRetry, SaveDigest and MarkDigestRetried share one record context, since nothing slow runs between them. The two minor findings (an undecodable payload, and a resend suppressing a heartbeat) are left as built, as the coordinator instructed.
Outside scope: none new.
