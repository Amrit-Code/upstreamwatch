Status: DONE_WITH_CONCERNS
Work order: .claude/plans/m2/WO-05-model-rules.md
Files changed:
- internal/model/rank.go (created)
- internal/model/rank_test.go (created)
- internal/model/state.go (created)
- internal/model/state_test.go (created)
- internal/model/digest_text.go (created)
- internal/model/digest_text_test.go (created)
- internal/model/testdata/TestDigestPlainText_Golden/full.golden (created)
- internal/model/testdata/TestDigestPlainText_Golden/no_changed_no_waiting.golden (created)
- internal/model/testdata/TestDigestPlainText_Golden/without_base_url.golden (created)
- internal/model/testdata/TestDigestPlainText_Golden/no_runs.golden (created)
- internal/model/testdata/TestDigestPlainText_Golden/empty_fields.golden (created)
- internal/model/testdata/TestDigestPlainText_Heartbeat_Golden.golden (created)
- internal/model/testdata/TestDigestPlainText_More/with_url.golden (created)
- internal/model/testdata/TestDigestPlainText_More/without_url.golden (created)
- internal/model/testdata/TestDigestPlainText_More/only_more_in_a_section.golden (created)
- .claude/plans/m2/WO-05-model-rules.report.md (created; this report)

Commands run:
- `go test ./internal/model -run 'TestDigest' -update -count=1` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	0.208s
  (every golden file was read after generation; contents checked by hand against requirements 5 to 8)
- `make check` -> exit 0 (final run, after the last edit to rank.go)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/extract/unity	(cached)
  ?   	github.com/Amrit-Code/upstreamwatch/internal/findings	[no test files]
  ok  	github.com/Amrit-Code/upstreamwatch/internal/gitsrc	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/httpx	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/ingest	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/issues/github	(cached)
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	1.207s
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
- `go test -race -count=1 -run 'TestRankAdvisory|TestTransition|TestEffective|TestDigest' ./internal/model/` -> exit 0
  ok  	github.com/Amrit-Code/upstreamwatch/internal/model	1.103s
- `go test -race -count=1 -coverprofile=cover.out ./internal/model/` then `go tool cover -func=cover.out` -> exit 0
  (before the `signed` helper was removed; every function in the three new files at 100.0%, `signed` 66.7%; the helper no longer exists)
- `git status --porcelain` -> exit 0
  ?? internal/model/digest_text.go
  ?? internal/model/digest_text_test.go
  ?? internal/model/rank.go
  ?? internal/model/rank_test.go
  ?? internal/model/state.go
  ?? internal/model/state_test.go
  ?? internal/model/testdata/
  (the report file sits under .claude/plans/, written after this command)

Decisions:
- RankAdvisory: an EPSS percentile that rounds to 0 points (below p2.5) is left out of the reason, following "parts that contribute nothing are left out"; a severity value outside the stored set scores and shows as `unknown 30`; a confidence value outside high/medium/low has no penalty.
- Transition: the note is trimmed first and the UTF-8, length and control-character checks run on the trimmed note (the value that is stored). Control characters are `unicode.IsControl` (C0, DEL and C1) except `\n` and `\t`.
- Transition: the returned `SnoozeUntil` is converted to UTC like `ChangedAt` (times are UTC everywhere). Error texts name the reason (`finding 7 is resolved: invalid finding state change`, `snooze has no end: ...`, `note has 2001 characters, more than 2000: ...`); the contract is `errors.Is(err, ErrInvalidTransition)`. On error the returned state is zero.
- PlainText: items in a section are separated by a blank line (as in the PLAN.md section 6 sample); the `… and N more` line sits after a blank line below the last item; every section is followed by a blank line; the WAITING, `Last runs:` and counts lines follow with no blank lines between them (as in PLAN.md). The text ends with one newline.
- PlainText: a section with no items but a positive MoreNew/MoreChanged is still shown (heading plus the `… and N more` line), since the header count and `Digest.IsEmpty` both count it.
- PlainText `Last runs:`: a run's time is `FinishedAt`, or `StartedAt` while running or when `FinishedAt` is zero; a run with no time at all is shown without a time; an unrecognised status is shown as its stored text, and an empty status is left out. Runs keep the order of `LastRuns`.
- Golden helper: `assertGolden` and the `-update` flag live in `digest_text_test.go` (the model package had no golden helper and `internal/testutil/golden` does not exist); the test binary imports `time/tzdata` so `Europe/London` loads without system zone data. The main golden uses a July date so BST (UTC+1) shows that run times are converted to GeneratedAt's location.

Outside scope:
- `docs/domain-model.md` names the rules `Finding.Rank()` and `Transition(to)`, and its "Built so far" section says `Finding` has no `Transition` yet. The work order produces `RankAdvisory(AdvisoryRankInput)`, `Finding.Transition(StateChange)`, `FindingState.Effective(now)`, `Digest.PlainText()` and `Digest.Subject()`; the doc needs a docs-sync.
- Requirement 3 rejects every control character except newline and tab, which includes `\r`. HTML forms submit textarea line breaks as CRLF, so a multi-line note typed in the web UI will be rejected unless the web handler (or `findings.Service`) normalises `\r\n` to `\n` before calling `Transition`. Nothing in internal/model was changed for this; whichever work order builds the ack/snooze form should say who normalises.

Concerns:
- The CRLF point above: as written, the rule is correct to the work order but will reject ordinary multi-line notes from a browser unless a caller normalises line endings first. Accepting `\r` only as part of `\r\n` inside Transition would be the alternative; that changes the rule, so it is left to the lead.
- The WAITING line text is fixed by the work order and reads `1 issue candidates` when Waiting is 1.
